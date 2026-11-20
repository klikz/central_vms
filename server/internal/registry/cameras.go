package registry

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Camera — cameras jadvali (id: agent bergan UUIDv4, o'zgarmas — protocol.md §6).
type Camera struct {
	ID            string `json:"id"`
	ClientID      int64  `json:"client_id"`
	Name          string `json:"name"`
	OnvifXaddr    string `json:"onvif_xaddr,omitempty"`
	RTSPMasked    string `json:"rtsp_masked"`
	CodecMain     string `json:"codec_main"`
	CodecSub      string `json:"codec_sub"`
	BFrameMain    bool   `json:"bframe_main"`
	BFrameSub     bool   `json:"bframe_sub"`
	PlayProfile   string `json:"play_profile"`
	TranscodeMode string `json:"transcode_mode"`
	Enabled       bool   `json:"enabled"`
	Position      int    `json:"position"`
	Status        string `json:"status"`
	LastCheckAt   string `json:"last_check_at,omitempty"`
	CodecProbeAt  string `json:"codec_probe_at,omitempty"`
	// LastError — oxirgi muvaffaqiyatsizlik sababi (probe yoki oqim);
	// UI'da ko'rsatiladi (1.0-draft.12).
	LastError   string `json:"last_error,omitempty"`
	LastErrorAt string `json:"last_error_at,omitempty"`
}

// SyncCamera — cams_sync(upsert) ma'lumotlari (proto paketiga bog'liq emas).
type SyncCamera struct {
	ID         string
	Name       string
	RTSPMasked string
	CodecMain  string
	CodecSub   string
	BFrameMain bool
	BFrameSub  bool
	OnvifXaddr string
	Status     string
	Position   int
}

// UpsertCamera — sync maydonlarini yangilaydi; SERVER siyosati
// (play_profile, transcode_mode, enabled) SYNC'DA O'ZGARMAYDI (protocol.md §6.4:
// policy — admin PATCH'i). Boshqa client'ning id'si qayta yozilmaydi.
func (r *Registry) UpsertCamera(clientID int64, c SyncCamera) error {
	if c.ID == "" || c.Name == "" {
		return fmt.Errorf("camera upsert: %w", ErrBadInput)
	}
	// 1.0-draft.12: sync'da `status` bo'sh kelsa MAVJUD holat saqlanadi —
	// aks holda to'liq cams_sync `cam_status` orqali kelgan "online"ni
	// bosib "offline" qilib qo'yardi (server qayta boshlanganda
	// kameralar doim offline ko'rinar edi; 2026-11-21 kuzatuvi).
	var owner int64
	var curStatus string
	err := r.db.QueryRow("SELECT client_id, status FROM cameras WHERE id=?", c.ID).Scan(&owner, &curStatus)
	switch {
	case err == sql.ErrNoRows: // yangi — muammo yo'q
		curStatus = ""
	case err != nil:
		return wrap(err, "camera tekshirish")
	case owner != clientID:
		return fmt.Errorf("camera upsert: id %s boshqa clientga (%d) tegishli: %w", c.ID, owner, ErrExists)
	}
	_, err = r.db.Exec(
		`INSERT INTO cameras (id, client_id, name, rtsp_masked, codec_main, codec_sub,
		    bframe_main, bframe_sub, onvif_xaddr, status, position)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET
		    name=excluded.name, rtsp_masked=excluded.rtsp_masked,
		    codec_main=excluded.codec_main, codec_sub=excluded.codec_sub,
		    bframe_main=excluded.bframe_main, bframe_sub=excluded.bframe_sub,
		    onvif_xaddr=excluded.onvif_xaddr, status=excluded.status, position=excluded.position`,
		c.ID, clientID, c.Name, c.RTSPMasked, def(c.CodecMain, "unknown"), def(c.CodecSub, "unknown"),
		b2i(c.BFrameMain), b2i(c.BFrameSub), c.OnvifXaddr, def(def(c.Status, curStatus), "offline"), c.Position)
	return wrap(err, "camera upsert")
}

// DeleteCamera — cams_sync(op:delete). Cascade: favorites (FK) va sessiyalar
// o'chadi; layouts sloti bo'shatiladi (slots_json ichida — S1.10 test).
func (r *Registry) DeleteCamera(cameraID string) error {
	res, err := r.db.Exec("DELETE FROM cameras WHERE id=?", cameraID)
	if err != nil {
		return wrap(err, "camera o'chirish")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("camera o'chirish: %w", ErrNotFound)
	}
	return nil
}

// SetCameraStatus — cam_status xabari.
func (r *Registry) SetCameraStatus(cameraID, status, errMsg string, rttMs int64) error {
	switch status {
	case "online", "offline", "error":
	default:
		return fmt.Errorf("camera status: %w", ErrBadInput)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// Online kamera uchun sabab saqlanmaydi: masalan MediaMTX'ning
	// GET_PARAMETER'ni qo'llamasligi (keepalive_unsupported) — bu xato
	// emas, zanjir fallback bilan ishlagan (2026-11-20).
	e := strings.TrimSpace(errMsg)
	if status == "online" {
		e = ""
	}
	res, err := r.db.Exec(
		`UPDATE cameras SET status=?, last_check_at=?,
		 last_error=?, last_error_at=CASE WHEN ?='' THEN NULL ELSE ? END
		 WHERE id=?`, status, now, e, e, now, cameraID)
	if err != nil {
		return wrap(err, "camera status")
	}
	_ = rttMs
	_ = rttMs // S2'da kuzatuv uchun saqlanadi (hozir logda ko'rinadi)
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("camera status: %w", ErrNotFound)
	}
	return nil
}

// SetCameraError — oqim/sessiya muvaffaqiyatsizligining sababini saqlaydi
// (holatni o'zgartirmaydi): masalan agent publish_failed deb yuborsa,
// operator kamerani "nima uchun ko'rinmayotgani"ni UI'da ko'radi
// (1.0-draft.12).
func (r *Registry) SetCameraError(cameraID, msg string) error {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return nil
	}
	_, err := r.db.Exec(`UPDATE cameras SET last_error=?, last_error_at=? WHERE id=?`,
		msg, time.Now().UTC().Format(time.RFC3339), cameraID)
	return wrap(err, "camera xatosi")
}

// SaveCodecProbe — codec_probe xabari (SDP tahlili natijasi).
func (r *Registry) SaveCodecProbe(cameraID, codecMain, codecSub string, bMain, bSub bool) error {
	res, err := r.db.Exec(
		`UPDATE cameras SET codec_main=?, codec_sub=?, bframe_main=?, bframe_sub=?,
		        codec_probe_at=strftime('%Y-%m-%dT%H:%M:%SZ','now')
		 WHERE id=?`, def(codecMain, "unknown"), def(codecSub, "unknown"),
		b2i(bMain), b2i(bSub), cameraID)
	if err != nil {
		return wrap(err, "codec probe")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("codec probe: %w", ErrNotFound)
	}
	return nil
}

// PatchCameraPolicy — admin PATCH (root §6): faqat siyosat maydonlari.
func (r *Registry) PatchCameraPolicy(cameraID string, playProfile, transcodeMode *string, enabled *bool) error {
	if playProfile != nil && *playProfile != "main" && *playProfile != "sub" {
		return fmt.Errorf("camera patch: %w", ErrBadInput)
	}
	if transcodeMode != nil && !validTranscodeMode(*transcodeMode) {
		return fmt.Errorf("camera patch: %w", ErrBadInput)
	}
	if playProfile != nil {
		if _, err := r.db.Exec("UPDATE cameras SET play_profile=? WHERE id=?", *playProfile, cameraID); err != nil {
			return wrap(err, "camera play_profile")
		}
	}
	if transcodeMode != nil {
		if _, err := r.db.Exec("UPDATE cameras SET transcode_mode=? WHERE id=?", *transcodeMode, cameraID); err != nil {
			return wrap(err, "camera transcode_mode")
		}
	}
	if enabled != nil {
		if _, err := r.db.Exec("UPDATE cameras SET enabled=? WHERE id=?", b2i(*enabled), cameraID); err != nil {
			return wrap(err, "camera enabled")
		}
	}
	return nil
}

func (r *Registry) GetCamera(cameraID string) (Camera, error) {
	var c Camera
	err := r.db.QueryRow(
		`SELECT id, client_id, name, onvif_xaddr, rtsp_masked, codec_main, codec_sub,
		        bframe_main, bframe_sub, play_profile, transcode_mode, enabled, position,
		        status, COALESCE(last_check_at,''), COALESCE(codec_probe_at,''),
		        COALESCE(last_error,''), COALESCE(last_error_at,'')
		 FROM cameras WHERE id=?`, cameraID).
		Scan(&c.ID, &c.ClientID, &c.Name, &c.OnvifXaddr, &c.RTSPMasked, &c.CodecMain, &c.CodecSub,
			&c.BFrameMain, &c.BFrameSub, &c.PlayProfile, &c.TranscodeMode, &c.Enabled, &c.Position,
			&c.Status, &c.LastCheckAt, &c.CodecProbeAt,
			&c.LastError, &c.LastErrorAt)
	return c, wrap(err, "camera o'qish")
}

// ListCamerasByClient — UI bloki (position tartibida).
func (r *Registry) ListCamerasByClient(clientID int64) ([]Camera, error) {
	rows, err := r.db.Query(
		`SELECT id, client_id, name, onvif_xaddr, rtsp_masked, codec_main, codec_sub,
		        bframe_main, bframe_sub, play_profile, transcode_mode, enabled, position,
		        status, COALESCE(last_check_at,''), COALESCE(codec_probe_at,''),
		        COALESCE(last_error,''), COALESCE(last_error_at,'')
		 FROM cameras WHERE client_id=? ORDER BY position, name`, clientID)
	if err != nil {
		return nil, wrap(err, "cameras list")
	}
	defer rows.Close()
	var out []Camera
	for rows.Next() {
		var c Camera
		if err := rows.Scan(&c.ID, &c.ClientID, &c.Name, &c.OnvifXaddr, &c.RTSPMasked, &c.CodecMain,
			&c.CodecSub, &c.BFrameMain, &c.BFrameSub, &c.PlayProfile, &c.TranscodeMode, &c.Enabled,
			&c.Position, &c.Status, &c.LastCheckAt, &c.CodecProbeAt,
			&c.LastError, &c.LastErrorAt); err != nil {
			return nil, wrap(err, "cameras scan")
		}
		out = append(out, c)
	}
	return out, wrap(rows.Err(), "cameras rows")
}

func def(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ListCamerasByAgent — agent_id bo'yicha kameralar (testlar va S2.7 uchun).
func (r *Registry) ListCamerasByAgent(agentID string) ([]Camera, error) {
	rows, err := r.db.Query(
		`SELECT k.id, k.client_id, k.name, k.rtsp_masked, k.codec_main, k.codec_sub,
		        k.bframe_main, k.bframe_sub, k.play_profile, k.transcode_mode, k.enabled,
		        k.position, k.status, COALESCE(k.last_check_at,''), COALESCE(k.codec_probe_at,''),
		        COALESCE(k.last_error,''), COALESCE(k.last_error_at,''),
		        COALESCE(k.onvif_xaddr,'')
		 FROM cameras k JOIN clients c ON c.id = k.client_id WHERE c.agent_id=? ORDER BY k.position`, agentID)
	if err != nil {
		return nil, wrap(err, "kameralar (agent)")
	}
	defer rows.Close()
	var out []Camera
	for rows.Next() {
		var c Camera
		var en, bm, bs int
		if err := rows.Scan(&c.ID, &c.ClientID, &c.Name, &c.RTSPMasked, &c.CodecMain, &c.CodecSub,
			&bm, &bs, &c.PlayProfile, &c.TranscodeMode, &en,
			&c.Position, &c.Status, &c.LastCheckAt, &c.CodecProbeAt,
			&c.LastError, &c.LastErrorAt, &c.OnvifXaddr); err != nil {
			return nil, wrap(err, "kameralar scan")
		}
		c.Enabled, c.BFrameMain, c.BFrameSub = en != 0, bm != 0, bs != 0
		out = append(out, c)
	}
	return out, rows.Err()
}
