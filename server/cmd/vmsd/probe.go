package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"central-vms/server/internal/config"
)

// runProbe — `make probe` / `vmsd --probe` (root §4.2, §16.3).
// S0: muhitni tekshirish va GPU/ffmpeg haqida fakt chiqarish.
// S2: NVENC/NVDEC parallel sessiya limiti yoki software sig'imi O'LCHANADI
// (internal/gpu + bir necha test encode) va natija `settings` ga yoziladi.
// GPU modeli nomiga qarab tarmoq QILINMAYDI — faqat imkoniyatlarga qaraladi
// (REJA §2.1/5: model o'zgaruvchan).
func runProbe(lg *slog.Logger, cfg config.Config) error {
	lg.Info("probe boshlandi", slog.String("ffmpeg", cfg.Media.FFmpegPath))

	encs, decs, err := ffmpegCodecs(cfg.Media.FFmpegPath)
	if err != nil {
		lg.Warn("ffmpeg topilmadi yoki javob bermadi — transcode zaxirasi joy GPU/CPU'ga o'tadi",
			slog.Any("err", err))
		fmt.Println("probe: ffmpeg IMKONIYATLARI: topilmadi")
	} else {
		backend := pickBackend(encs, decs)
		fmt.Printf("probe: ffmpeg transcode backend (taxmin): %s\n", backend)
		fmt.Printf("probe: encoderlar: %s\n", strings.Join(filter(encs, "h264_"), ","))
		fmt.Printf("probe: dekodler:   %s\n", strings.Join(filter(decs, "hevc_"), ","))
	}

	if out, err := exec.Command("nvidia-smi", "--query-gpu=name,memory.total",
		"--format=csv,noheader").Output(); err == nil {
		fmt.Printf("probe: GPU: %s\n", strings.TrimSpace(string(out)))
	} else {
		fmt.Println("probe: GPU: nvidia-smi yo'q (NVIDIA emas yoki drayver yo'q) — " +
			"vaapi/amf/software yo'li tekshiriladi")
	}
	fmt.Println("probe: S2'da parallel sessiya limiti o'lchanib settings.server_max_transcodes ga yoziladi")
	return nil
}

// ffmpegCodecs — `ffmpeg -hide_banner -encoders/-decoders` chiqishi (nomlar).
func ffmpegCodecs(path string) (encoders, decoders []string, err error) {
	list := func(flagName string) ([]string, error) {
		cmd := exec.Command(path, "-hide_banner", flagName)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("ffmpeg %s: %w: %s", flagName, err, strings.TrimSpace(stderr.String()))
		}
		var names []string
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			// Format: " V..... h264_nvenc   ..." (flag so'zi nuqta bilan to'ldirilgan).
			if len(f) >= 3 && len(f[0]) == 6 && strings.Count(f[0], ".") >= 5 {
				names = append(names, f[1])
			}
		}
		return names, nil
	}
	encoders, err = list("-encoders")
	if err != nil {
		return nil, nil, err
	}
	decoders, err = list("-decoders")
	if err != nil {
		return nil, nil, err
	}
	return encoders, decoders, nil
}

// pickBackend — imkoniyatlar bo'yicha backend tartibib (nvenc → vaapi/amf → software).
// S2'da internal/gpu paketi to'liq versiyasini yozadi (fixture testlar bilan).
func pickBackend(encs, decs []string) string {
	has := func(list []string, name string) bool {
		for _, n := range list {
			if n == name {
				return true
			}
		}
		return false
	}
	if has(encs, "h264_nvenc") && has(decs, "hevc_cuvid") {
		return "nvenc"
	}
	if has(encs, "h264_vaapi") || has(encs, "h264_amf") {
		return "vaapi/amf"
	}
	if has(encs, "libx264") {
		return "software(libx264)"
	}
	return "yo'q"
}

func filter(list []string, prefix string) []string {
	var out []string
	for _, n := range list {
		if strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	return out
}
