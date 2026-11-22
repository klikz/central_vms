// S1.3: login/enroll rate-limit (5/5 min, root §9) va rol middleware (JWT → context).
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Rate — IP bo'yicha oyna limitlari (bitta egasi bor goroutine yo'q:
// qisqa mutex, holat oddiy — murakkab lock tartibi yo'q, TASKS §0.3).
type Rate struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	clock  Clock
	hits   map[string][]time.Time
}

func NewRate(limit int, window time.Duration, clock Clock) *Rate {
	if clock == nil {
		clock = realClock{}
	}
	return &Rate{limit: limit, window: window, clock: clock, hits: map[string][]time.Time{}}
}

// Allow — so'rov ruxsat etiladimi? Uchinchi qiymat: keyingi oynagacha sekunt.
func (r *Rate) Allow(key string) (ok bool, retryAfterS int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.clock.Now()
	// Kalitlar (IP) umuman tozalanmas edi: har noyob IP uchun abadiy
	// element qolardi va uzoq ishlagan vmsd'da RSS o'sib borardi
	// (2026-11-21 auditi).
	if len(r.hits) > 4096 {
		for k, v := range r.hits {
			if len(v) == 0 || now.Sub(v[len(v)-1]) >= r.window {
				delete(r.hits, k)
			}
		}
	}
	orig := r.hits[key]
	kept := make([]time.Time, 0, len(orig)+1)
	for _, t := range orig {
		if now.Sub(t) < r.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= r.limit {
		wait := r.window - now.Sub(kept[0])
		if wait < time.Second {
			wait = time.Second
		}
		if wait > r.window {
			wait = r.window
		}
		r.hits[key] = kept
		return false, int(wait.Seconds())
	}
	kept = append(kept, now)
	r.hits[key] = kept
	return true, 0
}

// Reset — muvaffaqiyatli urinishdan keyin hisobni tozalash (login OK).
func (r *Rate) Reset(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.hits, key)
}

// --- Rol middleware ---

type ctxKey int

const ctxClaims ctxKey = 1

// ErrNoAuth — handler'lar uchun aniq xato.
var ErrNoAuth = errors.New("auth: ruxsat yo'q")

// Middleware — `Authorization: Bearer <access>` → claims → context.
// Muvaffaqiyatsiz: 401 JSON (kod: missing_token | bad_token | expired).
func (t *Tokens) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeAuthErr(w, http.StatusUnauthorized, "missing_token", "")
			return
		}
		c, err := t.Verify(strings.TrimPrefix(h, "Bearer "), "access")
		if err != nil {
			code := "bad_token"
			if errors.Is(err, ErrTokenExpired) {
				code = "expired"
			}
			writeAuthErr(w, http.StatusUnauthorized, code, "")
			return
		}
		// Token avlodi (users.token_epoch): parol almashtirish, hisobni
		// o'chirish yoki rol pasaytirishdan keyin eski access token
		// 15 daqiqagacha ishlayverardi — endi darhol rad etiladi
		// (2026-11-21 server auditi).
		if t.epoch != nil {
			if e, err := t.epochCached(c.Sub); err == nil && e != c.Epoch {
				writeAuthErr(w, http.StatusUnauthorized, "token_revoked", "")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxClaims, c)))
	})
}

// epochCached — har so'rovda DB'ga yurmaslik uchun qisqa muddatli kesh
// (5 s). Kesh xatolik ehtimoli: o'chirishdan keyin eng ko'p 5 s.
func (t *Tokens) epochCached(userID int64) (int64, error) {
	t.mu.Lock()
	if t.epochs == nil {
		t.epochs = map[int64]epochEntry{}
	}
	now := t.clock.Now()
	ttl := t.epochTTL
	if ttl == 0 {
		ttl = epochCacheTTL
	}
	if e, ok := t.epochs[userID]; ok && now.Sub(e.at) < ttl {
		t.mu.Unlock()
		return e.v, nil
	}
	t.mu.Unlock()
	v, err := t.epoch(userID)
	if err != nil {
		return 0, err
	}
	t.mu.Lock()
	t.epochs[userID] = epochEntry{v: v, at: now}
	if len(t.epochs) > 4096 { // xotira sizmasligi uchun
		for k, e := range t.epochs {
			if now.Sub(e.at) > ttl {
				delete(t.epochs, k)
			}
		}
	}
	t.mu.Unlock()
	return v, nil
}

// RequireRole — rol mos kelmasa 403 insufficient_role.
func RequireRole(role string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := ClaimsFrom(r)
		if c == nil {
			writeAuthErr(w, http.StatusUnauthorized, "missing_token", "")
			return
		}
		if c.Role != role {
			writeAuthErr(w, http.StatusForbidden, "insufficient_role",
				fmt.Sprintf("%s roli kerak", role))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClaimsFrom — middleware o'rnatgan claims (nil → auth yo'q).
func ClaimsFrom(r *http.Request) (*Claims, bool) {
	c, ok := r.Context().Value(ctxClaims).(*Claims)
	return c, ok
}

func writeAuthErr(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + code + `","detail":"` + detail + `"}`))
}
