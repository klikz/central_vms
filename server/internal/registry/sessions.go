// S2.4: sessions jadvali CRUD (holat mashinasining DB qismi; asosiy
// holatlar menejerda, DB — tiklash/audit uchun yoziladi).
package registry

import (
	"fmt"
)

// SessionRow — sessions jadvalidagi yozuv (S2 menejeri bilan sinxron).
type SessionRow struct {
	ID        string
	CameraID  string
	UserID    int64
	Profile   string
	Kind      string
	State     string
	ExpiresAt string
	PublishID string
}

// InsertSession — sessiyani yaratadi (state=pending).
func (r *Registry) InsertSession(id, cameraID string, userID int64, profile, kind string, expiresAt string) error {
	_, err := r.db.Exec(
		`INSERT INTO sessions (id, camera_id, user_id, profile, kind, state, expires_at)
		 VALUES (?,?,?,?,?,'pending',?)`, id, cameraID, userID, profile, kind, expiresAt)
	if err != nil {
		return fmt.Errorf("session yaratish: %w", err)
	}
	return nil
}

// SetSessionState — holat o'tishini yozadi (ended_at faqat ended/failed'da).
func (r *Registry) SetSessionState(id, state string) error {
	var err error
	if state == "ended" || state == "failed" {
		_, err = r.db.Exec(
			`UPDATE sessions SET state=?, ended_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=?`,
			state, id)
	} else {
		_, err = r.db.Exec(`UPDATE sessions SET state=? WHERE id=?`, state, id)
	}
	if err != nil {
		return fmt.Errorf("session state: %w", err)
	}
	return nil
}

// TouchSessionTTL — expires_at'ni yangilaydi (media_refresh).
func (r *Registry) TouchSessionTTL(id, expiresAt string) error {
	if _, err := r.db.Exec(`UPDATE sessions SET expires_at=? WHERE id=?`, expiresAt, id); err != nil {
		return fmt.Errorf("session ttl: %w", err)
	}
	return nil
}

// ListActiveSessions — server qayta boshlanganda tiklash uchun
// (pending/publishing/playing/stopping).
func (r *Registry) ListActiveSessions() ([]SessionRow, error) {
	rows, err := r.db.Query(
		`SELECT s.id, s.camera_id, s.user_id, s.profile, s.kind, s.state,
		        COALESCE(s.expires_at,'')
		 FROM sessions s
		 WHERE s.state IN ('pending','publishing','playing','stopping')`)
	if err != nil {
		return nil, fmt.Errorf("sessions ro'yxati: %w", err)
	}
	defer rows.Close()
	var out []SessionRow
	for rows.Next() {
		var s SessionRow
		if err := rows.Scan(&s.ID, &s.CameraID, &s.UserID, &s.Profile, &s.Kind, &s.State, &s.ExpiresAt); err != nil {
			return nil, fmt.Errorf("sessions scan: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CountActiveSessions — limitlar uchun (global/per-user).
func (r *Registry) CountActiveSessions(userID int64) (int, error) {
	var n int
	var err error
	if userID > 0 {
		err = r.db.QueryRow(
			`SELECT COUNT(*) FROM sessions WHERE user_id=? AND state IN ('pending','publishing','playing')`,
			userID).Scan(&n)
	} else {
		err = r.db.QueryRow(
			`SELECT COUNT(*) FROM sessions WHERE state IN ('pending','publishing','playing')`).Scan(&n)
	}
	if err != nil {
		return 0, fmt.Errorf("sessions sanash: %w", err)
	}
	return n, nil
}
