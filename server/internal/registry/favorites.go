package registry

import (
	"encoding/json"
	"fmt"
)

// --- Favorites (root §6/§11) ---

// AddFavorite — takror qo'shish xato emas (INSERT OR IGNORE).
func (r *Registry) AddFavorite(userID int64, cameraID string, position int) error {
	_, err := r.db.Exec(
		`INSERT OR IGNORE INTO favorites (user_id, camera_id, position) VALUES (?,?,?)`,
		userID, cameraID, position)
	return wrap(err, "favorite qo'shish")
}

func (r *Registry) RemoveFavorite(userID int64, cameraID string) error {
	res, err := r.db.Exec("DELETE FROM favorites WHERE user_id=? AND camera_id=?", userID, cameraID)
	if err != nil {
		return wrap(err, "favorite o'chirish")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("favorite o'chirish: %w", ErrNotFound)
	}
	return nil
}

// ListFavoriteCameraIDs — tartib bilan (position).
func (r *Registry) ListFavoriteCameraIDs(userID int64) ([]string, error) {
	rows, err := r.db.Query(
		"SELECT camera_id FROM favorites WHERE user_id=? ORDER BY position, camera_id", userID)
	if err != nil {
		return nil, wrap(err, "favorites list")
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, wrap(err, "favorites scan")
		}
		out = append(out, id)
	}
	return out, wrap(rows.Err(), "favorites rows")
}

// ReorderFavorites — [cameraID...] tartibi; ro'yxatdagi barcha yozuvlar
// shu tartib bilan qayta yoziladi (tx — orasida bo'sh holat ko'rinmaydi).
func (r *Registry) ReorderFavorites(userID int64, orderedIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return wrap(err, "favorites reorder")
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec("DELETE FROM favorites WHERE user_id=?", userID); err != nil {
		return wrap(err, "favorites reorder clear")
	}
	for i, id := range orderedIDs {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO favorites (user_id, camera_id, position) VALUES (?,?,?)`,
			userID, id, i); err != nil {
			return wrap(err, "favorites reorder set")
		}
	}
	return tx.Commit()
}

// --- Layouts (root §11: saqlash/yuklash/default, aralash joy) ---

type Layout struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	Grid      int    `json:"grid"`
	SlotsJSON string `json:"slots_json"`
	IsDefault bool   `json:"is_default"`
	UpdatedAt string `json:"updated_at"`
}

func validGrid(g int) bool { return g >= 1 && g <= 4 }

func (r *Registry) CreateLayout(userID int64, name string, grid int, slotsJSON string) (int64, error) {
	if name == "" || !validGrid(grid) || !json.Valid([]byte(slotsJSON)) {
		return 0, fmt.Errorf("layout yaratish: %w", ErrBadInput)
	}
	res, err := r.db.Exec(
		"INSERT INTO layouts (user_id, name, grid, slots_json) VALUES (?,?,?,?)",
		userID, name, grid, slotsJSON)
	if err != nil {
		return 0, wrap(err, "layout yaratish")
	}
	return res.LastInsertId()
}

func (r *Registry) ListLayouts(userID int64) ([]Layout, error) {
	rows, err := r.db.Query(
		`SELECT id, user_id, name, grid, slots_json, is_default, updated_at
		 FROM layouts WHERE user_id=? ORDER BY name`, userID)
	if err != nil {
		return nil, wrap(err, "layouts list")
	}
	defer rows.Close()
	var out []Layout
	for rows.Next() {
		var l Layout
		if err := rows.Scan(&l.ID, &l.UserID, &l.Name, &l.Grid, &l.SlotsJSON, &l.IsDefault, &l.UpdatedAt); err != nil {
			return nil, wrap(err, "layouts scan")
		}
		out = append(out, l)
	}
	return out, wrap(rows.Err(), "layouts rows")
}

// UpdateLayout — nom/grid/slot qisman; nil → o'zgarmaydi.
func (r *Registry) UpdateLayout(layoutID, userID int64, name *string, grid *int, slotsJSON *string) error {
	if name == nil && grid == nil && slotsJSON == nil {
		return nil
	}
	if grid != nil && !validGrid(*grid) {
		return fmt.Errorf("layout yangilash: %w", ErrBadInput)
	}
	if slotsJSON != nil && !json.Valid([]byte(*slotsJSON)) {
		return fmt.Errorf("layout yangilash: slots json: %w", ErrBadInput)
	}
	if name != nil {
		if _, err := r.db.Exec("UPDATE layouts SET name=?, updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=? AND user_id=?", *name, layoutID, userID); err != nil {
			return wrap(err, "layout name")
		}
	}
	if grid != nil {
		if _, err := r.db.Exec("UPDATE layouts SET grid=?, updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=? AND user_id=?", *grid, layoutID, userID); err != nil {
			return wrap(err, "layout grid")
		}
	}
	if slotsJSON != nil {
		if _, err := r.db.Exec("UPDATE layouts SET slots_json=?, updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=? AND user_id=?", *slotsJSON, layoutID, userID); err != nil {
			return wrap(err, "layout slots")
		}
	}
	return nil
}

func (r *Registry) DeleteLayout(layoutID, userID int64) error {
	res, err := r.db.Exec("DELETE FROM layouts WHERE id=? AND user_id=?", layoutID, userID)
	if err != nil {
		return wrap(err, "layout o'chirish")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("layout o'chirish: %w", ErrNotFound)
	}
	return nil
}

// SetDefaultLayout — user bittadan ko'p default saqlamaydi (tx: oldini tozalash).
func (r *Registry) SetDefaultLayout(layoutID, userID int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return wrap(err, "layout default")
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec("UPDATE layouts SET is_default=0 WHERE user_id=?", userID); err != nil {
		return wrap(err, "layout default clear")
	}
	res, err := tx.Exec("UPDATE layouts SET is_default=1 WHERE id=? AND user_id=?", layoutID, userID)
	if err != nil {
		return wrap(err, "layout default set")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("layout default: %w", ErrNotFound)
	}
	return tx.Commit()
}

// ClearCameraSlots — kamera o'chirilganda barcha layout slotlarini bo'shatish
// (S1 qabul: "favorites/layouts sloti bo'shaydi"). slots_json — massiv;
// mos element null bo'ladi.
func (r *Registry) ClearCameraSlots(cameraID string) error {
	type row struct {
		id    int64
		slots string
	}
	rows, err := r.db.Query("SELECT id, slots_json FROM layouts")
	if err != nil {
		return wrap(err, "slots tozalash o'qish")
	}
	var all []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.id, &x.slots); err != nil {
			rows.Close()
			return wrap(err, "slots scan")
		}
		all = append(all, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return wrap(err, "slots rows")
	}
	for _, x := range all {
		var arr []any
		if err := json.Unmarshal([]byte(x.slots), &arr); err != nil {
			continue // buzilgan json — layout'ni buzmaymiz (UI o'zi tiklaydi)
		}
		changed := false
		for i, v := range arr {
			if s, ok := v.(string); ok && s == cameraID {
				arr[i] = nil
				changed = true
			}
		}
		if changed {
			b, _ := json.Marshal(arr)
			if _, err := r.db.Exec("UPDATE layouts SET slots_json=? WHERE id=?", string(b), x.id); err != nil {
				return wrap(err, "slots yozish")
			}
		}
	}
	return nil
}
