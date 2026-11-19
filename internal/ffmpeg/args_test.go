package ffmpeg

import (
	"strings"
	"testing"
)

// §8.1/§8.2: FFmpeg argument matritsasi — remux (-c copy), re-encode
// (baseline -bf 0), GPU/CPU backendlar, whip/rtsp chiqish.

func commonFlagsOK(t *testing.T, args []string) {
	t.Helper()
	for _, f := range []string{
		"-hide_banner", "-loglevel", "error", "-nostats", "-progress", "pipe:1",
		"-analyzeduration", "500000", "-probesize", "500000",
	} {
		if !contains(args, f) {
			t.Errorf("%q yo'q (§8.2)", f)
		}
	}
	if !contains(args, "-rtsp_transport") || !contains(args, "tcp") {
		t.Error("-rtsp_transport tcp yo'q (§8.2)")
	}
}

func contains(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}

func TestArgs_RemuxCopy(t *testing.T) {
	a := BuildArgs(Job{
		Src:  Source{URL: "rtsp://cam/x"},
		Dst:  Dest{Scheme: "rtsp", URL: "rtsp://srv:8554/c?token=T"},
		Mode: ModeCopy,
	})
	commonFlagsOK(t, a)
	if !contains(a, "-c") || !contains(a, "copy") {
		t.Errorf("-c copy yo'q: %v", a)
	}
	if contains(a, "-c:v") {
		t.Error("copy rejimida -c:v bo'lmasligi kerak")
	}
	if a[len(a)-1] != "rtsp://srv:8554/c?token=T" {
		t.Errorf("chiqish URL'i oxirda bo'lishi kerak: %v", a)
	}
}

func TestArgs_ReencodeBaselineCPU(t *testing.T) {
	a := BuildArgs(Job{
		Src:  Source{URL: "rtsp://cam/x"},
		Dst:  Dest{Scheme: "whip", URL: "http://srv:8889/c/whip", HeaderName: "Authorization", HeaderValue: "Bearer T"},
		Mode: ModeReencode,
		Enc:  EncParams{Width: 1280, Height: 720, FPS: 10, BitrateKbps: 800, MaxrateKbps: 1000, GovSec: 2, Profile: "baseline", Backend: BackendCPU},
	})
	commonFlagsOK(t, a)
	for _, f := range []string{"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency",
		"-profile:v", "baseline", "-bf", "0", "-pix_fmt", "yuv420p"} {
		if !contains(a, f) {
			t.Errorf("%q yo'q (§8.2 H.264 baseline) — %v", f, a)
		}
	}
	// -g = fps*gop_s = 20 (§8.2 misoli).
	i := idx(a, "-g")
	if i < 0 || a[i+1] != "20" {
		t.Errorf("-g 20 kutilardi: %v", a)
	}
	if !contains(a, "-f") || !contains(a, "whip") {
		t.Error("-f whip yo'q")
	}
	// WHIP auth: -headers "Authorization: Bearer T".
	hi := idx(a, "-headers")
	if hi < 0 || !strings.Contains(a[hi+1], "Authorization: Bearer T") {
		t.Errorf("WHIP headers yo'q: %v", a)
	}
}

func TestArgs_ReencodeGPU(t *testing.T) {
	a := BuildArgs(Job{
		Src:  Source{URL: "rtsp://cam/x"},
		Dst:  Dest{Scheme: "whip", URL: "http://s/whip"},
		Mode: ModeReencode,
		Enc:  EncParams{FPS: 10, BitrateKbps: 800, MaxrateKbps: 1000, GovSec: 2, Profile: "baseline", Backend: BackendGPU},
	})
	for _, f := range []string{"h264_nvenc", "-rc", "cbr", "-b:v", "800k", "-maxrate", "1M", "-bufsize", "1M"} {
		if !contains(a, f) {
			t.Errorf("%q yo'q (§8.2 GPU) — %v", f, a)
		}
	}
}

// H.265 manba → HEVC dekodlash (GPU: hevc_cuvid; CPU: default) + H.264 kodlagich.
func TestArgs_H265Source(t *testing.T) {
	a := BuildArgs(Job{
		Src:  Source{URL: "rtsp://cam/x", Codec: "h265"},
		Dst:  Dest{Scheme: "whip", URL: "http://s/whip"},
		Mode: ModeReencode,
		Enc:  EncParams{FPS: 10, BitrateKbps: 800, GovSec: 2, Profile: "baseline", Backend: BackendGPU},
	})
	if !contains(a, "hevc_cuvid") {
		t.Errorf("hevc_cuvid dekodlagich yo'q: %v", a)
	}
	if !contains(a, "h264_nvenc") {
		t.Error("h264_nvenc kodlagich yo'q")
	}
}

// §8.2: FFmpeg ≥ 7.1 bo'lmasa -f whip yo'q — versiya parse.
func TestVersion_ParseAndWhipSupport(t *testing.T) {
	tests := []struct {
		line string
		maj  int
		min  int
	}{
		{"ffmpeg version n9.0.2-17-g2a571b6068-20260930 Copyright", 9, 0},
		{"ffmpeg version 7.1 Copyright (c) 2000-2023", 7, 1},
		{"ffmpeg version 7.0.2-static Copyright", 7, 0},
		{"ffmpeg version git-2020-01-01-abc Copyright", 0, 0},
	}
	for _, tc := range tests {
		maj, min, ok := ParseVersionLine(tc.line)
		if tc.maj == 0 {
			if ok {
				t.Errorf("%q: ok=true kutilmadi", tc.line)
			}
			continue
		}
		if !ok || maj != tc.maj || min != tc.min {
			t.Errorf("%q → %d.%d ok=%v", tc.line, maj, min, ok)
		}
	}
	if !HasWhipMuxer("ffmpeg version n9.0.2 Copyright") {
		t.Error("9.0.2 whip'ni qo'llab-quvvatlashi kerak")
	}
	if HasWhipMuxer("ffmpeg version 7.0.2 Copyright") {
		t.Error("7.0.2 whip'ni qo'llab-quvvatlamasligi kerak (§8.2 ≥7.1)")
	}
}

func idx(a []string, s string) int {
	for i, x := range a {
		if x == s {
			return i
		}
	}
	return -1
}
