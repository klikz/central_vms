// Package audit — audit_log yozuvlari (root §13; S1: login, enroll, play,
// admin amallari). Yozuv xatosi asosiy amalni bekor QILMAYDI — chaqiruvchi
// logga ogohlantirish yozadi (audit yo'qolishi xavfsizlik hodisasi emas).
package audit

import (
	"database/sql"
	"fmt"
)

// Action kodlari (docs/api.md bilan kelishilgan; UI filtrida ishlatiladi).
const (
	Login        = "login"
	LoginFailed  = "login_failed"
	Logout       = "logout"
	EnrollCreate = "enroll_create"
	EnrollUse    = "enroll_use"
	Play         = "play"
	Preview      = "preview"
	SessionStop  = "session_stop"
	AdminAction  = "admin_action"
	UserCreate   = "user_create"
	UserUpdate   = "user_update"
)

// Log — audit yozuvchisi (bitta DB, SQLite'da MaxOpenConns=1).
type Log struct {
	db *sql.DB
}

func New(db *sql.DB) *Log { return &Log{db: db} }

// Add — bitta yozuv. userID=0 → NULL (anonim: enroll, uzilgan sessiyalar).
func (l *Log) Add(userID int64, action, target string) error {
	var uid any
	if userID != 0 {
		uid = userID
	}
	_, err := l.db.Exec(
		"INSERT INTO audit_log (user_id, action, target) VALUES (?, ?, ?)", uid, action, target)
	if err != nil {
		return fmt.Errorf("audit: %s %q: %w", action, target, err)
	}
	return nil
}

// List — oxirgi N yozuv (admin panel, S4 to'liq filtr bilan).
func (l *Log) List(limit int) ([]Entry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := l.db.Query(
		`SELECT id, COALESCE(user_id,0), action, target, at
		 FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("audit: list: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.UserID, &e.Action, &e.Target, &e.At); err != nil {
			return nil, fmt.Errorf("audit: scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Entry — bitta audit yozuvi.
type Entry struct {
	ID     int64  `json:"id"`
	UserID int64  `json:"user_id"`
	Action string `json:"action"`
	Target string `json:"target"`
	At     string `json:"at"`
}
