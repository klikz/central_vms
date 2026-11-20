package wsagent

import (
	"errors"
	"path/filepath"
	"regexp"
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

var codeRe = regexp.MustCompile(`^[0-9]{8}$`)

func TestEnrollFlow(t *testing.T) {
	e, reg, _ := testEnroll(t)
	code, exp, err := e.CreateCode(0, 0)
	if err != nil {
		t.Fatalf("CreateCode: %v", err)
	}
	if !exp.Equal(time.Date(2026, 11, 21, 5, 0, 0, 0, time.UTC)) {
		t.Fatalf("ttl: %s", exp)
	}
	if !codeRe.MatchString(code) {
		t.Fatalf("kod 8 xonali son bo'lishi kerak: %q", code)
	}
	agentID, secret, clientID, err := e.Redeem(EnrollInput{
		Code: code, Hostname: "joy1", OS: "ubuntu", LanIP: "10.0.5.9",
		AgentVersion: "0.1.0", Name: "Oxangaron",
	}, "10.0.0.5")
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	// Operator nomi (§3.1 `name`): client shu nom bilan ko'rinadi.
	c1, err := reg.GetClientByAgentID(agentID)
	if err != nil {
		t.Fatalf("mijoz topilmadi: %v", err)
	}
	if c1.Name != "Oxangaron" {
		t.Fatalf("nom saqlanmadi: %q", c1.Name)
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
	if _, _, _, err := e.Redeem(EnrollInput{Code: code, Hostname: "joy2", OS: "ubuntu"}, "10.0.0.6"); !strings.Contains(err.Error(), "allaqachon") {
		t.Fatalf("takror redeem: %v", err)
	}
}

func TestEnrollBadCodeAndExpiry(t *testing.T) {
	e, _, fc := testEnroll(t)
	if _, _, _, err := e.Redeem(EnrollInput{Code: "YOQ-KOD-XYYY"}, "1.1.1.1"); !strings.Contains(err.Error(), "kod noto'g'ri") {
		t.Fatalf("yo'q kod: %v", err)
	}
	code, _, _ := e.CreateCode(0, 0)
	fc.Add(24*time.Hour + time.Minute)
	if _, _, _, err := e.Redeem(EnrollInput{Code: code}, "1.1.1.1"); !strings.Contains(err.Error(), "kod noto'g'ri") {
		t.Fatalf("muddati o'tgan kod: %v", err)
	}
}

func TestEnrollRateLimit(t *testing.T) {
	e, _, _ := testEnroll(t)
	_, _, _ = e.CreateCode(0, 0)
	var lastErr error
	for i := range 7 {
		_, _, _, lastErr = e.Redeem(EnrollInput{Code: "YOMOQ-XYYY-ZZZZ-QQQQ"}, "9.9.9.9")
		_ = i
	}
	if lastErr == nil || !strings.Contains(lastErr.Error(), "juda ko'p") {
		t.Fatalf("rate-limit ishlamadi: %v", lastErr)
	}
	// Boshqa IP ga ta'sir yo'q (kod noto'g'ri bo'lsa ham rate emas).
	if _, _, _, err := e.Redeem(EnrollInput{}, "9.9.9.8"); strings.Contains(err.Error(), "juda ko'p") {
		t.Fatalf("boshqa IP rate-limitga uchradi: %v", err)
	}
}

func TestHashSecretStable(t *testing.T) {
	h := HashSecret("abc")
	if len(h) != 64 || HashSecret("abc") != h || HashSecret("abd") == h {
		t.Fatalf("HashSecret: %s", h)
	}
}

// Operator talabi: kod — 8 xonali son, kiritish oson bo'lishi uchun
// bo'sh joy/tire bilan yozilgani ham qabul qilinadi.
func TestEnrollCodeIsNumericAndTolerant(t *testing.T) {
	e, _, _ := testEnroll(t)
	code, _, err := e.CreateCode(0, 0)
	if err != nil {
		t.Fatalf("CreateCode: %v", err)
	}
	if !codeRe.MatchString(code) {
		t.Fatalf("kutilgan 8 xonali son, chiqdi: %q", code)
	}
	// Guruhlab yozilgan variant (telefon orqali aytishga qulay).
	spaced := code[:4] + " " + code[4:]
	if _, _, _, err := e.Redeem(EnrollInput{Code: spaced, Hostname: "joy2", OS: "ubuntu", LanIP: "10.0.5.10", AgentVersion: "0.1.0"}, "10.0.0.6"); err != nil {
		t.Fatalf("bo'sh joyli kod qabul qilinmadi: %v", err)
	}
	// Kod bir marta ishlatiladi — ikkinchi urinish rad etilishi kerak.
	if _, _, _, err := e.Redeem(EnrollInput{Code: code, Hostname: "joy3", OS: "ubuntu", LanIP: "10.0.5.11", AgentVersion: "0.1.0"}, "10.0.0.7"); !errors.Is(err, ErrAlreadyUsed) {
		t.Fatalf("takroriy redeem: %v", err)
	}
}

// Kodlar tasodifiy va chalkashmasligi kerak (bir xil kod ikki marta
// yaratilmasin — 10^8 fazada bir necha yuzta kod uchun ehtimol juda kichik,
// lekin format tekshiruvi shu yerda ham qoladi).
func TestEnrollCodesUnique(t *testing.T) {
	e, _, _ := testEnroll(t)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c, _, err := e.CreateCode(0, 0)
		if err != nil {
			t.Fatalf("CreateCode: %v", err)
		}
		if !codeRe.MatchString(c) {
			t.Fatalf("format: %q", c)
		}
		if seen[c] {
			t.Fatalf("takroriy kod: %q", c)
		}
		seen[c] = true
	}
}

// Ikki mijoz bir xil nom bilan ro'yxatdan o'tolmaydi (clients.name UNIQUE),
// lekin xato "kod allaqachon ishlatilgan" emas, aniq "nom band" bo'lishi
// kerak — aks holda operator kodni ayb lab, chalkashadi (2026-11-20).
func TestEnrollNameTaken(t *testing.T) {
	e, reg, _ := testEnroll(t)
	c1, _, _ := e.CreateCode(0, 0)
	if _, _, _, err := e.Redeem(EnrollInput{Code: c1, Name: "Oxangaron"}, "10.0.0.5"); err != nil {
		t.Fatalf("birinchi enroll: %v", err)
	}
	c2, _, _ := e.CreateCode(0, 0)
	_, _, _, err := e.Redeem(EnrollInput{Code: c2, Name: "Oxangaron"}, "10.0.0.6")
	if !errors.Is(err, ErrNameTaken) {
		t.Fatalf("nom band xatosi kutilmadi: %v", err)
	}
	// Kod isrof bo'lmaganmi? Yo'q — kod bir marta ishlatiladi, shuning
	// uchun operator yangi kod olishi kerak; bu yerda faqat xato turi muhim.
	_ = reg
}
