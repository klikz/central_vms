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
}

// TokenError — aniq xato kodlari (errorlint uchun %w zanjiri bilan).
var (
	ErrTokenExpired = fmt.Errorf("token muddati o'tgan")
	ErrTokenBad     = fmt.Errorf("token yaroqsiz")
)

// Tokens — HMAC-SHA256 JWT (HS256) imzolash/tekshirish. Access 15 min,
// refresh 7 kun (root §9); kaliz config.auth.jwt_secret (≥32 belgi).
type Tokens struct {
	key        []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	clock      Clock
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

func (t *Tokens) IssueAccess(userID int64, username, role string) (string, time.Time, error) {
	return t.issue(Claims{Sub: userID, User: username, Role: role, Typ: "access"}, t.accessTTL)
}

func (t *Tokens) IssueRefresh(userID int64, username, role string) (string, time.Time, error) {
	jti := make([]byte, 9)
	if _, err := rand.Read(jti); err != nil {
		return "", time.Time{}, fmt.Errorf("auth: jti: %w", err)
	}
	return t.issue(Claims{Sub: userID, User: username, Role: role, Typ: "refresh",
		JTI: base64.RawURLEncoding.EncodeToString(jti)}, t.refreshTTL)
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
