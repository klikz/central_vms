// S2.3: media tokenlari — mint (bir marta ko'rinadi, DB'da sha256) va
// MediaMTX auth-hook (`POST /internal/auth`, protocol.md §15).
// Token "bir martalik" EMAS: TTL davomida bitta session/path/action uchun
// qayta ishlatiladi (WHIP POST+PATCH+DELETE), lekin boshqa path/action'da
// rad etiladi.
package media

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TokenRec — media_tokens jadvalidagi yozuv.
type TokenRec struct {
	Hash      string
	SessionID string
	Path      string
	Action    string // publish | read
	ExpiresAt string // RFC3339 (DB ustuni bilan bir xil)
}

// Tokens — mint/tekshiruv (bitta db; soat fake clock uchun interfeys orqali).
type Tokens struct {
	db    *sql.DB
	clock Clock
}

// Clock — vaqt manbai (media paketi ichida mustaqil interfeys).
type Clock interface{ Now() time.Time }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

func NewTokens(db *sql.DB, c Clock) *Tokens {
	if c == nil {
		c = realClock{}
	}
	return &Tokens{db: db, clock: c}
}

// Mint — yangi token: 256-bit entropy; DB'ga faqat sha256 hash yoziladi.
// sessionID bo'sh bo'lsa (tc publish'ning o'zi) bo'sh qoladi.
func (t *Tokens) Mint(sessionID, path, action string, ttl time.Duration) (string, time.Time, error) {
	if action != "publish" && action != "read" {
		return "", time.Time{}, fmt.Errorf("token: action %q noto'g'ri", action)
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, fmt.Errorf("token entropy: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(b)
	exp := t.clock.Now().Add(ttl)
	_, err := t.db.Exec(
		`INSERT INTO media_tokens (token_hash, session_id, path, action, expires_at) VALUES (?,?,?,?,?)`,
		hashToken(raw), nilIfEmpty(sessionID), path, action, exp.Format(time.RFC3339))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("token saqlash: %w", err)
	}
	return raw, exp, nil
}

// Lookup — token hash bo'yicha yozuv (muddat tekshiruv bilan).
func (t *Tokens) Lookup(raw string) (*TokenRec, error) {
	var r TokenRec
	var session sql.NullString
	err := t.db.QueryRow(
		`SELECT token_hash, COALESCE(session_id,''), path, action, expires_at
		 FROM media_tokens WHERE token_hash=?`, hashToken(raw)).
		Scan(&r.Hash, &session, &r.Path, &r.Action, &r.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("token o'qish: %w", err)
	}
	r.SessionID = session.String
	exp, perr := time.Parse(time.RFC3339, r.ExpiresAt)
	if perr != nil || t.clock.Now().After(exp) {
		return nil, nil
	}
	return &r, nil
}

// Extend — token muddatini uzaytirish (media_refresh; §7.1).
func (t *Tokens) Extend(raw string, extend time.Duration) error {
	rec, err := t.Lookup(raw)
	if err != nil || rec == nil {
		return fmt.Errorf("token uzatish: topilmadi yoki eskirgan")
	}
	cur, perr := time.Parse(time.RFC3339, rec.ExpiresAt)
	if perr != nil {
		return perr
	}
	newExp := cur.Add(extend)
	if t.clock.Now().After(cur) {
		newExp = t.clock.Now().Add(extend)
	}
	_, err = t.db.Exec(`UPDATE media_tokens SET expires_at=? WHERE token_hash=?`,
		newExp.UTC().Format(time.RFC3339), hashToken(raw))
	return err
}

// Sweep — muddati o'tgan tokenlarni tozalash (S4 backup/retention bilan birga).
func (t *Tokens) Sweep() (int64, error) {
	res, err := t.db.Exec("DELETE FROM media_tokens WHERE expires_at < ?", t.clock.Now().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("token sweep: %w", err)
	}
	return res.RowsAffected()
}

func hashToken(raw string) string {
	s := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(s[:])
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// --- Auth-hook (protocol.md §15) ---

// HookReq — MediaMTX yuboradigan so'rov (rasmiy maydonlar).
type HookReq struct {
	Action    string `json:"action"`
	Path      string `json:"path"`
	Protocol  string `json:"protocol"`
	User      string `json:"user"`
	Password  string `json:"password"`
	Token     string `json:"token"`
	IP        string `json:"ip"`
	ID        string `json:"id"`
	Query     string `json:"query"`
	UserAgent string `json:"userAgent"`
}

// ExtractToken — token manbasini protokol bo'yicha topadi:
// RTSP: URL query `?token=` (MediaMTX `token` maydoniga soladi yoki query'da);
// WHIP/WHEP: `Authorization: Bearer` (MediaMTX `token` maydonida);
// zaxira: query satridan o'qish. (protocol.md §15 tuzatishlari.)
func ExtractToken(req HookReq, authHeader string) string {
	if req.Token != "" {
		return req.Token
	}
	if authHeader != "" {
		if v := strings.TrimPrefix(authHeader, "Bearer "); v != authHeader {
			return v
		}
	}
	// query: "token=T&x=1"
	for _, kv := range strings.Split(req.Query, "&") {
		if k, v, ok := strings.Cut(kv, "="); ok && k == "token" {
			return v
		}
	}
	return ""
}

// Hook — vmsd'ning /internal/auth handler'i. Tekshiruv zanjiri:
// token → sha256 → DB → path+action mosligi → expireAt javobda (MediaMTX
// sessiyani o'zi yopadi — TTL watchdog ikki tomonlama).
type Hook struct {
	tokens *Tokens
	lg     logger
}

type logger interface {
	Warn(msg string, args ...any)
}

func NewHook(t *Tokens, lg logger) *Hook { return &Hook{tokens: t, lg: lg} }

func (h *Hook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req HookReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		hookDeny(w, "bad_json")
		return
	}
	switch req.Action {
	case "publish", "read", "playback":
		// davom etadi
	case "api", "metrics", "pprof":
		// MediaMTX bularni hook'dan tashqarida qo'yadi (authHTTPExclude);
		// kelib qolsa — localhost check'ni buzmaslik uchun rad etamiz.
		hookDeny(w, "action_excluded")
		return
	default:
		hookDeny(w, "bad_action")
		return
	}
	raw := ExtractToken(req, r.Header.Get("Authorization"))
	if raw == "" {
		hookDeny(w, "no_token")
		return
	}
	rec, err := h.tokens.Lookup(raw)
	if err != nil {
		h.lg.Warn("auth-hook DB", "err", err)
		hookDeny(w, "internal")
		return
	}
	if rec == nil {
		hookDeny(w, "unknown_or_expired_token")
		return
	}
	wantAction := req.Action
	if wantAction == "playback" {
		wantAction = "read"
	}
	if rec.Path != req.Path || rec.Action != wantAction {
		hookDeny(w, "path_action_mismatch")
		return
	}
	// allow + expireAt (MediaMTX sessiyani muddatida o'zi yopadi).
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"allow": true, "expireAt": rec.ExpiresAt, // allaqachon RFC3339 (UTC)
	})
}

func hookDeny(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": reason})
}
