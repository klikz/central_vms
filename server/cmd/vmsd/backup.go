package main

import (
	"fmt"

	"central-vms/server/internal/config"
	"central-vms/server/internal/store"
)

// runBackup — S4.4: bazaning ISSIQ nusxasi (`VACUUM INTO`) — server
// ishlab turganda ham xavfsiz, WAL bilan izchil. `deploy/backup.sh`
// shu rejimdan foydalanadi (sqlite3 CLI talab qilinmaydi).
//
//	vmsd -config config/prod.yaml -backup /var/backups/vmsd/vmsd-....db
func runBackup(cfg config.Config, out string) error {
	if out == "" {
		return fmt.Errorf("backup: chiqish fayli ko'rsatilmadi")
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.Exec(`VACUUM INTO ?`, out); err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}

	// Nusxa butunligi tekshiriladi: buzilgan fayl qabul qilinmaydi.
	cp, err := store.Open(out)
	if err != nil {
		return fmt.Errorf("nusxani ochish: %w", err)
	}
	defer cp.Close()
	var res string
	if err := cp.QueryRow(`PRAGMA integrity_check;`).Scan(&res); err != nil {
		return fmt.Errorf("integrity_check: %w", err)
	}
	if res != "ok" {
		return fmt.Errorf("nusxa buzilgan: %s", res)
	}
	return nil
}
