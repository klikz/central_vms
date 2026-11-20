package cams

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// C0.8 qabul mezoni: fixture'lar mavjud va o'qiladigan (parse bo'ladi).
// To'liq SDP tahlili (kodek/profile/B-frame) C2.6 da qo'shiladi — u yerda
// mediacommon ishlatiladi. Bu test fixture bazasining buzilmaganini ushlaydi.

func fixturePath(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(append([]string{"..", "..", "testdata"}, parts...)...)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture topilmadi: %s (testdata/gen.sh ishga tushirilsin): %v", p, err)
	}
	return p
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("o'qib bo'lmadi %s: %v", p, err)
	}
	return string(b)
}

func TestFixtures_SDP(t *testing.T) {
	cases := []struct {
		file        string
		wantRtpmap  string
		wantFmtpHas []string
	}{
		{
			file:        "sdp/h264-baseline.sdp",
			wantRtpmap:  "H264/90000",
			wantFmtpHas: []string{"profile-level-id=42", "packetization-mode=1", "sprop-parameter-sets="},
		},
		{
			file:        "sdp/h264-main-nob.sdp",
			wantRtpmap:  "H264/90000",
			wantFmtpHas: []string{"profile-level-id=4d", "packetization-mode=1", "sprop-parameter-sets="},
		},
		{
			file:        "sdp/h264-high-bframes.sdp",
			wantRtpmap:  "H264/90000",
			wantFmtpHas: []string{"profile-level-id=64", "packetization-mode=1", "sprop-parameter-sets="},
		},
		{
			file:        "sdp/h265-main.sdp",
			wantRtpmap:  "H265/90000",
			wantFmtpHas: []string{"sprop-sps=", "sprop-pps=", "sprop-vps="},
		},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			sdp := readFile(t, fixturePath(t, strings.Split(tc.file, "/")...))
			if !strings.Contains(sdp, "v=0") {
				t.Fatal("SDP 'v=0' bilan boshlanishi kerak")
			}
			if !strings.Contains(sdp, "m=video") {
				t.Fatal("m=video qatori yo'q")
			}
			if !strings.Contains(sdp, "a=rtpmap:96 "+tc.wantRtpmap) {
				t.Fatalf("rtpmap noto'g'ri, kutilgan: %s\n%s", tc.wantRtpmap, sdp)
			}
			var fmtp string
			for _, line := range strings.Split(sdp, "\n") {
				if strings.HasPrefix(line, "a=fmtp:96") {
					fmtp = line
				}
			}
			if fmtp == "" {
				t.Fatal("a=fmtp:96 qatori yo'q")
			}
			for _, want := range tc.wantFmtpHas {
				if !strings.Contains(fmtp, want) {
					t.Fatalf("fmtp'da %q yo'q: %s", want, fmtp)
				}
			}
			if !strings.Contains(sdp, "a=control:") {
				t.Fatal("a=control qatori yo'q (track tanlash shu orqali)")
			}
		})
	}
}

func TestFixtures_AnnexB(t *testing.T) {
	cases := []struct {
		file     string
		nalTypes []int // kutilgan NAL turlari (h264: 7=SPS 8=PPS 5=IDR; h265: 32=VPS 33=SPS 34=PPS 20=IDR_N_LP)
	}{
		{file: "annexb/h264-baseline.h264", nalTypes: []int{7, 8, 5}},
		{file: "annexb/h264-main-nob.h264", nalTypes: []int{7, 8, 5}},
		{file: "annexb/h264-high-bf3.h264", nalTypes: []int{7, 8, 5}},
		{file: "annexb/h265-main.h265", nalTypes: []int{32, 33, 34, 20}}, // 20 = IDR_N_LP
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			b, err := os.ReadFile(fixturePath(t, strings.Split(tc.file, "/")...))
			if err != nil {
				t.Fatal(err)
			}
			if len(b) < 100 {
				t.Fatalf("fayl juda kichik: %d bayt", len(b))
			}
			if !(bytesHaveStartCode(b)) {
				t.Fatal("Annex-B start code (00 00 01 / 00 00 00 01) topilmadi")
			}
			found := nalTypesIn(b)
			for _, want := range tc.nalTypes {
				if !found[want] {
					t.Fatalf("NAL turi %d topilmadi (bor: %v)", want, keysOf(found))
				}
			}
		})
	}
}

func bytesHaveStartCode(b []byte) bool {
	for i := 0; i+2 < len(b); i++ {
		if b[i] == 0 && b[i+1] == 0 && (b[i+2] == 1 || (b[i+2] == 0 && i+3 < len(b) && b[i+3] == 1)) {
			return true
		}
	}
	return false
}

// nalTypesIn — NAL turlarini to'playdi (h264: 5 bit, h265: 6 bit — ikkalasi ham
// bu test uchun yetarli: kutilgan qiymatlar to'qnashmaydi).
func nalTypesIn(b []byte) map[int]bool {
	out := map[int]bool{}
	for i := 0; i+3 < len(b); i++ {
		if b[i] != 0 || b[i+1] != 0 {
			continue
		}
		var start int
		switch {
		case b[i+2] == 1:
			start = i + 3
		case b[i+2] == 0 && b[i+3] == 1:
			start = i + 4
		default:
			continue
		}
		if start >= len(b) {
			break
		}
		out[int((b[start]>>1)&0x3F)] = true // h265
		out[int(b[start]&0x1F)] = true      // h264
		i = start
	}
	return out
}

func keysOf(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestFixtures_FFmpegEncodersDump(t *testing.T) {
	txt := readFile(t, fixturePath(t, "ffmpeg-encoders.txt"))
	if !strings.Contains(txt, "Encoders:") {
		t.Fatal("dump formati kutilmagan (-hide_banner -encoders chiqishi bo'lishi kerak)")
	}
	for _, want := range []string{"libx264", "hevc"} {
		if !strings.Contains(strings.ToLower(txt), want) {
			t.Fatalf("dump'da %q yo'q", want)
		}
	}
}

type wsddEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		ProbeMatches struct {
			ProbeMatch []struct {
				Types  string `xml:"Types"`
				Scopes string `xml:"Scopes"`
				XAddrs string `xml:"XAddrs"`
			} `xml:"ProbeMatch"`
		} `xml:"ProbeMatches"`
	} `xml:"Body"`
}

func TestFixtures_WSDD(t *testing.T) {
	t.Run("dahua bitta kamera", func(t *testing.T) {
		var env wsddEnvelope
		if err := xml.Unmarshal([]byte(readFile(t, fixturePath(t, "wsdd", "probe-match-dahua.xml"))), &env); err != nil {
			t.Fatalf("XML parse: %v", err)
		}
		pm := env.Body.ProbeMatches.ProbeMatch
		if len(pm) != 1 {
			t.Fatalf("ProbeMatch soni: %d", len(pm))
		}
		if !strings.Contains(pm[0].XAddrs, "192.168.10.31") {
			t.Fatalf("XAddrs noto'g'ri: %s", pm[0].XAddrs)
		}
		if !strings.Contains(pm[0].Types, "NetworkVideoTransmitter") {
			t.Fatalf("Types noto'g'ri: %s", pm[0].Types)
		}
		if !strings.Contains(pm[0].Scopes, "IPC-HFW1230S") {
			t.Fatalf("Scopes'da model yo'q: %s", pm[0].Scopes)
		}
	})

	t.Run("bo'sh javob", func(t *testing.T) {
		var env wsddEnvelope
		if err := xml.Unmarshal([]byte(readFile(t, fixturePath(t, "wsdd", "probe-match-empty.xml"))), &env); err != nil {
			t.Fatalf("XML parse: %v", err)
		}
		if len(env.Body.ProbeMatches.ProbeMatch) != 0 {
			t.Fatal("bo'sh javobda match bo'lmasligi kerak")
		}
	})
}

func TestFixtures_CSV(t *testing.T) {
	basic := readFile(t, fixturePath(t, "csv", "cameras-basic.csv"))
	rows := strings.Split(strings.TrimSpace(basic), "\n")
	if len(rows) != 4 { // sarlavha + 3 kamera
		t.Fatalf("qatorlar soni: %d", len(rows))
	}
	for _, r := range rows {
		if n := len(strings.Split(r, ",")); n != 7 {
			t.Fatalf("ustunlar soni %d (7 bo'lishi kerak): %q", n, r)
		}
	}

	bad := readFile(t, fixturePath(t, "csv", "cameras-bad.csv"))
	if !strings.Contains(bad, ",,554") {
		t.Fatal("bo'sh hostli qator kutilgan")
	}
	if !strings.Contains(bad, "99999") {
		t.Fatal("yaroqsiz portli qator kutilgan")
	}
}
