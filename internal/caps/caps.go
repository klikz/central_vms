// Package caps — joy mashinasi imkoniyatlari provasi (docs/protocol.md §4.1 caps).
//
// GPU turi, CPU yadrolari, RAM, FFmpeg versiyasi va kodek imkoniyatlari —
// bular `hello` xabarida serverga yuboriladi va transcode siyosati shundan kelib chiqadi.
// Parse funksiyalari sof (fixture bilan testlanadi); Detect tizimga murojaat qiladi.
package caps

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Encoders — "ffmpeg -encoders" chiqishidagi nomlar to'plami.
type Encoders map[string]bool

// FFmpeg "-encoders" satri: ` V....D h264_nvenc   NVIDIA NVENC H.264 encoder (codec h264)`
// 6 ta bayroq belgisi + nom + tavsif. Sarlavha qatorlari (` V..... = Video`) mos kelmaydi.
var encLine = regexp.MustCompile(`^\s*[VASX][.FVSPBD]{5}\s+([A-Za-z0-9_]+)\s+\S`)

// ParseEncoders — `ffmpeg -encoders` matnini parser qiladi (fixture: testdata/ffmpeg-encoders.txt).
func ParseEncoders(text string) Encoders {
	out := Encoders{}
	for _, line := range strings.Split(text, "\n") {
		if m := encLine.FindStringSubmatch(line); m != nil {
			out[m[1]] = true
		}
	}
	return out
}

// Has — nom bo'lsa true.
func (e Encoders) Has(name string) bool { return e[name] }

// PickGPU — H.264 GPU kodlagichni tanlash (§4: zaxira tartibi nvenc → vaapi → amf → none).
func PickGPU(e Encoders) string {
	switch {
	case e.Has("h264_nvenc"):
		return "nvenc"
	case e.Has("h264_vaapi"):
		return "vaapi"
	case e.Has("h264_amf"):
		return "amf"
	default:
		return "none"
	}
}

// HasCPUx264 — CPU zaxira kodlagich (libx264) bormi.
func HasCPUx264(e Encoders) bool { return e.Has("libx264") || e.Has("libopenh264") }

// HEVCDecode — `ffmpeg -decoders` matnida HEVC dekod bormi (profillar uchun, §12).
func HEVCDecode(decodersText string) bool {
	for _, name := range []string{"hevc_cuvid", "hevc_vaapi", "hevc_qsv", "hevc"} {
		if strings.Contains(decodersText, name) {
			return true
		}
	}
	return false
}

// ParseCPUCount — /proc/cpuinfo dagi "processor" satrlari soni.
func ParseCPUCount(cpuinfo string) int {
	n := 0
	for _, line := range strings.Split(cpuinfo, "\n") {
		if strings.HasPrefix(line, "processor") {
			n++
		}
	}
	if n == 0 {
		return 1
	}
	return n
}

var memLine = regexp.MustCompile(`^MemTotal:\s+(\d+)\s+kB`)

// ParseMemMB — /proc/meminfo MemTotal → MB.
func ParseMemMB(meminfo string) int {
	for _, line := range strings.Split(meminfo, "\n") {
		if m := memLine.FindStringSubmatch(line); m != nil {
			kb, err := strconv.Atoi(m[1])
			if err != nil || kb <= 0 {
				break
			}
			return kb / 1024
		}
	}
	return 0
}

var ffver = regexp.MustCompile(`ffmpeg version (\S+)`)

// ParseFFmpegVersion — `ffmpeg -version` birinchi satridan versiya.
func ParseFFmpegVersion(out string) string {
	if m := ffver.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// ReadFileString — /proc fayllari uchun kichik yordamchi (xato bo'lsa bo'sh satr).
func ReadFileString(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}
