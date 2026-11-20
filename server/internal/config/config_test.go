package config

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestLoad_Defaults(t *testing.T) {
	// Minimal fayl: jwt_secret majburiy (32 belgi).
	p := writeFile(t, "auth:\n  jwt_secret: \"0123456789abcdef0123456789abcdef\"\nlog_file: \"\"\n")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	d := Default()
	if c.Bind != d.Bind || c.Limits.UserMaxSessions != 16 || c.Limits.GlobalMaxSessions != 48 {
		t.Fatalf("defaultlar qo'llanmadi: bind=%q limits=%+v", c.Bind, c.Limits)
	}
	if c.Media.PublishTTLS != 60 || c.Media.ReadTokenTTLS != 30 {
		t.Fatalf("media TTL defaultlari: %+v", c.Media)
	}
	if c.LogFile != "" {
		t.Fatalf("log_file bo'sh qolishi kerak edi, %q", c.LogFile)
	}
	if c.Path != p {
		t.Fatalf("Path=%q", c.Path)
	}
}

func TestLoad_Overrides(t *testing.T) {
	p := writeFile(t, `
bind: ":9091"
limits:
  user_max_sessions: 4
  global_max_sessions: 16
media:
  server_max_transcodes: 8
auth:
  jwt_secret: "0123456789abcdef0123456789abcdef"
log_file: ""
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Bind != ":9091" || c.Limits.UserMaxSessions != 4 || c.Media.MaxTranscodes != 8 {
		t.Fatalf("override'lar o'qilmadi: %+v", c)
	}
	if c.Limits.PreviewMax != 8 {
		t.Fatalf("berilmagan maydon defaultdan o'zgarishi kerak emas: %d", c.Limits.PreviewMax)
	}
}

func TestLoad_UnknownField(t *testing.T) {
	p := writeFile(t, "bindd: \":8081\"\nauth:\n  jwt_secret: \"0123456789abcdef0123456789abcdef\"\nlog_file: \"\"\n")
	if _, err := Load(p); err == nil {
		t.Fatal("noma'lum maydon uchun xato kutilardi")
	}
}

func TestValidate_Secrets(t *testing.T) {
	p := writeFile(t, "log_file: \"\"\n") // jwt_secret yo'q
	if _, err := Load(p); err == nil {
		t.Fatal("jwt_secret yo'q — xato kutilardi")
	}
	t.Setenv("VMSD_JWT_SECRET", "env-secret-0123456789abcdef01234567")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("env orqali o'qilishi kerak edi: %v", err)
	}
	if len(c.Auth.JWTSecret) < 32 {
		t.Fatalf("env secret qisqa: %q", c.Auth.JWTSecret)
	}
}

func TestValidate_AuthHookBind(t *testing.T) {
	p := writeFile(t, `
mediamtx:
  auth_hook_bind: "0.0.0.0:8189"
auth:
  jwt_secret: "0123456789abcdef0123456789abcdef"
log_file: ""
`)
	if _, err := Load(p); err == nil {
		t.Fatal("auth_hook_bind 0.0.0.0 — xato kutilardi (faqat localhost)")
	}
}

func TestValidate_LimitsConsistency(t *testing.T) {
	p := writeFile(t, `
limits:
  user_max_sessions: 20
  global_max_sessions: 16
auth:
  jwt_secret: "0123456789abcdef0123456789abcdef"
log_file: ""
`)
	if _, err := Load(p); err == nil {
		t.Fatal("global < user — xato kutilardi")
	}
}
