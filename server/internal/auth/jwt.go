// Package auth — JWT access/refresh (S1.2). Soat interfeysi orqali fake
// clock bilan testlanadi (TASKS §0.2 — haqiqiy uyqu taqiqlangan).
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Clock — vaqt manbai (testlarda fake).
type Clock interface {
	Now() time.Time
}

// Claims — JWT payload (oddiy: user_id, role, typ, exp).
type Claims struct {
	Sub    int64  `json:"sub"`
	User   string `json:"user"`
	Role   string `json:"role"`
	Typ    string `json:"typ"` // access | refresh
	Issued int64  `json:"iat"`
	Expire int64  `json:"exp"`
	JTI    string `json:"jti,omitempty"` // refresh uchun: rotatsiya kuzatuvi
	// Epoch — foydalanuvchining token avlodi (users.token_epoch).
	// Parol almashtirish / hisobni o'chirish / rol pasaytirishda
	// o'stiriladi va eski access token darhol rad etiladi
	// (2026-11-21 server auditi).
	Epoch int64 `json:"tep,omitempty"`
}

// TokenError — aniq xato kodlari (errorlint uchun %w zanjiri bilan).
var (
	ErrTokenExpired = fmt.Errorf("token muddati o'tgan")
	ErrTokenBad     = fmt.Errorf("token yaroqsiz")
)

// Tokens — HMAC-SHA256 JWT (HS256) imzolash/tekshirish. Access 15 min,
// refresh 7 kun (root §9); kaliz config.auth.jwt_secret (≥32 belgi).
// RefreshStore — refresh tokenlarning DB hisobi (rotatsiya/bekor qilish).
type RefreshStore interface {
	SaveRefresh(jti string, userID int64, expiresAt time.Time) error
	HasRefresh(jti string) (bool, error)
	DeleteRefresh(jti string) error
	DeleteRefreshForUser(userID int64) error
}

// EpochSource — foydalanuvchi token avlodini o'qish (middleware tekshiradi).
type EpochSource func(userID int64) (int64, error)

type Tokens struct {
	key        []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	clock      Clock
	refresh    RefreshStore
	epoch      EpochSource

	// epochCache — token avlodini har so'rovda DB'dan o'qimaslik uchun
	// qisqa muddatli kesh (5 s).
	mu     sync.Mutex
	epochs map[int64]epochEntry
	// epochTTL — kesh muddati (0 → har so'rovda DB). Testlar uchun
	// sozlanadi.
	epochTTL time.Duration
}

// SetEpochCacheTTL — avlod keshini sozlash (0 — kesh yo'q).
func (t *Tokens) SetEpochCacheTTL(d time.Duration) { t.epochTTL = d }

// epochEntry — kesh yozuvi.
type epochEntry struct {
	v  int64
	at time.Time
}

// epochCacheTTL — kesh muddati.
const epochCacheTTL = 5 * time.Second

// SetRefreshStore — refresh rotatsiyasini DB bilan bog'lash (nil bo'lsa
// eski xatti-harakat: JTI kuzatilmaydi).
func (t *Tokens) SetRefreshStore(s RefreshStore) { t.refresh = s }

// SetEpochSource — access token'dagi avlodni DB bilan solishtirish.
func (t *Tokens) SetEpochSource(f EpochSource) { t.epoch = f }

// ValidateRefresh — refresh JTI hali ro'yxatdami (rotatsiya/revocation).
// Store sozlanmagan bo'lsa true (eski xatti-harakat).
func (t *Tokens) ValidateRefresh(jti string) (bool, error) {
	if t.refresh == nil || jti == "" {
		return true, nil
	}
	return t.refresh.HasRefresh(jti)
}

// DropRefresh — refreshni o'chirish (rotatsiya yoki logout).
func (t *Tokens) DropRefresh(jti string) error {
	if t.refresh == nil || jti == "" {
		return nil
	}
	return t.refresh.DeleteRefresh(jti)
}

// RevokeUser — foydalanuvchining barcha refresh tokenlarini o'chirish.
func (t *Tokens) RevokeUser(userID int64) error {
	if t.refresh == nil {
		return nil
	}
	return t.refresh.DeleteRefreshForUser(userID)
}

func NewTokens(secret string, accessTTL, refreshTTL time.Duration, clock Clock) (*Tokens, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("auth: jwt secret kamida 32 belgi")
	}
	if clock == nil {
		clock = realClock{}
	}
	return &Tokens{
		key:        []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		clock:      clock,
	}, nil
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// IssueAccess — access token. epochFn sozlangan bo'lsa avlod raqami ham
// qo'yiladi (middleware uni DB bilan solishtiradi).
func (t *Tokens) IssueAccess(userID int64, username, role string) (string, time.Time, error) {
	c := Claims{Sub: userID, User: username, Role: role, Typ: "access"}
	if t.epoch != nil {
		// Avlod raqami token'ga qo'yiladi: parol almashtirish /
		// disable / rol pasaytirishdan keyin eski access token
		// darhol rad etiladi (2026-11-21 server auditi).
		if e, err := t.epoch(userID); err == nil {
			c.Epoch = e
		}
	}
	return t.issue(c, t.accessTTL)
}

func (t *Tokens) IssueRefresh(userID int64, username, role string) (string, time.Time, error) {
	jti := make([]byte, 9)
	if _, err := rand.Read(jti); err != nil {
		return "", time.Time{}, fmt.Errorf("auth: jti: %w", err)
	}
	jtis := base64.RawURLEncoding.EncodeToString(jti)
	tok, exp, err := t.issue(Claims{Sub: userID, User: username, Role: role, Typ: "refresh",
		JTI: jtis}, t.refreshTTL)
	if err != nil {
		return "", time.Time{}, err
	}
	// Refresh DB'ga yoziladi: rotatsiyada eski JTI o'chiriladi, shuning
	// uchun ko'chirilgan/eski cookie keyingi so'rovda rad etiladi.
	if t.refresh != nil {
		if err := t.refresh.SaveRefresh(jtis, userID, exp); err != nil {
			return "", time.Time{}, fmt.Errorf("auth: refresh saqlash: %w", err)
		}
	}
	return tok, exp, nil
}

func (t *Tokens) issue(c Claims, ttl time.Duration) (string, time.Time, error) {
	now := t.clock.Now()
	c.Issued = now.Unix()
	c.Expire = now.Add(ttl).Unix()
	hb, err := json.Marshal(c)
	if err != nil {
		return "", time.Time{}, err
	}
	payload := base64.RawURLEncoding.EncodeToString(hb)
	signing := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." + payload // {"alg":"HS256","typ":"JWT"}
	sig := t.sign(signing)
	return signing + "." + sig, time.Unix(c.Expire, 0).UTC(), nil
}

// Verify — imzo + muddat + (talab qilingan) tur tekshiruvi.
func (t *Tokens) Verify(token, wantTyp string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrTokenBad
	}
	if !hmac.Equal([]byte(t.sign(parts[0]+"."+parts[1])), []byte(parts[2])) {
		return nil, ErrTokenBad
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrTokenBad
	}
	var c Claims
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, ErrTokenBad
	}
	if wantTyp != "" && c.Typ != wantTyp {
		return nil, fmt.Errorf("%w: tur %q, kutilgan %q", ErrTokenBad, c.Typ, wantTyp)
	}
	if t.clock.Now().Unix() >= c.Expire {
		return nil, ErrTokenExpired
	}
	return &c, nil
}

func (t *Tokens) sign(data string) string {
	m := hmac.New(sha256.New, t.key)
	m.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// SecretEqual — vaqtiga bog'liq bo'lmagan taqqoslash (agent_secret uchun).
func SecretEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
