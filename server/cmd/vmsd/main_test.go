package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"central-vms/server/internal/config"
	"central-vms/server/internal/store"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// listenFirst — server[0] ni (API) ephemeral portga bog'laydi (testlar uchun).
func listenFirst(servers []*http.Server) (net.Listener, error) {
	return net.Listen("tcp", servers[0].Addr)
}

func testCfg(t *testing.T) config.Config {
	t.Helper()
	c := config.Default()
	c.Bind = "127.0.0.1:0"
	c.MediaMTX.AuthHookBind = "127.0.0.1:0"
	c.DBPath = filepath.Join(t.TempDir(), "vmsd.db")
	c.LogFile = ""
	c.Auth.JWTSecret = "0123456789abcdef0123456789abcdef"
	return c
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHealthz(t *testing.T) {
	cfg := testCfg(t)
	servers := newHTTPServer(cfg, testLogger(), nil)
	ln, err := listenFirst(servers)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = servers[0].Serve(ln) }()
	defer servers[0].Close()

	resp, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["ok"] != true || body["protocol_label"] != "1.0-draft.5" {
		t.Fatalf("healthz mazmuni: %v", body)
	}
}

func TestStaticHandler_Missing(t *testing.T) {
	h := staticHandler(filepath.Join(t.TempDir(), "yoq"), testLogger())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, 503 kutilardi", rec.Code)
	}
}

func TestServeUntilSignal_Graceful(t *testing.T) {
	cfg := testCfg(t)
	servers := newHTTPServer(cfg, testLogger(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveUntilSignal(ctx, testLogger(), cfg, servers) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveUntilSignal: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("graceful shutdown tugamadi")
	}
}

func TestProbeRuns(t *testing.T) {
	if _, err := os.Stat("/usr/bin/ffmpeg"); err != nil {
		t.Skip("ffmpeg yo'q — probe testi o'tkazib yuborildi")
	}
	if err := runProbe(testLogger(), testCfg(t), nil, true); err != nil {
		t.Fatalf("runProbe: %v", err)
	}
}

// S4.4: -backup — bazaning issiq nusxasi olinadi va butunligi tekshiriladi.
func TestRunBackup(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DBPath = filepath.Join(dir, "vms.db")
	cfg.Migrations = filepath.Join("..", "..", "migrations")
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MigrateUp(db, cfg.Migrations); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('k','v')`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	out := filepath.Join(dir, "backup.db")
	if err := runBackup(cfg, out); err != nil {
		t.Fatalf("backup: %v", err)
	}
	cp, err := store.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer cp.Close()
	var v string
	if err := cp.QueryRow(`SELECT value FROM settings WHERE key='k'`).Scan(&v); err != nil || v != "v" {
		t.Fatalf("nusxa mazmuni yo'q: %v", err)
	}
	if err := runBackup(cfg, ""); err == nil {
		t.Error("bo'sh chiqish yo'li qabul qilindi")
	}
}
