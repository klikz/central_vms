// S1.14: admin endpoint'lari (faqat admin roli).
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"central-vms/server/internal/audit"
	"central-vms/server/internal/auth"
	"central-vms/server/internal/registry"
	"central-vms/server/internal/wsagent"
)

func (d *Deps) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	us, err := d.Reg.ListUsers()
	if err != nil {
		d.regErr(w, err)
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
		d.fail(w, http.StatusInternalServerError, "internal", err)
		return
	}
	id, err := d.Reg.CreateUser(in.Username, hash, in.Role, in.DisplayName)
	if err != nil {
		d.regErr(w, err)
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
		d.regErr(w, err)
		return
	}
	// Xavfsizlikka tegishli o'zgarishlarda (parol, disable, rol) mavjud
	// access tokenlar DARHOL yaroqsiz bo'ladi: avlod o'stiriladi va
	// refresh'lar o'chiriladi. Avval bu o'zgarishlar access token
	// muddati (15 daqiqa) tugaguncha kuchga kirmas edi (2026-11-21
	// server auditi).
	if hash != nil || (in.Disabled != nil) || (in.Role != nil) {
		if err := d.Reg.BumpUserTokenEpoch(id); err != nil {
			d.Lg.Warn("token avlod o'stirish", "user", id, "err", err)
		}
		if err := d.Tokens.RevokeUser(id); err != nil {
			d.Lg.Warn("refresh bekor qilish", "user", id, "err", err)
		}
	}
	cl, _ := auth.ClaimsFrom(r)
	_ = d.Audit.Add(cl.Sub, audit.UserUpdate, r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Ulanish so'rovlari (protocol.md §3.3) ---

// handleJoinsList — so'rovlar ro'yxati (?status=pending|approved|rejected).
func (d *Deps) handleJoinsList(w http.ResponseWriter, r *http.Request) {
	list, err := d.Reg.ListJoinRequests(r.URL.Query().Get("status"))
	if err != nil {
		d.regErr(w, err)
		return
	}
	if list == nil {
		list = []registry.JoinRequest{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": list, "count": len(list)})
}

// handleJoinApprove — mijozni yaratadi va so'rovni bog'laydi. Nom
// berilmasa so'rovdagi nom ishlatiladi; band bo'lsa 409 name_taken.
func (d *Deps) handleJoinApprove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_id", "")
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in)
	out, err := d.Join.Approve(id, in.Name, claimsSub(r))
	if err != nil {
		switch {
		case errors.Is(err, wsagent.ErrNameTaken):
			d.fail(w, http.StatusConflict, "name_taken", err)
		case errors.Is(err, wsagent.ErrBadKey):
			d.fail(w, http.StatusBadRequest, "bad_request", err)
		default:
			d.regErr(w, err)
		}
		return
	}
	_ = d.Audit.Add(claimsSub(r), audit.JoinApprove, out.AgentID)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "approved", "agent_id": out.AgentID, "request_id": out.RequestID,
	})
}

// handleJoinReject — so'rovni rad etadi (sabab agent UI'sida ko'rsatiladi).
func (d *Deps) handleJoinReject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_id", "")
		return
	}
	var in struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in)
	if err := d.Join.Reject(id, in.Note, claimsSub(r)); err != nil {
		d.regErr(w, err)
		return
	}
	_ = d.Audit.Add(claimsSub(r), audit.JoinReject, strconv.FormatInt(id, 10))
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

// handleJoinDelete — eski so'rovni o'chiradi.
func (d *Deps) handleJoinDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_id", "")
		return
	}
	if err := d.Join.Delete(id); err != nil {
		d.regErr(w, err)
		return
	}
	_ = d.Audit.Add(claimsSub(r), audit.JoinDelete, strconv.FormatInt(id, 10))
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- IP bo'yicha oldindan ruxsat (§3.3) ---

func (d *Deps) handlePreauthList(w http.ResponseWriter, r *http.Request) {
	list, err := d.Reg.ListPreauth()
	if err != nil {
		d.regErr(w, err)
		return
	}
	if list == nil {
		list = []registry.PreauthIP{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "count": len(list)})
}

// handlePreauthAdd — operator IP yozib qo'yadi: shu manzildan kelgan
// so'rov avtomatik tasdiqlanadi.
func (d *Deps) handlePreauthAdd(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IP   string `json:"ip"`
		Name string `json:"name"`
		Note string `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", "")
		return
	}
	id, err := d.Reg.AddPreauth(in.IP, in.Name, in.Note, claimsSub(r))
	if err != nil {
		switch {
		case errors.Is(err, registry.ErrBadInput):
			d.fail(w, http.StatusBadRequest, "bad_ip", err)
		case errors.Is(err, registry.ErrExists):
			d.fail(w, http.StatusConflict, "exists", err)
		default:
			d.regErr(w, err)
		}
		return
	}
	_ = d.Audit.Add(claimsSub(r), audit.PreauthAdd, in.IP)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "ip": in.IP})
}

func (d *Deps) handlePreauthDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_id", "")
		return
	}
	if err := d.Reg.DeletePreauth(id); err != nil {
		d.regErr(w, err)
		return
	}
	_ = d.Audit.Add(claimsSub(r), audit.PreauthDel, strconv.FormatInt(id, 10))
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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
		d.regErr(w, err)
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
	c, err := d.Reg.GetClientByID(id)
	if err != nil {
		d.regErr(w, err)
		return
	}
	if err := d.Reg.DeleteClient(id); err != nil {
		d.regErr(w, err)
		return
	}
	if d.Agent != nil && d.Agent.Kick(c.AgentID, 4004, "mijoz_ochirildi") {
		_ = d.Audit.Add(claimsSub(r), audit.AdminAction, "agent_kick:"+c.AgentID)
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
		d.regErr(w, err)
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
		d.regErr(w, err)
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
		d.fail(w, http.StatusInternalServerError, "internal", err)
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

// handleUserDelete — api.md'da hujjatlangan `DELETE /api/admin/users/{id}`
// (avval umulqon register qilinmagan edi — 405 qaytardi).
func (d *Deps) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "bad_input", "")
		return
	}
	cl, _ := auth.ClaimsFrom(r)
	if cl != nil && cl.Sub == id {
		writeErr(w, http.StatusBadRequest, "self_delete", "")
		return
	}
	if err := d.Reg.DeleteUser(id); err != nil {
		d.regErr(w, err)
		return
	}
	_ = d.Audit.Add(cl.Sub, "user_delete", strconv.FormatInt(id, 10))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleMediaState — api.md: `GET /api/admin/media/state` (faol sessiyalar,
// FFmpeg jarayonlari, backend; operator "nima uchun preview yo'q" deganda
// shu yerga qaraydi).
func (d *Deps) handleMediaState(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{}
	if d.Media != nil {
		if st := d.Media.StateSnapshot(); st != nil {
			out["media"] = st
		}
	}
	if d.Agent != nil {
		out["agents_online"] = d.Agent.Count()
	}
	if d.UI != nil {
		out["ui_conns"] = d.UI.Count()
	}
	if total, online, err := d.Reg.CountCameras(); err == nil {
		out["cameras_total"] = total
		out["cameras_online"] = online
	}
	writeJSON(w, http.StatusOK, out)
}
