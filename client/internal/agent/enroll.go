package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// EnrollInfo — §3.1 so'rovi.
type EnrollInfo struct {
	Code         string
	Hostname     string
	OS           string
	LanIP        string
	AgentVersion string
}

// EnrollResult — §3.1 javobi.
type EnrollResult struct {
	AgentID         string    `json:"agent_id"`
	AgentSecret     string    `json:"agent_secret"`
	ProtocolVersion int       `json:"protocol_version"`
	ServerTime      time.Time `json:"server_time"`
}

// Enroll xato kodlari (§3.1)
var (
	ErrBadCode         = errors.New("enroll: kod noto'g'ri yoki muddati tugagan")
	ErrAlreadyEnrolled = errors.New("enroll: bu kod allaqachon ishlatilgan")
	ErrRateLimited     = errors.New("enroll: juda ko'p urinish — keyinroq qayta urinib ko'ring")
	ErrBadResponse     = errors.New("enroll: server javobi kutilmagan")
)

// EnrollError — HTTP holati va server xato kodini saqlaydi (UI'da aniq ko'rsatish uchun).
type EnrollError struct {
	Status int
	Code   string // bad_code | already_enrolled | rate_limited | …
	Err    error
}

func (e *EnrollError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("enroll: HTTP %d (%s)", e.Status, e.Code)
	}
	return fmt.Sprintf("enroll: HTTP %d", e.Status)
}

func (e *EnrollError) Unwrap() error { return e.Err }

// enrollURL — ws://host:8081 → http://host:8081/api/agent/enroll (§3.1).
func enrollURL(serverURL string) (string, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", fmt.Errorf("enroll: server_url: %w", err)
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("enroll: nodirab sxema %q", u.Scheme)
	}
	u.Path = "/api/agent/enroll"
	u.RawQuery, u.Fragment = "", ""
	return u.String(), nil
}

// Enroll — bir martalik kod bilan agent_id/agent_secret oladi (§3.1).
// Muvaffaqiyatda qiymatlar chaqiruvchi tomonidan keystore'ga saqlanadi.
func Enroll(ctx context.Context, serverURL string, info EnrollInfo) (*EnrollResult, error) {
	endpoint, err := enrollURL(serverURL)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]string{
		"code": info.Code, "hostname": info.Hostname, "os": info.OS,
		"lan_ip": info.LanIP, "agent_version": info.AgentVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("enroll: so'rov yig'ilmadi: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("enroll: so'rov: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, &EnrollError{Status: 0, Err: fmt.Errorf("ulanib bo'lmadi: %w", err)}
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		return nil, &EnrollError{Status: res.StatusCode, Err: err}
	}

	if res.StatusCode != http.StatusOK {
		var e struct {
			Error   string `json:"error"`
			AgentID string `json:"agent_id"`
		}
		_ = json.Unmarshal(raw, &e)
		env := &EnrollError{Status: res.StatusCode, Code: e.Error}
		switch {
		case strings.Contains(e.Error, "bad_code"):
			env.Err = ErrBadCode
		case strings.Contains(e.Error, "already_enrolled"):
			env.Err = ErrAlreadyEnrolled
		case strings.Contains(e.Error, "rate_limit"):
			env.Err = ErrRateLimited
		default:
			env.Err = ErrBadResponse
		}
		return nil, env
	}

	var out EnrollResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &EnrollError{Status: res.StatusCode, Err: fmt.Errorf("javob parse: %w", err)}
	}
	if out.AgentID == "" || out.AgentSecret == "" {
		return nil, &EnrollError{Status: res.StatusCode, Err: fmt.Errorf("%w: agent_id/agent_secret bo'sh", ErrBadResponse)}
	}
	return &out, nil
}
