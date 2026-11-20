package cams

import (
	"encoding/base64"
	"strings"
	"testing"
)

// spropNALs — H.264 manbasidagi sprop-parameter-sets (MediaMTX 2026-11-20
// SDP'sidan olilingan haqiqiy qiymat).
func TestSpropNALsH264(t *testing.T) {
	m := Media{Kind: "video", Rtpmap: map[int]string{96: "H264/90000"},
		Fmtp: map[int]string{96: "packetization-mode=1; profile-level-id=640016; sprop-parameter-sets=Z2QAFqy0BQF/y4CIAAADAAgAAAMApHixdQ==,aO8Pyw=="}}
	got := spropNALs(m)
	if len(got) != 2 {
		t.Fatalf("2 ta NAL kutilardi, got %d", len(got))
	}
	if got[0][0]&0x1f != 7 {
		t.Errorf("SPS kutilardi (tur 7), got %d", got[0][0]&0x1f)
	}
	if got[1][0]&0x1f != 8 {
		t.Errorf("PPS kutilardi (tur 8), got %d", got[1][0]&0x1f)
	}
	if h265Stream(m) {
		t.Error("H.264 bandi H.265 deb belgilandi")
	}
}

// Padding tushirib qoldirilgan base64 ham qabul qilinadi.
func TestSpropNALsNoPadding(t *testing.T) {
	raw := []byte{0x67, 0x42, 0x00, 0x1e, 0xab}
	b64 := strings.TrimRight(base64.StdEncoding.EncodeToString(raw), "=")
	m := Media{Fmtp: map[int]string{96: "sprop-parameter-sets=" + b64}}
	got := spropNALs(m)
	if len(got) != 1 || string(got[0]) != string(raw) {
		t.Fatalf("padding yo'q base64 buzuq: %v", got)
	}
}

// H.265: sprop-vps/sps/pps uchta alohida attribute.
func TestSpropNALsH265(t *testing.T) {
	enc := func(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
	m := Media{Rtpmap: map[int]string{96: "H265/90000"},
		Fmtp: map[int]string{96: "sprop-vps=" + enc([]byte{0x40, 0x01}) +
			";sprop-sps=" + enc([]byte{0x42, 0x01}) +
			";sprop-pps=" + enc([]byte{0x44, 0x01})}}
	if !h265Stream(m) {
		t.Fatal("H.265 aniqlanmadi")
	}
	got := spropNALs(m)
	if len(got) != 3 {
		t.Fatalf("3 ta NAL kutilardi, got %d", len(got))
	}
	for i, want := range []int{32, 33, 34} {
		if t2 := int((got[i][0] >> 1) & 0x3f); t2 != want {
			t.Errorf("NAL %d: tur %d kutilardi, got %d", i, want, t2)
		}
	}
}

// Kalit kadr + parameter set qarori (injektsiya aynan shu yerda qilinadi).
func TestKeyKadrParamSetBor(t *testing.T) {
	h264 := func(typ byte) []byte { return []byte{typ, 0x11} }
	if !keyKadr([][]byte{h264(5)}, false) {
		t.Error("H.264 IDR kalit kadr deb topilmadi")
	}
	if keyKadr([][]byte{h264(1)}, false) {
		t.Error("H.264 P-kadr kalit kadr deb topildi")
	}
	if !paramSetBor([][]byte{h264(7), h264(8), h264(5)}, false) {
		t.Error("SPS/PPS mavjudligi topilmadi")
	}
	if paramSetBor([][]byte{h264(5)}, false) {
		t.Error("parameter set yo'q yerda bor deb topildi")
	}
	// H.265: bosh 2 baytli, tur (b0>>1)&0x3f.
	h265 := func(typ byte) []byte { return []byte{typ << 1, 0x01} }
	if !keyKadr([][]byte{h265(19)}, true) {
		t.Error("H.265 IDR_W_RADL kalit kadr deb topilmadi")
	}
	if !paramSetBor([][]byte{h265(33)}, true) {
		t.Error("H.265 SPS topilmadi")
	}
	// H.265 NAL'ini H.264 niqobi bilan adashtirmaslik kerak.
	if keyKadr([][]byte{h265(19)}, false) {
		t.Error("H.265 NAL H.264 IDR deb adashtirildi")
	}
}
