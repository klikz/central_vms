package rtspclient

import (
	"context"
	"time"

	"central-vms/client/internal/clock"
)

// backoffDelay — urinish orasidagi kutish: base, 2*base, 4*base...
// (§9.3 bilan bir xil ko'rinish; alohida funksiya — sof testlanadi).
func backoffDelay(base time.Duration, attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 20 {
		return 24 * time.Hour // hisoblashdan himoya (amalda yetarlicha katta)
	}
	return base << attempt
}

// WithRetry — retry siyosati BUTUN paket uchun bitta joyda (TASKS C2.5).
// clk — fake clock bilan deterministik testlar (TASKS §0.2).
func WithRetry(ctx context.Context, clk clock.Clock, attempts int, base time.Duration, op func() error) error {
	if attempts < 1 {
		attempts = 1
	}
	var last error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if last = op(); last == nil {
			return nil
		}
		if i == attempts-1 {
			break
		}
		timer := clk.After(backoffDelay(base, i))
		select {
		case <-timer:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return last
}
