// Package janitor — davomiy tozalash ishlari: token sweep va retention
// (S4.3). Bitta ticker: soatiga bir marta (sozlanadi).
package janitor

import (
	"context"
	"log/slog"
	"time"
)

// Deps — minimal interfeyslar (testda fake bilan almashtiriladi).
type Deps struct {
	Purge         func(days int) (int64, int64, error) // registry.PurgeOlderThan
	SweepTokens   func() (int64, error)                // media.Tokens.Sweep
	RetentionDays int
	Interval      time.Duration // 0 → 1 soat
	Log           *slog.Logger
}

// Run — ctx bekor qilinguniga bir marta darhol, keyin Interval bilan
// takrorlaydi. Xatolar loglanadi, sikl davom etadi.
func Run(ctx context.Context, d Deps) {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Interval <= 0 {
		d.Interval = time.Hour
	}
	once := func() {
		if d.SweepTokens != nil {
			if n, err := d.SweepTokens(); err != nil {
				d.Log.Warn("token sweep xatosi", "err", err)
			} else if n > 0 {
				d.Log.Info("muddati o'tgan tokenlar o'chirildi", "son", n)
			}
		}
		if d.Purge != nil {
			s, a, err := d.Purge(d.RetentionDays)
			if err != nil {
				d.Log.Warn("retention xatosi", "err", err, "retention_days", d.RetentionDays)
			} else if s > 0 || a > 0 {
				d.Log.Info("retention: eski yozuvlar o'chirildi",
					"sessions", s, "audit", a, "retention_days", d.RetentionDays)
			}
		}
	}
	once()
	t := time.NewTicker(d.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			once()
		}
	}
}
