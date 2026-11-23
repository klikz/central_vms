package caps

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"central-vms/client/internal/proto"
)

// Detect — joy mashinasi imkoniyatlarini aniqlaydi (§4.1 caps maydonlari).
// ffmpeg topilmasa xato qaytadi — agent ishlayveradi, lekin UI'da ogohlantirish bo'ladi.
func Detect(ctx context.Context, ffmpegPath string) (proto.Caps, error) {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	ver, err := runFF(ctx, ffmpegPath, "-hide_banner", "-version")
	if err != nil {
		return proto.Caps{}, fmt.Errorf("caps: ffmpeg ishga tushmadi: %w", err)
	}
	enc, _ := runFF(ctx, ffmpegPath, "-hide_banner", "-encoders")
	dec, _ := runFF(ctx, ffmpegPath, "-hide_banner", "-decoders")

	// /proc — Linux'ga xos; Windows'da yo'q. CPU uchun universal
	// fallback (runtime.NumCPU), RAM uchun platforma yordamchisi
	// (mem_unix.go / mem_windows.go). 2026-11-23 Windows auditi.
	cores := ParseCPUCount(ReadFileString("/proc/cpuinfo"))
	if cores <= 0 {
		cores = runtime.NumCPU()
	}
	return proto.Caps{
		GPU:           PickGPU(ParseEncoders(enc)),
		CPUCores:      cores,
		RAMMB:         totalRAMMB(),
		FFmpegVersion: ParseFFmpegVersion(ver),
		HEVCDecode:    HEVCDecode(dec),
	}, nil
}

func runFF(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var sb strings.Builder
	cmd.Stdout = &sb
	cmd.Stderr = &sb
	if err := cmd.Run(); err != nil {
		return sb.String(), fmt.Errorf("%w: %v", err, strings.TrimSpace(firstLine(sb.String())))
	}
	return sb.String(), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
