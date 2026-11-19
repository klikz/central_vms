package rtspclient

import (
	"bytes"
	"testing"
)

// RFC 6184 H.264 RTP packetizer: singleton (sig'im MTU) yoki FU-A
// bo'laklash (S/E bitlari). Marker AU oxirida.

func TestPacketizer_Singleton(t *testing.T) {
	p := NewH264Packetizer(96, 0x1234)
	nal := []byte{0x65, 0xAA, 0xBB, 0xCC} // IDR
	pkts := p.Pack(nal, 1000, true, 1400)
	if len(pkts) != 1 {
		t.Fatalf("paketlar = %d, 1 kutilardi", len(pkts))
	}
	r := pkts[0]
	// RTP: V=2 P=0 X=0 CC=1 → 0x80; M bit + PT=96; seq; ts; ssrc.
	if r[0] != 0x80 {
		t.Errorf("RTP ilk bayt = %02x", r[0])
	}
	if r[1]&0x80 == 0 || int(r[1]&0x7f) != 96 {
		t.Errorf("marker/PT = %02x", r[1])
	}
	if !bytes.Equal(r[12:], nal) {
		t.Errorf("payload: % x", r[12:])
	}
}

func TestPacketizer_SequenceAndTimestamp(t *testing.T) {
	p := NewH264Packetizer(96, 1)
	a := p.Pack([]byte{0x61, 1}, 90000, false, 1400)
	b := p.Pack([]byte{0x61, 2}, 90000, true, 1400)
	sa, sb := seqOf(a[0]), seqOf(b[0])
	if sb != sa+1 {
		t.Errorf("seq %d → %d, +1 kutilardi", sa, sb)
	}
	if tsOf(a[0]) != 90000 || tsOf(b[0]) != 90000 {
		t.Errorf("timestamp o'zgardi")
	}
	if a[0][1]&0x80 != 0 {
		t.Error("marker=false bo'lgan paketda M=1")
	}
}

// MTU chegarasi → FU-A: indicator (F/NRI saqlanadi, type=28),
// first: S=1, last: E=1, bitlar uzluksiz.
func TestPacketizer_FUA(t *testing.T) {
	p := NewH264Packetizer(96, 7)
	nal := make([]byte, 3000)
	nal[0] = 0x67 // SPS: NRI=01 (0x60 bitlari)
	for i := 1; i < len(nal); i++ {
		nal[i] = byte(i)
	}
	pkts := p.Pack(nal, 4500, true, 1000)
	if len(pkts) < 4 {
		t.Fatalf("FU-A bo'laklari = %d, kamida 4 kutilardi (3000 bayt / ~996)", len(pkts))
	}
	var reassembled []byte
	for i, r := range pkts {
		pl := r[12:]
		if len(pl) < 2 {
			t.Fatalf("FU-A payload qisqa")
		}
		ind, fu := pl[0], pl[1]
		if ind&0x1f != 28 {
			t.Errorf("b0: FU type = %d, 28 kutilardi", ind&0x1f)
		}
		if ind&0x60 != nal[0]&0x60 {
			t.Errorf("b%d: NRI buzildi: %02x vs %02x", i, ind&0x60, nal[0]&0x60)
		}
		if i == 0 && fu&0x80 == 0 {
			t.Error("birinchi bo'lakda S=1 bo'lishi kerak")
		}
		if i == len(pkts)-1 && fu&0x40 == 0 {
			t.Error("oxirgi bo'lakda E=1 bo'lishi kerak")
		}
		if i > 0 && pkts[i-1][1]&0x80 != 0 {
			t.Error("marker faqat oxirgi pakethu bo'lishi kerak")
		}
		reassembled = append(reassembled, pl[2:]...)
	}
	// FU-A payloadi NAL header'siz — boshiga original header qo'yiladi.
	full := append([]byte{nal[0]}, reassembled...)
	if !bytes.Equal(full, nal) {
		t.Errorf("FU-A qayta yig'ish xato: %d bayt, kutilardi %d", len(full), len(nal))
	}
}

// Chegara: NAL hajmi = maxPayload → singleton (bo'laklanmaydi).
func TestPacketizer_ExactMTU(t *testing.T) {
	p := NewH264Packetizer(96, 1)
	nal := make([]byte, 1400)
	nal[0] = 0x41
	pkts := p.Pack(nal, 1, true, 1400)
	if len(pkts) != 1 {
		t.Fatalf("chegara singleton: %d paket", len(pkts))
	}
}

func seqOf(r []byte) uint16 { return uint16(r[2])<<8 | uint16(r[3]) }
func tsOf(r []byte) uint32 {
	return uint32(r[4])<<24 | uint32(r[5])<<16 | uint32(r[6])<<8 | uint32(r[7])
}
