// Admin PATCH'larini bitta UPDATE'ga yig'ish (atomik yozuv + not-found
// aniqligi). 2026-11-21 server auditi: avval har maydon alohida
// UPDATE bilan yozilardi — bittasi xato bo'lsa qolganlari commit
// bo'lib qolardi (qisman yozuv) va `RowsAffected` tekshirilmagani
// uchun mavjud bo'lmagan id'ga 200 OK + yolg'on audit yozuvi chiqardi.
package registry

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// dynUpdate — `sets` ichidagi ustunlarni BITTA UPDATE'da yozadi.
// `order` — aniqlik uchun ustunlar tartibi (map tartibi tasodifiy).
// Hech bir maydon so'ralmasa: qator mavjudligi tekshiriladi.
func dynUpdate(db *sql.DB, table string, sets map[string]any, order []string,
	where string, whereArgs []any, ctx string) error {
	var parts []string
	var args []any
	for _, col := range order {
		if v, ok := sets[col]; ok {
			parts = append(parts, col+"=?")
			args = append(args, v)
		}
	}
	if len(parts) == 0 {
		var one int
		err := db.QueryRow("SELECT 1 FROM "+table+" WHERE "+where, whereArgs...).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%s: %w", ctx, ErrNotFound)
		}
		return wrap(err, ctx)
	}
	args = append(args, whereArgs...)
	q := "UPDATE " + table + " SET " + strings.Join(parts, ", ") + " WHERE " + where
	res, err := db.Exec(q, args...)
	if err != nil {
		return wrap(err, ctx)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%s: %w", ctx, ErrNotFound)
	}
	return nil
}
