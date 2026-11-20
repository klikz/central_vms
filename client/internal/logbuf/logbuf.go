// Package logbuf — oxirgi N log qatorini xotirada ushlab turadi.
// Lokal UI "Jurnal" ekrani shu orqali ko'radi (docs/protocol.md §17).
//
// Muhim tartib: redact handler KEYIN, bu handler oldinda ishlaydi — ya'ni
// Ring'ga tushadigan qatorlar allaqachon yashirilgan bo'ladi (§10.2).
package logbuf

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Ring — aylana bufer. Qulflangan: UI so'rovi va log yozuvi turli goroutine'larda.
type Ring struct {
	mu  sync.Mutex
	cap int
	buf []string
}

// New — sig'imi capacity qatorli bufer. capacity < 1 bo'lsa 1 ga tenglashadi.
func New(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{cap: capacity, buf: make([]string, 0, capacity)}
}

// Add — yangi qator. Bufer to'lganda eng eski qator chiqib ketadi.
func (r *Ring) Add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) < r.cap {
		r.buf = append(r.buf, line)
		return
	}
	// O'zgartirish: birinchi elementni olib tashlash (copy bilan siljitish).
	copy(r.buf, r.buf[1:])
	r.buf[len(r.buf)-1] = line
}

// Recent — oxirgi limit ta qator, eski → yangi tartibda.
// limit <= 0 bo'lsa butun bufer qaytadi.
func (r *Ring) Recent(limit int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 || limit > len(r.buf) {
		limit = len(r.buf)
	}
	out := make([]string, limit)
	copy(out, r.buf[len(r.buf)-limit:])
	return out
}

// Len — buferdagi qatorlar soni.
func (r *Ring) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.buf)
}

// Handler — slog.Handler o'rami: qatorni formatlab Ring'ga yozadi va next'ga
// o'tkazadi. next nil bo'lsa faqat buferga yozadi.
type Handler struct {
	next slog.Handler
	ring *Ring
}

// NewHandler — next handler natijalarini ham saqlaydi.
func NewHandler(next slog.Handler, r *Ring) *Handler {
	return &Handler{next: next, ring: r}
}

// Enabled — filtr next handler'da (daraja sozlamasi ikki tomonlama ishlatiladi:
// stderr ham, bufer ham bir xil darajadan past yozuvlarni olmasligi kerak).
func (h *Handler) Enabled(ctx context.Context, l slog.Level) bool {
	if h.next != nil {
		return h.next.Enabled(ctx, l)
	}
	return true
}

// Handle — qatorni formatlab yozadi. Format: `vaqt DARAJA xabar (kalit=qiymat…)`.
func (h *Handler) Handle(_ context.Context, rec slog.Record) error {
	var sb strings.Builder
	sb.WriteString(rec.Time.Format(time.RFC3339))
	sb.WriteByte(' ')
	sb.WriteString(strings.ToUpper(rec.Level.String()))
	sb.WriteByte(' ')
	sb.WriteString(rec.Message)
	if rec.NumAttrs() > 0 {
		sb.WriteString("  (")
		first := true
		rec.Attrs(func(a slog.Attr) bool {
			if !first {
				sb.WriteString(" ")
			}
			first = false
			sb.WriteString(fmt.Sprintf("%s=%v", a.Key, a.Value))
			return true
		})
		sb.WriteByte(')')
	}
	h.ring.Add(sb.String())

	if h.next != nil {
		return h.next.Handle(context.Background(), rec)
	}
	return nil
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var next slog.Handler
	if h.next != nil {
		next = h.next.WithAttrs(attrs)
	}
	return &Handler{next: next, ring: h.ring}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	var next slog.Handler
	if h.next != nil {
		next = h.next.WithGroup(name)
	}
	return &Handler{next: next, ring: h.ring}
}
