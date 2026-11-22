package registry

import (
	"database/sql"
	"fmt"
)

// User — users jadvali ko'rinuvchi qismi (pass_hash tashqariga chiqmaydi).
type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"` // admin | viewer
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
	LastLoginAt string `json:"last_login_at,omitempty"`
	Disabled    bool   `json:"disabled"`
}

// validRole — rol sxemasi (root §9).
func validRole(role string) bool { return role == "admin" || role == "viewer" }

// CreateUser — pass_hash auth.HashPassword natijasi (bu paket argon2 bilmaydi).
func (r *Registry) CreateUser(username, passHash, role, displayName string) (int64, error) {
	if username == "" || passHash == "" || !validRole(role) {
		return 0, fmt.Errorf("user yaratish: %w", ErrBadInput)
	}
	res, err := r.db.Exec(
		`INSERT INTO users (username, pass_hash, role, display_name) VALUES (?,?,?,?)`,
		username, passHash, role, displayName)
	if err != nil {
		return 0, wrap(err, "user yaratish")
	}
	id, err := res.LastInsertId()
	return id, wrap(err, "user id")
}

// GetUserByName — login uchun (pass_hash bilan qaytadi — chaqiruvchi tekshiradi).
func (r *Registry) GetUserByName(username string) (User, string, error) {
	var u User
	var hash string
	err := r.db.QueryRow(
		`SELECT id, username, role, display_name, created_at, COALESCE(last_login_at,''), disabled, pass_hash
		 FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.Role, &u.DisplayName, &u.CreatedAt, &u.LastLoginAt, &u.Disabled, &hash)
	if err != nil {
		return User{}, "", wrap(err, "user o'qish")
	}
	return u, hash, nil
}

// GetUserByID — JWT claims → user (middleware/handlerlar uchun).
func (r *Registry) GetUserByID(id int64) (User, error) {
	var u User
	err := r.db.QueryRow(
		`SELECT id, username, role, display_name, created_at, COALESCE(last_login_at,''), disabled
		 FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &u.Role, &u.DisplayName, &u.CreatedAt, &u.LastLoginAt, &u.Disabled)
	if err != nil {
		return User{}, wrap(err, "user o'qish")
	}
	return u, nil
}

// ListUsers — admin ro'yxati.
func (r *Registry) ListUsers() ([]User, error) {
	rows, err := r.db.Query(
		`SELECT id, username, role, display_name, created_at, COALESCE(last_login_at,''), disabled
		 FROM users ORDER BY id`)
	if err != nil {
		return nil, wrap(err, "users list")
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.DisplayName, &u.CreatedAt, &u.LastLoginAt, &u.Disabled); err != nil {
			return nil, wrap(err, "users scan")
		}
		out = append(out, u)
	}
	return out, wrap(rows.Err(), "users rows")
}

// UpdateUser — qisman yangilash: nil maydon o'zgarmaydi. PassHash bo'sh bo'lmasa
// almashtiriladi (hash chaqiruvchida tayyorlanadi).
// DeleteUser — foydalanuvchini o'chirish (api.md: DELETE
// /api/admin/users/{id}). Oxirgi admin o'chirilmaydi (aks holda
// tizim boshqaruvsiz qoladi).
func (r *Registry) DeleteUser(id int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return wrap(err, "user o'chirish")
	}
	defer func() { _ = tx.Rollback() }()
	var role string
	var disabled int
	if err := tx.QueryRow("SELECT role, disabled FROM users WHERE id=?", id).Scan(&role, &disabled); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("user o'chirish: %w", ErrNotFound)
		}
		return wrap(err, "user o'qish")
	}
	if role == "admin" && disabled == 0 {
		var n int
		if err := tx.QueryRow("SELECT count(*) FROM users WHERE role='admin' AND disabled=0").Scan(&n); err != nil {
			return wrap(err, "admin sanash")
		}
		if n <= 1 {
			return fmt.Errorf("user o'chirish: oxirgi faol admin: %w", ErrExists)
		}
	}
	if _, err := tx.Exec("DELETE FROM refresh_tokens WHERE user_id=?", id); err != nil {
		return wrap(err, "refresh tozalash")
	}
	res, err := tx.Exec("DELETE FROM users WHERE id=?", id)
	if err != nil {
		return wrap(err, "user o'chirish")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user o'chirish: %w", ErrNotFound)
	}
	return tx.Commit()
}

func (r *Registry) UpdateUser(id int64, role *string, displayName *string, disabled *bool, passHash *string) error {
	if role != nil && !validRole(*role) {
		return fmt.Errorf("user yangilash: %w", ErrBadInput)
	}
	sets := map[string]any{}
	if role != nil {
		sets["role"] = *role
	}
	if displayName != nil {
		sets["display_name"] = *displayName
	}
	if disabled != nil {
		v := 0
		if *disabled {
			v = 1
		}
		sets["disabled"] = v
	}
	if passHash != nil && *passHash != "" {
		sets["pass_hash"] = *passHash
	}
	return dynUpdate(r.db, "users", sets,
		[]string{"role", "display_name", "disabled", "pass_hash"},
		"id=?", []any{id}, "user yangilash")
}

// TouchLogin — last_login_at (UTC) yangilash.
func (r *Registry) TouchLogin(id int64) error {
	_, err := r.db.Exec(
		"UPDATE users SET last_login_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=?", id)
	return wrap(err, "login touch")
}
