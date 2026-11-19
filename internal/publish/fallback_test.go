package publish

import (
	"context"
	"errors"
	"os"
	"testing"

	"central-vms/client/internal/ffmpeg"
)

// §8.1/§8.2: Go yo'li interop xatosida FFmpeg zaxira publish (-f whip /
// -f rtsp). whip muxeri FFmpeg ≥ 7.1 talab qiladi.

func TestFallback_ArgsWired(t *testing.T) {
	plan := StartPlan{
		Key:    Key{CameraID: "cam1", Profile: "main", Ingest: "rtsp_tcp"},
		Ingest: "rtsp_tcp", URL: "rtsp://srv:8554/c?token=T",
	}
	f, err := StartFallback(context.Background(), plan, "rtsp://cam/1", "h264",
		ffmpeg.EncParams{FPS: 10, BitrateKbps: 800, GovSec: 2, Backend: ffmpeg.BackendCPU},
		ffmpeg.ModeReencode, nil)
	if err != nil {
		t.Fatalf("StartFallback: %v", err)
	}
	defer f.Close()
	if f.PID() <= 0 {
		t.Fatal("jarayon ochilmadi")
	}
	// Manba yo'q — FFmpeg xato bilan chiqadi; reason transcode_failed (§13).
	err = f.Wait()
	if err == nil {
		t.Fatal("buzilgan manba uchun xato kutilardi")
	}
	if r := ReasonFor(err, SideServer); r != "" {
		t.Logf("reason: %q", r)
	}
}

// FFmpeg gate band bo'lsa — limit_exceeded (§10.4).
func TestFallback_GateRejects(t *testing.T) {
	g := NewFFmpegGate(0) // default 4 → cap 6
	for i := 0; i < 6; i++ {
		g.TryAcquire()
	}
	plan := StartPlan{Ingest: "rtsp_tcp", URL: "rtsp://srv/c"}
	_, err := StartFallback(context.Background(), plan, "rtsp://cam/1", "h264",
		ffmpeg.EncParams{}, ffmpeg.ModeCopy, g)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("err = %v, ErrLimitExceeded kutilardi", err)
	}
}

// Real ishga tushirish (server'siz): testsrc → mp4 fayl — progress
// callback'lari va fayl hosil bo'ladi (plumbing tekshiruvi).
func TestFallback_RealRunProgress(t *testing.T) {
	out := t.TempDir() + "/out.mp4"
	h, err := ffmpeg.Run(context.Background(), ffmpeg.Options{
		Path: "ffmpeg",
		Args: []string{"-hide_banner", "-loglevel", "error", "-nostats",
			"-progress", "pipe:1",
			"-f", "lavfi", "-i", "testsrc=duration=1:size=128x72:rate=10",
			"-c:v", "libx264", "-preset", "ultrafast", out},
	})
	if err != nil {
		t.Skipf("ffmpeg ishga tushmadi: %v", err)
	}
	var last ffmpeg.Progress
	n := 0
	h.OnProgress(func(p ffmpeg.Progress) { n++; last = p })
	if err := h.Wait(); err != nil {
		t.Fatalf("ffmpeg Wait: %v", err)
	}
	if n == 0 {
		t.Fatal("progress callback'lari kelmadi")
	}
	// ffmpeg 9 mp4 muxeri out_bytes/fps'ni progress'da bermaydi —
	// frame/out_time_ms/bitrate keladi (stream muxerlarda out_bytes bor).
	if last.Frame < 10 || !last.Done {
		t.Errorf("progress: %+v", last)
	}
	if fi, serr := os.Stat(out); serr != nil || fi.Size() == 0 {
		t.Errorf("chiqish fayli yo'q/bo'sh: %v", serr)
	}
	// §4.6: ffmpeg progress → Stat (bitrate "  71.5kbits/s" → 71).
	st := StatFromProgress(last, "tcp")
	if st.BitrateKbps <= 0 {
		t.Errorf("StatFromProgress bitrate: %+v (progress=%+v)", st, last)
	}
}

// §8.2: whip muxeri versiya tekshiruvi (7.0 → yo'q, 9.0 → bor).
func TestFallback_WhipVersionGate(t *testing.T) {
	if !whipMuxerAvailable("ffmpeg version n9.0.2-17-g2a571b6068 Copyright") {
		t.Error("9.0.2 da whip bo'lishi kerak")
	}
	if whipMuxerAvailable("ffmpeg version 7.0.2 Copyright") {
		t.Error("7.0.2 da whip bo'lmasligi kerak")
	}
}
