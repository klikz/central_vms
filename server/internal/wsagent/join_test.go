// §3 (1.0-draft.10): ulanish so'rovi testlari — so'rov → admin tasdiqi →
// agent_id; IP bo'yicha oldindan ruxsat; rad etish; rate-limit.
package wsagent

import (
	"errors"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"central-vms/server/internal/registry"
	"central-vms/server/internal/store"
)

type fakeClock struct{ t time.Time }

func (f *fakeClock) Now() time.Time      { return f.t }
func (f *fakeClock) Add(d time.Duration) { f.t = f.t.Add(d) }

func testJoiner(t *testing.T) (*Joiner, *registry.Registry, *fakeClock) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
	if _, err := store.MigrateUp(db, dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	fc := &fakeClock{t: time.Date(2026, 11, 20, 5, 0, 0, 0, time.UTC)}
	lg := slog.Default()
	return NewJoiner(db, registry.New(db), lg, 5, fc), registry.New(db), fc
}

func joinInput(name string) JoinInput {
	return JoinInput{
		JoinKey: randHexLower(32), AgentSecret: "sir-" + randHexLower(32),
		Hostname: "joy-pc", OS: "Ubuntu 24.04", LanIP: "192.168.0.55",
		AgentVersion: "0.3.1", Name: name,
	}
}

// Asosiy oqim: so'rov pending → admin tasdiqlaydi → agent polling orqali
// agent_id oladi. Sir serverdan hech qachon qaytmaydi.
func TestJoinRequestApprovePoll(t *testing.T) {
	j, reg, _ := testJoiner(t)
	in := joinInput("Oxangaron")

	out, err := j.Request(in, "203.0.113.7")
	if err != nil {
		t.Fatalf("so'rov: %v", err)
	}
	if out.Status != "pending" || out.RequestID == 0 {
		t.Fatalf("kutilgan pending: %+v", out)
	}
	// Hali mijoz yo'q — tasdiqdan oldin hech kim qo'shilmaydi.
	if _, err := reg.GetClientByAgentID(out.AgentID); !errors.Is(err, registry.ErrNotFound) && out.AgentID != "" {
		t.Fatalf("tasdiqdan oldin mijoz yaratilgan: %+v", out)
	}
	list, err := reg.ListJoinRequests("pending")
	if err != nil || len(list) != 1 {
		t.Fatalf("so'rovlar ro'yxati: %v %+v", err, list)
	}
	if list[0].Name != "Oxangaron" || list[0].SourceIP != "203.0.113.7" ||
		list[0].Hostname != "joy-pc" || list[0].AgentSecretHash == "" {
		t.Fatalf("so'rov maydonlari: %+v", list[0])
	}

	ap, err := j.Approve(list[0].ID, "Oxangaron", 0)
	if err != nil {
		t.Fatalf("tasdiq: %v", err)
	}
	if ap.Status != "approved" || !strings.HasPrefix(ap.AgentID, "ag_") {
		t.Fatalf("tasdiq natijasi: %+v", ap)
	}
	c, err := reg.GetClientByAgentID(ap.AgentID)
	if err != nil {
		t.Fatalf("mijoz: %v", err)
	}
	if c.Name != "Oxangaron" {
		t.Fatalf("nom: %q", c.Name)
	}

	// Agent polling: shu kalit bilan agent_id ni oladi, sirni EMAS.
	out2, err := j.Request(in, "203.0.113.7")
	if err != nil {
		t.Fatalf("polling: %v", err)
	}
	if out2.Status != "approved" || out2.AgentID != ap.AgentID {
		t.Fatalf("polling natijasi: %+v", out2)
	}
}

// Agent bir necha marta so'rov yuborsa yangi qator ochilmaydi (bitta
// doimiy kalit — bitta so'rov), ma'lumotlar yangilanadi.
func TestJoinRequestIsIdempotentPerKey(t *testing.T) {
	j, _, _ := testJoiner(t)
	in := joinInput("Uy")
	a, _ := j.Request(in, "10.0.0.5")
	in.Hostname = "joy-pc-2"
	b, err := j.Request(in, "10.0.0.5")
	if err != nil {
		t.Fatalf("takror so'rov: %v", err)
	}
	if a.RequestID != b.RequestID {
		t.Fatalf("takror so'rov yangi qator ochdi: %d vs %d", a.RequestID, b.RequestID)
	}
	list, _ := j.reg.ListJoinRequests("pending")
	if len(list) != 1 || list[0].Hostname != "joy-pc-2" {
		t.Fatalf("ma'lumot yangilanmadi: %+v", list)
	}
}

// IP bo'yicha oldindan ruxsat: operator IP yozib qo'ygan bo'lsa, so'rov
// darhol tasdiqlanadi va admin qaramasdan mijoz paydo bo'ladi.
func TestJoinPreauthAutoApprove(t *testing.T) {
	j, reg, _ := testJoiner(t)
	if _, err := reg.AddPreauth("192.168.0.0/22", "Ofis", "", 0); err != nil {
		t.Fatalf("preauth: %v", err)
	}
	out, err := j.Request(joinInput(""), "192.168.0.42")
	if err != nil {
		t.Fatalf("so'rov: %v", err)
	}
	if out.Status != "approved" || !strings.HasPrefix(out.AgentID, "ag_") {
		t.Fatalf("avtomatik tasdiq ishlamadi: %+v", out)
	}
	c, err := reg.GetClientByAgentID(out.AgentID)
	if err != nil {
		t.Fatalf("mijoz: %v", err)
	}
	if c.Name != "Ofis" {
		t.Fatalf("preauth nomi ishlatilmadi: %q", c.Name)
	}
	// Boshqa tarmoq — tasdiq kutiladi.
	out2, err := j.Request(joinInput(""), "10.10.0.9")
	if err != nil {
		t.Fatalf("so'rov 2: %v", err)
	}
	if out2.Status != "pending" {
		t.Fatalf("tarmoq chegarasi buzildi: %+v", out2)
	}
}

// Preauth avtomatik tasdiqda nom band bo'lsa jarayon to'xtamaydi:
// nomga raqam qo'shiladi (hech kim qarab turgan emas).
func TestJoinPreauthNameCollisionSuffix(t *testing.T) {
	j, reg, _ := testJoiner(t)
	if _, err := reg.CreateClient("Ofis", 0, "ag_boshqa", HashSecret("sir-boshqa")); err != nil {
		t.Fatalf("mijoz: %v", err)
	}
	if _, err := reg.AddPreauth("10.0.0.7", "Ofis", "", 0); err != nil {
		t.Fatalf("preauth: %v", err)
	}
	out, err := j.Request(joinInput(""), "10.0.0.7")
	if err != nil {
		t.Fatalf("so'rov: %v", err)
	}
	if out.Status != "approved" {
		t.Fatalf("tasdiq kutilmagan: %+v", out)
	}
	c, err := reg.GetClientByAgentID(out.AgentID)
	if err != nil {
		t.Fatalf("mijoz: %v", err)
	}
	if c.Name != "Ofis (2)" {
		t.Fatalf("nom raqami: %q", c.Name)
	}
}

// Admin tasdig'ida nom band bo'lsa — aniq xato (operator nomni
// tahrirlashi uchun), avtomatik raqam qo'shilmaydi.
func TestJoinApproveNameTaken(t *testing.T) {
	j, reg, _ := testJoiner(t)
	if _, err := reg.CreateClient("Oxangaron", 0, "ag_mavjud", HashSecret("sir-mavjud")); err != nil {
		t.Fatalf("mijoz: %v", err)
	}
	out, _ := j.Request(joinInput("Oxangaron"), "10.0.0.5")
	if _, err := j.Approve(out.RequestID, "", 0); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("nom band xatosi kutilmadi: %v", err)
	}
	// So'rov pending bo'lib qoladi — operator boshqa nom bilan qayta urinishi mumkin.
	q, err := reg.GetJoinRequestByID(out.RequestID)
	if err != nil || q.Status != "pending" {
		t.Fatalf("so'rov holati: %+v %v", q, err)
	}
	if _, err := j.Approve(out.RequestID, "Oxangaron 2", 0); err != nil {
		t.Fatalf("boshqa nom bilan tasdiq: %v", err)
	}
}

// Rad etilgan so'rov: agent aniq xabar oladi va qayta urinishda ham
// rad holati qaytadi (admin qayta tasdiqlashi uchun so'rov o'chiriladi).
func TestJoinRejectedFlow(t *testing.T) {
	j, reg, _ := testJoiner(t)
	in := joinInput("Noto'g'ri PC")
	out, _ := j.Request(in, "10.0.0.5")
	if err := j.Reject(out.RequestID, "bu PC eski", 0); err != nil {
		t.Fatalf("rad: %v", err)
	}
	_, err := j.Request(in, "10.0.0.5")
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("rad holati qaytmadi: %v", err)
	}
	// O'chirilsa — yangi so'rov yana pending bo'ladi.
	if err := j.Delete(out.RequestID); err != nil {
		t.Fatalf("o'chirish: %v", err)
	}
	if _, err := reg.GetJoinRequestByID(out.RequestID); !errors.Is(err, registry.ErrNotFound) {
		t.Fatalf("o'chirilmadi: %v", err)
	}
	out2, err := j.Request(in, "10.0.0.5")
	if err != nil || out2.Status != "pending" {
		t.Fatalf("qayta so'rov: %+v %v", out2, err)
	}
}

// Kalit talablari: qisqa/noto'g'ri join_key va qisqa sir rad etiladi.
func TestJoinBadKey(t *testing.T) {
	j, _, _ := testJoiner(t)
	if _, err := j.Request(JoinInput{JoinKey: "qisqa", AgentSecret: "sir-sir-sir-sir-sir-123456"}, "10.0.0.5"); !errors.Is(err, ErrBadKey) {
		t.Fatalf("join_key tekshiruvi ishlamadi: %v", err)
	}
	if _, err := j.Request(JoinInput{JoinKey: randHexLower(32), AgentSecret: "qisqa"}, "10.0.0.5"); !errors.Is(err, ErrBadKey) {
		t.Fatalf("sir tekshiruvi ishlamadi: %v", err)
	}
}

// Rate-limit: bitta IP'dan 5 so'rov / 5 daqiqa (ro'yxat shovutlab
// qo'yilmasin). Soat o'tsa — yana bo'sh.
func TestJoinRateLimit(t *testing.T) {
	j, _, fc := testJoiner(t)
	var lastErr error
	for range 7 {
		_, lastErr = j.Request(joinInput(""), "9.9.9.9")
	}
	if !errors.Is(lastErr, ErrRateLimited) {
		t.Fatalf("rate-limit ishlamadi: %v", lastErr)
	}
	fc.Add(6 * time.Minute)
	if _, err := j.Request(joinInput(""), "9.9.9.9"); err != nil {
		t.Fatalf("oyna o'tgach ham cheklangan: %v", err)
	}
}

// Tasdiqdan keyin so'rovni qayta tasdiqlab bo'lmaydi (mijoz takrorlanmasin).
func TestJoinApproveTwice(t *testing.T) {
	j, _, _ := testJoiner(t)
	out, _ := j.Request(joinInput("Bir marta"), "10.0.0.5")
	if _, err := j.Approve(out.RequestID, "", 0); err != nil {
		t.Fatalf("tasdiq: %v", err)
	}
	if _, err := j.Approve(out.RequestID, "", 0); err == nil {
		t.Fatal("takroriy tasdiq o'tib ketdi")
	}
}

// Tasdiqni kutish (polling) qat'iy limitga urilmasligi kerak: agent 15 s
// davrda so'raydi va bu normal holat (2026-11-20: bitta limit 5 urinishda
// polling'ni to'xtatib, tasdiqni hech qachon ko'ra olmas qilib qo'ygandi).
func TestJoinPollingAllowed(t *testing.T) {
	j, _, _ := testJoiner(t)
	in := joinInput("Kutayotgan")
	out, err := j.Request(in, "10.0.0.30")
	if err != nil || out.Status != "pending" {
		t.Fatalf("birinchi so'rov: %+v %v", out, err)
	}
	for i := range 12 {
		got, err := j.Request(in, "10.0.0.30")
		if err != nil {
			t.Fatalf("%d-urinish (polling) rad etildi: %v", i+2, err)
		}
		if got.Status != "pending" || got.RequestID != out.RequestID {
			t.Fatalf("polling natijasi: %+v", got)
		}
	}
	// Yangi kalitlar uchun limit baribir ishlaydi.
	var lastErr error
	for range 8 {
		_, lastErr = j.Request(joinInput(""), "10.0.0.30")
	}
	if !errors.Is(lastErr, ErrRateLimited) {
		t.Fatalf("yangi so'rovlar limiti ishlamadi: %v", lastErr)
	}
}
