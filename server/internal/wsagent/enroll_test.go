package wsagent

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"central-vms/server/internal/registry"
	"central-vms/server/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

type fakeClock struct{ t time.Time }

func (f *fakeClock) Now() time.Time      { return f.t }
func (f *fakeClock) Add(d time.Duration) { f.t = f.t.Add(d) }

func testEnroll(t *testing.T) (*Enrollment, *registry.Registry, *fakeClock) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
	if _, err := store.MigrateUp(db, dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	reg := registry.New(db)
	fc := &fakeClock{t: time.Date(2026, 11, 20, 5, 0, 0, 0, time.UTC)}
	return NewEnrollment(db, reg, 24*time.Hour, 5, 5*time.Minute, fc), reg, fc
}

func TestEnrollFlow(t *testing.T) {
	e, reg, _ := testEnroll(t)
	code, exp, err := e.CreateCode(0, 0)
	if err != nil {
		t.Fatalf("CreateCode: %v", err)
	}
	if !exp.Equal(time.Date(2026, 11, 21, 5, 0, 0, 0, time.UTC)) {
		t.Fatalf("ttl: %s", exp)
	}
	if len(code) != 19 || strings.Count(code, "-") != 3 {
		t.Fatalf("kod formati: %q", code)
	}
	agentID, secret, clientID, err := e.Redeem(code, "joy1", "ubuntu", "10.0.5.9", "0.1.0", "10.0.0.5")
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if !strings.HasPrefix(agentID, "ag_") || len(secret) < 40 {
		t.Fatalf("agentID=%s secret len=%d", agentID, len(secret))
	}
	c, err := reg.GetClientByAgentID(agentID)
	if err != nil || c.ID != clientID {
		t.Fatalf("client topilmadi: %v", err)
	}
	// DB'da xom secret YO'Q (sha256 saqlanadi).
	var stored string
	if err := reg.DB().QueryRow("SELECT agent_secret_hash FROM clients WHERE id=?", clientID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == secret || strings.Contains(stored, secret) {
		t.Fatal("agent_secret xom holda saqlangan!")
	}
	if stored != HashSecret(secret) {
		t.Fatal("secret hash noto'g'ri")
	}
	// Bir martalik: ikkinchi urinish — 409.
	if _, _, _, err := e.Redeem(code, "joy2", "ubuntu", "", "", "10.0.0.6"); !strings.Contains(err.Error(), "allaqachon") {
		t.Fatalf("takror redeem: %v", err)
	}
}

func TestEnrollBadCodeAndExpiry(t *testing.T) {
	e, _, fc := testEnroll(t)
	if _, _, _, err := e.Redeem("YOQ-KOD-XYYY", "", "", "", "", "1.1.1.1"); !strings.Contains(err.Error(), "kod noto'g'ri") {
		t.Fatalf("yo'q kod: %v", err)
	}
	code, _, _ := e.CreateCode(0, 0)
	fc.Add(24*time.Hour + time.Minute)
	if _, _, _, err := e.Redeem(code, "", "", "", "", "1.1.1.1"); !strings.Contains(err.Error(), "kod noto'g'ri") {
		t.Fatalf("muddati o'tgan kod: %v", err)
	}
}

func TestEnrollRateLimit(t *testing.T) {
	e, _, _ := testEnroll(t)
	_, _, _ = e.CreateCode(0, 0)
	var lastErr error
	for i := range 7 {
		_, _, _, lastErr = e.Redeem("YOMOQ-XYYY-ZZZZ-QQQQ", "", "", "", "", "9.9.9.9")
		_ = i
	}
	if lastErr == nil || !strings.Contains(lastErr.Error(), "juda ko'p") {
		t.Fatalf("rate-limit ishlamadi: %v", lastErr)
	}
	// Boshqa IP ga ta'sir yo'q (kod noto'g'ri bo'lsa ham rate emas).
	if _, _, _, err := e.Redeem("", "", "", "", "", "9.9.9.8"); strings.Contains(err.Error(), "juda ko'p") {
		t.Fatalf("boshqa IP rate-limitga uchradi: %v", err)
	}
}

func TestHashSecretStable(t *testing.T) {
	h := HashSecret("abc")
	if len(h) != 64 || HashSecret("abc") != h || HashSecret("abd") == h {
		t.Fatalf("HashSecret: %s", h)
	}
}
