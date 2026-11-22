// Token bekor qilish (revocation) — refresh JTI hisobi va token avlodi.
// 2026-11-21 server auditi: logout/eski refresh cookie/rol o'zgarishi
// server tomonida umuman hisobga olinmas edi.
package registry

import (
	"database/sql"
	"time"
)

// SaveRefresh — yangi refresh token yozuvi (JTI bir martalik).
func (r *Registry) SaveRefresh(jti string, userID int64, expiresAt time.Time) error {
	_, err := r.db.Exec(`INSERT INTO refresh_tokens (jti, user_id, expires_at) VALUES (?,?,?)
		ON CONFLICT(jti) DO UPDATE SET user_id=excluded.user_id, expires_at=excluded.expires_at`,
		jti, userID, expiresAt.UTC().Format(time.RFC3339))
	return wrap(err, "refresh saqlash")
}

// HasRefresh — JTI hali yaroqlimi (rotatsiyada eskisi o'chiriladi).
func (r *Registry) HasRefresh(jti string) (bool, error) {
	var one int
	err := r.db.QueryRow("SELECT 1 FROM refresh_tokens WHERE jti=?", jti).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, wrap(err, "refresh o'qish")
	}
	return true, nil
}

// DeleteRefresh — bitta refreshni o'chirish (rotatsiya/logout).
func (r *Registry) DeleteRefresh(jti string) error {
	_, err := r.db.Exec("DELETE FROM refresh_tokens WHERE jti=?", jti)
	return wrap(err, "refresh o'chirish")
}

// DeleteRefreshForUser — hisob bo'yicha barcha refreshlarni o'chirish
// (parol almashtirish / disable / o'chirish).
func (r *Registry) DeleteRefreshForUser(userID int64) error {
	_, err := r.db.Exec("DELETE FROM refresh_tokens WHERE user_id=?", userID)
	return wrap(err, "refresh o'chirish")
}

// UserTokenEpoch — foydalanuvchi token avlodi (middleware solishtiradi).
func (r *Registry) UserTokenEpoch(userID int64) (int64, error) {
	var e int64
	err := r.db.QueryRow("SELECT token_epoch FROM users WHERE id=?", userID).Scan(&e)
	if err == sql.ErrNoRows {
		return 0, nil // hisob yo'q: token baribir boshqa joyda rad etiladi
	}
	return e, wrap(err, "token avlod o'qish")
}

// BumpUserTokenEpoch — mavjud access tokenlarni darhol yaroqsiz qilish.
func (r *Registry) BumpUserTokenEpoch(userID int64) error {
	_, err := r.db.Exec("UPDATE users SET token_epoch=token_epoch+1 WHERE id=?", userID)
	return wrap(err, "token avlod o'stirish")
}
