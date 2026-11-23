// configgen_test.go — birinchi ishga tushirishda config avtomatik
// yaratilishi (2026-11-23 talab): yaratilgan fayl Load+Validate'dan
// o'tadi, jwt_secret to'liq va tasodifiy, yo'llar absolut (binar
// papkasidan), mavjud faylga tegilmaydi.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"central-vms/server/internal/config"
)

func TestEnsureServerConfig_CreatesValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.yaml")

	created, err := ensureServerConfig(path)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		t.Errorf("fayl izinlari %o — 0600 atrofida bo'lishi kerak", st.Mode().Perm())
	}

	cfg, err := config.Load(path) // Load Validate'ni ham o'tkazadi
	if err != nil {
		t.Fatalf("yaratilgan config yaroqsiz: %v", err)
	}
	if len(cfg.Auth.JWTSecret) < 32 {
		t.Fatalf("jwt_secret qisqa: %d", len(cfg.Auth.JWTSecret))
	}
	if !filepath.IsAbs(cfg.DBPath) && !strings.Contains(cfg.DBPath, "/") {
		t.Fatalf("db_path absolut emas: %s", cfg.DBPath)
	}
	if !strings.HasPrefix(cfg.DBPath, filepath.ToSlash(exeBaseDir())) &&
		!strings.Contains(cfg.DBPath, dir) {
		// exeBaseDir — build papkasi; temp ichida yaratilganda ham
		// yo'l binar papkasiga ishora qiladi (ikkalasidan biri).
		t.Fatalf("db_path kutilmagan joyda: %s", cfg.DBPath)
	}

	// Ikkinchi chaqiruv — TEGILMAYDI.
	before, _ := os.ReadFile(path)
	created2, err := ensureServerConfig(path)
	if created2 || err != nil {
		t.Fatalf("mavjud faylga tegildi: created=%v err=%v", created2, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("mavjud config mazmuni o'zgardi")
	}

	// Ikki yaratilgan faylda secret'lar FARQ qilishi kerak (tasodifiy).
	p2 := filepath.Join(dir, "c2.yaml")
	if _, err := ensureServerConfig(p2); err != nil {
		t.Fatal(err)
	}
	c2, err := config.Load(p2)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Auth.JWTSecret == cfg.Auth.JWTSecret {
		t.Fatal("jwt_secret takrorlandi — generatsiya tasodifiy emas")
	}
}
