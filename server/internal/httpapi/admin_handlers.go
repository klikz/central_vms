// S1.14: admin endpoint'lari (faqat admin roli).
package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"central-vms/server/internal/audit"
	"central-vms/server/internal/auth"
)

func (d *Deps) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	us, err := d.Reg.ListUsers()
	if err != nil {
		regErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, us)
}

func (d *Deps) handleAdminUserCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		Role        string `json:"role"`
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", "")
		return
	}
	if len(in.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "weak_password", "parol kamida 8 belgi")
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	id, err := d.Reg.CreateUser(in.Username, hash, in.Role, in.DisplayName)
	if err != nil {
		regErr(w, err)
		return
	}
	cl, _ := auth.ClaimsFrom(r)
	_ = d.Audit.Add(cl.Sub, audit.UserCreate, in.Username)
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (d *Deps) handleAdminUserPatch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_id", "")
		return
	}
	var in struct {
		Role        *string `json:"role"`
		DisplayName *string `json:"display_name"`
		Disabled    *bool   `json:"disabled"`
		Password    *string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", "")
		return
	}
	var hash *string
	if in.Password != nil {
		if len(*in.Password) < 8 {
			writeErr(w, http.StatusBadRequest, "weak_password", "")
			return
		}
		h, err := auth.HashPassword(*in.Password)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", "")
			return
		}
		hash = &h
	}
	if err := d.Reg.UpdateUser(id, in.Role, in.DisplayName, in.Disabled, hash); err != nil {
		regErr(w, err)
		return
	}
	cl, _ := auth.ClaimsFrom(r)
	_ = d.Audit.Add(cl.Sub, audit.UserUpdate, r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleEnrollCreate — bir martalik kod (24 soat, config.enrollment.ttl_h).
func (d *Deps) handleEnrollCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SiteName string `json:"site_name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	var siteID int64
	if in.SiteName != "" {
		s, err := d.Reg.GetSiteByName(in.SiteName)
		if err != nil {
			regErr(w, err)
			return
		}
		siteID = s.ID
	}
	code, exp, err := d.Enr.CreateCode(siteID, claimsSub(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	_ = d.Audit.Add(claimsSub(r), audit.EnrollCreate, in.SiteName)
	writeJSON(w, http.StatusCreated, map[string]any{
		"code": code, "expires_at": exp.Format(time.RFC3339),
		"note": "kod faqat shu javobda ko'rinadi — bir marta ishlatiladi",
	})
}

func (d *Deps) handleClientPatch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_id", "")
		return
	}
	var in struct {
		Name          *string `json:"name"`
		SiteID        *int64  `json:"site_id"`
		MaxUploads    *int    `json:"max_uploads"`
		MaxTranscodes *int    `json:"max_transcodes"`
		TranscodeMode *string `json:"transcode_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", "")
		return
	}
	if err := d.Reg.PatchClient(id, in.Name, in.SiteID, in.MaxUploads, in.MaxTranscodes, in.TranscodeMode); err != nil {
		regErr(w, err)
		return
	}
	// Limit/siyosat o'zgarsa agentga darhol config_push (protocol.md §5.7).
	if c, err := d.Reg.GetClientByAgentID(d.agentIDFor(id)); err == nil {
		_ = d.Agent.PushConfig(c.AgentID, pushLimits(c), protoSettings())
	}
	_ = d.Audit.Add(claimsSub(r), audit.AdminAction, "client:"+r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *Deps) agentIDFor(clientID int64) string {
	cs, err := d.Reg.ListClients()
	if err != nil {
		return ""
	}
	for _, c := range cs {
		if c.ID == clientID {
			return c.AgentID
		}
	}
	return ""
}

func (d *Deps) handleClientDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_id", "")
		return
	}
	if err := d.Reg.DeleteClient(id); err != nil {
		regErr(w, err)
		return
	}
	_ = d.Audit.Add(claimsSub(r), audit.AdminAction, "client_delete:"+r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *Deps) handleCameraPolicy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PlayProfile   *string `json:"play_profile"`
		TranscodeMode *string `json:"transcode_mode"`
		Enabled       *bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", "")
		return
	}
	if err := d.Reg.PatchCameraPolicy(r.PathValue("id"), in.PlayProfile, in.TranscodeMode, in.Enabled); err != nil {
		regErr(w, err)
		return
	}
	_ = d.Audit.Add(claimsSub(r), audit.AdminAction, "camera_policy:"+r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleSecretRotate — yangi agent_secret (admin amali; agentga secret_rotate).
func (d *Deps) handleSecretRotate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_id", "")
		return
	}
	agentID := d.agentIDFor(id)
	if agentID == "" {
		writeErr(w, http.StatusNotFound, "not_found", "")
		return
	}
	secret, err := d.Agent.RotateSecret(agentID, 60)
	if err != nil {
		regErr(w, err)
		return
	}
	_ = d.Audit.Add(claimsSub(r), audit.AdminAction, "secret_rotate:"+agentID)
	writeJSON(w, http.StatusOK, map[string]string{
		"agent_secret": secret, "note": "bir marta ko'rinadi; agent state fayliga yoziladi",
	})
}

func (d *Deps) handleAudit(w http.ResponseWriter, r *http.Request) {
	items, err := d.Audit.List(200)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func claimsSub(r *http.Request) int64 {
	if c, ok := auth.ClaimsFrom(r); ok {
		return c.Sub
	}
	return 0
}
