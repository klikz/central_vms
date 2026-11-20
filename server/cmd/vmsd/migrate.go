package main

import (
	"fmt"
	"log/slog"

	"central-vms/server/internal/config"
	"central-vms/server/internal/store"
)

// runMigrate — `make migrate-up|down|status` uchun amallar (root §4.2).
// Har amal alohida jarayonda bajariladi: vmsd ishlamayotgan paytda ham
// ishlatilishi mumkin (prod operatsiyalar).
func runMigrate(lg *slog.Logger, cfg config.Config, action string) error {
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	switch action {
	case "up":
		done, err := store.MigrateUp(db, cfg.Migrations)
		if err != nil {
			return err
		}
		if len(done) == 0 {
			fmt.Println("migrate up: yangi migratsiya yo'q")
			return nil
		}
		fmt.Printf("migrate up: qo'llanildi %v\n", done)
		return nil
	case "down":
		v, err := store.MigrateDown(db, cfg.Migrations)
		if err != nil {
			return err
		}
		if v == 0 {
			fmt.Println("migrate down: qo'llanilgan migratsiya yo'q")
			return nil
		}
		fmt.Printf("migrate down: %04d qaytarildi\n", v)
		return nil
	case "status":
		applied, available, err := store.Status(db, cfg.Migrations)
		if err != nil {
			return err
		}
		fmt.Printf("qo'llanilgan: %v\nmavjud:     %v\n", applied, available)
		return nil
	default:
		return fmt.Errorf("migrate: noma'lum amal %q (up|down|status)", action)
	}
}
