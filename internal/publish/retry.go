package publish

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"central-vms/client/internal/clock"
	"time"
)

// §9.3: publish ochilishida qayta urinishlar — 1 s / 2 s / 4 s
// (jami ~7 s, serverning 10 s media_ready taymautiga yetmaydi).

const (
	retryAttempts = 4 // 1 boshlang'ich + 3 qayta urinish
	retryBase     = time.Second
	maxRetryDelay = 24 * time.Hour
)

// ErrPublishFailed — barcha urinishlar tugadi (§13 reason: publish_failed).
// Oxirgi sabab %w bilan saqlanadi (token_rejected kabi aniqroq reasonlar
// uchun §13 matritsasi keyinchalik shundan tanlaydi).
var ErrPublishFailed = errors.New("publish: ochib bo'lmadi")

// retryDelay — eksponensial: base<<attempt, 24 soatga qadar.
func retryDelay(base time.Duration, attempt int) time.Duration {
	if attempt > 30 { // siljitish toshishi oldini oladi
		return maxRetryDelay
	}
	d := base << attempt
	if d <= 0 || d > maxRetryDelay {
		return maxRetryDelay
	}
	return d
}

// OpenWithRetry — prod kirish nuqtasi (§9.3 parametrlari bilan).
func OpenWithRetry(ctx context.Context, clk clock.Clock, log *slog.Logger, open func(context.Context) error) error {
	return openWithRetry(ctx, clk, log, retryBase, open)
}

func openWithRetry(ctx context.Context, clk clock.Clock, log *slog.Logger, base time.Duration, open func(context.Context) error) error {
	if clk == nil {
		clk = clock.Real{}
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	var last error
	for i := 0; i < retryAttempts; i++ {
		if i > 0 {
			d := retryDelay(base, i-1)
			log.Warn("publish_retry", "attempt", i, "wait_ms", d.Milliseconds(), "cause", last)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-clk.After(d):
			}
		}
		if err := open(ctx); err == nil {
			return nil
		} else {
			last = err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return fmt.Errorf("%w: %w", ErrPublishFailed, last)
}
