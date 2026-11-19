package publish

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

// §9.3: publish ochilishida 1 s / 2 s / 4 s (jami ~7 s — server 10 s
// taymautiga yetmaydi), keyin publish_failed.
// Ketma-ketlik TOZA funksiya bilan (retryDelays), xatti-harakat Real soat
// + kichik bazada (fake soatda goroutine navbat race'si bor — rtspclient
// C2.5'dagi tajriba).

func TestRetry_DelaysSequence(t *testing.T) {
	base := time.Second
	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	var total time.Duration
	for i, w := range want {
		got := retryDelay(base, i)
		if got != w {
			t.Errorf("delay %d = %v, %v kutilardi", i, got, w)
		}
		total += got
	}
	if total >= 10*time.Second {
		t.Errorf("jami kutish %v — 10 s server taymautiga yetdi", total)
	}
	if max := retryDelay(base, 10); max > 24*time.Hour {
		t.Errorf("cap buzildi: %v", max)
	}
}

func TestRetry_OpenSucceedsFirstTry(t *testing.T) {
	calls := 0
	err := OpenWithRetry(context.Background(), nil, nil, func(context.Context) error {
		calls++
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

// Real soat, 5 ms baza: 4 urinish ~35 ms'da tugaydi, publish_failed.
func TestRetry_ExhaustsToPublishFailed(t *testing.T) {
	var at []time.Duration
	t0 := time.Now()
	calls := 0
	err := openWithRetry(context.Background(), nil,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), 5*time.Millisecond,
		func(context.Context) error {
			at = append(at, time.Since(t0))
			calls++
			return errors.New("connection refused")
		})
	if !errors.Is(err, ErrPublishFailed) {
		t.Fatalf("err = %v, ErrPublishFailed kutilardi", err)
	}
	if calls != 4 {
		t.Errorf("urinishlar = %d, 4 kutilardi", calls)
	}
	// O'sish ketma-ketligi: har keyingi oralik oldingidan katta.
	for i := 1; i < len(at)-1; i++ {
		gap := at[i+1] - at[i]
		prev := at[i] - at[i-1]
		if gap < prev {
			t.Errorf("backoff kamaydi: %v → %v", prev, gap)
		}
	}
}

// Orada muvaffaqiyat → darhol qaytadi.
func TestRetry_SucceedsOnThirdTry(t *testing.T) {
	calls := 0
	err := openWithRetry(context.Background(), nil, nil, 2*time.Millisecond,
		func(context.Context) error {
			calls++
			if calls < 3 {
				return errors.New("flaky")
			}
			return nil
		})
	if err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

// ctx bekor qilinsa — kutishda darhol chiqadi.
func TestRetry_CtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := openWithRetry(ctx, nil, nil, time.Hour, // kutish juda katta
		func(context.Context) error {
			calls++
			if calls == 1 {
				cancel()
			}
			return errors.New("nope")
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, context.Canceled kutilardi", err)
	}
	if calls != 1 {
		t.Errorf("urinishlar = %d, 1 kutilardi", calls)
	}
}

// Oxirgi sabab saqlanadi (aniqroq reason tanlash uchun, §13).
func TestRetry_ErrWrapsLastCause(t *testing.T) {
	cause := errors.New("401 unauthorized")
	err := openWithRetry(context.Background(), nil, nil, time.Millisecond,
		func(context.Context) error { return cause })
	if !errors.Is(err, cause) {
		t.Errorf("oxirgi sabab yo'qolgan: %v", err)
	}
}
