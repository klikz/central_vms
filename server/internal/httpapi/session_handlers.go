// S2.6: sessiya endpoint'lari — POST /api/cameras/{id}/play,
// /preview, /refresh, DELETE /api/sessions/{id} (docs/api.md; root §6).
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"central-vms/server/internal/auth"
	"central-vms/server/internal/media"
	"central-vms/server/internal/registry"
)

// maxExtendS — POST /api/sessions/refresh uchun eng katta uzaytirish
// (api.md: 2 daqiqa).
const maxExtendS = 120

func (d *Deps) handleCameraPlay(w http.ResponseWriter, r *http.Request, kind string) {
	if d.Media == nil {
		writeErr(w, http.StatusServiceUnavailable, "media_offline", "media menejeri yo'q")
		return
	}
	cam, err := d.Reg.GetCamera(r.PathValue("id"))
	if err != nil {
		d.regErr(w, err)
		return
	}
	var in struct {
		Profile string `json:"profile"`
		On      *bool  `json:"on"`
		// Brauzer imkoniyatlari (protocol.md §16.4): HEVC o'ynalsa H.265
		// manbani transcodesiz uzatish mumkin.
		Browser struct {
			HEVC bool `json:"hevc"`
		} `json:"browser"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.Profile != "" && in.Profile != "main" && in.Profile != "sub" {
		writeErr(w, http.StatusBadRequest, "bad_profile", "")
		return
	}
	cl, _ := auth.ClaimsFrom(r)
	// preview {on:false} — foydalanuvchining shu kameradagi preview'sini
	// yopish (api.md).
	if kind == "preview" && in.On != nil && !*in.On {
		for _, sid := range d.Media.SessionsFor(cl.Sub, cam.ID, "preview") {
			_ = d.Media.StopAs(sid, cl.Sub, "user")
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	res, err := d.Media.Start(cam, cl.Sub, in.Profile, kind, media.BrowserCaps{HEVC: in.Browser.HEVC})
	var le *media.LimitError
	switch {
	case errors.As(err, &le):
		writeErr(w, http.StatusTooManyRequests, le.Kind, "joy band — keyinroq qayta urinib ko'ring")
	case errors.Is(err, media.ErrDisabled):
		writeErr(w, http.StatusForbidden, "disabled", "")
	case errors.Is(err, media.ErrAgentOffline):
		writeErr(w, http.StatusConflict, "client_offline", "")
	case err != nil:
		d.Lg.Warn("play", "camera", cam.ID, "err", err)
		writeErr(w, http.StatusServiceUnavailable, "media_offline", "")
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
	cl, _ := auth.ClaimsFrom(r)
	var in struct {
		SessionID string `json:"session_id"`
		ExtendS   int    `json:"extend_s"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.SessionID == "" {
		writeErr(w, http.StatusBadRequest, "bad_json", "")
		return
	}
	// §api.md: TTL uzatish 2 daqiqa. Cheklanmasa mijoz `extend_s`ni
	// qadamsiz qilib sessiyani "mangaga" chiqarardi — upload/transcode
	// slotlari abadiy band qolardi (2026-11-21 auditi).
	if in.ExtendS < 0 || in.ExtendS > maxExtendS {
		writeErr(w, http.StatusBadRequest, "bad_extend", "")
		return
	}
	if err := d.Media.Refresh(in.SessionID, cl.Sub, in.ExtendS); err != nil {
		if errors.Is(err, media.ErrNotOwner) {
			writeErr(w, http.StatusForbidden, "session_not_yours", "")
			return
		}
		d.fail(w, http.StatusNotFound, "session_gone", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *Deps) handleSessionStop(w http.ResponseWriter, r *http.Request) {
	if d.Media == nil {
		writeErr(w, http.StatusServiceUnavailable, "media_offline", "")
		return
	}
	cl, _ := auth.ClaimsFrom(r)
	if err := d.Media.StopAs(r.PathValue("id"), cl.Sub, "user"); err != nil {
		if errors.Is(err, media.ErrNotOwner) {
			writeErr(w, http.StatusForbidden, "session_not_yours", "")
			return
		}
		d.fail(w, http.StatusNotFound, "session_gone", err)
		return
	}
	_ = d.Audit.Add(cl.Sub, "stop", r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleSessions — UI holat paneli: faol sessiyalar (admin) yoki o'nikisi.
func (d *Deps) handleSessions(w http.ResponseWriter, r *http.Request) {
	rows, err := d.Reg.ListActiveSessions()
	if err != nil {
		d.regErr(w, err)
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
