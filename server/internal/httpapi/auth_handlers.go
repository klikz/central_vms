// S1.14: auth endpoint'lari — login (rate-limit + audit), refresh (cookie),
// logout, /api/me, enroll (ochiq) va enroll yaratish (admin).
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"central-vms/server/internal/audit"
	"central-vms/server/internal/auth"
	"central-vms/server/internal/registry"
	"central-vms/server/internal/wsagent"
)

const refreshCookie = "vms_refresh"

func (d *Deps) setRefreshCookie(w http.ResponseWriter, token string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookie,
		Value:    token,
		Path:     "/api/auth",
		Expires:  exp,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false, // TLS yo'q muhit (root §12); prod TLS'da true qilinadi
	})
}

func (d *Deps) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if ok, retry := d.Login.Allow(ip); !ok {
		_ = d.Audit.Add(0, audit.LoginFailed, "rate:"+ip)
		writeErr(w, http.StatusTooManyRequests, "rate_limited", retrySekund(retry))
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Username == "" {
		writeErr(w, http.StatusBadRequest, "bad_json", "")
		return
	}
	u, hash, err := d.Reg.GetUserByName(in.Username)
	if err != nil {
		if !errors.Is(err, registry.ErrNotFound) {
			d.Lg.Error("login DB xatosi", "user", in.Username, "err", err)
		}
		_ = d.Audit.Add(0, audit.LoginFailed, in.Username)
		writeErr(w, http.StatusUnauthorized, "bad_credentials", "")
		return
	}
	ok, err := auth.VerifyPassword(in.Password, hash)
	if err != nil {
		d.Lg.Error("parol hash formati", "user", u.Username, "err", err)
		writeErr(w, http.StatusInternalServerError, "internal", "")
		return
	}
	if !ok || u.Disabled {
		_ = d.Audit.Add(u.ID, audit.LoginFailed, u.Username)
		writeErr(w, http.StatusUnauthorized, "bad_credentials", "")
		return
	}
	d.Login.Reset(ip)
	if err := d.Reg.TouchLogin(u.ID); err != nil {
		d.Lg.Warn("touch login", "err", err)
	}
	_ = d.Audit.Add(u.ID, audit.Login, u.Username)

	access, aexp, err := d.Tokens.IssueAccess(u.ID, u.Username, u.Role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	refresh, rexp, err := d.Tokens.IssueRefresh(u.ID, u.Username, u.Role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	d.setRefreshCookie(w, refresh, rexp)
	writeJSON(w, http.StatusOK, map[string]any{
		"access": access, "expires_in": int(time.Until(aexp).Seconds()),
		"user": map[string]any{"id": u.ID, "username": u.Username, "role": u.Role},
	})
}

func (d *Deps) handleRefresh(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(refreshCookie)
	if err != nil || c.Value == "" {
		writeErr(w, http.StatusUnauthorized, "no_refresh", "")
		return
	}
	cl, err := d.Tokens.Verify(c.Value, "refresh")
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "bad_refresh", "")
		return
	}
	u, err := d.Reg.GetUserByID(cl.Sub)
	if err != nil || u.Disabled {
		writeErr(w, http.StatusUnauthorized, "bad_refresh", "")
		return
	}
	access, aexp, err := d.Tokens.IssueAccess(u.ID, u.Username, u.Role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	// Refresh rotatsiyasi: eski cookie yangisiga almashtiriladi.
	refresh, rexp, err := d.Tokens.IssueRefresh(u.ID, u.Username, u.Role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	d.setRefreshCookie(w, refresh, rexp)
	writeJSON(w, http.StatusOK, map[string]any{
		"access": access, "expires_in": int(time.Until(aexp).Seconds()),
	})
}

func (d *Deps) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(refreshCookie); err == nil {
		if cl, verr := d.Tokens.Verify(c.Value, "refresh"); verr == nil {
			_ = d.Audit.Add(cl.Sub, audit.Logout, cl.User)
		}
	}
	d.setRefreshCookie(w, "", time.Unix(0, 0))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *Deps) handleMe(w http.ResponseWriter, r *http.Request) {
	cl, _ := auth.ClaimsFrom(r)
	u, err := d.Reg.GetUserByID(cl.Sub)
	if err != nil {
		regErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": u.ID, "username": u.Username, "role": u.Role, "display_name": u.DisplayName,
		"limits": map[string]int{
			"user_max_sessions":   d.Cfg.Limits.UserMaxSessions,
			"global_max_sessions": d.Cfg.Limits.GlobalMaxSessions,
			"preview_max":         d.Cfg.Limits.PreviewMax,
		},
	})
}

// handleJoin — OCHIQ endpoint (protocol.md §3.1): agent ulanish so'rovi
// yuboradi. Mijoz faqat admin tasdig'idan (yoki IP oldindan ruxsat
// etilgan bo'lsa avtomatik) keyin yaratiladi. Bir martalik kod
// 1.0-draft.10 bilan bekor qilindi.
func (d *Deps) handleJoin(w http.ResponseWriter, r *http.Request) {
	var in wsagent.JoinInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", "")
		return
	}
	out, err := d.Join.Request(in, clientIP(r))
	switch {
	case errors.Is(err, wsagent.ErrRateLimited):
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "")
	case errors.Is(err, wsagent.ErrBadKey):
		writeErr(w, http.StatusBadRequest, "bad_key", err.Error())
	case errors.Is(err, wsagent.ErrRejected):
		writeJSON(w, http.StatusForbidden, map[string]any{
			"status": "rejected", "request_id": out.RequestID,
			"error": "rejected", "detail": err.Error(),
		})
	case err != nil:
		d.Lg.Error("join", "ip", clientIP(r), "err", err)
		writeErr(w, http.StatusInternalServerError, "internal", "")
	default:
		if out.Status == "approved" {
			_ = d.Audit.Add(0, audit.JoinApprove, out.AgentID)
		}
		resp := map[string]any{"status": out.Status, "request_id": out.RequestID}
		if out.AgentID != "" {
			resp["agent_id"] = out.AgentID
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func retrySekund(s int) string {
	return "qayta urinish " + itoa(s) + " s keyin mumkin"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
