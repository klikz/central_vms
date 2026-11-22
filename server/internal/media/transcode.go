// S2.7: server transcode menejeri — kamera BO'YICHA BITTA FFmpeg jarayoni
// (root §16.2: "N foydalanuvchi 1 kamerani ko'rsa 1 ta FFmpeg").
// Backend argumentlari gpu.Caps'dan quriladi — GPU modeli nomiga
// tarmoq YO'Q (REJA §2.1/5). Manba: MediaMTX RTSP (cam/...), natija:
// MediaMTX RTSP publish (tc/...) — ichki token bilan (§15).
package media

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"central-vms/server/internal/gpu"
	"central-vms/server/internal/proto"
)

// ErrTooMany — server_max_transcodes chegarasi (S2.5 limitlari).
var ErrTooMany = errors.New("server transcode limiti to'ldi")

// TranscodeSpec — bitta kamera uchun topshiriq.
type TranscodeSpec struct {
	CameraID    string
	SrcPath     string // cam/{agent}/{camera}
	DstPath     string // tc/{agent}/{camera}
	SrcToken    string // cam path'ni o'qish uchun read token
	DstToken    string // tc path'ga publish uchun ichki token
	CodecIn     string // h264 | h265
	Caps        gpu.Caps
	Params      proto.TranscodeParams
	RTSPBase    string // rtsp://host:port
	SrcHostPort string // manba (MediaMTX) — localhost
}

// Stats — FFmpeg `-progress` chiqishidan o'qilgan jonli ko'rsatkichlar.
type Stats struct {
	FPS         float64
	BitrateKbps int
	OutTimeS    float64
	Frames      int64
	StartedAt   time.Time
}

// Transcoder — jarayonlar реестri (bitta egasi, mutex bilan).
type Transcoder struct {
	lg         *slog.Logger
	ffmpegPath string
	max        int

	mu        sync.Mutex
	procs     map[string]*transcodeProc
	bad       map[gpu.Backend]bool // ishlamagan backendlar (drayver/xato)
	onFailure func(cameraID string, backend gpu.Backend)
	// onEnd — HAR qanday jarayon tugaganda chaqiriladi (exit kodi
	// muhim emas). Menejer shu orqali tirik publish'da transcode'ni
	// qayta ko'taradi (2026-11-21 server auditidagi asosiy xato).
	onEnd func(cameraID string, elapsed time.Duration, failed bool)
}

// SetOnFailure — jarayon darhol (5 s ichida) yiqilsa chaqiriladi
// ( menejer boshqa backend bilan qayta urinishi uchun).
// SetOnEnd — jarayon tugash habari (elapsed = yashagan vaqt).
func (t *Transcoder) SetOnEnd(f func(cameraID string, elapsed time.Duration, failed bool)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onEnd = f
}

func (t *Transcoder) SetOnFailure(f func(cameraID string, backend gpu.Backend)) {
	t.mu.Lock()
	t.onFailure = f
	t.mu.Unlock()
}

// IsBad — operator/menejer tanlovi uchun: bu backend ishlamagan.
func (t *Transcoder) IsBad(b gpu.Backend) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.bad[b]
}

type transcodeProc struct {
	cancel context.CancelFunc
	cmd    *cmdHandle
	stats  Stats
	spec   TranscodeSpec
}

// cmdHandle — exec.Cmd o'rami (testlar fake binar bilan almashtiradi).
type cmdHandle struct{ c *exec.Cmd }

func NewTranscoder(ffmpegPath string, max int, lg *slog.Logger) *Transcoder {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	if max <= 0 {
		max = 5
	}
	return &Transcoder{lg: lg, ffmpegPath: ffmpegPath, max: max,
		procs: map[string]*transcodeProc{}, bad: map[gpu.Backend]bool{}}
}

// Count — faol transcode'lar (metrics/UI).
func (t *Transcoder) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.procs)
}

// Limit — bir vaqtda yurishi mumkin bo'lgan FFmpeg jarayonlari (sozlama).
func (t *Transcoder) Limit() int { return t.max }

// Get — kamera bo'yicha statistika.
func (t *Transcoder) Get(cameraID string) (Stats, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.procs[cameraID]
	if !ok {
		return Stats{}, false
	}
	return p.stats, true
}

// Start — FFmpeg'ni ishga tushiradi (idempotent: allaqachon bo'lsa — hech narsa).
func (t *Transcoder) Start(spec TranscodeSpec) error {
	t.mu.Lock()
	if _, ok := t.procs[spec.CameraID]; ok {
		t.mu.Unlock()
		return nil
	}
	if len(t.procs) >= t.max {
		t.mu.Unlock()
		return ErrTooMany
	}
	ctx, cancel := context.WithCancel(context.Background())
	args := ffmpegArgs(t.ffmpegPath, spec)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	outRD, outWR, err := os.Pipe()
	if err != nil {
		t.stopLocked(spec.CameraID, &transcodeProc{cancel: cancel})
		return fmt.Errorf("progress pipe: %w", err)
	}
	cmd.Stdout = outWR
	p := &transcodeProc{cancel: cancel, cmd: &cmdHandle{c: cmd}, spec: spec,
		stats: Stats{StartedAt: time.Now()}}
	t.procs[spec.CameraID] = p
	t.mu.Unlock()

	t.lg.Info("transcode boshlandi", "camera", spec.CameraID, "backend", spec.Caps.Backend,
		"encoder", spec.Caps.Encoder, "src", spec.SrcPath, "dst", spec.DstPath)
	if serr := cmd.Start(); serr != nil {
		outRD.Close()
		outWR.Close()
		t.stopLocked(spec.CameraID, p)
		return fmt.Errorf("ffmpeg boshlanmadi: %w", serr)
	}
	go t.progress(spec.CameraID, p, outRD)
	go t.watch(spec.CameraID, p, &stderr, outWR)
	return nil
}

// Stop — kamera bo'yicha jarayonni to'xtatish (graceful: SIGINT, so'ng kill).
func (t *Transcoder) Stop(cameraID, reason string) {
	t.mu.Lock()
	p, ok := t.procs[cameraID]
	t.mu.Unlock()
	if !ok {
		return
	}
	t.lg.Info("transcode to'xtatildi", "camera", cameraID, "reason", reason)
	t.stopLocked(cameraID, p)
}

// StopAll — server o'chayotganda.
func (t *Transcoder) StopAll() {
	t.mu.Lock()
	ids := make([]string, 0, len(t.procs))
	for id := range t.procs {
		ids = append(ids, id)
	}
	t.mu.Unlock()
	for _, id := range ids {
		t.Stop(id, "server_shutdown")
	}
}

func (t *Transcoder) stopLocked(cameraID string, p *transcodeProc) {
	t.mu.Lock()
	if cur, ok := t.procs[cameraID]; ok && cur == p {
		delete(t.procs, cameraID)
	}
	t.mu.Unlock()
	p.cancel() // exec.CommandContext SIGKILL yuboradi
}

// progress — `-progress pipe:1` chizig'larini o'qib Stats'ni yangilaydi.
func (t *Transcoder) progress(cameraID string, p *transcodeProc, rd *os.File) {
	defer rd.Close()
	sc := bufio.NewScanner(io.LimitReader(rd, 1<<20))
	for sc.Scan() {
		k, v, ok := ParseProgress(sc.Text())
		if !ok {
			continue
		}
		t.mu.Lock()
		ApplyProgress(&p.stats, k, v)
		t.mu.Unlock()
	}
}

// watch — jarayon tugashini kuzatish (exit + stderr log).
func (t *Transcoder) watch(cameraID string, p *transcodeProc, stderr *bytes.Buffer, outWR *os.File) {
	outWR.Close() // bolalar pipe yopilsa scanner tugaydi
	t0 := p.stats.StartedAt
	err := p.cmd.c.Wait()
	elapsed := time.Since(t0)
	t.mu.Lock()
	cur, ok := t.procs[cameraID]
	if ok && cur == p {
		delete(t.procs, cameraID)
	}
	fb := t.onFailure
	end := t.onEnd
	t.mu.Unlock()
	if end != nil {
		end(cameraID, elapsed, err != nil)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		t.lg.Warn("transcode tugadi", "camera", cameraID, "err", err, "stderr", msg)
		// Darhol yiqilish — encoder ishlamayapti (drayver/parametr):
		// backendni "yomon" deb belgilaymiz va menejerga xabar qilamiz.
		if elapsed < 5*time.Second && p.spec.Caps.Backend != gpu.BackendSoft {
			t.mu.Lock()
			t.bad[p.spec.Caps.Backend] = true
			t.mu.Unlock()
			if fb != nil {
				fb(cameraID, p.spec.Caps.Backend)
			}
		}
		return
	}
	t.lg.Info("transcode tugadi", "camera", cameraID, "exit", 0)
}

// ffmpegArgs — sof funksiya: backend + parametrlardan FFmpeg qatori.
// Golden testlar bilan bog'langan (fixture: testdata/ffmpeg_args/*.txt).
func ffmpegArgs(ffmpegPath string, s TranscodeSpec) []string {
	src := fmt.Sprintf("%s/%s?token=%s", s.RTSPBase, s.SrcPath, s.SrcToken)
	dst := fmt.Sprintf("%s/%s?token=%s", s.RTSPBase, s.DstPath, s.DstToken)
	a := []string{ffmpegPath, "-hide_banner", "-loglevel", "error", "-nostdin",
		"-progress", "pipe:1"}

	// GPU tezlashtirilgan decode (imkoniyat bo'lsa va manba H.265 bo'lsa).
	if s.Caps.HEVCDecoder != "" && s.CodecIn == "h265" {
		switch s.Caps.Backend {
		case gpu.BackendNVENC:
			a = append(a, "-hwaccel", "cuda", "-c:v", s.Caps.HEVCDecoder)
		case gpu.BackendVAAPI:
			a = append(a, "-hwaccel", "vaapi")
		}
	}
	a = append(a,
		"-rtsp_transport", "tcp",
		"-i", src,
		"-an", // audio transcode S3'da (hozircha ovoz talab qilinmaydi)
		"-c:v", encoderFor(s.Caps),
	)
	a = append(a, encodeArgs(s.Caps.Backend, s.Params)...)
	a = append(a,
		"-f", "rtsp",
		"-rtsp_transport", "tcp",
		dst,
	)
	return a
}

func encoderFor(c gpu.Caps) string {
	if c.Encoder != "" {
		return c.Encoder
	}
	switch c.Backend {
	case gpu.BackendNVENC:
		return "h264_nvenc"
	case gpu.BackendVAAPI:
		return "h264_vaapi"
	case gpu.BackendAMF:
		return "h264_amf"
	default:
		return "libx264"
	}
}

// encodeArgs — backendga xos rate control/preset argumentlari.
// Qiymatlar default (sub profil): 1280x720@15, 1200 kbps CBR, GOP 2 s.
func encodeArgs(b gpu.Backend, p proto.TranscodeParams) []string {
	w, h, fps, br := p.Width, p.Height, p.FPS, p.BitrateKbps
	if w == 0 {
		w = 1280
	}
	if h == 0 {
		h = 720
	}
	if fps == 0 {
		fps = 15
	}
	if br == 0 {
		br = 1200
	}
	maxr := p.MaxrateKbps
	if maxr == 0 {
		maxr = br * 12 / 10
	}
	gops := p.GopS
	if gops == 0 {
		gops = 2
	}
	gop := gops * fps
	scale := fmt.Sprintf("%d:%d", w, h)
	switch b {
	case gpu.BackendNVENC:
		return []string{"-vf", "scale=" + scale, "-r", strconv.Itoa(fps),
			"-preset", "p2", "-tune", "ll", "-rc", "cbr",
			"-b:v", kbps(br), "-maxrate", kbps(maxr), "-bufsize", kbps(maxr * 2),
			"-g", strconv.Itoa(gop), "-pix_fmt", "p010le"}
	case gpu.BackendVAAPI:
		return []string{"-vf", "scale=" + scale + ":format=nv12", "-r", strconv.Itoa(fps),
			"-b:v", kbps(br), "-maxrate", kbps(maxr), "-bufsize", kbps(maxr * 2),
			"-g", strconv.Itoa(gop), "-pix_fmt", "nv12"}
	case gpu.BackendAMF:
		return []string{"-vf", "scale=" + scale, "-r", strconv.Itoa(fps),
			"-rc", "cbr", "-b:v", kbps(br), "-maxrate", kbps(maxr), "-bufsize", kbps(maxr * 2),
			"-g", strconv.Itoa(gop), "-pix_fmt", "nv12"}
	default:
		return []string{"-vf", "scale=" + scale, "-r", strconv.Itoa(fps),
			"-preset", "veryfast", "-tune", "zerolatency", "-rc", "cbr",
			"-b:v", kbps(br), "-maxrate", kbps(maxr), "-bufsize", kbps(maxr * 2),
			"-g", strconv.Itoa(gop), "-pix_fmt", "yuv420p"}
	}
}

func kbps(n int) string { return strconv.Itoa(n) + "k" }

// ParseProgress — FFmpeg `-progress` chizig'ini parse qiladi (sof funksiya;
// golden test: testdata/progress/*.txt).
func ParseProgress(line string) (key, val string, ok bool) {
	k, v, found := strings.Cut(strings.TrimSpace(line), "=")
	if !found || k == "" {
		return "", "", false
	}
	return k, v, true
}

// ApplyProgress — progress juftligini Stats'ga yozadi. Birlik qoldig'i
// ("1198.4kbits/s") tashlab tashlanadi — FFmpeg formati shunday.
func ApplyProgress(st *Stats, key, val string) {
	num := val
	if i := strings.IndexFunc(val, func(r rune) bool {
		return !(r == '.' || r == '-' || r == '+' || (r >= '0' && r <= '9'))
	}); i >= 0 {
		num = val[:i]
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return
	}
	switch key {
	case "frame":
		st.Frames = int64(f)
	case "fps":
		st.FPS = f
	case "bitrate": // "1200.2kbits/s" → kbps
		st.BitrateKbps = int(f)
	case "out_time_ms":
		st.OutTimeS = f / 1e6
	}
}
