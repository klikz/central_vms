package rtspclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"central-vms/client/internal/clock"
)

// Backoff ketma-ketligi sof funksiya sifatida tekshiriladi (deterministik).
func TestBackoffDelay(t *testing.T) {
	base := time.Second
	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	for i, w := range want {
		if got := backoffDelay(base, i); got != w {
			t.Errorf("backoffDelay(%v, %d) = %v, xohlangan %v", base, i, got, w)
		}
	}
	if got := backoffDelay(base, -1); got != time.Second {
		t.Errorf("manfiy attempt: %v, xohlangan %v", got, time.Second)
	}
	if got := backoffDelay(base, 60); got != 24*time.Hour {
		t.Errorf("checklov: %v, xohlangan 24s", got)
	}
}

// Xatti-harakat testlari: urinishlar soni va xato uzatish.
// clock.Real + kichik bazaviy ta'til (5 ms) — testda time.Sleep YO'Q,
// umumiy davomiylik ~15 ms (TASKS §0.2 ruhi: deterministik natija).
func TestWithRetryKeymaKuchidanSongMuvaffaqiyat(t *testing.T) {
	var calls int
	err := WithRetry(context.Background(), clock.Real{}, 3, 5*time.Millisecond, func() error {
		calls++
		if calls < 3 {
			return errors.New("vaqtinchalik xato")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, xohlangan 3", calls)
	}
}

func TestWithRetryTugagachOxirgiXato(t *testing.T) {
	var calls int
	err := WithRetry(context.Background(), clock.Real{}, 3, 5*time.Millisecond, func() error {
		calls++
		return errors.New("doim xato")
	})
	if err == nil || err.Error() != "doim xato" {
		t.Fatalf("oxirgi xato qaytishi kerak edi, oldi: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, xohlangan 3", calls)
	}
}

func TestWithRetryBekorQilish(t *testing.T) {
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := WithRetry(ctx, clock.Real{}, 5, time.Second, func() error {
		calls++
		return errors.New("bajarilmadi")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ctx.Canceled kutilardi, oldi: %v", err)
	}
	if calls != 0 {
		t.Errorf("calls = %d, xohlangan 0", calls)
	}
}

// Bekor qilish ta'til kutayotganda ham ishlashi kerak (Real soat,
// 50 ms bazada darhol bekor qilinadi).
func TestWithRetryBekorQilishTaymautta(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	done := make(chan error, 1)
	go func() {
		done <- WithRetry(ctx, clock.Real{}, 5, 50*time.Millisecond, func() error {
			calls++
			if calls == 1 {
				cancel() // ta'tilga kirishdan oldin bekor qilamiz
			}
			return errors.New("xato")
		})
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ctx.Canceled kutilardi, oldi: %v", err)
		}
		if calls != 1 {
			t.Errorf("calls = %d, xohlangan 1", calls)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WithRetry bekor qilishga javob bermadi")
	}
}

func TestWithRetryBirinchiUrinishdaYaxshi(t *testing.T) {
	calls := 0
	if err := WithRetry(context.Background(), clock.Real{}, 3, time.Second, func() error {
		calls++
		return nil
	}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, xohlangan 1", calls)
	}
}

func TestWithRetryAttempsNol(t *testing.T) {
	calls := 0
	err := WithRetry(context.Background(), clock.Real{}, 0, time.Second, func() error {
		calls++
		return errors.New("x")
	})
	if err == nil || calls != 1 {
		t.Fatalf("attempts<1 bo'lsa ham 1 marta bajarilishi kerak: calls=%d err=%v", calls, err)
	}
}
