package rtspclient

import (
	"bytes"
	"testing"
)

// Packetizer → tarmoq → ParseRTP → Depacketizer: AU bir xil qaytishi kerak.

func TestRTP_RoundTrip(t *testing.T) {
	pk := NewH264Packetizer(96, 0xABCD)
	small := []byte{0x61, 0x09, 0xF8, 0x01}
	big := make([]byte, 2500)
	big[0] = 0x65
	for i := 1; i < len(big); i++ {
		big[i] = byte(i * 7)
	}
	aus := [][]byte{small, big}

	dep := &H264Depacketizer{}
	var got [][]byte
	var gotAUs int
	for i, nal := range aus {
		marker := i == len(aus)-1 // marker faqat AU oxirida
		for _, raw := range pk.Pack(nal, 90000, marker, 1200) {
			p, err := ParseRTP(raw)
			if err != nil {
				t.Fatal(err)
			}
			if p.SSRC != 0xABCD || p.PT != 96 {
				t.Errorf("sarlavha: %+v", p)
			}
			if au, ok := dep.Add(p); ok {
				gotAUs++
				got = append(got, au...)
			}
		}
	}
	if gotAUs != 1 {
		t.Errorf("AU soni = %d, 1 kutilardi (marker oxirgi pakethu)", gotAUs)
	}
	if len(got) != 2 {
		t.Fatalf("NAL soni = %d, 2 kutilardi", len(got))
	}
	// Kichik NAL bir xil; katta NAL FU-A orqali qayta yig'iladi.
	if !bytes.Equal(got[0], small) {
		t.Errorf("small NAL farq: % x", got[0][:min(8, len(got[0]))])
	}
	if !bytes.Equal(got[1], big) {
		t.Errorf("big NAL farq: len %d vs %d", len(got[1]), len(big))
	}
}

func TestRTP_BadVersion(t *testing.T) {
	if _, err := ParseRTP([]byte{0x00, 60, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}); err == nil {
		t.Error("versiya 2 bo'lmagan paket qabul qilindi")
	}
	if _, err := ParseRTP([]byte{0x80}); err == nil {
		t.Error("qisqa paket qabul qilindi")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
