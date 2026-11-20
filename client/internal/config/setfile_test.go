package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetValues_KeepsCommentsAndParses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.yaml")
	src := `server_url: ws://192.168.1.10:8081      # eski manzil
ui_bind: 127.0.0.1:8082
log_level: info                         # debug|info|warn|error

limits:
  max_uploads: 8                        # server config_push bilan yangilanadi

publish:
  whip_fallback: rtsp_tcp
`
	if err := os.WriteFile(path, []byte(src), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := SetValues(path, map[string]string{
		"server_url": "ws://192.168.0.27:8081",
		"log_level":  "debug",
	}); err != nil {
		t.Fatalf("SetValues: %v", err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if !strings.Contains(s, "server_url: ws://192.168.0.27:8081") || !strings.Contains(s, "# eski manzil") {
		t.Fatalf("izoh saqlanmadi yoki qiymat o'zgarmadi:\n%s", s)
	}
	if !strings.Contains(s, "log_level: debug") || !strings.Contains(s, "# debug|info|warn|error") {
		t.Fatalf("log_level/izoh:\n%s", s)
	}
	// Bloklar buzilmaganligi + o'qib bo'lishi (eng muhim tekshiruv).
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("qayta o'qib bo'lmadi: %v\n%s", err, s)
	}
	if cfg.ServerURL != "ws://192.168.0.27:8081" || cfg.LogLevel != "debug" {
		t.Fatalf("Load natija: %+v", cfg)
	}
	if cfg.Limits.MaxUploads != 8 {
		t.Fatalf("limits bloki buzildi: %+v", cfg.Limits)
	}
}

func TestSetValues_AppendsMissingKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("server_url: ws://a:1\n\nlimits:\n  max_uploads: 8\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := SetValues(path, map[string]string{"log_level": "warn"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("qayta o'qib bo'lmadi: %v", err)
	}
	if cfg.LogLevel != "warn" {
		t.Fatalf("qo'shilgan kalit o'qilmadi: %+v", cfg)
	}
	if cfg.Limits.MaxUploads != 8 {
		t.Fatalf("limits buzildi: %+v", cfg.Limits)
	}
}

func TestSetValues_RejectsBadKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("server_url: ws://a:1\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := SetValues(path, map[string]string{"Server URL": "x"}); err == nil {
		t.Fatal("yaroqsiz kalit qabul qilindi")
	}
}
