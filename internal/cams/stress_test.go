package cams

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"central-vms/client/internal/clock"
)

// C4.8 (§12): 2000 mock kamera — probe sikli, parallelism ≤5, xotira
// chegarasi (100 kamera + 8 publish ≤ 150 MB).

// TestStress_2000ProbeCycle — 2000 kamera, hammasi offline (timeout 1 ms):
// sikl barcha kameralarni qamrab oladi, parallelik hech qachon 5 dan
// oshmaydi (§12.2), sikl vaqti 4 soatlik byudjetdan ancha past.
func TestStress_2000ProbeCycle(t *testing.T) {
	const n = 2000
	targets := make([]Target, n)
	for i := range targets {
		targets[i] = Target{ID: idFor(i), URL: "rtsp://10.0.0.1/media"}
	}

	var cur, peak atomic.Int32
	dial := func(ctx context.Context, _ string) (Prober, func() error, error) {
		c := cur.Add(1)
		if c > peak.Load() {
			peak.Store(c)
		}
		defer cur.Add(-1)
		// timeout=1 ms bilan ProbeOnce darhol xatoga uchraydi (uyqu yo'q).
		return nil, nil, errors.New("refused")
	}
	w := NewWatcher(WatcherConfig{
		Clock: clock.NewFake(time.Unix(0, 0)), Interval: 4 * time.Hour,
		Parallel: 5, Timeout: time.Millisecond, Dial: dial,
		Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Emit: func(ProbeResult) {},
		List: func() []Target { return targets },
	})

	start := time.Now()
	results := w.ProbeCycle(context.Background())
	el := time.Since(start)

	if len(results) != n {
		t.Fatalf("natijalar = %d, %d kutilardi", len(results), n)
	}
	if pk := peak.Load(); pk > 5 {
		t.Errorf("peak parallel = %d, ≤5 (§12.2) bo'lishi kerak", pk)
	}
	// §12: 2000 kamera sikli ≤ 4 soat. Eng yomon real holat (5 s timeout,
	// 400 oqim): 2000/5×5 s = 2000 s ≈ 33 daq. Bu testda timeout 1 ms.
	if el > 4*time.Hour {
		t.Errorf("sikl = %v, 4 soatlik byudjetdan oshdi", el)
	}
	t.Logf("2000 kamera sikli: %v, peak parallel=%d", el, peak.Load())
}

func idFor(i int) string {
	const hexd = "0123456789abcdef"
	b := []byte("cam-0000")
	b[4] = hexd[(i>>12)&0xf]
	b[5] = hexd[(i>>8)&0xf]
	b[6] = hexd[(i>>4)&0xf]
	b[7] = hexd[i&0xf]
	return string(b)
}

// TestStress_GoroutineBound — watcher goroutine'lari Parallel bilan bog'liq
// (sizib chiqish yo'q): 2000 kamera siklidan keyin goroutine soni qaytadi.
func TestStress_GoroutineBound(t *testing.T) {
	targets := make([]Target, 2000)
	for i := range targets {
		targets[i] = Target{ID: idFor(i), URL: "rtsp://h/m"}
	}
	dial := func(context.Context, string) (Prober, func() error, error) {
		return &fakeProber{}, func() error { return nil }, nil
	}
	w := NewWatcher(WatcherConfig{
		Clock: clock.NewFake(time.Unix(0, 0)), Interval: time.Minute, Parallel: 5,
		Timeout: time.Second, Dial: dial,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Emit: func(ProbeResult) {},
		List: func() []Target { return targets },
	})
	before := runtime.NumGoroutine()
	w.ProbeCycle(context.Background())
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		runtime.Gosched()
	}
	t.Errorf("goroutine'lar qaytmadi: before=%d after=%d", before, runtime.NumGoroutine())
}
