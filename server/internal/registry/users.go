package registry

import (
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
func (r *Registry) UpdateUser(id int64, role *string, displayName *string, disabled *bool, passHash *string) error {
	if role != nil && !validRole(*role) {
		return fmt.Errorf("user yangilash: %w", ErrBadInput)
	}
	if role != nil {
		if _, err := r.db.Exec("UPDATE users SET role=? WHERE id=?", *role, id); err != nil {
			return wrap(err, "user role")
		}
	}
	if displayName != nil {
		if _, err := r.db.Exec("UPDATE users SET display_name=? WHERE id=?", *displayName, id); err != nil {
			return wrap(err, "user name")
		}
	}
	if disabled != nil {
		v := 0
		if *disabled {
			v = 1
		}
		if _, err := r.db.Exec("UPDATE users SET disabled=? WHERE id=?", v, id); err != nil {
			return wrap(err, "user disabled")
		}
	}
	if passHash != nil && *passHash != "" {
		if _, err := r.db.Exec("UPDATE users SET pass_hash=? WHERE id=?", *passHash, id); err != nil {
			return wrap(err, "user pass")
		}
	}
	return nil
}

// TouchLogin — last_login_at (UTC) yangilash.
func (r *Registry) TouchLogin(id int64) error {
	_, err := r.db.Exec(
		"UPDATE users SET last_login_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=?", id)
	return wrap(err, "login touch")
}
