package cams

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"central-vms/client/internal/clock"
)

// fakeProber — har bir metodni qo'lda boshqariladigan soxta RTSP prober.
type fakeProber struct {
	gpErr   error
	optErr  error
	desc    []byte
	descErr error

	mu    sync.Mutex
	calls []string
}

func (f *fakeProber) GetParameter(_ context.Context) error {
	f.mu.Lock()
	f.calls = append(f.calls, "get_parameter")
	f.mu.Unlock()
	return f.gpErr
}

func (f *fakeProber) Options(_ context.Context) ([]string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "options")
	f.mu.Unlock()
	if f.optErr != nil {
		return nil, f.optErr
	}
	return []string{"OPTIONS"}, nil
}

func (f *fakeProber) Describe(_ context.Context) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "describe")
	f.mu.Unlock()
	if f.descErr != nil {
		return nil, f.descErr
	}
	return f.desc, nil
}

func staticDial(p Prober, dialErr error) DialProber {
	return func(_ context.Context, _ string) (Prober, func() error, error) {
		if dialErr != nil {
			return nil, nil, dialErr
		}
		return p, func() error { return nil }, nil
	}
}

func TestWatcherProbeChain(t *testing.T) {
	errBoom := errors.New("xato")
	tests := []struct {
		name  string
		dial  DialProber
		want  ProbeResult
		calls []string
	}{
		{"get_parameter_bo'lsa", staticDial(&fakeProber{}, nil),
			ProbeResult{Status: StatusOnline, Method: "get_parameter"}, []string{"get_parameter"}},
		{"options_fallback", staticDial(&fakeProber{gpErr: errBoom}, nil),
			ProbeResult{Status: StatusOnline, Method: "options", Err: "keepalive_unsupported"},
			[]string{"get_parameter", "options"}},
		{"describe_fallback", staticDial(&fakeProber{gpErr: errBoom, optErr: errBoom, desc: []byte("v=0")}, nil),
			ProbeResult{Status: StatusOnline, Method: "describe", Err: "keepalive_unsupported"},
			[]string{"get_parameter", "options", "describe"}},
		{"hammasi_uzilgan", staticDial(&fakeProber{gpErr: errBoom, optErr: errBoom, descErr: errBoom}, nil),
			ProbeResult{Status: StatusOffline, Method: "", Err: "unreachable"},
			[]string{"get_parameter", "options", "describe"}},
		{"ulanib_bo'lmadi", staticDial(nil, errors.New("refused")),
			ProbeResult{Status: StatusOffline, Method: "dial", Err: "unreachable"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ProbeOnce(context.Background(), tc.dial, "rtsp://10.0.0.1:554/x", 5*time.Second)
			got.ID = "" // ProbeOnce ID bermaydi — chaqiruvchi qo'shadi
			if got.Status != tc.want.Status || got.Method != tc.want.Method || got.Err != tc.want.Err {
				t.Errorf("natija = %+v, xohlangan %+v", got, tc.want)
			}
			if p, ok := unwrapProber(tc.dial); ok && tc.calls != nil {
				p.mu.Lock()
				defer p.mu.Unlock()
				if fmt.Sprint(p.calls) != fmt.Sprint(tc.calls) {
					t.Errorf("chaqiruvlar = %v, xohlangan %v", p.calls, tc.calls)
				}
			}
		})
	}
}

func unwrapProber(d DialProber) (*fakeProber, bool) {
	p, _, err := d(context.Background(), "rtsp://x")
	if err != nil {
		return nil, false
	}
	f, ok := p.(*fakeProber)
	return f, ok
}

// §12.2: parallel probe'lar — bir vaqtda ko'pi bilan 5 ta.
func TestWatcherPacingMax5(t *testing.T) {
	const n = 100
	var cur, maxCur atomic.Int64
	entered := make(chan struct{}, n)
	rel := make(chan struct{}) // token bermasa prober bloklanadi

	dial := func(_ context.Context, _ string) (Prober, func() error, error) {
		return &gateProber{entered: entered, rel: rel, cur: &cur, maxCur: &maxCur}, func() error { return nil }, nil
	}
	var logBuf bytes.Buffer
	w := NewWatcher(WatcherConfig{
		Clock: clock.NewFake(time.Unix(0, 0)), Interval: time.Minute, Parallel: 5,
		Timeout: 5 * time.Second, Dial: dial,
		List: func() []Target {
			out := make([]Target, n)
			for i := range out {
				out[i] = Target{ID: fmt.Sprintf("c%03d", i), URL: "rtsp://h/x"}
			}
			return out
		},
		Emit: func(ProbeResult) {},
		Log:  slog.New(slog.NewTextHandler(&logBuf, nil)),
	})

	done := make(chan struct{})
	go func() { w.ProbeCycle(context.Background()); close(done) }()

	// 5 tasi kiradi, 6-misi kirmasligi kerak.
	for i := 0; i < 5; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("faqat %d probe boshlandi, 5 kutilardi", i)
		}
	}
	select {
	case <-entered:
		t.Fatal("5 dan ortiq parallel probe boshlandi (§12.2)")
	default:
	}
	// barchasini chiqaramiz.
	for i := 0; i < n; i++ {
		rel <- struct{}{}
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("sikl tugamadi")
	}
	if m := maxCur.Load(); m > 5 {
		t.Errorf("maksimal parallel = %d, chegaradan ortiq", m)
	}
	if m := maxCur.Load(); m != 5 {
		t.Errorf("maksimal parallel = %d, 5 kutilardi (pacing ishlamayapti)", m)
	}
}

type gateProber struct {
	entered chan struct{}
	rel     chan struct{}
	cur     *atomic.Int64
	maxCur  *atomic.Int64
}

func (g *gateProber) GetParameter(_ context.Context) error {
	c := g.cur.Add(1)
	for {
		m := g.maxCur.Load()
		if c <= m || g.maxCur.CompareAndSwap(m, c) {
			break
		}
	}
	g.entered <- struct{}{}
	<-g.rel
	g.cur.Add(-1)
	return nil
}
func (g *gateProber) Options(_ context.Context) ([]string, error) { return []string{"OPTIONS"}, nil }
func (g *gateProber) Describe(_ context.Context) ([]byte, error)  { return []byte("v=0"), nil }

// §12.2: har 60 s'da yangi sikl + probe_cycle_s log'da.
func TestWatcherCycleSchedulingAndLog(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	cycled := make(chan struct{}, 8) // List har sikl boshida chaqiriladi
	var logBuf bytes.Buffer
	w := NewWatcher(WatcherConfig{
		Clock: clk, Interval: 60 * time.Second, Parallel: 5, Timeout: time.Second,
		Dial: staticDial(&fakeProber{}, nil),
		List: func() []Target {
			cycled <- struct{}{}
			return []Target{{ID: "c1", URL: "rtsp://h/x"}}
		},
		Emit: func(ProbeResult) {},
		Log:  slog.New(slog.NewTextHandler(&logBuf, nil)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() { w.Run(ctx); close(runDone) }()

	// Birinchi sikl darhol boshlanadi.
	select {
	case <-cycled:
	case <-time.After(5 * time.Second):
		t.Fatal("birinchi sikl boshlanmadi")
	}
	clk.Advance(60 * time.Second)
	select {
	case <-cycled:
	case <-time.After(5 * time.Second):
		t.Fatal("60 s'dan keyin ikkinchi sikl boshlanmadi")
	}
	cancel()
	<-runDone
	if !bytes.Contains(logBuf.Bytes(), []byte("probe_cycle_s")) {
		t.Errorf("log'da probe_cycle_s yo'q: %s", logBuf.String())
	}
}

// Event-driven cam_status: faqat holat O'ZGARSA yuboriladi (§12.1).
func TestWatcherEmitOnlyOnChange(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	online := true
	dial := func(_ context.Context, _ string) (Prober, func() error, error) {
		if online {
			return &fakeProber{}, func() error { return nil }, nil
		}
		return nil, nil, errors.New("refused")
	}
	var mu sync.Mutex
	var events []ProbeResult
	w := NewWatcher(WatcherConfig{
		Clock: clk, Interval: time.Minute, Parallel: 5, Timeout: time.Second, Dial: dial,
		List: func() []Target { return []Target{{ID: "c1", URL: "rtsp://h/x"}} },
		Emit: func(r ProbeResult) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, r)
		},
		Log: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	})
	ctx := context.Background()
	w.ProbeCycle(ctx) // unknown → online: hodisa
	w.ProbeCycle(ctx) // online → online: yo'q
	online = false
	w.ProbeCycle(ctx) // online → offline: hodisa
	w.ProbeCycle(ctx) // offline → offline: yo'q
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("hodisalar = %d, xohlangan 2: %+v", len(events), events)
	}
	if events[0].Status != StatusOnline || events[1].Status != StatusOffline {
		t.Errorf("ketma-ketlik xato: %+v", events)
	}
}

// §10.2/C4.1: holat o'zgarishi strukturali maydonlar bilan loglanadi.
type lockBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockBuf) str() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestWatcher_StatusChangeStructuredLog(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	dial := func(_ context.Context, _ string) (Prober, func() error, error) {
		return nil, nil, errors.New("refused")
	}
	lb := &lockBuf{}
	lg := slog.New(slog.NewTextHandler(lb, nil))
	w := NewWatcher(WatcherConfig{
		Clock: clk, Interval: time.Minute, Parallel: 2, Timeout: time.Second, Dial: dial,
		Log: lg, Emit: func(ProbeResult) {},
		List: func() []Target { return []Target{{ID: "cam-7", URL: "rtsp://10.0.0.7/media"}} },
	})
	w.ProbeCycle(context.Background())
	out := lb.str()
	for _, want := range []string{"camera_id=cam-7", "new=offline", "reason=unreachable"} {
		if !strings.Contains(out, want) {
			t.Errorf("logda %q yo'q: %s", want, out)
		}
	}
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("offline WARN bo'lishi kerak: %s", out)
	}
}
