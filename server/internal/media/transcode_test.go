// S2.7 transcode testlari: golden argumentlar + fake ffmpeg bilan
// jarayon hayoti (start/stop/limit/stats).
package media

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"central-vms/server/internal/gpu"
	"central-vms/server/internal/proto"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func fixturePath(name string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", name)
}

func specNVENC() TranscodeSpec {
	return TranscodeSpec{
		CameraID: "c1", SrcPath: "cam/ag_1/c1", DstPath: "tc/ag_1/c1",
		SrcToken: "SRC", DstToken: "DST", CodecIn: "h265",
		Caps:     gpu.Caps{Backend: gpu.BackendNVENC, Encoder: "h264_nvenc", HEVCDecoder: "hevc_cuvid"},
		RTSPBase: "rtsp://127.0.0.1:8554",
	}
}

func TestFFmpegArgs_Golden(t *testing.T) {
	// nvenc (H.265 manba → cuvid decode)
	got := strings.Join(ffmpegArgs("ffmpeg", specNVENC()), " ")
	wantB, err := os.ReadFile(fixturePath("ffmpeg_args/nvenc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got != strings.TrimSpace(string(wantB)) {
		t.Fatalf("nvenc args farq:\ngot:  %s\nwant: %s", got, strings.TrimSpace(string(wantB)))
	}

	// software (GPU yo'q mashina) — kichik parametrlar bilan
	s := specNVENC()
	s.CodecIn = "h264"
	s.Caps = gpu.Caps{Backend: gpu.BackendNone, Encoder: "libx264"}
	s.Params = proto.TranscodeParams{Width: 640, Height: 360, FPS: 10, BitrateKbps: 600, GopS: 2}
	got2 := strings.Join(ffmpegArgs("ffmpeg", s), " ")
	want2, _ := os.ReadFile(fixturePath("ffmpeg_args/cpu.txt"))
	if got2 != strings.TrimSpace(string(want2)) {
		t.Fatalf("cpu args farq:\ngot:  %s\nwant: %s", got2, strings.TrimSpace(string(want2)))
	}
}

func TestEncodeArgs_VAAPIHasNoNVENCFlags(t *testing.T) {
	a := encodeArgs(gpu.BackendVAAPI, proto.TranscodeParams{})
	joined := strings.Join(a, " ")
	if strings.Contains(joined, "p010le") || strings.Contains(joined, "p2") {
		t.Fatalf("vaapi qatorida nvenc bayroqlari bor: %s", joined)
	}
	if !strings.Contains(joined, "h264_vaapi") && !strings.Contains(joined, "nv12") {
		t.Fatalf("vaapi format belgilanmadi: %s", joined)
	}
}

func TestParseProgress(t *testing.T) {
	b, err := os.ReadFile(fixturePath("progress/sample.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var st Stats
	for _, line := range strings.Split(string(b), "\n") {
		if k, v, ok := ParseProgress(line); ok {
			ApplyProgress(&st, k, v)
		}
	}
	if st.Frames != 120 || st.FPS != 14.98 || st.BitrateKbps != 1198 {
		t.Fatalf("stats: %+v", st)
	}
	if st.OutTimeS < 8.0 || st.OutTimeS > 8.01 {
		t.Fatalf("out_time: %v", st.OutTimeS)
	}
}

func TestTranscoderStartStopStats(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash yo'q")
	}
	tr := NewTranscoder(fixturePath("fakeffmpeg.sh"), 2, discardLogger())
	defer tr.StopAll()

	if err := tr.Start(specNVENC()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if tr.Count() != 1 {
		t.Fatalf("count: %d", tr.Count())
	}
	// Idempotent: ikkinchi Start xato bermaydi va sonni oshirmaydi.
	if err := tr.Start(specNVENC()); err != nil {
		t.Fatalf("takror start: %v", err)
	}
	if tr.Count() != 1 {
		t.Fatalf("takror start count: %d", tr.Count())
	}

	// Progress o'qilishi kerak (fake ffmpeg bosadi).
	deadline := time.Now().Add(3 * time.Second)
	var st Stats
	for time.Now().Before(deadline) {
		if s, ok := tr.Get("c1"); ok && s.Frames > 0 && s.FPS > 0 {
			st = s
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.Frames == 0 {
		t.Fatalf("progress o'qilmadi: %+v", st)
	}
	if st.BitrateKbps != 1200 {
		t.Fatalf("bitrate: %+v", st)
	}

	tr.Stop("c1", "user")
	if tr.Count() != 0 {
		t.Fatalf("stopdan keyin count: %d", tr.Count())
	}
	waitForGone(t, "fakeffmpeg.sh")
}

func TestTranscoderLimit(t *testing.T) {
	tr := NewTranscoder(fixturePath("fakeffmpeg.sh"), 1, discardLogger())
	defer tr.StopAll()
	if err := tr.Start(specNVENC()); err != nil {
		t.Fatal(err)
	}
	s2 := specNVENC()
	s2.CameraID = "c2"
	if err := tr.Start(s2); err != ErrTooMany {
		t.Fatalf("limit kutilardi: %v", err)
	}
}

func TestTranscoderBadBinary(t *testing.T) {
	tr := NewTranscoder(filepath.Join(t.TempDir(), "yoq-binar"), 1, discardLogger())
	defer tr.StopAll()
	if err := tr.Start(specNVENC()); err == nil {
		t.Fatal("noma'lum binar uchun xato kutilardi")
	}
	if tr.Count() != 0 {
		t.Fatalf("muvaffaqiyatsiz startdan keyin count: %d", tr.Count())
	}
}

// waitForGone — fake ffmpeg jarayoni haqiqatan o'lganini tekshirish
// (goleak'tan tashqari: resurs sizib chiqmasligi uchun).
func waitForGone(t *testing.T, name string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("pgrep", "-f", name).Output()
		if err != nil || len(bytes.TrimSpace(out)) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s jarayoni tirik qoldi", name)
}
