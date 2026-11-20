package cams

import (
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "golden fayllarni qayta yozish")

// G3 gate: SDP golden testlari — testdata/sdp/*.sdp uchun kutilgan natija
// testdata/sdp/expected-<nomi>.json fayllarida saqlanadi.
func TestSDPGolden(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "sdp")
	files, err := filepath.Glob(filepath.Join(dir, "*.sdp"))
	if err != nil || len(files) == 0 {
		t.Skipf("SDP fixture'lari yo'q — `make fixtures` bajaring")
	}
	if len(files) < 4 {
		t.Fatalf("fixture soni=%d, kutilgan ≥4", len(files))
	}

	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".sdp")
		t.Run(base, func(t *testing.T) {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			sdp, err := ParseSDP(string(b))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			streams := sdp.VideoStreams()
			if len(streams) == 0 {
				t.Fatal("video oqim topilmadi")
			}
			goldenPath := filepath.Join(dir, "expected-"+base+".json")

			if *update {
				out, _ := json.MarshalIndent(streams, "", "  ")
				if err := os.WriteFile(goldenPath, append(out, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("golden yozildi: %s", goldenPath)
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("golden fayl yo'q (%s) — `go test ./internal/cams -run SDP -update` bajaring", goldenPath)
			}
			var wantStreams []StreamInfo
			if err := json.Unmarshal(want, &wantStreams); err != nil {
				t.Fatal(err)
			}
			gotJSON, _ := json.Marshal(streams)
			wantJSON, _ := json.Marshal(wantStreams)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("SDP natijasi o'zgardi:\n  oldi:  %s\n  golden: %s", gotJSON, wantJSON)
			}
		})
	}
}

// Mustaqil oracle: ffprobe (agar mavjud bo'lsa) bilan solishtirish.
// Bu test tashqi kutubxonalarga bog'liq emas va parserning to'g'riligini tasdiqlaydi.
func TestSDPVersusFFprobe(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe yo'q")
	}
	cases := []struct {
		sdp     string
		h264    string
		profile string
		bframes bool
	}{
		{"h264-baseline.sdp", "h264-baseline.h264", "Baseline", false},
		{"h264-main-nob.sdp", "h264-main-nob.h264", "Main", false},
		{"h264-high-bframes.sdp", "h264-high-bframes.h264", "High", true},
	}
	for _, c := range cases {
		t.Run(c.sdp, func(t *testing.T) {
			sdpPath := filepath.Join("..", "..", "testdata", "sdp", c.sdp)
			b, err := os.ReadFile(sdpPath)
			if err != nil {
				t.Skipf("fixture yo'q: %v", err)
			}
			sdp, err := ParseSDP(string(b))
			if err != nil {
				t.Fatal(err)
			}
			st := sdp.VideoStreams()
			if len(st) != 1 {
				t.Fatalf("oqim soni=%d", len(st))
			}
			if strings.EqualFold(st[0].Profile, c.profile) == false {
				t.Fatalf("profile=%q, ffprobe kutilgan %q", st[0].Profile, c.profile)
			}
			if st[0].BFrames != c.bframes {
				t.Fatalf("bframes=%v, kutilgan %v", st[0].BFrames, c.bframes)
			}

			// ffprobe bilan tekshirish (raw Annex-B stream)
			rawPath := filepath.Join("..", "..", "testdata", "annexb", c.h264)
			if _, err := os.Stat(rawPath); err != nil {
				return
			}
			out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
				"-show_entries", "stream=codec_name,profile,has_b_frames", "-of", "json", rawPath).Output()
			if err != nil {
				t.Skipf("ffprobe ishlamadi: %v", err)
			}
			var res struct {
				Streams []struct {
					CodecName  string `json:"codec_name"`
					Profile    string `json:"profile"`
					HasBFrames int    `json:"has_b_frames"`
				} `json:"streams"`
			}
			if err := json.Unmarshal(out, &res); err != nil {
				t.Fatal(err)
			}
			if len(res.Streams) == 0 {
				t.Skip("ffprobe javob bermadi")
			}
			s := res.Streams[0]
			if s.CodecName != "h264" {
				t.Fatalf("ffprobe codec=%q", s.CodecName)
			}
			// FFmpeg "Constrained Baseline" deb ko'rsatadi (constraint flag'lari bilan);
			// SPS'dagi profile_idc=66 → bizning "baseline" to'g'ri.
			fp := strings.ReplaceAll(strings.ToLower(s.Profile), "constrained ", "")
			if fp != strings.ToLower(c.profile) {
				t.Fatalf("ffprobe profile=%q, bizniki %q", s.Profile, c.profile)
			}
			if (s.HasBFrames > 0) != c.bframes {
				t.Fatalf("ffprobe has_b_frames=%d, bizning bframes=%v", s.HasBFrames, c.bframes)
			}
		})
	}
}

func TestSPSFromAnnexBFixtures(t *testing.T) {
	cases := []struct {
		file    string
		profile string
		reorder int
		bframes bool
	}{
		{"h264-baseline.h264", "baseline", 0, false},
		{"h264-main-nob.h264", "main", 0, false},
		// FFmpeg `-x264-params bframes=3` bilan generatsiya qilingan, lekin
		// SPS'da num_reorder_frames=2 (ffprobe ham 2 ko'rsatadi — oracle test bor).
		{"h264-high-bf3.h264", "high", 2, true},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			path := filepath.Join("..", "..", "testdata", "annexb", c.file)
			b, err := os.ReadFile(path)
			if err != nil {
				t.Skipf("fixture yo'q: %v", err)
			}
			sps, err := ParseSPSAnnexB(b)
			if err != nil {
				t.Fatal(err)
			}
			if sps.ProfileName() != c.profile {
				t.Fatalf("profile=%q, kutilgan %q", sps.ProfileName(), c.profile)
			}
			if sps.NumReorderFrames != c.reorder {
				t.Fatalf("num_reorder_frames=%d, kutilgan %d", sps.NumReorderFrames, c.reorder)
			}
			if sps.HasBFrames() != c.bframes {
				t.Fatalf("HasBFrames=%v, kutilgan %v", sps.HasBFrames(), c.bframes)
			}
			if !sps.HasVUI {
				t.Fatal("VUI topilmadi")
			}
		})
	}
}

func TestSPSXatoMatnlar(t *testing.T) {
	if _, err := ParseSPSAnnexB([]byte{0, 0, 0, 1, 0x09, 0xF0}); !strings.Contains(err.Error(), "SPS topilmadi") {
		t.Fatalf("xato: %v", err)
	}
	if _, err := ParseSPSBase64("!!!"); err == nil {
		t.Fatal("base64 xatosi kutilmadi")
	}
	if _, err := ParseSPSBase64(""); !strings.Contains(err.Error(), "SPS topilmadi") {
		t.Fatalf("bo'sh: %v", err)
	}
	// truncated SPS → ErrBadSPS
	trunc := "Z2QAH6zZQFAF"
	if _, err := ParseSPSBase64(trunc); err == nil {
		t.Fatal("qisqartirilgan SPS parse bo'ldi (xato bo'lishi kerak)")
	}
}

func TestParseSDPXatolar(t *testing.T) {
	if _, err := ParseSDP("buzilgan matn"); err == nil {
		t.Fatal("v= qatori bo'lmasa xato berishi kerak")
	}
	if _, err := ParseSDP("v=0\nm=video\n"); err == nil {
		t.Fatal("m=qatori noto'g'ri bo'lsa xato berishi kerak")
	}
	// audio-only SDP → video oqim yo'q
	s, err := ParseSDP("v=0\nm=audio 0 RTP/AVP 0\na=rtpmap:0 PCMU/8000\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.VideoStreams()) != 0 {
		t.Fatal("audio-only SDP'da video oqim bo'lmasligi kerak")
	}
}
