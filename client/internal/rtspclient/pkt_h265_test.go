package rtspclient

import (
	"bytes"
	"testing"
)

// RFC 7798 H.265 RTP: singleton (2 baytli NAL header saqlanadi) va
// FU (RFC 7798 turi=49, S/E bitlari).

func TestH265Packetizer_Singleton(t *testing.T) {
	p := NewH265Packetizer(96, 5)
	nal := []byte{0x40, 0x01, 0xDE, 0xAD} // VPS: type=32 (0x40>>1)
	pkts := p.Pack(nal, 700, true, 1400)
	if len(pkts) != 1 {
		t.Fatalf("paketlar = %d", len(pkts))
	}
	if !bytes.Equal(pkts[0][12:], nal) {
		t.Errorf("payload: % x", pkts[0][12:])
	}
	if pkts[0][1]&0x80 == 0 {
		t.Error("marker yo'q")
	}
}

func TestH265Packetizer_FU(t *testing.T) {
	p := NewH265Packetizer(96, 5)
	nal := make([]byte, 2600)
	nal[0] = 0x42 // type=33 (SPS): (0x42>>1)&0x3f = 33
	nal[1] = 0x01
	for i := 2; i < len(nal); i++ {
		nal[i] = byte(i)
	}
	pkts := p.Pack(nal, 9000, true, 1000)
	if len(pkts) < 3 {
		t.Fatalf("FU bo'laklari = %d, kamida 3 kutilardi", len(pkts))
	}
	var reas []byte
	for i, r := range pkts {
		pl := r[12:]
		if len(pl) < 2 {
			t.Fatal("FU payload qisqa")
		}
		// RFC 7798: [bayt0: tur=49 (1..6-bit)] [bayt1: asl NAL bayt1 nusxasi]
		// [bayt2: FU boshi S|E|tur] [bo'lak].
		if len(pl) < 3 {
			t.Fatal("FU payload qisqa")
		}
		h0, fu := pl[0], pl[2]
		if typ := (h0 >> 1) & 0x3f; typ != 49 {
			t.Errorf("b%d: FU type = %d, 49 kutilardi", i, typ)
		}
		if pl[1] != nal[1] {
			t.Errorf("b%d: NAL bayt1 nusxasi buzildi", i)
		}
		if nalType := fu & 0x3f; nalType != uint8((nal[0]>>1)&0x3f) {
			t.Errorf("b%d: NAL type buzildi: %d", i, nalType)
		}
		if i == 0 && fu&0x80 == 0 {
			t.Error("S=1 kutilardi")
		}
		if i == len(pkts)-1 && fu&0x40 == 0 {
			t.Error("E=1 kutilardi")
		}
		reas = append(reas, pl[3:]...)
	}
	full := append([]byte{nal[0], nal[1]}, reas...)
	if !bytes.Equal(full, nal) {
		t.Errorf("qayta yig'ish xato (%d bayt)", len(full))
	}
}

// Marker faqat oxirgi pakethu.
func TestH265Packetizer_MarkerOnlyLast(t *testing.T) {
	p := NewH265Packetizer(96, 1)
	nal := make([]byte, 2100)
	nal[0], nal[1] = 0x26, 0x01 // type=19 (IDR)
	pkts := p.Pack(nal, 1, true, 1000)
	for i := 0; i < len(pkts)-1; i++ {
		if pkts[i][1]&0x80 != 0 {
			t.Errorf("b%d: marker noto'g'ri", i)
		}
	}
	if pkts[len(pkts)-1][1]&0x80 == 0 {
		t.Error("oxirgi pakethu marker yo'q")
	}
}
