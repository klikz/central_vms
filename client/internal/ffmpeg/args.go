package ffmpeg

import (
	"regexp"
	"strconv"
)

// §8.1/§8.2: FFmpeg argument matritsasi (remux / re-encode / whip-rtsp chiqish).

// Mode — oqim bilan ishlash usuli.
type Mode int

const (
	ModeCopy     Mode = iota // -c copy remux (C3.13)
	ModeReencode             // qayta kodlash (C3.11/C3.12)
)

// Backend — kodlagich tanlovi (server `caps` dan, §5.2).
const (
	BackendCPU = "cpu"
	BackendGPU = "gpu"
)

type Source struct {
	URL   string // rtsp://… (kamera)
	Codec string // "h264" | "h265" (bo'sh → avtomatik)
}

type Dest struct {
	Scheme      string // "whip" | "rtsp"
	URL         string // token allaqachon ichida (rtsp) yoki header orqali (whip)
	HeaderName  string // whip uchun "Authorization"
	HeaderValue string
}

// EncParams — media_start.transcode.params (§5.2).
type EncParams struct {
	Width, Height, FPS, BitrateKbps, MaxrateKbps, GovSec int
	Profile                                              string // "baseline"
	Backend                                              string // cpu | gpu
}

// Job — BuildArgs uchun kirish.
type Job struct {
	Src  Source
	Dst  Dest
	Mode Mode
	Enc  EncParams
}

// BuildArgs — §8.2 qoidalariga muvofiq to'liq argumentlar ro'yxati.
func BuildArgs(j Job) []string {
	a := []string{
		"-hide_banner", "-loglevel", "error", "-nostats", "-progress", "pipe:1",
		"-rtsp_transport", "tcp", "-max_delay", "500000",
		"-analyzeduration", "500000", "-probesize", "500000",
		"-fflags", "+discardcorrupt",
	}
	if j.Src.Codec == "h265" && j.Mode == ModeReencode && j.Enc.Backend == BackendGPU {
		a = append(a, "-c:v", "hevc_cuvid") // GPU HEVC dekodlash (§8.1)
	}
	a = append(a, "-i", j.Src.URL, "-an")

	switch j.Mode {
	case ModeCopy:
		a = append(a, "-c", "copy")
	case ModeReencode:
		a = append(a, encodeFlags(j.Enc)...)
	}

	if j.Dst.Scheme == "whip" {
		if j.Dst.HeaderName != "" && j.Dst.HeaderValue != "" {
			a = append(a, "-headers", j.Dst.HeaderName+": "+j.Dst.HeaderValue)
		}
		a = append(a, "-f", "whip", j.Dst.URL)
		return a
	}
	a = append(a, "-f", "rtsp", j.Dst.URL)
	return a
}

// encodeFlags — §8.2 H.264 baseline/zerolatency qoidalari (GPU yoki CPU).
func encodeFlags(e EncParams) []string {
	fps := e.FPS
	if fps <= 0 {
		fps = 10
	}
	gov := e.GovSec
	if gov <= 0 {
		gov = 2
	}
	g := fps * gov
	br := e.BitrateKbps
	if br <= 0 {
		br = 800
	}
	mr := e.MaxrateKbps
	if mr <= 0 {
		mr = br + 200
	}
	prof := e.Profile
	if prof == "" {
		prof = "baseline"
	}
	if e.Backend == BackendGPU {
		return []string{"-c:v", "h264_nvenc", "-preset", "p1", "-tune", "ll",
			"-rc", "cbr", "-b:v", kfmt(br), "-maxrate", kfmt(mr), "-bufsize", kfmt(mr * 2),
			"-g", itoa(g), "-profile:v", prof}
	}
	return []string{"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency",
		"-profile:v", prof, "-bf", "0", "-pix_fmt", "yuv420p",
		"-g", itoa(g), "-r", itoa(fps),
		"-b:v", kfmt(br), "-maxrate", kfmt(mr), "-bufsize", kfmt(mr * 2)}
}

func kfmt(kbps int) string {
	if kbps >= 1000 && kbps%1000 == 0 {
		return itoa(kbps/1000) + "M"
	}
	return itoa(kbps) + "k"
}

func itoa(n int) string { return strconv.Itoa(n) }

var versionRe = regexp.MustCompile(`ffmpeg version\s+n?(\d+)\.(\d+)`)

// ParseVersionLine — "ffmpeg version n9.0.2-17-…" → (9, 0, true).
func ParseVersionLine(line string) (maj, min int, ok bool) {
	m := versionRe.FindStringSubmatch(line)
	if m == nil {
		return 0, 0, false
	}
	maj, _ = strconv.Atoi(m[1])
	min, _ = strconv.Atoi(m[2])
	return maj, min, true
}

// HasWhipMuxer — §8.2: `-f whip` FFmpeg ≥ 7.1 da bor.
func HasWhipMuxer(versionLine string) bool {
	maj, min, ok := ParseVersionLine(versionLine)
	if !ok {
		return false
	}
	return maj > 7 || (maj == 7 && min >= 1)
}
