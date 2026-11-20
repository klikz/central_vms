package rtspclient

import "testing"

// 1.0-draft.12: H.265 kamera oqimi uchun depaketizator (RFC 7798).
func TestH265DepacketizerSingleton(t *testing.T) {
	d := NewH265Depacketizer()
	sps := []byte{0x42, 0x01, 0x05, 0x06} // tur 33 (SPS)
	nals, ok := d.Add(RTPPacket{Payload: sps, Marker: true})
	if !ok || len(nals) != 1 || string(nals[0]) != string(sps) {
		t.Fatalf("singleton NAL buzildi: %v ok=%v", nals, ok)
	}
}

func TestH265DepacketizerFU(t *testing.T) {
	d := NewH265Depacketizer()
	// IDR_W_RADL (tur 19) ikki bo'lakka bo'lingan.
	// Tashqi NAL boshi tur 49 (FU): 49<<1 = 0x62. FU boshida haqiqiy
	start := []byte{0x62, 0x01, 0x93, 0xAA, 0xBB} // S=1, type=19 → 0x80|19
	end := []byte{0x62, 0x01, 0x53, 0xCC}         // E=1 → 0x40|19
	if _, ok := d.Add(RTPPacket{Payload: start}); ok {
		t.Fatal("birinchi bo'lak AU deb berildi")
	}
	nals, ok := d.Add(RTPPacket{Payload: end, Marker: true})
	if !ok || len(nals) != 1 {
		t.Fatalf("FU yig'ilmadi: %v ok=%v", nals, ok)
	}
	got := nals[0]
	if (got[0]>>1)&0x3f != 19 {
		t.Errorf("NAL turi tiklanmadi: 0x%02x", got[0])
	}
	if got[0]&0x81 != 0x26&0x81 {
		t.Errorf("NAL boshi bitlari buzildi: 0x%02x", got[0])
	}
	want := []byte{0xAA, 0xBB, 0xCC}
	if string(got[2:]) != string(want) {
		t.Errorf("FU payload buzildi: % x", got[2:])
	}
}

func TestH265DepacketizerAP(t *testing.T) {
	d := NewH265Depacketizer()
	vps := []byte{0x40, 0x01, 0x01}
	sps := []byte{0x42, 0x01, 0x02, 0x03}
	// AP: o'z NAL boshi (tur 48 → 0x60), so'ng [ST][NAL] juftliklari.
	pl := []byte{0x60, 0x01}
	pl = append(pl, byte(len(vps)>>8), byte(len(vps)))
	pl = append(pl, vps...)
	pl = append(pl, byte(len(sps)>>8), byte(len(sps)))
	pl = append(pl, sps...)
	nals, ok := d.Add(RTPPacket{Payload: pl, Marker: true})
	if !ok || len(nals) != 2 {
		t.Fatalf("AP ajratilmadi: %d ta NAL ok=%v", len(nals), ok)
	}
	if string(nals[0]) != string(vps) || string(nals[1]) != string(sps) {
		t.Errorf("AP NAL'lari noto'g'ri: % x / % x", nals[0], nals[1])
	}
}

// Round-trip: paketlash → depaketlash bir xil NAL'ni qaytarishi kerak
// (FU turi 49 — ikkala tomonda bir xil bo'lishi shart).
func TestH265RoundTrip(t *testing.T) {
	// Katta IDR NAL'i (2 baytli bosh + to'ldirish) — FU bo'linishiga majbur.
	nal := []byte{19 << 1, 0x01}
	for i := 0; i < 3000; i++ {
		nal = append(nal, byte(i))
	}
	pk := NewH265Packetizer(96, 1)
	pkts := pk.Pack(nal, 42000, true, 1200)
	if len(pkts) < 3 {
		t.Fatalf("FU bo'linishi kutilardi, got %d paket", len(pkts))
	}
	dep := NewH265Depacketizer()
	var got [][]byte
	for i, raw := range pkts {
		p, err := ParseRTP(raw)
		if err != nil {
			t.Fatal(err)
		}
		if i == len(pkts)-1 {
			p.Marker = true
		}
		if n, ok := dep.Add(p); ok {
			got = append(got, n...)
		}
	}
	if len(got) != 1 || string(got[0]) != string(nal) {
		t.Fatalf("round-trip buzildi: %d ta NAL, uzunliklar %d/%d", len(got), len(nal), func() int {
			if len(got) > 0 {
				return len(got[0])
			}
			return 0
		}())
	}
}
