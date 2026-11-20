// Soat va IP bo'yicha urinish oynasi — §3.1 (join) va §3 (auth) tomonidan
// ishlatiladi. Testlarda fake soat bilan tekshiriladi.
package wsagent

import (
	"sync"
	"time"
)

// clock — soat interfeysi (testlarda fake; auth.Clock bilan bir xil shakl).
type clock interface{ Now() time.Time }

type realNow struct{}

func (realNow) Now() time.Time { return time.Now().UTC() }

// rateWin — IP bo'yicha oddiy oyna (limit/window).
type rateWin struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	c      clock
	hits   map[string][]time.Time
}

func newRateWin(limit int, window time.Duration, c clock) *rateWin {
	if c == nil {
		c = realNow{}
	}
	return &rateWin{limit: limit, window: window, c: c, hits: map[string][]time.Time{}}
}

func (r *rateWin) allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.c.Now()
	orig := r.hits[key]
	kept := make([]time.Time, 0, len(orig)+1)
	for _, t := range orig {
		if now.Sub(t) < r.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= r.limit {
		r.hits[key] = kept
		return false
	}
	kept = append(kept, now)
	r.hits[key] = kept
	return true
}
