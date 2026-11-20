// Package gpu — server quvvatini aniqlash: transcode backend tanlash
// (nvenc → vaapi/amf → software) va imkoniyatlar ro'yxati.
// REJA §2.1/5 (tasdiqlangan): GPU modeli oldindan belgilangan EMAS —
// model nomiga qarab tarmoq taqiqlangan, faqat IMKONIYATLARGA qaraladi.
package gpu

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Backend — FFmpeg transcode backend kaliti (settings/server_transcode_backend).
type Backend string

const (
	BackendNVENC Backend = "nvenc"
	BackendVAAPI Backend = "vaapi"
	BackendAMF   Backend = "amf"
	BackendSoft  Backend = "cpu" // libx264 (software)
	BackendNone  Backend = "none"
)

// Caps — aniqlangan imkoniyatlar (probe natijasi ham shunga yoziladi).
type Caps struct {
	Backend     Backend
	Encoder     string // h264_nvenc | h264_vaapi | h264_amf | libx264
	HEVCDecoder string // hevc_cuvid | hevc_vaapi | "" (software ffmpeg o'zi hal qiladi)
	GPUName     string // faqat audit/log uchun — MANTIQ BUNGA TAYANMAYDI
	Encoders    []string
	Decoders    []string
}

// SelectBackend — safar imkoniyatlar ro'yxatidan backend tanlaydi (sof funksiya,
// fixture testlar shu yerda: testdata/encoders/*.txt).
func SelectBackend(encoders, decoders []string) (Backend, string, string) {
	has := func(list []string, name string) bool {
		for _, n := range list {
			if n == name {
				return true
			}
		}
		return false
	}
	switch {
	case has(encoders, "h264_nvenc"):
		dec := ""
		if has(decoders, "hevc_cuvid") {
			dec = "hevc_cuvid"
		}
		return BackendNVENC, "h264_nvenc", dec
	case has(encoders, "h264_vaapi"):
		dec := ""
		if has(decoders, "hevc_vaapi") {
			dec = "hevc_vaapi"
		}
		return BackendVAAPI, "h264_vaapi", dec
	case has(encoders, "h264_amf"):
		return BackendAMF, "h264_amf", ""
	case has(encoders, "libx264"):
		return BackendSoft, "libx264", ""
	default:
		return BackendNone, "", ""
	}
}

// Detect — ffmpeg -encoders/-decoders + nvidia-smi (ixtiyoriy) orqali aniqlash.
// Xato bo'lsa BackendNone qaytaradi — dastur yiqilmaydi (transcode
// so'ralganda aniq xato xabari beriladi).
func Detect(ffmpegPath string) (Caps, error) {
	enc, err := listCodecs(ffmpegPath, "-encoders", "encoders")
	if err != nil {
		return Caps{Backend: BackendNone}, err
	}
	dec, err := listCodecs(ffmpegPath, "-decoders", "decoders")
	if err != nil {
		return Caps{Backend: BackendNone}, err
	}
	b, e, d := SelectBackend(enc, dec)
	c := Caps{Backend: b, Encoder: e, HEVCDecoder: d, Encoders: enc, Decoders: dec}
	if out, err := exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader").Output(); err == nil {
		c.GPUName = strings.TrimSpace(string(out))
	}
	return c, nil
}

// listCodecs — `ffmpeg -hide_banner <flag>` chiqishidan nomlar ro'yxati.
// Format: " V..... h264_nvenc   H.264 / AVC / MPEG-4 AVC (encoders)..."
func listCodecs(ffmpegPath, flag, kind string) ([]string, error) {
	cmd := exec.Command(ffmpegPath, "-hide_banner", flag)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gpu: ffmpeg %s: %w: %s", flag, err, strings.TrimSpace(stderr.String()))
	}
	return ParseCodecList(string(out)), nil
}

// ParseCodecList — ffmpeg chiqish matnini parse qiladi (golden testlar bilan
// bog'langan; "aqlli" parser yo'q — faqat format qoidasi).
func ParseCodecList(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		// flag maydoni: birinchi belgi turi (V/A/S/D), qolgani nuqta/flag harflari
		// ("V....D", " V..... "). Format qoidasi — "aqlli" parser yo'q (TASKS §0.3).
		if len(f) >= 2 && f[1] != "=" && len(f[0]) == 6 && strings.Count(f[0], ".") >= 4 &&
			strings.ContainsRune("VASD", rune(f[0][0])) {
			names = append(names, f[1])
		}
	}
	return names
}
