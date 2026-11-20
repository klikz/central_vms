package cams

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"central-vms/client/internal/clock"
)

// Status watcher — arzon holat probe'i (docs/protocol.md §12).
// Zanjir: GET_PARAMETER → (bo'lmasa) OPTIONS → (bo'lmasa) DESCRIBE.
// Pacing: bir vaqtda ≤ Parallel probe; sikl oralig'i Interval (60 s).
// Hodisalar (cam_status) faqat holat o'zgarganda yuboriladi.

// Target — probe obyekti. URL kreds bilan chaqiruvchi tomonda quriladi
// (keystore'ga watcher kirish huquqi yo'q, §10.1).
type Target struct {
	ID  string
	URL string
	// WantCodec — kamera kodeki noma'lum bo'lsa, probe DESCRIBE orqali
	// uni ham aniqlaydi (1.0-draft.12).
	WantCodec bool
}

// Prober — rtspclient'ning watcher ishlatadigan qismi (fake bilan
// testlanadi; haqiqiysi: internal/rtspclient.Conn).
type Prober interface {
	GetParameter(ctx context.Context) error
	Options(ctx context.Context) ([]string, error)
	Describe(ctx context.Context) ([]byte, error)
}

// DialProber — vaqtinchalik ulanish ochadi; close bilan qaytaradi.
type DialProber func(ctx context.Context, url string) (Prober, func() error, error)

// ProbeResult — bitta kameraning sikl natijasi.
type ProbeResult struct {
	ID     string
	Status Status
	Method string // get_parameter | options | describe | dial
	Err    string // keepalive_unsupported | unreachable (xom kod; redaktasiya chaqiruvchida)
	// Codec — DESCRIBE SDP'idan olingan kodek (h264|h265); bo'sh = noma'lum.
	// 1.0-draft.12: kodek noma'lum bo'lsa agent H.265 kamerani H.264 deb
	// e'lon qilardi va oqim buzilar edi (§4.5, §8.1).
	Codec string
}

// WatcherConfig — barcha sozlamalar struktura orqali (global yo'q, §0.3).
type WatcherConfig struct {
	Clock    clock.Clock
	Interval time.Duration // sikl oralig'i (0 → 60 s, §12.2)
	Parallel int           // bir vaqtda ko'pi bilan (0 → 5, §12.2)
	Timeout  time.Duration // bitta kamera uchun (0 → 5 s)
	Dial     DialProber
	List     func() []Target
	Emit     func(ProbeResult) // event-driven cam_status (faqat o'zgarishda)
	Log      *slog.Logger
}

// Watcher — sikl davomida kameralarni proberlaydi. Holat yozuvi (state.json)
// chaqiruvchining ishi: watcher faqat natija tarqatadi.
type Watcher struct {
	cfg  WatcherConfig
	mu   sync.Mutex
	prev map[string]Status
}

func NewWatcher(cfg WatcherConfig) *Watcher {
	if cfg.Interval <= 0 {
		cfg.Interval = 60 * time.Second
	}
	if cfg.Parallel <= 0 {
		cfg.Parallel = 5
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.Real{}
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.Emit == nil {
		cfg.Emit = func(ProbeResult) {}
	}
	return &Watcher{cfg: cfg, prev: map[string]Status{}}
}

// Run — siklni ctx bekor qilinguncha takrorlaydi (§12.2: har Interval).
func (w *Watcher) Run(ctx context.Context) error {
	tick := w.cfg.Clock.NewTicker(w.cfg.Interval)
	defer tick.Stop()
	for {
		w.ProbeCycle(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C():
		}
	}
}

// ProbeCycle — bitta to'liq sikl: ≤Parallel parallel probe, natijalar
// ID bo'yicha tartiblanadi (deterministik hodisalar), o'zgarganlari Emit
// qilinadi, davomiylik probe_cycle_s log'ida (§12.2).
func (w *Watcher) ProbeCycle(ctx context.Context) []ProbeResult {
	start := w.cfg.Clock.Now()
	targets := w.cfg.List()
	results := make([]ProbeResult, len(targets))
	var wg sync.WaitGroup
	sem := make(chan struct{}, w.cfg.Parallel)
	for i, t := range targets {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			results[i] = ProbeResult{ID: t.ID, Status: StatusUnknown, Err: "cancelled"}
			continue
		}
		wg.Add(1)
		go func(i int, t Target) {
			defer wg.Done()
			defer func() { <-sem }()
			r := ProbeOnceCodec(ctx, w.cfg.Dial, t.URL, w.cfg.Timeout, t.WantCodec)
			r.ID = t.ID
			results[i] = r
		}(i, t)
	}
	wg.Wait()

	sort.Slice(results, func(a, b int) bool { return results[a].ID < results[b].ID })
	w.mu.Lock()
	for _, r := range results {
		if r.Status == StatusUnknown {
			continue // bekor qilingan probe — holat haqida xulosa yo'q
		}
		if w.prev[r.ID] != r.Status {
			old := w.prev[r.ID]
			w.prev[r.ID] = r.Status
			// §10.2: strukturali xato/holat maydonlari — camera_id, reason.
			lvl := slog.LevelInfo
			if r.Status == StatusOffline {
				lvl = slog.LevelWarn
			}
			w.cfg.Log.Log(ctx, lvl, "kamera holati o'zgardi",
				"camera_id", r.ID, "old", string(old), "new", string(r.Status),
				"method", r.Method, "reason", r.Err)
			w.cfg.Emit(r)
		}
	}
	w.mu.Unlock()
	w.cfg.Log.Info("probe sikli tugadi", "cams", len(targets),
		"probe_cycle_s", int(w.cfg.Clock.Now().Sub(start).Seconds()))
	return results
}

// ProbeOnce — bitta kamera zanjiri: GET_PARAMETER → OPTIONS → DESCRIBE.
// Muvaffaqiyatli fallback bo'lsa Err=keepalive_unsupported (§12.1).
// ProbeOnceCodec — wantCodec=true bo'lsa kodek DESCRIBE orqali aniqlanadi
// (kamera sessiyalarini tejab, faqat kerak bo'lganda qo'shimcha DESCRIBE).
func ProbeOnceCodec(ctx context.Context, dial DialProber, url string, timeout time.Duration, wantCodec bool) ProbeResult {
	return probeOnce(ctx, dial, url, timeout, wantCodec)
}

// ProbeOnce — holat probe'i (kodek aniqlanmaydi).
func ProbeOnce(ctx context.Context, dial DialProber, url string, timeout time.Duration) ProbeResult {
	return probeOnce(ctx, dial, url, timeout, false)
}

// maybeCodec — xohish bo'lsa, joriy ulanishda DESCRIBE qilib kodekni oladi.
func maybeCodec(ctx context.Context, want bool, p Prober) string {
	if !want {
		return ""
	}
	sdp, err := p.Describe(ctx)
	if err != nil {
		return ""
	}
	return codecFromSDP(sdp)
}

func probeOnce(ctx context.Context, dial DialProber, url string, timeout time.Duration, wantCodec bool) ProbeResult {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	p, closeFn, err := dial(ctx, url)
	if err != nil {
		return ProbeResult{Status: StatusOffline, Method: "dial", Err: "unreachable"}
	}
	defer func() { _ = closeFn() }()
	// Kodek bir ulanishda o'qiladi: ayrim kameralar (Hikvision) qo'shimcha
	// parallel sessiyani rad etadi yoki ikkinchi ulanishda 401 qaytaradi
	// (2026-11-20: kodek shu sababli noma'lum qolardi).
	if err := p.GetParameter(ctx); err == nil {
		return ProbeResult{Status: StatusOnline, Method: "get_parameter", Codec: maybeCodec(ctx, wantCodec, p)}
	}
	if _, err := p.Options(ctx); err == nil {
		return ProbeResult{Status: StatusOnline, Method: "options", Err: "keepalive_unsupported",
			Codec: maybeCodec(ctx, wantCodec, p)}
	}
	if sdp, err := p.Describe(ctx); err == nil {
		return ProbeResult{Status: StatusOnline, Method: "describe", Err: "keepalive_unsupported",
			Codec: codecFromSDP(sdp)}
	}
	return ProbeResult{Status: StatusOffline, Err: "unreachable"}
}

// codecFromSDP — DESCRIBE javobidagi birinchi video oqim kodeki.
func codecFromSDP(b []byte) string {
	s, err := ParseSDP(string(b))
	if err != nil {
		return ""
	}
	if vs := s.VideoStreams(); len(vs) > 0 {
		return vs[0].Codec
	}
	return ""
}
