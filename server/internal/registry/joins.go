// §3 (1.0-draft.10): ulanish so'rovlari va IP bo'yicha oldindan ruxsat.
// Ulanish kodi bekor qilindi — agent so'rov yuboradi, mijoz faqat admin
// tasdig'idan (yoki preauth IP mosligidan) keyin yaratiladi.
package registry

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// JoinRequest — join_requests jadvali qatori.
type JoinRequest struct {
	ID              int64  `json:"id"`
	JoinKeyHash     string `json:"-"` // hech qachon tashqariga chiqmaydi
	AgentSecretHash string `json:"-"` // agent o'zi yaratgan sirning hash'i
	Hostname        string `json:"hostname"`
	Name            string `json:"name"`
	OS              string `json:"os"`
	LanIP           string `json:"lan_ip"`
	SourceIP        string `json:"source_ip"`
	AgentVersion    string `json:"agent_version"`
	CapsJSON        string `json:"caps_json"`
	Status          string `json:"status"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	DecidedAt       string `json:"decided_at,omitempty"`
	Note            string `json:"note"`
	ClientID        int64  `json:"client_id,omitempty"`
}

// PreauthIP — IP bo'yicha oldindan ruxsat (protocol.md §3.3).
type PreauthIP struct {
	ID        int64  `json:"id"`
	IP        string `json:"ip"`
	Name      string `json:"name"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
}

// JoinInfo — agent yuborgan so'rov maydonlari.
type JoinInfo struct {
	JoinKeyHash     string
	AgentSecretHash string
	Hostname        string
	Name            string
	OS              string
	LanIP           string
	SourceIP        string
	AgentVersion    string
	CapsJSON        string
}

// UpsertJoinRequest — so'rovni qayd etadi (yoki mavjud so'rovning
// ma'lumotlarini yangilaydi). Qator qaytariladi; `created` — yangi qo'shildi.
func (r *Registry) UpsertJoinRequest(in JoinInfo) (JoinRequest, bool, error) {
	if in.JoinKeyHash == "" {
		return JoinRequest{}, false, fmt.Errorf("join: %w", ErrBadInput)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// Avval borligini tekshiramiz: ON CONFLICT holatida RowsAffected
	// "yangi qo'shildimi" degan savolga ishonchli javob bermaydi.
	var existed int
	existErr := r.db.QueryRow(`SELECT 1 FROM join_requests WHERE join_key_hash=? LIMIT 1`,
		in.JoinKeyHash).Scan(&existed)
	created := errors.Is(existErr, sql.ErrNoRows) // qator yo‘q → yangi so‘rov
	if existErr != nil && !created {
		return JoinRequest{}, false, wrap(existErr, "join tekshirish")
	}
	_, err := r.db.Exec(`INSERT INTO join_requests
		(join_key_hash, agent_secret_hash, hostname, name, os, lan_ip, source_ip,
		 agent_version, caps_json, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(join_key_hash) DO UPDATE SET
			agent_secret_hash=excluded.agent_secret_hash,
			hostname=excluded.hostname, name=excluded.name, os=excluded.os,
			lan_ip=excluded.lan_ip, source_ip=excluded.source_ip,
			agent_version=excluded.agent_version, caps_json=excluded.caps_json,
			updated_at=excluded.updated_at`,
		in.JoinKeyHash, in.AgentSecretHash, in.Hostname, in.Name, in.OS, in.LanIP,
		in.SourceIP, in.AgentVersion, in.CapsJSON, now)
	if err != nil {
		return JoinRequest{}, false, wrap(err, "join so'rov")
	}
	q, err := r.GetJoinRequest(in.JoinKeyHash)
	return q, created, err
}

// GetJoinRequest — kalit bo'yicha so'rov (§3.2 polling shu orqali).
func (r *Registry) GetJoinRequest(keyHash string) (JoinRequest, error) {
	return r.getJoin("join_key_hash", keyHash)
}

// GetJoinRequestByID — admin amallari uchun (id bo'yicha).
func (r *Registry) GetJoinRequestByID(id int64) (JoinRequest, error) {
	return r.getJoin("id", id)
}

func (r *Registry) getJoin(column string, arg any) (JoinRequest, error) {
	var q JoinRequest
	var clientID sql.NullInt64
	err := r.db.QueryRow(`SELECT id, join_key_hash, agent_secret_hash, hostname, name, os,
		lan_ip, source_ip, agent_version, caps_json, status, created_at, updated_at,
		COALESCE(decided_at,''), COALESCE(note,''), client_id
		FROM join_requests WHERE `+column+`=?`, arg).
		Scan(&q.ID, &q.JoinKeyHash, &q.AgentSecretHash, &q.Hostname, &q.Name, &q.OS,
			&q.LanIP, &q.SourceIP, &q.AgentVersion, &q.CapsJSON, &q.Status,
			&q.CreatedAt, &q.UpdatedAt, &q.DecidedAt, &q.Note, &clientID)
	if errors.Is(err, sql.ErrNoRows) {
		return JoinRequest{}, ErrNotFound
	}
	if err != nil {
		return JoinRequest{}, wrap(err, "join so'rov o'qish")
	}
	q.ClientID = clientID.Int64
	return q, nil
}

// ListJoinRequests — status bo'yicha (” = barchasi). Yangi avval.
func (r *Registry) ListJoinRequests(status string) ([]JoinRequest, error) {
	q := `SELECT id, join_key_hash, agent_secret_hash, hostname, name, os, lan_ip,
		source_ip, agent_version, caps_json, status, created_at, updated_at,
		COALESCE(decided_at,''), COALESCE(note,''), client_id
		FROM join_requests`
	args := []any{}
	if status != "" {
		q += ` WHERE status=?`
		args = append(args, status)
	}
	q += ` ORDER BY (status='pending') DESC, created_at DESC LIMIT 200`
	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, wrap(err, "join ro'yxati")
	}
	defer rows.Close()
	var out []JoinRequest
	for rows.Next() {
		var it JoinRequest
		var clientID sql.NullInt64
		if err := rows.Scan(&it.ID, &it.JoinKeyHash, &it.AgentSecretHash, &it.Hostname,
			&it.Name, &it.OS, &it.LanIP, &it.SourceIP, &it.AgentVersion, &it.CapsJSON,
			&it.Status, &it.CreatedAt, &it.UpdatedAt, &it.DecidedAt, &it.Note,
			&clientID); err != nil {
			return nil, wrap(err, "join ro'yxati qatori")
		}
		it.ClientID = clientID.Int64
		out = append(out, it)
	}
	return out, wrap(rows.Err(), "join ro'yxati yakuni")
}

// ApproveJoin — so'rovni tasdiqlaydi: mijoz yaratiladi va so'rov bog'lanadi.
// Bitta tranzaksiya: mijoz yaratilib, so'rov "approved" bo'lmasa, ikkalasi
// ham qaytariladi (yetim mijoz qolmasin).
func (r *Registry) ApproveJoin(requestID int64, agentID, secretHash, name string, decidedBy int64) (int64, error) {
	name = SanitizeName(name)
	if name == "" || agentID == "" || secretHash == "" {
		return 0, fmt.Errorf("join tasdiq: %w", ErrBadInput)
	}
	tx, err := r.db.Begin()
	if err != nil {
		return 0, wrap(err, "join tranzaksiya")
	}
	defer func() { _ = tx.Rollback() }()

	var status string
	var clientID sql.NullInt64
	err = tx.QueryRow(`SELECT status, client_id FROM join_requests WHERE id=?`, requestID).
		Scan(&status, &clientID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, wrap(err, "join tekshirish")
	}
	if status != "pending" {
		return 0, fmt.Errorf("join tasdiq: so'rov holati %q: %w", status, ErrExists)
	}
	var exists int
	if err := tx.QueryRow(`SELECT 1 FROM clients WHERE name=? LIMIT 1`, name).Scan(&exists); err == nil {
		return 0, fmt.Errorf("join tasdiq: %w (%s)", ErrNameTaken, name)
	}
	var siteArg any
	res, err := tx.Exec(`INSERT INTO clients (name, site_id, agent_id, agent_secret_hash) VALUES (?,?,?,?)`,
		name, siteArg, agentID, secretHash)
	if err != nil {
		return 0, wrap(err, "mijoz yaratish")
	}
	newID, _ := res.LastInsertId()
	if _, err := tx.Exec(`UPDATE join_requests SET status='approved', client_id=?, decided_at=?,
		decided_by=?, updated_at=? WHERE id=? AND status='pending'`,
		newID, time.Now().UTC().Format(time.RFC3339), nullInt64(decidedBy),
		time.Now().UTC().Format(time.RFC3339), requestID); err != nil {
		return 0, wrap(err, "join tasdiq belgilash")
	}
	if err := tx.Commit(); err != nil {
		return 0, wrap(err, "join tranzaksiya commit")
	}
	return newID, nil
}

// RejectJoin — so'rovni rad etadi (sabab `note`da ko'rsatiladi).
func (r *Registry) RejectJoin(requestID int64, note string, decidedBy int64) error {
	res, err := r.db.Exec(`UPDATE join_requests SET status='rejected', decided_at=?, decided_by=?,
		updated_at=?, note=? WHERE id=? AND status='pending'`,
		time.Now().UTC().Format(time.RFC3339), nullInt64(decidedBy),
		time.Now().UTC().Format(time.RFC3339), strings.TrimSpace(note), requestID)
	if err != nil {
		return wrap(err, "join rad")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteJoinRequest — eski so'rovni butunlay o'chiradi (agent qayta
// so'rov yuborsa yangi qator paydo bo'ladi).
func (r *Registry) DeleteJoinRequest(requestID int64) error {
	res, err := r.db.Exec(`DELETE FROM join_requests WHERE id=?`, requestID)
	if err != nil {
		return wrap(err, "join o'chirish")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddPreauth — IP bo'yicha oldindan ruxsat qo'shadi. `ip` — aniq manzil
// yoki CIDR. Takror qo'shish — ErrExists.
func (r *Registry) AddPreauth(ip, name, note string, createdBy int64) (int64, error) {
	ip = strings.TrimSpace(ip)
	name = SanitizeName(name)
	if !validIPIPv46CIDR(ip) {
		return 0, fmt.Errorf("preauth: IP/CIDR yaroqsiz: %q: %w", ip, ErrBadInput)
	}
	res, err := r.db.Exec(`INSERT INTO preauth_ips (ip, name, note, created_by) VALUES (?,?,?,?)`,
		ip, name, strings.TrimSpace(note), nullInt64(createdBy))
	if err != nil {
		return 0, wrap(err, "preauth")
	}
	return res.LastInsertId()
}

// ListPreauth — barcha oldindan ruxsatlar.
func (r *Registry) ListPreauth() ([]PreauthIP, error) {
	rows, err := r.db.Query(`SELECT id, ip, COALESCE(name,''), COALESCE(note,''), created_at
		FROM preauth_ips ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		return nil, wrap(err, "preauth ro'yxati")
	}
	defer rows.Close()
	var out []PreauthIP
	for rows.Next() {
		var it PreauthIP
		if err := rows.Scan(&it.ID, &it.IP, &it.Name, &it.Note, &it.CreatedAt); err != nil {
			return nil, wrap(err, "preauth qatori")
		}
		out = append(out, it)
	}
	return out, wrap(rows.Err(), "preauth yakuni")
}

// DeletePreauth — oldindan ruxsatni olib tashlash.
func (r *Registry) DeletePreauth(id int64) error {
	res, err := r.db.Exec(`DELETE FROM preauth_ips WHERE id=?`, id)
	if err != nil {
		return wrap(err, "preauth o'chirish")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MatchPreauth — kelgan IP preauth ro'yxatidagi biror manzil/CIDRga
// mos keladimi. Mos yozuv qaytariladi (nomi mijoz nomi sifatida ishlatiladi).
func (r *Registry) MatchPreauth(ip string) (*PreauthIP, error) {
	all, err := r.ListPreauth()
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}
	addr := net.ParseIP(strings.TrimSpace(ip))
	if addr == nil {
		return nil, nil
	}
	for _, p := range all {
		if ipMatches(p.IP, addr) {
			cp := p
			return &cp, nil
		}
	}
	return nil, nil
}

// ipMatches — aniq manzil yoki CIDR mosligi.
func ipMatches(pattern string, addr net.IP) bool {
	if strings.Contains(pattern, "/") {
		_, netw, err := net.ParseCIDR(pattern)
		return err == nil && netw.Contains(addr)
	}
	p := net.ParseIP(pattern)
	return p != nil && p.Equal(addr)
}

// validIPIPv46CIDR — preauth kaliti uchun tekshiruv.
func validIPIPv46CIDR(s string) bool {
	if strings.Contains(s, "/") {
		_, _, err := net.ParseCIDR(s)
		return err == nil
	}
	return net.ParseIP(s) != nil
}

func nullInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
