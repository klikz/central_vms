package registry

import (
	"fmt"
	"strings"
)

// Site — joy (nom unikal, root §5 izoh).
type Site struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Code     string `json:"code"`
	Position int    `json:"position"`
}

// Client — agent bloki (agent_id server tomonidan `ag_…` formatda beriladi).
type Client struct {
	ID              int64  `json:"id"`
	SiteID          int64  `json:"site_id,omitempty"`
	Name            string `json:"name"`
	AgentID         string `json:"agent_id"`
	AgentSecretHash string `json:"-"` // hech qachon tashqariga chiqmaydi
	Status          string `json:"status"`
	Version         string `json:"version"`
	OS              string `json:"os"`
	LanIP           string `json:"lan_ip"`
	CapsJSON        string `json:"caps_json"`
	MaxUploads      int    `json:"max_uploads"`
	MaxTranscodes   int    `json:"max_transcodes"`
	TranscodeMode   string `json:"transcode_mode"`
	LastSeenAt      string `json:"last_seen_at,omitempty"`
	EnrolledAt      string `json:"enrolled_at"`
	CamerasCount    int    `json:"cameras_count"`
	CamerasOnline   int    `json:"online_count"`
}

func validTranscodeMode(m string) bool {
	switch m {
	case "auto", "server", "client", "off":
		return true
	}
	return false
}

// CreateSite — nom unikal (UNIQUE → ErrExists).
func (r *Registry) CreateSite(name, code string, position int) (int64, error) {
	if name == "" {
		return 0, fmt.Errorf("site yaratish: %w", ErrBadInput)
	}
	res, err := r.db.Exec("INSERT INTO sites (name, code, position) VALUES (?,?,?)", name, code, position)
	if err != nil {
		return 0, wrap(err, "site yaratish")
	}
	return res.LastInsertId()
}

func (r *Registry) ListSites() ([]Site, error) {
	rows, err := r.db.Query("SELECT id, name, code, position FROM sites ORDER BY position, name")
	if err != nil {
		return nil, wrap(err, "sites list")
	}
	defer rows.Close()
	var out []Site
	for rows.Next() {
		var s Site
		if err := rows.Scan(&s.ID, &s.Name, &s.Code, &s.Position); err != nil {
			return nil, wrap(err, "sites scan")
		}
		out = append(out, s)
	}
	return out, wrap(rows.Err(), "sites rows")
}

func (r *Registry) GetSiteByName(name string) (Site, error) {
	var s Site
	err := r.db.QueryRow("SELECT id, name, code, position FROM sites WHERE name=?", name).
		Scan(&s.ID, &s.Name, &s.Code, &s.Position)
	return s, wrap(err, "site o'qish")
}

// CreateClient — enrollment natijasi: agent_id/secret_hash server beradi.
func (r *Registry) CreateClient(name string, siteID int64, agentID, secretHash string) (int64, error) {
	if name == "" || agentID == "" || secretHash == "" {
		return 0, fmt.Errorf("client yaratish: %w", ErrBadInput)
	}
	var siteArg any
	if siteID != 0 {
		siteArg = siteID
	}
	// Nom UNIQUE (0001_init). Band bo'lsa aniq xato qaytamiz: aks holda
	// enroll "kod allaqachon ishlatilgan" degan chalkash xabarga aylanardi
	// (2026-11-20: ikkinchi mijoz bir xil nom bilan ro'yxatdan o'tganda
	// kuzatildi — kod yaroqli, lekin nom band edi).
	var exists int
	if err := r.db.QueryRow(`SELECT 1 FROM clients WHERE name=? LIMIT 1`, name).Scan(&exists); err == nil {
		return 0, fmt.Errorf("client yaratish: %w (%s)", ErrNameTaken, name)
	}
	res, err := r.db.Exec(
		`INSERT INTO clients (name, site_id, agent_id, agent_secret_hash) VALUES (?,?,?,?)`,
		name, siteArg, agentID, secretHash)
	if err != nil {
		return 0, wrap(err, "client yaratish")
	}
	return res.LastInsertId()
}

// GetClientByAgentID — WS handshake shu maydon orqali (index: UNIQUE).
// GetClientByID — id bo'yicha client (sessiya menejeri camera.ClientID orqali
// agent_id'ni topadi — protocol.md §7: kamera id'si agentniki).
func (r *Registry) GetClientByID(id int64) (Client, error) {
	var c Client
	err := r.db.QueryRow(
		`SELECT id, COALESCE(site_id,0), name, agent_id, agent_secret_hash, status, version, os, lan_ip, caps_json,
		        max_uploads, max_transcodes, transcode_mode, enrolled_at, COALESCE(last_seen_at,'')
		 FROM clients WHERE id = ?`, id).
		Scan(&c.ID, &c.SiteID, &c.Name, &c.AgentID, &c.AgentSecretHash, &c.Status, &c.Version, &c.OS, &c.LanIP,
			&c.CapsJSON, &c.MaxUploads, &c.MaxTranscodes, &c.TranscodeMode, &c.EnrolledAt, &c.LastSeenAt)
	return c, wrap(err, "client o'qish")
}

func (r *Registry) GetClientByAgentID(agentID string) (Client, error) {
	var c Client
	err := r.db.QueryRow(
		`SELECT id, COALESCE(site_id,0), name, agent_id, agent_secret_hash, status, version, os, lan_ip, caps_json,
		        max_uploads, max_transcodes, transcode_mode, enrolled_at, COALESCE(last_seen_at,'')
		 FROM clients WHERE agent_id = ?`, agentID).
		Scan(&c.ID, &c.SiteID, &c.Name, &c.AgentID, &c.AgentSecretHash, &c.Status, &c.Version, &c.OS, &c.LanIP,
			&c.CapsJSON, &c.MaxUploads, &c.MaxTranscodes, &c.TranscodeMode, &c.EnrolledAt, &c.LastSeenAt)
	return c, wrap(err, "client o'qish")
}

func (r *Registry) ListClients() ([]Client, error) {
	rows, err := r.db.Query(
		`SELECT c.id, COALESCE(c.site_id,0), c.name, c.agent_id, c.agent_secret_hash, c.status, c.version, c.os, c.lan_ip,
		        c.caps_json, c.max_uploads, c.max_transcodes, c.transcode_mode, c.enrolled_at,
		        COALESCE(c.last_seen_at,''),
		        (SELECT count(*) FROM cameras k WHERE k.client_id=c.id),
		        (SELECT count(*) FROM cameras k WHERE k.client_id=c.id AND k.status='online')
		 FROM clients c ORDER BY c.name`)
	if err != nil {
		return nil, wrap(err, "clients list")
	}
	defer rows.Close()
	var out []Client
	for rows.Next() {
		var c Client
		if err := rows.Scan(&c.ID, &c.SiteID, &c.Name, &c.AgentID, &c.AgentSecretHash, &c.Status, &c.Version, &c.OS,
			&c.LanIP, &c.CapsJSON, &c.MaxUploads, &c.MaxTranscodes, &c.TranscodeMode,
			&c.EnrolledAt, &c.LastSeenAt, &c.CamerasCount, &c.CamerasOnline); err != nil {
			return nil, wrap(err, "clients scan")
		}
		out = append(out, c)
	}
	return out, wrap(rows.Err(), "clients rows")
}

// UpdateClientHello — hello xabari holati (version/os/lan_ip/caps, online).
func (r *Registry) UpdateClientHello(clientID int64, version, os, lanIP, capsJSON string) error {
	_, err := r.db.Exec(
		`UPDATE clients SET version=?, os=?, lan_ip=?, caps_json=?, status='online',
		        last_seen_at=strftime('%Y-%m-%dT%H:%M:%SZ','now')
		 WHERE id=?`, version, os, lanIP, capsJSON, clientID)
	return wrap(err, "client hello")
}

// MarkClientSeen/Offline — heartbeat va uzilish (S1.12).
func (r *Registry) MarkClientSeen(clientID int64) error {
	_, err := r.db.Exec(
		`UPDATE clients SET status='online', last_seen_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=?`, clientID)
	return wrap(err, "client seen")
}

func (r *Registry) MarkClientOffline(clientID int64) error {
	_, err := r.db.Exec(
		`UPDATE clients SET status='offline', last_seen_at=strftime('%Y-%m-%dT%H:%M:%SZ','now')
		 WHERE id=? AND status='online'`, clientID)
	if err != nil {
		return wrap(err, "client offline")
	}
	// Kameralar ham offline bo'ladi (root §17: "client offline → kameralar offline").
	_, err = r.db.Exec(`UPDATE cameras SET status='offline' WHERE client_id=? AND status!='error'`, clientID)
	return wrap(err, "cameras offline")
}

// ResetOnlineStates — server qayta boshlanganda DB'da qolgan "online"
// belgilarini tozalaydi (2026-11-20: restartdan keyin birorta WS sessiya
// yo'q, lekin clients.status='online' bo'lib qolardi va Boshqaruv/Kuzatuv
// noto‘g'ri "online" ko'rsatardi). Qaytadi: nechta mijoz offline qilindi.
func (r *Registry) ResetOnlineStates() (int64, error) {
	res, err := r.db.Exec(`UPDATE clients SET status='offline' WHERE status='online'`)
	if err != nil {
		return 0, wrap(err, "reset online states")
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, wrap(err, "reset online rows")
	}
	if _, err := r.db.Exec(`UPDATE cameras SET status='offline' WHERE status='online'`); err != nil {
		return n, wrap(err, "reset cameras states")
	}
	return n, nil
}

// SanitizeName — operator nomini saqlashga yaroqli holatga keltiradi:
// qirib-tartib, uzunlik 64 belgi (rune), boshqariladigan belgilar olib
// tashlanadi. Bo‘sh qolsa "" qaytadi (chaqiruvchi avtomatik nomni ishlatadi).
func SanitizeName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	out := make([]rune, 0, len(raw))
	for _, r := range raw {
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		out = append(out, r)
	}
	s2 := strings.Join(strings.Fields(string(out)), " ")
	if len([]rune(s2)) > 64 {
		s2 = string([]rune(s2)[:64])
	}
	return s2
}

// UpdateClientName — mijozning ko‘rinadigan nomini yangilaydi.
// Bo‘sh nom berilsa hech narsa qilinmaydi (avtomatik nomni yo‘q qilmaslik uchun).
func (r *Registry) UpdateClientName(clientID int64, name string) error {
	name = SanitizeName(name)
	if name == "" {
		return nil
	}
	_, err := r.db.Exec(`UPDATE clients SET name=? WHERE id=?`, name, clientID)
	return wrap(err, "client nomi")
}

// UpdateClientSecret — secret_rotate keyin DB hash (S1.13).
func (r *Registry) UpdateClientSecret(clientID int64, secretHash string) error {
	_, err := r.db.Exec("UPDATE clients SET agent_secret_hash=? WHERE id=?", secretHash, clientID)
	return wrap(err, "client secret")
}

// PatchClient — admin sozlamalari (nom/site/limitlar/siyosat; nil → o'zgarmaydi).
func (r *Registry) PatchClient(clientID int64, name *string, siteID *int64, maxUploads, maxTranscodes *int, mode *string) error {
	if mode != nil && !validTranscodeMode(*mode) {
		return fmt.Errorf("client patch: %w", ErrBadInput)
	}
	if name != nil {
		if _, err := r.db.Exec("UPDATE clients SET name=? WHERE id=?", *name, clientID); err != nil {
			return wrap(err, "client name")
		}
	}
	if siteID != nil {
		var v any
		if *siteID != 0 {
			v = *siteID
		}
		if _, err := r.db.Exec("UPDATE clients SET site_id=? WHERE id=?", v, clientID); err != nil {
			return wrap(err, "client site")
		}
	}
	if maxUploads != nil && *maxUploads > 0 {
		if _, err := r.db.Exec("UPDATE clients SET max_uploads=? WHERE id=?", *maxUploads, clientID); err != nil {
			return wrap(err, "client max_uploads")
		}
	}
	if maxTranscodes != nil && *maxTranscodes > 0 {
		if _, err := r.db.Exec("UPDATE clients SET max_transcodes=? WHERE id=?", *maxTranscodes, clientID); err != nil {
			return wrap(err, "client max_transcodes")
		}
	}
	if mode != nil {
		if _, err := r.db.Exec("UPDATE clients SET transcode_mode=? WHERE id=?", *mode, clientID); err != nil {
			return wrap(err, "client mode")
		}
	}
	return nil
}

// DeleteClient — agent o'chirish (cascade: kameralar o'chadi, S1 qabul bandi).
func (r *Registry) DeleteClient(clientID int64) error {
	res, err := r.db.Exec("DELETE FROM clients WHERE id=?", clientID)
	if err != nil {
		return wrap(err, "client o'chirish")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("client o'chirish: %w", ErrNotFound)
	}
	return nil
}
