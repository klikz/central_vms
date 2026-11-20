package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"central-vms/server/internal/auth"
	"central-vms/server/internal/registry"
)

// runSeedAdmin — `make admin` / `vmsd -seed-admin user:parol`.
// Mavjud user bo'lsa parolini yangilaydi, bo'lmasa admin yaratadi.
func runSeedAdmin(lg *slog.Logger, db *sql.DB, spec string) error {
	user, pass, ok := strings.Cut(spec, ":")
	if !ok || user == "" || len(pass) < 8 {
		return fmt.Errorf("seed-admin: format username:parol (parol kamida 8 belgi)")
	}
	hash, err := auth.HashPassword(pass)
	if err != nil {
		return err
	}
	reg := registry.New(db)
	if _, _, err := reg.GetUserByName(user); err == nil {
		u, _, _ := reg.GetUserByName(user)
		if err := reg.UpdateUser(u.ID, nil, nil, nil, &hash); err != nil {
			return err
		}
		lg.Info("admin paroli yangilandi", "user", user)
		return nil
	}
	id, err := reg.CreateUser(user, hash, "admin", "")
	if err != nil {
		return err
	}
	lg.Info("admin yaratildi", "user", user, "id", id)
	return nil
}
