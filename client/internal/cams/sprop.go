package cams

import (
	"encoding/base64"
	"strings"
)

// sprop.go — SDP'dagi "out-of-band" parameter set'lari (SPS/PPS/VPS).
//
// 2026-11-20'da topilgan xato: ba'zi manbalar (MediaMTX, ayrim kameralar)
// SPS/PPS'ni faqat SDP'da sprop-parameter-sets sifatida beradi va oqimda
// takrorlamaydi. Agent oqimni NAL darajasida qayta yuborgani uchun
// MediaMTX'ga parameter set'lar hech qachon bormasdi — natijada preview
// "qora" chiqardi (ffprobe: "non-existing PPS 0 referenced"). Pull qatlami
// endi ularni SDP'dan olib har bir kalit kadrdan oldin qo'shadi.

// h265Stream — SDP media bandi H.265 mi (rtpmap bo'yicha).
func h265Stream(m Media) bool {
	for _, rm := range m.Rtpmap {
		if strings.Contains(strings.ToUpper(rm), "H265") {
			return true
		}
	}
	return false
}

// spropNALs — media bandidagi sprop parameter set'lari (NAL baytlari,
// boshlanish kodisiz). H.264: sprop-parameter-sets=SPS,PPS;
// H.265: sprop-vps / sprop-sps / sprop-pps.
func spropNALs(m Media) [][]byte {
	var out [][]byte
	decode := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		// Base64'da padding tushirib qoldirilishi mumkin — to'ldiramiz.
		if pad := len(v) % 4; pad != 0 {
			v += strings.Repeat("=", 4-pad)
		}
		if n, err := base64.StdEncoding.DecodeString(v); err == nil && len(n) > 1 {
			out = append(out, n)
		}
	}
	for _, fm := range m.Fmtp {
		for _, kv := range strings.Split(fm, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(kv), "=")
			if !ok {
				continue
			}
			switch strings.TrimSpace(k) {
			case "sprop-parameter-sets":
				for _, part := range strings.Split(v, ",") {
					decode(part)
				}
			case "sprop-vps", "sprop-sps", "sprop-pps":
				decode(v)
			}
		}
	}
	return out
}

// nalType — NAL boshi (boshlanish kodisiz) dan tur. H.265'da ikki baytli
// bosh, H.264'da bitta baytli.
func nalType(n []byte, h265 bool) int {
	if len(n) < 2 {
		return -1
	}
	if h265 {
		return int(n[0]>>1) & 0x3f
	}
	return int(n[0]) & 0x1f
}

// keyKadr — AU tarkibida kalit kadr (H.264 IDR=5; H.265 IRAP=16..21) bormi.
func keyKadr(nals [][]byte, h265 bool) bool {
	for _, n := range nals {
		t := nalType(n, h265)
		if t == 5 || (h265 && t >= 16 && t <= 21) {
			return true
		}
	}
	return false
}

// paramSetBor — AU'da parameter set NAL'lari bormi (H.264: 7/8; H.265: 32/33/34).
func paramSetBor(nals [][]byte, h265 bool) bool {
	for _, n := range nals {
		t := nalType(n, h265)
		if t == 7 || t == 8 || (h265 && t >= 32 && t <= 34) {
			return true
		}
	}
	return false
}
