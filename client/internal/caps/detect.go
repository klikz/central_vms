package caps

import (
	"context"
	"fmt"
	"os/exec"
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

	return proto.Caps{
		GPU:           PickGPU(ParseEncoders(enc)),
		CPUCores:      ParseCPUCount(ReadFileString("/proc/cpuinfo")),
		RAMMB:         ParseMemMB(ReadFileString("/proc/meminfo")),
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
