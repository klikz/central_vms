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

	mu       sync.Mutex
	inflight map[string]bool
}

func newPreviewSvc(cs *camsvc.Service, ks *keystore.Store, store *preview.Store,
	capt *preview.Capturer, log *slog.Logger) *previewSvc {
	ps := &previewSvc{cs: cs, ks: ks, store: store, cap: capt, log: log,
		inflight: map[string]bool{}}
	ps.w = cams.NewWatcher(cams.WatcherConfig{
		Interval: previewInterval, Parallel: 3, Timeout: 5 * time.Second,
		Dial: ps.dial, List: ps.targets, Emit: ps.emit, Log: log,
	})
	return ps
}

// targets — faol kameralar (probe URL'i kredslar bilan).
func (ps *previewSvc) targets() []cams.Target {
	var out []cams.Target
	for _, c := range ps.cs.List() {
		if !c.Enabled {
			continue
		}
		out = append(out, cams.Target{ID: c.ID, URL: ps.authURL(c)})
	}
	return out
}

// authURL — rtsp://user:pass@host:port/path (parol keystore'dan; §10.1).
func (ps *previewSvc) authURL(c cams.Camera) string {
	u := media.CamRTSPURL(c)
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
	user := "admin"
	p.User = url.UserPassword(user, pass)
	return p.String()
}

func (ps *previewSvc) dial(ctx context.Context, u string) (cams.Prober, func() error, error) {
	c, err := rtspclient.Dial(ctx, u, rtspclient.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, nil, err
	}
	return c, c.Close, nil
}

// emit — holat O'ZGARGANDA keladi (watcher dedup). Offline→online:
// snapshot darhol; online→offline: rasm eski holicha qoladi (UI'da
// status belgisi ko'rsatiladi).
func (ps *previewSvc) emit(r cams.ProbeResult) {
	if err := ps.cs.SetStatus(r.ID, r.Status, time.Now()); err != nil {
		ps.log.Warn("cam_status saqlanmadi", "camera_id", r.ID, "err", err)
	}
	ps.log.Info("cam_status", "camera_id", r.ID, "status", string(r.Status), "method", r.Method)
	if r.Status == cams.StatusOnline {
		if err := ps.Capture(context.Background(), r.ID); err != nil {
			ps.log.Warn("preview olinmadi", "camera_id", r.ID, "err", err)
		}
	}
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
	defer func() {
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
