package ffmpeg

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// §8.2: exec.CommandContext, process group kill, -progress pipe:1 parse,
// Wait() goroutine, zombie yo'q.

func TestParseProgress_Stream(t *testing.T) {
	in := `frame=120
fps=9.95
bitrate=780.2kbits/s
out_time_ms=12000000
out_bytes=1173000
progress=continue
frame=180
fps=10.02
bitrate=781.0kbits/s
out_bytes=1760000
progress=end
`
	var got []Progress
	err := ScanProgress(strings.NewReader(in), func(p Progress) { got = append(got, p) })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("blok soni = %d, 2 kutilardi", len(got))
	}
	if got[0].Frame != 120 || got[0].FPS != 9.95 || got[0].OutBytes != 1173000 {
		t.Errorf("blok0: %+v", got[0])
	}
	if got[0].Bitrate != "780.2kbits/s" {
		t.Errorf("bitrate = %q", got[0].Bitrate)
	}
	if !got[1].Done {
		t.Error("progress=end → Done=true bo'lishi kerak")
	}
}

func TestParseProgress_BuzilganChiziqlar(t *testing.T) {
	in := "frame=abc\nnovalue\n=out\n=5\nout_bytes=42\nprogress=continue\n"
	var got []Progress
	if err := ScanProgress(strings.NewReader(in), func(p Progress) { got = append(got, p) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].OutBytes != 42 || got[0].Frame != 0 {
		t.Fatalf("nosag'armolar e'tiborsiz qoldirilishi kerak: %+v", got)
	}
}

// `sleep 60` jarayonini guruh sifatida o'ldirish — zombie qolmaydi.
func TestRun_KillProcessGroup(t *testing.T) {
	h, err := Run(context.Background(), Options{Path: "sleep", Args: []string{"60"}})
	if err != nil {
		t.Fatal(err)
	}
	pid := h.PID()
	if pid <= 0 {
		t.Fatalf("PID noto'g'ri: %d", pid)
	}
	if err := h.Stop(2 * time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := h.Wait(); err == nil {
		t.Fatal("o'ldirilgan jarayon xato qaytarmadi")
	}
	// Zombie tekshiruvi: Wait'dan keyin state Z bo'lmasligi kerak.
	if st, serr := procState(pid); serr == nil && st == "Z" {
		t.Errorf("zombie qoldi: pid=%d state=Z", pid)
	}
}

// ctx bekor qilinsa — jarayon o'ldiriladi.
func TestRun_CtxCancelKills(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h, err := Run(ctx, Options{Path: "sleep", Args: []string{"60"}})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	done := make(chan error, 1)
	go func() { done <- h.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("cancel'dan keyin jarayon o'ldirilmadi")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel jarayonni to'xtatmadi")
	}
}

// Muvaffaqiyatli jarayon: exit 0 → Wait nil.
func TestRun_Success(t *testing.T) {
	h, err := Run(context.Background(), Options{Path: "true"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Wait(); err != nil {
		t.Fatalf("Wait = %v, nil kutilardi", err)
	}
}

// Progress callback: FFmpeg'siz, `-progress` formatida chiqaruvchi `sh`.
func TestRun_ProgressCallback(t *testing.T) {
	h, err := Run(context.Background(), Options{
		Path: "sh",
		Args: []string{"-c", "printf 'frame=1\\nout_bytes=10\\nprogress=continue\\nframe=2\\nout_bytes=20\\nprogress=end\\n'"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var ps []Progress
	h.OnProgress(func(p Progress) { ps = append(ps, p) })
	if err := h.Wait(); err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps[1].Frame != 2 || !ps[1].Done {
		t.Fatalf("progress callback: %+v", ps)
	}
}

// Noma'lum bajaruvchi — aniq xato (panic yo'q).
func TestRun_YoqBajaruvchi(t *testing.T) {
	if _, err := Run(context.Background(), Options{Path: "/nonexistent/ffmpeg-xyz"}); err == nil {
		t.Fatal("xato kutilardi")
	}
}

// --- yordamchi: /proc state (Linux) ---
func procState(pid int) (string, error) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", err
	}
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return "", os.ErrInvalid
	}
	return f[2], nil
}
