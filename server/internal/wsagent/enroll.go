// S1.8: enrollment — bir martalik kod (24 soat) + POST /api/agent/enroll mantiqi
// (protocol.md §3.1; root §9). Kod DB'da sha256 hash; agent_secret ham sha256.
package wsagent

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"central-vms/server/internal/registry"
)

// HashSecret — agent_secret uchun yagona hash manbai (WS auth ham shu orqali).
func HashSecret(secret string) string {
	s := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(s[:])
}

// Enrollment xato kodlari (docs/api.md: 401/409/429).
var (
	ErrBadCode     = errors.New("enroll: kod noto'g'ri yoki muddati o'tgan")
	ErrAlreadyUsed = errors.New("enroll: kod allaqachon ishlatilgan")
	ErrRateLimited = errors.New("enroll: juda ko'p urinish")
)

// clock — soat interfeysi (testlarda fake; auth.Clock bilan bir xil shakl).
type clock interface{ Now() time.Time }

type realNow struct{}

func (realNow) Now() time.Time { return time.Now().UTC() }

// rateWin — IP bo'yicha oddiy oyna (5/5 min).
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

// Enrollment — kod yaratish/qabul qilish (bitta db, lock yo'q).
type Enrollment struct {
	db       *sql.DB
	registry *registry.Registry
	ttl      time.Duration
	rate     *rateWin
}

func NewEnrollment(db *sql.DB, reg *registry.Registry, ttl time.Duration, rateLimit int, rateWindow time.Duration, c clock) *Enrollment {
	return &Enrollment{db: db, registry: reg, ttl: ttl, rate: newRateWin(rateLimit, rateWindow, c)}
}

// CreateCode — admin panel uchun. Qaytariladigan kod BIR MARTA ko'rinadi
// (DB'da faqat sha256 hash).
func (e *Enrollment) CreateCode(siteID, createdByUserID int64) (string, time.Time, error) {
	raw, err := randCode()
	if err != nil {
		return "", time.Time{}, err
	}
	exp := e.rate.c.Now().Add(e.ttl)
	var siteArg, byArg any
	if siteID != 0 {
		siteArg = siteID
	}
	if createdByUserID != 0 { // 0 → NULL: FK "yo'q user"ga ishora qilmasin
		byArg = createdByUserID
	}
	_, err = e.db.Exec(
		`INSERT INTO enrollment_codes (code_hash, site_id, created_by, expires_at) VALUES (?,?,?,?)`,
		HashSecret(raw), siteArg, byArg, exp.Format(time.RFC3339))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("enroll kod yaratish: %w", err)
	}
	return raw, exp, nil
}

// EnrollInput — §3.1 so'rov maydonlari (struct: pozitsion parametrlar
// chalkash bo'lib ketdi, yangi maydon qo'shish xatolik tug'dirardi).
type EnrollInput struct {
	Code         string
	Hostname     string
	OS           string
	LanIP        string
	AgentVersion string
	// Name — operator bergan ko'rinadigan nom (masalan "Oxangaron").
	// Bo'sh bo'lsa avtomatik `agent-…` nomi qo'llanadi; admin keyin
	// o'zgartirishi mumkin (PATCH /api/clients/{id}).
	Name string
}

// Redeem — ochiq endpoint mantiqi. ip bo'yicha rate-limit.
// Muvaffaqiyatda: yangi client (nom — so'rovda berilgan yoki avtomatik),
// agent_id `ag_…`, agent_secret BIR MARTA qaytariladi (DB'da sha256).
func (e *Enrollment) Redeem(in EnrollInput, ip string) (agentID, secret string, clientID int64, err error) {
	code := in.Code
	if !e.rate.allow(ip) {
		return "", "", 0, ErrRateLimited
	}
	code = normalizeCode(code)
	if code == "" {
		return "", "", 0, ErrBadCode
	}
	sum := HashSecret(code)
	var expiresAt string
	var usedAt sql.NullString
	qerr := e.db.QueryRow(
		`SELECT expires_at, used_at FROM enrollment_codes WHERE code_hash=?`, sum).
		Scan(&expiresAt, &usedAt)
	if qerr == sql.ErrNoRows {
		return "", "", 0, ErrBadCode
	}
	if qerr != nil {
		return "", "", 0, fmt.Errorf("enroll: %w", qerr)
	}
	exp, perr := time.Parse(time.RFC3339, expiresAt)
	if perr != nil || e.rate.c.Now().After(exp) {
		return "", "", 0, ErrBadCode
	}
	if usedAt.Valid {
		return "", "", 0, ErrAlreadyUsed
	}

	agentID = "ag_" + randHexLower(16)
	if agentID == "ag_" {
		return "", "", 0, fmt.Errorf("enroll: entropy: %w", errors.New("rand ishlamadi"))
	}
	secret = randToken(32)
	displayName := registry.SanitizeName(in.Name)
	if displayName == "" {
		displayName = "agent-" + agentID[3:] // avtomatik: admin keyin PATCH qiladi
	}
	clientID, err = e.registry.CreateClient(displayName, 0, agentID, HashSecret(secret))
	if err != nil {
		if errors.Is(err, registry.ErrExists) {
			return "", "", 0, ErrAlreadyUsed
		}
		return "", "", 0, err
	}
	res, err := e.db.Exec(
		`UPDATE enrollment_codes SET used_at=?, client_id=? WHERE code_hash=? AND used_at IS NULL`,
		e.rate.c.Now().Format(time.RFC3339), clientID, sum)
	if err != nil {
		return "", "", 0, fmt.Errorf("enroll belgilash: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Parallel ikkinchi redeem yutib ketdi — birinchisini qaytaramiz.
		return "", "", 0, ErrAlreadyUsed
	}
	return agentID, secret, clientID, nil
}

// --- kod/entropy yordamchilari ---

// Kod — 8 xonali RAQAMLI son (10^8 kombinatsiya). Operator talabi: kodni
// telefon orqali aytib/terib kiritish oson bo'lsin (2026-11-20). Xavf
// balansi: urinishlar IP bo'yicha 5 ta / 5 daqiqa (§3.1) — bitta IP'da
// kuniga ~1440 urinish, ya'ni to'liq qidiruv yillar talab qiladi; kod bir
// marta ishlatiladi va 24 soat yashaydi.
const codeDigits = 8

func randCode() (string, error) {
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(codeDigits), nil) // 10^8
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("enroll kod entropy: %w", err)
	}
	return fmt.Sprintf("%0*d", codeDigits, n), nil
}

// normalizeCode — kiritishni yengillashtiradi: bo'sh joy, tire va
// pastki chiziq tashlab yuboriladi ("4173 9265" = "4173-9265" = "41739265").
func normalizeCode(code string) string {
	out := make([]rune, 0, len(code))
	for _, r := range code {
		switch r {
		case ' ', '\t', '-', '_':
			continue
		}
		out = append(out, r)
	}
	return strings.TrimSpace(string(out))
}

func randHexLower(n int) string {
	b := make([]byte, n/2)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func randToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b) // 256-bit → 43 belgi
}
