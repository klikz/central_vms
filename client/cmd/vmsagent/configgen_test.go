// configgen_test.go — birinchi ishga tushirishda agent config'i
// avtomatik yaratilishi (2026-11-23): mijozning o'z YAML o'quvchisi
// (yamlsub) shablonni qabul qiladi, Validate o'tadi, server_url bo'sh,
// yo'llar binar papkasidan; mavjud faylga tegilmaydi.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"central-vms/client/internal/config"
)

func TestEnsureAgentConfig_CreatesValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "etc", "client.yaml")

	created, err := ensureAgentConfig(path)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		t.Errorf("izinlar %o — 0600 atrofida bo'lishi kerak", st.Mode().Perm())
	}

	cfg, err := config.Load(path) // Load = Parse + Validate
	if err != nil {
		t.Fatalf("yaratilgan config yamlsub/Validate'dan o'tmadi: %v", err)
	}
	if cfg.ServerURL != "" {
		t.Errorf("server_url bo'sh bo'lishi kerak edi: %q", cfg.ServerURL)
	}
	if cfg.UIBind != "127.0.0.1:8082" {
		t.Errorf("ui_bind: %q", cfg.UIBind)
	}
	if cfg.Limits.MaxUploads != 4 {
		t.Errorf("max_uploads: %d (prod tavsiyasi 4)", cfg.Limits.MaxUploads)
	}
	base := filepath.ToSlash(exeBaseDir())
	for _, p := range []string{cfg.StateDir, cfg.KeyFile, cfg.LogFile} {
		if !strings.HasPrefix(p, base) {
			t.Errorf("yo'l binar papkasidan emas: %s (base=%s)", p, base)
		}
	}

	// Mavjud fayl — tegilmaydi.
	before, _ := os.ReadFile(path)
	created2, err := ensureAgentConfig(path)
	if created2 || err != nil {
		t.Fatalf("mavjud faylga tegildi: created=%v err=%v", created2, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("mavjud config mazmuni o'zgardi")
	}
}
