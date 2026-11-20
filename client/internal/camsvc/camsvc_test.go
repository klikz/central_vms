package camsvc

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/keystore"
	"central-vms/client/internal/statestore"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "agent.key")
	if _, err := keystore.NewKeyFile(keyPath); err != nil {
		t.Fatal(err)
	}
	ks, err := keystore.Open(keyPath, filepath.Join(dir, "secrets.bin"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := statestore.Load(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &Service{St: st, KS: ks}
}

func TestCreateGetUpdateDelete(t *testing.T) {
	s := newSvc(t)
	epoch0 := s.St.CamsEpoch

	c, err := s.Create(cams.Camera{Name: "Ong eshik", Host: "192.168.10.20"}, "parol1")
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == "" || c.Position != 1 {
		t.Fatalf("yaratilgan: %+v", c)
	}
	if c.SecretRef != cams.SecretRefFor(c.ID, "main") {
		t.Fatalf("secret_ref: %q", c.SecretRef)
	}
	if v, ok := s.KS.Get(c.SecretRef); !ok || v != "parol1" {
		t.Fatalf("parol keystore'da yo'q: %q", v)
	}
	if s.St.CamsEpoch != epoch0+1 {
		t.Fatalf("epoch o'smadi: %d", s.St.CamsEpoch)
	}

	got, ok := s.Get(c.ID)
	if !ok || got.Name != "Ong eshik" {
		t.Fatalf("Get: %+v ok=%v", got, ok)
	}

	// Update: nom/host o'zgarsa ham id bir xil qoladi (§5)
	patch := cams.Camera{Name: "Chap eshik", Host: "192.168.10.21"}
	up, err := s.Update(c.ID, patch, nil)
	if err != nil {
		t.Fatal(err)
	}
	if up.ID != c.ID {
		t.Fatalf("id o'zgardi: %q → %q", c.ID, up.ID)
	}
	if up.Name != "Chap eshik" || up.Host != "192.168.10.21" {
		t.Fatalf("update: %+v", up)
	}
	if up.SecretRef != c.SecretRef {
		t.Fatal("secret_ref yo'qoldi (secret nil berilganda)")
	}

	// Parolni o'chirish
	if _, err := s.Update(c.ID, cams.Camera{Host: "192.168.10.21"}, strp("")); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.KS.Get(c.SecretRef); ok {
		t.Fatal("parol o'chirilmadi")
	}

	// Noma'lum id
	if _, err := s.Update("yoq", cams.Camera{Host: "h"}, nil); err == nil {
		t.Fatal("topilmagan idda xato kutilmadi")
	}

	// Delete + parol tozalash
	s.Create(cams.Camera{Name: "Hovli", Host: "10.0.0.9"}, "parol2")
	if n := s.Delete([]string{c.ID, "yoq-id"}); n != 1 {
		t.Fatalf("o'chirilganlar=%d", n)
	}
	if len(s.List()) != 1 {
		t.Fatalf("ro'yxat: %d", len(s.List()))
	}
	if s.St.CamsEpoch < epoch0+4 {
		t.Fatalf("epoch yetarli o'smadi: %d", s.St.CamsEpoch)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "agent.key")
	if _, err := keystore.NewKeyFile(keyPath); err != nil {
		t.Fatal(err)
	}
	ks, _ := keystore.Open(keyPath, filepath.Join(dir, "secrets.bin"))
	st, _ := statestore.Load(filepath.Join(dir, "state.json"))
	s := &Service{St: st, KS: ks}
	c, err := s.Create(cams.Camera{Name: "A", Host: "1.2.3.4"}, "maxfiy")
	if err != nil {
		t.Fatal(err)
	}

	// "Qayta ishga tushirish"
	ks2, _ := keystore.Open(keyPath, filepath.Join(dir, "secrets.bin"))
	st2, _ := statestore.Load(filepath.Join(dir, "state.json"))
	s2 := &Service{St: st2, KS: ks2}
	if len(s2.List()) != 1 || s2.List()[0].ID != c.ID {
		t.Fatalf("kameralar tiklanmadi: %+v", s2.List())
	}
	if s2.St.CamsEpoch != st.CamsEpoch {
		t.Fatalf("epoch tiklanmadi: %d vs %d", s2.St.CamsEpoch, st.CamsEpoch)
	}
	if v, ok := ks2.Get(c.SecretRef); !ok || v != "maxfiy" {
		t.Fatalf("parol tiklanmadi: %q", v)
	}
}

func TestCSVImportExport(t *testing.T) {
	s := newSvc(t)
	csvIn := "name,host,port,main_path,sub_path,enabled,secret\n" +
		"kamera-01,192.168.10.11,554,/cam/realmonitor?channel=1&subtype=0,/cam/realmonitor?channel=1&subtype=1,true,pass-a\n" +
		"kamera-02,192.168.10.12,,,,true,pass-b\n" +
		"yomon-qator,,554,,,,\n" + // host yo'q → xato
		"kamera-03,192.168.10.13,abc,,,,\n" // port xato
	rep, err := s.ImportCSV(strings.NewReader(csvIn), true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Created != 2 {
		t.Fatalf("yaratilgan=%d, kutilgan 2", rep.Created)
	}
	if len(rep.Errors) != 2 {
		t.Fatalf("xatolar=%v, kutilgan 2 ta", rep.Errors)
	}
	if !strings.Contains(rep.Errors[0], "qator 4") {
		t.Fatalf("xato qator raqami: %v", rep.Errors)
	}

	// Eksport: parollar mask'langan
	var buf bytes.Buffer
	if err := s.ExportCSV(&buf, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "pass-a") {
		t.Fatal("eksportda parol ochiq chiqdi")
	}
	if !strings.Contains(out, "***") {
		t.Fatal("eksportda mask belgisi yo'q")
	}

	// withSecrets=true → parollar chiqadi (faqat operator ruxsati bilan, §16.3)
	var buf2 bytes.Buffer
	if err := s.ExportCSV(&buf2, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf2.String(), "pass-a") {
		t.Fatal("with-secrets eksportda parol yo'q")
	}

	// Import → eksport → import (update yo'li): id saqlanadi
	var ids []string
	for _, c := range s.List() {
		ids = append(ids, c.ID)
	}
	var buf3 bytes.Buffer
	if err := s.ExportCSV(&buf3, false); err != nil {
		t.Fatal(err)
	}
	rep2, err := s.ImportCSV(strings.NewReader(buf3.String()), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Updated != 2 || rep2.Created != 0 {
		t.Fatalf("takroriy import: %+v (id bo'lgani uchun update bo'lishi kerak)", rep2)
	}
	for _, c := range s.List() {
		found := false
		for _, id := range ids {
			if id == c.ID {
				found = true
			}
		}
		if !found {
			t.Fatalf("id o'zgardi: %q", c.ID)
		}
	}
}

func TestCSVBosh(t *testing.T) {
	s := newSvc(t)
	if _, err := s.ImportCSV(strings.NewReader(""), true); err == nil {
		t.Fatal("bo'sh CSV'da xato kutilmadi")
	}
}

func TestCreateDuplicateID(t *testing.T) {
	s := newSvc(t)
	c, _ := s.Create(cams.Camera{Name: "A", Host: "1.1.1.1"}, "")
	if _, err := s.Create(cams.Camera{ID: c.ID, Name: "B", Host: "2.2.2.2"}, ""); err == nil {
		t.Fatal("bir xil id bilan ikkinchi kamera yaratilmasligi kerak")
	}
}

func TestListSortedByPosition(t *testing.T) {
	s := newSvc(t)
	s.Create(cams.Camera{Name: "uch", Host: "1.1.1.3", Position: 30}, "")
	s.Create(cams.Camera{Name: "bir", Host: "1.1.1.1", Position: 10}, "")
	s.Create(cams.Camera{Name: "ikki", Host: "1.1.1.2", Position: 20}, "")
	list := s.List()
	if list[0].Name != "bir" || list[1].Name != "ikki" || list[2].Name != "uch" {
		t.Fatalf("tartib: %v", []string{list[0].Name, list[1].Name, list[2].Name})
	}
}

func strp(v string) *string { return &v }

func TestSetStatus(t *testing.T) {
	svc := newSvc(t)
	c, err := svc.Create(cams.Camera{Name: "k", Host: "1.2.3.4", Port: 554, Enabled: true}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	ep0 := svc.St.CamsEpoch
	if err := svc.SetStatus(c.ID, cams.StatusOnline, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(c.ID)
	if got.Status != cams.StatusOnline || got.LastCheckAt == nil {
		t.Fatalf("status: %+v", got)
	}
	if svc.St.CamsEpoch != ep0 {
		t.Errorf("epoch o'shadi: %d → %d", ep0, svc.St.CamsEpoch)
	}
	if err := svc.SetStatus("yoq", cams.StatusOffline, time.Now()); err == nil {
		t.Error("noma'lum idda xato kutilardi")
	}
}

// OnChanged — ro'yxat o'zgarishi darhol signal berishi kerak (runtime
// shundan keyin cams_sync yuboradi; 2026-11-20: avval o'zgarish
// qayta ulanishgacha kutib qolardi).
func TestOnChangedFires(t *testing.T) {
	s := newSvc(t)
	var fired int
	s.OnChanged = func() { fired++ }

	c, err := s.Create(cams.Camera{Name: "A", Host: "10.0.0.5"}, "parol")
	if err != nil {
		t.Fatal(err)
	}
	if fired != 1 {
		t.Fatalf("Create dan keyin OnChanged %d marta", fired)
	}
	if _, err := s.Update(c.ID, cams.Camera{Name: "B", Host: "10.0.0.6"}, nil); err != nil {
		t.Fatal(err)
	}
	if fired != 2 {
		t.Fatalf("Update dan keyin OnChanged %d marta", fired)
	}
	if n := s.Delete([]string{c.ID}); n != 1 {
		t.Fatalf("Delete: %d", n)
	}
	if fired != 3 {
		t.Fatalf("Delete dan keyin OnChanged %d marta", fired)
	}
	// SetStatus (probe natijasi) ro'yxat o'zgarishi EMAS — signal yo'q.
	if err := s.SetStatus("yo'q", cams.StatusOnline, time.Now()); err == nil {
		t.Fatal("noma'lum kameraga SetStatus o'tdi")
	}
	if fired != 3 {
		t.Fatalf("SetStatus signal bermasligi kerak edi: %d", fired)
	}
}

// SetCodec — kodek o'rganilganda epoch o'sadi va OnChanged ishlaydi
// (server cams_sync orqali kodekni o'z vaqtida bilib oladi).
func TestSetCodec(t *testing.T) {
	s := newSvc(t)
	fires := 0
	s.OnChanged = func() { fires++ }
	c, err := s.Create(cams.Camera{ID: "c1", Name: "K1", Host: "10.0.0.5"}, "parol")
	if err != nil {
		t.Fatal(err)
	}
	if c.CodecMain != "" {
		t.Fatalf("yangi kamerada kodek bo'sh bo'lishi kerak: %+v", c)
	}
	if err := s.SetCodec("c1", "h265", ""); err != nil {
		t.Fatalf("SetCodec: %v", err)
	}
	got, ok := s.Get("c1")
	if !ok || got.CodecMain != "h265" {
		t.Fatalf("kodek yozilmadi: %+v", got)
	}
	if fires < 2 {
		t.Errorf("OnChanged kamerasi qo'shish + kodek uchun 2 marta ishlashi kerak edi, got %d", fires)
	}
	// O'zgarmas qiymat — takroriy sync chaqirilmaydi.
	before := fires
	if err := s.SetCodec("c1", "h265", ""); err != nil {
		t.Fatal(err)
	}
	if fires != before {
		t.Errorf("o'zgarish bo'lmasa OnChanged ishlamasligi kerak")
	}
	if err := s.SetCodec("yo'q", "h264", ""); err == nil {
		t.Error("noma'lum kamera uchun xato kutilardi")
	}
}
