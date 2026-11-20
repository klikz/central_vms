package discovery

import "testing"

// §16.2: natijalarni birlashtirish — unikal kalit MAC (yoki serial),
// bo'lmasa ip+port. Qayta topilgan kamera already_known, mavjud id saqlanadi.

func TestMergeKey_Priority(t *testing.T) {
	// MAC eng ustuvor.
	d := Device{MAC: "aa:bb", Serial: "S1", IP: "10.0.0.1"}
	if k := DeviceKey(d); k != "mac:aa:bb" {
		t.Errorf("MAC ustuvor: %q", k)
	}
	// MAC yo'q → serial.
	d = Device{Serial: "S1", IP: "10.0.0.1"}
	if k := DeviceKey(d); k != "serial:S1" {
		t.Errorf("serial key: %q", k)
	}
	// Ikkalasi yo'q → ip+port.
	d = Device{IP: "10.0.0.1"}
	if k := DeviceKey(d); k != "ip:10.0.0.1:554" {
		t.Errorf("ip+port key: %q", k)
	}
	// Hech narsa yo'q → bo'sh (moslashtirilmaydi).
	if k := DeviceKey(Device{}); k != "" {
		t.Errorf("bo'sh kalit kutilardi: %q", k)
	}
}

// 10 soxta kamera, 3 tasini qayta skanda takrorlanmasligi kerak.
func TestMergeRescanNoDuplicates(t *testing.T) {
	// Dastlabki 10 kamera (biri MAC, biri serial, qolgani ip+port bo'yicha).
	known := []Known{
		{ID: "id-mac", MAC: "aa:00", IP: "10.0.0.1"},
		{ID: "id-ser", Serial: "SN-7", IP: "10.0.0.2"},
		{ID: "id-ip1", IP: "10.0.0.3"},
		{ID: "id-ip2", IP: "10.0.0.4"},
		{ID: "id-ip3", IP: "10.0.0.5"},
		{ID: "id-ip4", IP: "10.0.0.6"},
		{ID: "id-ip5", IP: "10.0.0.7"},
		{ID: "id-ip6", IP: "10.0.0.8"},
		{ID: "id-ip7", IP: "10.0.0.9"},
		{ID: "id-ip8", IP: "10.0.0.10"},
	}
	// Qayta skan: hammasi qayta chiqadi (bir xil kanonik kalit bilan),
	// + 2 yangi kamera.
	var scan []Device
	for _, k := range known {
		scan = append(scan, Device{MAC: k.MAC, Serial: k.Serial, IP: k.IP})
	}
	scan = append(scan,
		Device{IP: "10.0.0.11"},
		Device{MAC: "bb:11", IP: "10.0.0.12"},
	)

	res := Merge(scan, known)
	if len(res) != 12 {
		t.Fatalf("natijalar = %d, 12 kutilardi", len(res))
	}
	var knownCnt, newCnt int
	for _, r := range res {
		if r.AlreadyKnown {
			knownCnt++
			if r.ExistingID == "" {
				t.Errorf("already_known, lekin ExistingID bo'sh: %+v", r)
			}
		} else {
			newCnt++
			if r.ExistingID != "" {
				t.Errorf("yangi kamera ExistingID bor: %+v", r)
			}
		}
	}
	if knownCnt != 10 || newCnt != 2 {
		t.Errorf("known=%d new=%d, xohlangan known=10 new=2", knownCnt, newCnt)
	}
}

// MAC bir xil, IP o'zgagan → same kamera (id saqlanadi).
func TestMergeMACStableAcrossIP(t *testing.T) {
	known := []Known{{ID: "id1", MAC: "cc:cc", IP: "10.0.0.1"}}
	res := Merge([]Device{{MAC: "cc:cc", IP: "10.0.0.99"}}, known)
	if len(res) != 1 || !res[0].AlreadyKnown || res[0].ExistingID != "id1" {
		t.Fatalf("MAC bo'yicha tanilmadi: %+v", res)
	}
}

// Bir xil skan ichida takror (bir kamera ikki marta) → bir marta natijada.
func TestMergeDedupsWithinScan(t *testing.T) {
	known := []Known{}
	scan := []Device{{IP: "10.0.0.5"}, {IP: "10.0.0.5"}, {MAC: "dd:dd", IP: "10.0.0.6"}}
	res := Merge(scan, known)
	if len(res) != 2 {
		t.Fatalf("skan ichida dedup ishlamadi: %d natija, 2 kutilardi", len(res))
	}
}
