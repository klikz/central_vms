// Package ffmpeg — FFmpeg subprocess boshqaruvi (docs/protocol.md §8.2):
// exec.CommandContext, process group kill, -progress pipe:1 parse,
// Wait() goroutine, zombie yo'q.
package ffmpeg

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// ErrNotStopped — Stop yetarli bo'lmadi (ichki holat).
var ErrNotStopped = errors.New("ffmpeg: jarayon to'xtamadi")

// Progress — `-progress pipe:1` birlari (§8.2: out_bytes, fps, bitrate).
type Progress struct {
	Frame     int
	FPS       float64
	Bitrate   string
	OutBytes  int64
	OutTimeMS int64
	Done      bool // progress=end
}

// ScanProgress — key=value bloklarini `progress=` satrida yakunlaydi.
// Nosag'arma chiziqlar e'tiborga olinmaydi.
func ScanProgress(r io.Reader, fn func(Progress)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), 64*1024)
	var p Progress
	have := false
	for sc.Scan() {
		line := sc.Text()
		k, v, ok := cut(line, "=")
		if !ok {
			continue
		}
		have = true
		switch k {
		case "frame":
			p.Frame, _ = strconv.Atoi(v)
		case "fps":
			p.FPS, _ = strconv.ParseFloat(v, 64)
		case "bitrate":
			p.Bitrate = v
		case "out_bytes":
			p.OutBytes, _ = strconv.ParseInt(v, 10, 64)
		case "out_time_ms":
			p.OutTimeMS, _ = strconv.ParseInt(v, 10, 64)
		case "progress":
			p.Done = v == "end"
			if have {
				fn(p)
			}
			p = Progress{}
			have = false
		}
	}
	return sc.Err()
}

func cut(s, sep string) (string, string, bool) {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return s[:i], s[i+len(sep):], true
		}
	}
	return "", "", false
}

// Options — bajaruvchi va argumentlar (chaqiruvchi §8.2 flag'larini qo'shadi).
type Options struct {
	Path string // bo'sh → "ffmpeg"
	Args []string
	Env  []string
}

// Handle — ishlayotgan subprocess.
type Handle struct {
	cmd  *exec.Cmd
	fin  chan struct{} // Wait tugagach yopiladi; werr shundan oldin yoziladi
	werr error

	mu     sync.Mutex
	onProg func(Progress)
	errBuf *tail // stderr oxiri (xato xabari uchun)
}

// Run — jarayonni guruh sifatida ochadi (Setpgid): kill(-pid) butun
// daraxtni o'ldiradi — zombie qolmaydi (§8.2).
func Run(ctx context.Context, o Options) (*Handle, error) {
	path := o.Path
	if path == "" {
		path = "ffmpeg"
	}
	cmd := exec.CommandContext(ctx, path, o.Args...)
	cmd.Env = o.Env
	cmd.SysProcAttr = sysProcAttr()
	// ctx bekor qilinganda: butun guruhga SIGKILL (ffmpeg bolalari bilan;
	// Windows'da — Kill(), procsys_windows.go).
	cmd.Cancel = func() error { return killForce(cmd) }
	cmd.WaitDelay = 3 * time.Second

	h := &Handle{cmd: cmd, fin: make(chan struct{}), errBuf: newTail(4096)}
	cmd.Stderr = h.errBuf

	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		// Har callback'da onProg dinamik o'qiladi — Run'dan keyin
		// OnProgress o'rnatish ham ishlaydi.
		_ = ScanProgress(out, func(p Progress) {
			h.mu.Lock()
			fn := h.onProg
			h.mu.Unlock()
			if fn != nil {
				fn(p)
			}
		})
	}()
	go func() { h.werr = cmd.Wait(); close(h.fin) }()
	return h, nil
}

// PID — jarayon PID'i (guruh ID'i bilan bir xil).
func (h *Handle) PID() int {
	if h.cmd.Process == nil {
		return 0
	}
	return h.cmd.Process.Pid
}

// OnProgress — progress callback'ni o'rnatadi (Run'dan keyin ham).
func (h *Handle) OnProgress(fn func(Progress)) {
	h.mu.Lock()
	h.onProg = fn
	h.mu.Unlock()
}

// Stop — avval SIGINT (muxer yopiladi), grace o'tsa SIGKILL (guruh).
func (h *Handle) Stop(grace time.Duration) error {
	pid := h.PID()
	if pid <= 0 {
		return nil
	}
	_ = killGraceful(h.cmd) // gracieuz yopilish (Unix: SIGINT; Win: Kill)
	select {
	case <-h.fin:
		return nil
	case <-time.After(grace):
		return killForce(h.cmd)
	}
}

// Wait — jarayon tugashini kutadi (idempotent; Stop bilan raqobatlashmaydi).
// Xato bo'lsa stderr oxiri qo'shiladi.
func (h *Handle) Wait() error {
	<-h.fin
	if h.werr != nil {
		tail := h.errBuf.String()
		if tail != "" {
			return errors.New(h.werr.Error() + ": " + tail)
		}
	}
	return h.werr
}

// tail — cheklangan halqali buffer (stderr xati xabari uchun).
type tail struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newTail(max int) *tail { return &tail{buf: make([]byte, 0, max), max: max} }

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	t.mu.Unlock()
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
