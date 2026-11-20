package janitor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// Run — darhol bir marta, keyin interval bilan; ctx bekor qilinsa chiqadi.
func TestRunTickAndStop(t *testing.T) {
	var sweeps, purges int32
	d := Deps{
		SweepTokens: func() (int64, error) { atomic.AddInt32(&sweeps, 1); return 3, nil },
		Purge: func(days int) (int64, int64, error) {
			atomic.AddInt32(&purges, 1)
			if days != 7 {
				t.Errorf("retention_days o'tkazilmadi: %d", days)
			}
			return 1, 2, nil
		},
		RetentionDays: 7,
		Interval:      20 * time.Millisecond,
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Run(ctx, d); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&purges) < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("janitor ctx bekor qilinishi bilan chiqmadi")
	}
	if atomic.LoadInt32(&sweeps) < 2 || atomic.LoadInt32(&purges) < 2 {
		t.Fatalf("takrorlanmadi: sweeps=%d purges=%d", sweeps, purges)
	}
}

// Xatolar siklni to'xtatmaydi va panic qilmaydi.
func TestRunSurvivesErrors(t *testing.T) {
	var purges int32
	d := Deps{
		SweepTokens:   func() (int64, error) { return 0, errors.New("bog'liq emas") },
		Purge:         func(int) (int64, int64, error) { atomic.AddInt32(&purges, 1); return 0, 0, errors.New("xato") },
		Interval:      10 * time.Millisecond,
		RetentionDays: 90,
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Run(ctx, d); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&purges) < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
}
