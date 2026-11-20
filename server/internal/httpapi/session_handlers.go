// S2.6: sessiya endpoint'lari — POST /api/cameras/{id}/play,
// /preview, /refresh, DELETE /api/sessions/{id} (docs/api.md; root §6).
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"central-vms/server/internal/auth"
	"central-vms/server/internal/media"
	"central-vms/server/internal/registry"
)

func (d *Deps) handleCameraPlay(w http.ResponseWriter, r *http.Request, kind string) {
	if d.Media == nil {
		writeErr(w, http.StatusServiceUnavailable, "media_offline", "media menejeri yo'q")
		return
	}
	cam, err := d.Reg.GetCamera(r.PathValue("id"))
	if err != nil {
		regErr(w, err)
		return
	}
	var in struct {
		Profile string `json:"profile"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.Profile != "" && in.Profile != "main" && in.Profile != "sub" {
		writeErr(w, http.StatusBadRequest, "bad_profile", "")
		return
	}
	cl, _ := auth.ClaimsFrom(r)
	res, err := d.Media.Start(cam, cl.Sub, in.Profile, kind)
	switch {
	case errors.Is(err, media.ErrFull):
		writeErr(w, http.StatusTooManyRequests, "full", "joy band — keyinroq qayta urinib ko'ring")
	case err != nil && strings.Contains(err.Error(), "o'chirilgan"):
		writeErr(w, http.StatusForbidden, "disabled", "")
	case err != nil && strings.Contains(err.Error(), "offline"):
		writeErr(w, http.StatusServiceUnavailable, "agent_offline", "")
	case err != nil:
		d.Lg.Warn("play", "camera", cam.ID, "err", err)
		writeErr(w, http.StatusServiceUnavailable, "media_error", "")
	default:
		_ = d.Audit.Add(cl.Sub, auditPlay, cam.ID)
		writeJSON(w, http.StatusOK, res)
	}
}

const auditPlay = "play"

func (d *Deps) handleSessionRefresh(w http.ResponseWriter, r *http.Request) {
	if d.Media == nil {
		writeErr(w, http.StatusServiceUnavailable, "media_offline", "")
		return
	}
	var in struct {
		SessionID string `json:"session_id"`
		ExtendS   int    `json:"extend_s"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.SessionID == "" {
		writeErr(w, http.StatusBadRequest, "bad_json", "")
		return
	}
	if err := d.Media.Refresh(in.SessionID, in.ExtendS); err != nil {
		writeErr(w, http.StatusNotFound, "session_gone", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *Deps) handleSessionStop(w http.ResponseWriter, r *http.Request) {
	if d.Media == nil {
		writeErr(w, http.StatusServiceUnavailable, "media_offline", "")
		return
	}
	if err := d.Media.Stop(r.PathValue("id"), "user"); err != nil {
		writeErr(w, http.StatusNotFound, "session_gone", err.Error())
		return
	}
	cl, _ := auth.ClaimsFrom(r)
	_ = d.Audit.Add(cl.Sub, "stop", r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleSessions — UI holat paneli: faol sessiyalar (admin) yoki o'nikisi.
func (d *Deps) handleSessions(w http.ResponseWriter, r *http.Request) {
	rows, err := d.Reg.ListActiveSessions()
	if err != nil {
		regErr(w, err)
		return
	}
	cl, _ := auth.ClaimsFrom(r)
	if cl.Role != "admin" {
		var mine []registry.SessionRow
		for _, s := range rows {
			if s.UserID == cl.Sub {
				mine = append(mine, s)
			}
		}
		rows = mine
	}
	writeJSON(w, http.StatusOK, rows)
}
