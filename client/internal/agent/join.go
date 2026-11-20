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
)

// JoinInfo — §3.1 ulanish so'rovi (1.0-draft.10: bir martalik kod bekor
// qilindi). Agent o'z `AgentSecret`ini o'zi yaratadi — server uni hash
// qilib saqlaydi va tasdiqdan keyin uni hech qachon qaytarmaydi.
type JoinInfo struct {
	JoinKey      string // doimiy kalit (hex, ≥32 belgi) — so'rov egalligini isbotlash uchun
	AgentSecret  string // agent yaratgan sir (≥32 belgi)
	Hostname     string
	Name         string // ko'rinadigan nom (config `client_name`)
	OS           string
	LanIP        string
	AgentVersion string
}

// JoinResult — §3.2 javobi.
type JoinResult struct {
	Status    string `json:"status"` // pending | approved | rejected
	AgentID   string `json:"agent_id,omitempty"`
	RequestID int64  `json:"request_id"`
	Detail    string `json:"detail,omitempty"`
}

// Join xato kodlari (§3.2).
var (
	ErrRejected    = errors.New("join: so'rov rad etilgan")
	ErrRateLimited = errors.New("join: juda ko'p urinish — keyinroq qayta urinib ko'ring")
	ErrBadKey      = errors.New("join: kalit yoki sir yaroqsiz")
	ErrBadResponse = errors.New("join: server javobi kutilmagan")
)

// JoinError — HTTP holati va server xato kodi (UI'da aniq ko'rsatish uchun).
type JoinError struct {
	Status int
	Code   string // rejected | rate_limited | bad_key | …
	Err    error
}

func (e *JoinError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("join: HTTP %d (%s)", e.Status, e.Code)
	}
	return fmt.Sprintf("join: HTTP %d", e.Status)
}

func (e *JoinError) Unwrap() error { return e.Err }

// joinURL — ws://host:8081 → http://host:8081/api/agent/join (§3.1).
func joinURL(serverURL string) (string, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", fmt.Errorf("join: server_url: %w", err)
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("join: nodirab sxema %q", u.Scheme)
	}
	u.Path = "/api/agent/join"
	u.RawQuery, u.Fragment = "", ""
	return u.String(), nil
}

// Join — ulanish so'rovini yuboradi yoki mavjud so'rovning holatini
// so'raydi (polling — shu chaqiruvni takrorlash). Tasdiqlangan bo'lsa
// `agent_id` qaytadi; sir qaytmaydi (u allaqachon bizda).
func Join(ctx context.Context, serverURL string, info JoinInfo) (*JoinResult, error) {
	endpoint, err := joinURL(serverURL)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{
		"join_key": info.JoinKey, "agent_secret": info.AgentSecret,
		"hostname": info.Hostname, "name": info.Name, "os": info.OS,
		"lan_ip": info.LanIP, "agent_version": info.AgentVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("join: so'rov yig'ilmadi: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("join: so'rov: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, &JoinError{Status: 0, Err: fmt.Errorf("ulanib bo'lmadi: %w", err)}
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		return nil, &JoinError{Status: res.StatusCode, Err: err}
	}

	if res.StatusCode != http.StatusOK {
		var e struct {
			Error  string `json:"error"`
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(raw, &e)
		env := &JoinError{Status: res.StatusCode, Code: e.Error, Err: errors.New(e.Detail)}
		switch {
		case strings.Contains(e.Error, "rejected"):
			env.Err = fmt.Errorf("%w: %s", ErrRejected, e.Detail)
		case strings.Contains(e.Error, "rate_limit"):
			env.Err = ErrRateLimited
		case strings.Contains(e.Error, "bad_key"):
			env.Err = ErrBadKey
		default:
			env.Err = fmt.Errorf("%w: %s", ErrBadResponse, e.Detail)
		}
		return nil, env
	}

	var out JoinResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &JoinError{Status: res.StatusCode, Err: fmt.Errorf("javob parse: %w", err)}
	}
	switch out.Status {
	case "pending", "approved", "rejected":
	default:
		return nil, &JoinError{Status: res.StatusCode, Err: fmt.Errorf("%w: holat %q", ErrBadResponse, out.Status)}
	}
	if out.Status == "approved" && out.AgentID == "" {
		return nil, &JoinError{Status: res.StatusCode, Err: fmt.Errorf("%w: agent_id bo'sh", ErrBadResponse)}
	}
	return &out, nil
}
