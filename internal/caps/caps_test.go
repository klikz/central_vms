package caps

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Skipf("fixture yo'q (%v) — `make fixtures` bajaring", err)
	}
	return string(b)
}

func TestParseEncoders_Fixture(t *testing.T) {
	enc := ParseEncoders(fixture(t, "ffmpeg-encoders.txt"))
	if len(enc) == 0 {
		t.Fatal("fixture'dan encoder topilmadi")
	}
	if !HasCPUx264(enc) {
		t.Fatal("libx264 topilmadi — fixture noto'g'ri")
	}
	// Bu mashinada RTX 4090 bor: nvenc bo'lishi kutiladi (fixture shu mashinada generatsiya qilingan)
	if got := PickGPU(enc); got != "nvenc" {
		t.Fatalf("GPU=%q, kutilgan nvenc", got)
	}
}

func TestParseEncoders_GPUYoqMashina(t *testing.T) {
	// Soxta chiqish: GPU yo'q, faqat CPU kodlagichlar
	out := `Encoders:
 V..... h264_libsvtav1       libsvtav1
 V..... libx264              libx264 H.264 / AVC / MPEG-4 AVC / MPEG-4 part 10
 V..... libopenh264          libopenh264 H.264
`
	enc := ParseEncoders(out)
	if got := PickGPU(enc); got != "none" {
		t.Fatalf("GPU=%q, kutilgan none", got)
	}
	if !HasCPUx264(enc) {
		t.Fatal("libx264 topilmadi")
	}
}

func TestPickGPU_Tartib(t *testing.T) {
	cases := []struct{ in, want string }{
		{"h264_nvenc h264_vaapi", "nvenc"},
		{"h264_vaapi h264_amf", "vaapi"},
		{"h264_amf", "amf"},
		{"", "none"},
	}
	for _, c := range cases {
		e := Encoders{}
		for _, n := range splitWords(c.in) {
			e[n] = true
		}
		if got := PickGPU(e); got != c.want {
			t.Fatalf("%q → %q, kutilgan %q", c.in, got, c.want)
		}
	}
}

func splitWords(s string) []string { return fields(s) }

func fields(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func TestParseCPUvaRAM(t *testing.T) {
	cpuinfo := "processor\t: 0\nmodel name\t: X\nprocessor\t: 1\nprocessor\t: 2\n"
	if got := ParseCPUCount(cpuinfo); got != 3 {
		t.Fatalf("yadrolar=%d, kutilgan 3", got)
	}
	if got := ParseCPUCount(""); got != 1 {
		t.Fatalf("bo'sh cpuinfo=%d, kutilgan 1", got)
	}
	mem := "MemTotal:       16384000 kB\nMemFree: 100 kB\n"
	if got := ParseMemMB(mem); got != 16000 {
		t.Fatalf("RAM=%d MB, kutilgan 16000", got)
	}
	if got := ParseMemMB("MemTotal: x kB"); got != 0 {
		t.Fatalf("buzilgan meminfo=%d", got)
	}
	if got := ParseMemMB(""); got != 0 {
		t.Fatalf("bo'sh meminfo=%d", got)
	}
}

func TestParseFFmpegVersion(t *testing.T) {
	out := "ffmpeg version n9.0.2-17-g2a571b6068-20260930 Copyright (c) 2000-2026\nlibavutil 59.0.0\n"
	if got := ParseFFmpegVersion(out); got != "n9.0.2-17-g2a571b6068-20260930" {
		t.Fatalf("versiya=%q", got)
	}
	if got := ParseFFmpegVersion("yoq"); got != "" {
		t.Fatalf("topolmagan holda=%q, kutilgan bo'sh", got)
	}
}

func TestHEVCDecode(t *testing.T) {
	cases := map[string]bool{
		"V..... hevc_cuvid": true,
		"V..... hevc_vaapi": true,
		"V..... hevc":       true,
		"V..... h264":       false,
		"":                  false,
	}
	for in, want := range cases {
		if got := HEVCDecode(in); got != want {
			t.Fatalf("%q → %v, kutilgan %v", in, got, want)
		}
	}
}

// Bu mashinada haqiqiy ffmpeg bilan tekshiruv (integration, tez)
func TestDetect_HaqiqiyFFmpeg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := Detect(ctx, "")
	if err != nil {
		t.Skipf("ffmpeg yo'q: %v", err)
	}
	if c.FFmpegVersion == "" {
		t.Fatal("ffmpeg_version bo'sh")
	}
	if c.CPUCores < 1 || c.RAMMB < 128 {
		t.Fatalf("tizim ko'rsatkichlari: %+v", c)
	}
	switch c.GPU {
	case "nvenc", "vaapi", "amf", "none":
	default:
		t.Fatalf("gpu=%q", c.GPU)
	}
	t.Logf("caps: gpu=%s cores=%d ram=%dMB ffmpeg=%s hevc_decode=%v",
		c.GPU, c.CPUCores, c.RAMMB, c.FFmpegVersion, c.HEVCDecode)
}

func TestDetect_FFmpegYoq(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Detect(ctx, "/not-yoq/ffmpeg"); err == nil {
		t.Fatal("ffmpeg bo'lmasa xato kutilmadi")
	}
}
