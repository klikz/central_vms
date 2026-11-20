package discovery

import (
	"path/filepath"
	"strings"
	"testing"

	"central-vms/client/internal/camsvc"
	"central-vms/client/internal/keystore"
	"central-vms/client/internal/statestore"
)

func newSvcForTest(t *testing.T) *camsvc.Service {
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
	return camsvc.New(st, ks)
}

// §16.3: 50 kamera bir amaliyotda; qisman xato hisoboti (bitta host bo'sh).
func TestBulkAdd_FiftyPartialFailure(t *testing.T) {
	svc := newSvcForTest(t)
	epoch0 := svc.St.CamsEpoch

	devs := make([]Device, 50)
	for i := range devs {
		devs[i] = Device{IP: "10.20.0." + itoa(i+1), Model: "IPC-HFW1230S", Port: 554}
	}
	// Bitta noto'g'ri host (bo'sh) → Create yeng'iladi, qolganlari o'tadi.
	devs[7].IP = ""

	rep := BulkAdd(svc, BulkAddInput{
		Devices:     devs,
		NamePattern: "{model}-{index:02d}",
		Password:    "Dahua123",
		Enabled:     true,
	})
	if len(rep.Added) != 49 {
		t.Errorf("qo'shilgan = %d, 49 kutilardi", len(rep.Added))
	}
	if len(rep.Failed) != 1 {
		t.Fatalf("xato hisoboti = %d, 1 kutilardi: %+v", len(rep.Failed), rep.Failed)
	}
	if !strings.Contains(rep.Failed[0].Err, "host") {
		t.Errorf("xato matni host haqida bo'lishi kerak: %q", rep.Failed[0].Err)
	}
	// epoch har muvaffaqiyatli kamera uchun o'sdi (§16.3 cams_epoch++).
	if svc.St.CamsEpoch != epoch0+49 {
		t.Errorf("cams_epoch = %d, xohlangan %d", svc.St.CamsEpoch, epoch0+49)
	}
	// Nomi pattern bo'yicha: IPC-HFW1230S-01 ... (indeks 1 dan).
	if rep.Added[0].Name != "IPC-HFW1230S-01" {
		t.Errorf("birinchi nom = %q, IPC-HFW1230S-01 kutilardi", rep.Added[0].Name)
	}
	// Parol keystore'da (§16.3 secrets.bin).
	if v, ok := svc.KS.Get(rep.Added[0].SecretRef); !ok || v != "Dahua123" {
		t.Errorf("parol keystore'da yo'q: %q ok=%v", v, ok)
	}
	// Dahua default yo'li va enabled.
	if rep.Added[0].MainPath == "" || !rep.Added[0].Enabled {
		t.Errorf("kamera to'liq emas: %+v", rep.Added[0].MainPath)
	}
}

// name_pattern variantlari: {model}-{index:02d}, Kirish-{n}, {ip}.
func TestBulkAdd_NamePatterns(t *testing.T) {
	svc := newSvcForTest(t)
	devs := []Device{
		{IP: "10.0.0.1", Model: "Dahua"},
		{IP: "10.0.0.2", Model: "Dahua"},
	}
	rep := BulkAdd(svc, BulkAddInput{Devices: devs, NamePattern: "Kirish-{n}", Password: "p", Enabled: true})
	if len(rep.Added) != 2 {
		t.Fatalf("qo'shilgan = %d", len(rep.Added))
	}
	if rep.Added[0].Name != "Kirish-1" || rep.Added[1].Name != "Kirish-2" {
		t.Errorf("Kirish-{n}: %q, %q", rep.Added[0].Name, rep.Added[1].Name)
	}

	svc2 := newSvcForTest(t)
	rep2 := BulkAdd(svc2, BulkAddInput{Devices: devs, NamePattern: "{ip}", Password: "p"})
	if rep2.Added[0].Name != "10.0.0.1" {
		t.Errorf("{ip}: %q", rep2.Added[0].Name)
	}
}

// Har kameraga alohida parol (per-device override, §16.3).
func TestBulkAdd_PerDevicePassword(t *testing.T) {
	svc := newSvcForTest(t)
	devs := []Device{{IP: "10.0.0.1", MAC: "aa:11"}, {IP: "10.0.0.2", MAC: "bb:22"}}
	rep := BulkAdd(svc, BulkAddInput{
		Devices:   devs,
		Password:  "umumiy",
		PerDevice: map[string]string{"mac:aa:11": "maxsus"},
	})
	if len(rep.Added) != 2 {
		t.Fatalf("qo'shilgan = %d", len(rep.Added))
	}
	// Birinchi (mac:aa:11) maxsus parol, ikkinchisi umumiy.
	if v, _ := svc.KS.Get(rep.Added[0].SecretRef); v != "maxsus" {
		t.Errorf("per-device parol = %q, maxsus kutilardi", v)
	}
	if v, _ := svc.KS.Get(rep.Added[1].SecretRef); v != "umumiy" {
		t.Errorf("umumiy parol = %q", v)
	}
}

// Interface mosligi: camsvc.Service CameraCreator'ni qanoatlantiradi.
var _ CameraCreator = (*camsvc.Service)(nil)

// Bo'sh Device ro'yxati — xato emas, bo'sh hisobot.
func TestBulkAdd_Empty(t *testing.T) {
	svc := newSvcForTest(t)
	rep := BulkAdd(svc, BulkAddInput{Devices: nil})
	if len(rep.Added) != 0 || len(rep.Failed) != 0 {
		t.Errorf("bo'sh kirishda bo'sh hisobot kutilardi: %+v", rep)
	}
}

// Kamera modeli bo'sh bo'lsa {model} IP'ga qaytadi (nom ma'noli bo'lsin).
func TestBulkAdd_ModelBoShIPgaQaytadi(t *testing.T) {
	svc := newSvcForTest(t)
	rep := BulkAdd(svc, BulkAddInput{
		Devices:     []Device{{IP: "10.0.0.5"}},
		NamePattern: "{model}-{index:02d}",
		Password:    "p",
	})
	if len(rep.Added) != 1 {
		t.Fatalf("qo'shilgan = %d", len(rep.Added))
	}
	if rep.Added[0].Name != "10.0.0.5-01" {
		t.Errorf("model bo'sh → {model} IP'ga qaytishi kerak: %q", rep.Added[0].Name)
	}
}
