package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/camsvc"
	"central-vms/client/internal/keystore"
	"central-vms/client/internal/media"
	"central-vms/client/internal/preview"
	"central-vms/client/internal/rtspclient"
)

// previewSvc — kamera holati kuzatuvi (§12.2 probe sikli) + preview
// snapshotlari (UI talabi): rasm kamera qo'shilganda / dastur qayta
// ishga tushganda / holat o'zgarganda BIR marta olinadi; rasmni bosish
// qo'shimcha siklni kutmaydi — darhol Capture chaqiriladi.

// previewInterval — holat sikli. §12.2 default 60 s, lekin preview
// yangiligi uchun qisqaroq qilingan (kamera ko'rinib turgan UI).
const previewInterval = 30 * time.Second

type previewSvc struct {
	cs    *camsvc.Service
	ks    *keystore.Store
	store *preview.Store
	cap   *preview.Capturer
	log   *slog.Logger
	w     *cams.Watcher
	// report — §4.4: probe natijasini serverga yuborish (runtime ulaydi).
	report func(cams.ProbeResult)
	// gate — kamera qurilmasi band bo'lsa probe/preview o'tkazib
	// yuboriladi (§12.1/§8.3b; 2026-11-21 audit topilmasi).
	gate *cams.Gate
	// urlID — probe URL'i → kamera ID (gate kaliti uchun).
	urlID map[string]string

	mu       sync.Mutex
	inflight map[string]bool
	probing  bool
}

func newPreviewSvc(cs *camsvc.Service, ks *keystore.Store, store *preview.Store,
	capt *preview.Capturer, log *slog.Logger, report func(cams.ProbeResult)) *previewSvc {
	ps := &previewSvc{cs: cs, ks: ks, store: store, cap: capt, log: log,
		inflight: map[string]bool{}, report: report,
		gate: cams.NewGate(), urlID: map[string]string{}}
	ps.w = cams.NewWatcher(cams.WatcherConfig{
		Interval: previewInterval, Parallel: 3, Timeout: 5 * time.Second,
		Dial: ps.dial, List: ps.targets, Emit: ps.emit, Log: log,
	})
	return ps
}

// targets — faol kameralar (probe URL'i kredslar bilan).
// Kamera oqimi (publish/pull) faol bo'lsa probe O'TKAZIB YUBORILADI:
// aks holda kamera sessiya limitlari tufayli probe 401 oladi va jonli
// oqim turgan kamera serverda "offline" bo'lib ko'rinardi.
func (ps *previewSvc) targets() []cams.Target {
	var out []cams.Target
	m := map[string]string{}
	for _, c := range ps.cs.List() {
		if !c.Enabled {
			continue
		}
		if ps.gate != nil && ps.gate.PublishCount(c.ID) > 0 {
			continue
		}
		u := ps.authURL(c)
		m[u] = c.ID
		out = append(out, cams.Target{ID: c.ID, URL: u, WantCodec: c.CodecMain == ""})
	}
	ps.mu.Lock()
	ps.urlID = m
	ps.mu.Unlock()
	return out
}

// SetGate — pull (media) va probe (preview) BITTA gate'ni bo'lishishi
// uchun (main.go'da yaratiladi).
func (ps *previewSvc) SetGate(g *cams.Gate) {
	if g == nil {
		return
	}
	ps.mu.Lock()
	ps.gate = g
	ps.mu.Unlock()
}

// camIDForURL — probe URL'idan gate kaliti (kamera ID).
func (ps *previewSvc) camIDForURL(u string) string {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.urlID[u]
}

// authURL — rtsp://user:pass@host:port/path (parol keystore'dan; §10.1).
func (ps *previewSvc) authURL(c cams.Camera) string {
	u := media.CamRTSPURL(c, "main")
	if c.SecretRef == "" {
		return u
	}
	pass, ok := ps.ks.Get(c.SecretRef)
	if !ok || pass == "" {
		return u
	}
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	user := c.User
	if user == "" {
		user = "admin"
	}
	p.User = url.UserPassword(user, pass)
	return p.String()
}

func (ps *previewSvc) dial(ctx context.Context, u string) (cams.Prober, func() error, error) {
	id := ps.camIDForURL(u)
	if ps.gate != nil && !ps.gate.TryProbe(id) {
		// Kamera band — natija "unknown" bo'ladi, holat buzilmaydi.
		return nil, nil, cams.ErrCameraBusy
	}
	c, err := rtspclient.Dial(ctx, u, rtspclient.Options{Timeout: 5 * time.Second})
	if err != nil {
		if ps.gate != nil {
			ps.gate.ReleaseProbe(id)
		}
		return nil, nil, err
	}
	closeFn := func() error {
		if ps.gate != nil {
			ps.gate.ReleaseProbe(id)
		}
		return c.Close()
	}
	return c, closeFn, nil
}

// emit — holat O'ZGARGANDA keladi (watcher dedup). Offline→online:
// snapshot darhol; online→offline: rasm eski holicha qoladi (UI'da
// status belgisi ko'rsatiladi).
func (ps *previewSvc) emit(r cams.ProbeResult) {
	if err := ps.cs.SetStatus(r.ID, r.Status, time.Now()); err != nil {
		ps.log.Warn("cam_status saqlanmadi", "camera_id", r.ID, "err", err)
	}
	ps.log.Info("cam_status", "camera_id", r.ID, "status", string(r.Status), "method", r.Method)
	if ps.report != nil {
		ps.report(r) // §4.4: serverga ham yetkazamiz
	}
	if r.Status == cams.StatusOnline {
		if err := ps.Capture(context.Background(), r.ID); err != nil {
			ps.log.Warn("preview olinmadi", "camera_id", r.ID, "err", err)
		}
	}
}

// ProbeNow — tashqaridan buyurilgan probe sikli (kamera qo'shilganda
// holatni 30 s kutmasdan bilib olish uchun). Bir vaqtda bitta sikl.
func (ps *previewSvc) ProbeNow() {
	ps.mu.Lock()
	if ps.probing {
		ps.mu.Unlock()
		return
	}
	ps.probing = true
	ps.mu.Unlock()
	defer func() {
		ps.mu.Lock()
		ps.probing = false
		ps.mu.Unlock()
	}()
	ps.w.ProbeCycle(context.Background())
}

// Run — dastur ishga tushganda darhol bir sikl (kamera ulangan bo'lsa
// rasm olinadi), so'ng har previewInterval-da takror.
func (ps *previewSvc) Run(ctx context.Context) {
	for ctx.Err() == nil {
		ps.w.ProbeCycle(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(previewInterval):
		}
	}
}

// --- localui.SnapshotSource ---

func (ps *previewSvc) File(id string) (string, bool) {
	p := ps.store.Path(id)
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	return p, true
}

func (ps *previewSvc) At(id string) *time.Time {
	mt, ok := ps.store.Stat(id)
	if !ok {
		return nil
	}
	return &mt
}

// Capture — single-flight: bitta kamera uchun parallel olish yo'q.
func (ps *previewSvc) Capture(ctx context.Context, id string) error {
	ps.mu.Lock()
	if ps.inflight[id] {
		ps.mu.Unlock()
		return nil
	}
	ps.inflight[id] = true
	ps.mu.Unlock()
	if ps.gate != nil && !ps.gate.TryProbe(id) {
		// Oqim faol — rasm uchun qo'shimcha RTSP sessiyasi ochmaymiz
		// (kamera sessiyani chiqarib tashlashi mumkin edi).
		ps.mu.Lock()
		delete(ps.inflight, id)
		ps.mu.Unlock()
		return nil
	}
	defer func() {
		if ps.gate != nil {
			ps.gate.ReleaseProbe(id)
		}
		ps.mu.Lock()
		delete(ps.inflight, id)
		ps.mu.Unlock()
	}()
	c, ok := ps.cs.Get(id)
	if !ok {
		return fmt.Errorf("%w: %s", camsvc.ErrNotFound, id)
	}
	return ps.cap.Capture(ctx, id, ps.authURL(c))
}

func (ps *previewSvc) Remove(id string) { ps.store.Remove(id) }
