// redact — parol/token/secret redaksiyasi (docs/protocol.md §10.2).
// Barcha log, xato va UI xabarlari shu funksiya orqali o'tadi.
package redact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
)

// masked — yashirilgan qiymat belgisi.
const masked = "****"

// isPrimitive — logda o'zi xavfsiz bo'lgan oddiy tiplar (satrga aylantirish talab qilinmaydi).
func isPrimitive(v any) bool {
	switch v.(type) {
	case bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, uintptr,
		float32, float64:
		return true
	default:
		return false
	}
}

// Qoidalar ketma-ket qo'llaniladi. Har birida saqlanadigan qismlar () bilan,
// yashiriladigan qism o'rniga **** qo'yiladi.
type rule struct {
	re   *regexp.Regexp
	repl string
}

var rules = []rule{
	// 1) URL userinfo: rtsp://admin:parol@host → rtsp://admin:****@host
	{regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^\s/@:]*:)([^\s/@]*)@`), "${1}****@"},
	// 2) Query token: ?token=XYZ → ?token=****
	{regexp.MustCompile(`(?i)([?&]token=)[^\s&"']+`), "${1}****"},
	// 3) Authorization: Bearer XYZ → Bearer ****
	{regexp.MustCompile(`(?i)(authorization[^\S\r\n]*[:=][^\S\r\n]*bearer[^\S\r\n]+)[A-Za-z0-9+/_=.\-]+`), "${1}****"},
	// 4) Alohida yozilgan Bearer token
	{regexp.MustCompile(`(?i)(bearer[^\S\r\n]+)[A-Za-z0-9+/_=.\-]{8,}`), "${1}****"},
	// 5) JSON maydonlari: "password": "..." , "agent_secret": "...", "publish_token": "..."
	{regexp.MustCompile(`(?i)("(?:password|passwd|pass|secret|agent_secret|publish_token|read_token|token|new_secret)"\s*:\s*")[^"]*(")`), "${1}" + masked + "${2}"},
	// 6) Kalit=qiymat ko'rinishi (log satrlari, formalar)
	{regexp.MustCompile(`(?i)\b(password|passwd|pass|agent_secret|publish_token|read_token|secret|token)=([^&\s,"']+)`), "${1}=****"},
}

// looseRules — qo'shimcha qattiqroq qoida: Go bosib chiqaradigan map/struct
// ko'rinishida (`map[password:sir]`) kalit `:` bilan ajraladi. Bu faqat
// murakkab (composite) qiymatlar uchun qo'llanadi — oddiy log xabarlari
// shu qoida bilan buzilmaydi.
var looseRules = append(append([]rule{}, rules...),
	rule{regexp.MustCompile(`(?i)\b(password|passwd|pass|secret|agent_secret|publish_token|read_token|token|new_secret)\s*[:=]\s*[^,)\s\]}]+`), masked},
)

// stringLoose — composite qiymatlar uchun qo'shimcha yashirish.
func stringLoose(s string) string {
	for _, r := range looseRules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}

// String — satrdagi maxfiy qiymatlarni yashiradi. Qayta qo'llash xavfsiz (idempotent).
func String(s string) string {
	if s == "" {
		return s
	}
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}

// Err — xato matnini qaytaradi (nil → bo'sh satr). Logga yozishdan oldin ishlatiladi.
func Err(err error) string {
	if err == nil {
		return ""
	}
	return String(err.Error())
}

// Wrap — xatoning matnini redaktasiya qilib, xato zanjirini saqlagan holda qaytaradi.
func Wrap(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(String(err.Error()))
}

// secretKeys — kalit nomi maxfiy bo'lgan maydonlar: qiymati qismiy emas,
// to'liq yashiriladi (chunki "token" qiymati o'zida "token=" yo'q).
var secretKeys = map[string]bool{
	"password": true, "passwd": true, "pass": true,
	"secret": true, "agent_secret": true, "new_secret": true,
	"token": true, "publish_token": true, "read_token": true,
	"authorization": true, "bearer": true, "pin": true, "ui_pin": true,
}

// --- slog Handler: log yozuvlari avtomatik redaktasiya qilinadi ---

type handler struct {
	next slog.Handler
}

// NewHandler — ichida redaksiya bo'lgan slog handler (text format).
func NewHandler(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	if opts == nil {
		opts = &slog.HandlerOptions{Level: slog.LevelInfo}
	}
	return NewHandlerWithNext(slog.NewTextHandler(w, opts))
}

// NewHandlerWithNext — yashirish bajarilgach KEYIN boshqa handler'ga o'tkazadi.
// Shu tartib tufayli logbuf buferiga yashirilgan qator tushadi, xomasi emas (§10.2).
func NewHandlerWithNext(next slog.Handler) slog.Handler {
	if next == nil {
		panic("redact: next handler nil bo'lishi mumkin emas")
	}
	return &handler{next: next}
}

func (h *handler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.next.Enabled(ctx, lvl)
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, String(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		nr.AddAttrs(redactAttr(a))
		return true
	})

	return h.next.Handle(ctx, nr)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	red := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		red = append(red, redactAttr(a))
	}
	return &handler{next: h.next.WithAttrs(red)}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{next: h.next.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	if secretKeys[strings.ToLower(a.Key)] {
		return slog.String(a.Key, masked)
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, String(a.Value.String()))
	case slog.KindAny:
		switch v := a.Value.Any().(type) {
		case error:
			// error matni maxfiy bo'lishi mumkin (URL, token)
			return slog.String(a.Key, Err(v))
		case slog.Attr:
			return redactAttr(v)
		default:
			// Primitivlar xavfsiz. Map/struct/pointer kabi qiymatlar `%+v` bilan
			// matn ko'rinishiga keltiriladi (maydon nomlari ko'rinadi — shunda
			// `Pass:parol` kabi juftliklar loose qoida bilan yashiriladi).
			if isPrimitive(v) {
				return a
			}
			return slog.String(a.Key, stringLoose(fmt.Sprintf("%+v", v)))
		}
	case slog.KindGroup:
		g := a.Value.Group()
		args := make([]any, 0, len(g))
		for _, ga := range g {
			args = append(args, redactAttr(ga))
		}
		return slog.Group(a.Key, args...)
	default:
		return a
	}
}
