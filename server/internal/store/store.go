// Package store — SQLite (modernc.org/sqlite, CGO yo'q) + migratsiya runner.
// Sxema: root REJA §5; migratsiyalar `migrations/` papkasida (build'ga ko'chiriladi).
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open — DB'ni ochadi va majburiy PRAGMA'larni qo'yadi (WAL — root §17
// "kamera o'sishi ~1000/yil" bandi; foreign_keys — cascade o'chirishlar
// favorites/layouts slotlari uchun kerak, S1).
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("store: katalog %s: %w", dir, err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: ochib bo'lmadi %s: %w", path, err)
	}
	// SQLite'da yozuvlar bir yozuvchi (S1'da single-writer goroutine),
	// shuning uchun max_open_conns = 1 — "database is locked" tug'ilmasin.
	db.SetMaxOpenConns(1)
	for _, p := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: %s: %w", p, err)
		}
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return db, nil
}
