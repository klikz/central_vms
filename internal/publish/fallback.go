package publish

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"central-vms/client/internal/ffmpeg"
)

// §8.1/§8.2: FFmpeg zaxira publish — Go yo'li (pion/gortsplib) interop
// xatosida `-f whip` / `-f rtsp` muxerlari orqali.

// ErrWhipUnsupported — FFmpeg < 7.1: `-f whip` muxeri yo'q (§8.2).
// Chaqiruvchi rtsp_tcp rejayl planini tanlaydi.
var ErrWhipUnsupported = errors.New("publish: FFmpeg -f whip qo'llab-quvvatlanmaydi")

// Fallback — FFmpeg orqali publish jarayoni + progress→Stat ko'prigi.
type Fallback struct {
	h    *ffmpeg.Handle
	gate *ffmpegGate // nil → gate yo'q

	last ffmpeg.Progress
	// mu himoyasi: progress goroutine yozadi, Stats() o'qiydi.
	mu   sync.Mutex
	done bool
}

// StartFallback — arg matritsasini quradi (§8.1/§8.2), gate oladi,
// jarayonni ochadi. gate nil bo'lmasa §10.4 capi qo'llanadi.
func StartFallback(ctx context.Context, plan StartPlan, srcURL, codec string,
	enc ffmpeg.EncParams, mode ffmpeg.Mode, gate *ffmpegGate) (*Fallback, error) {

	if plan.Ingest == "whip" && !whipMuxerAvailable(runFFmpegVersion(ctx)) {
		return nil, ErrWhipUnsupported
	}
	if gate != nil {
		if !gate.TryAcquire() {
			return nil, ErrLimitExceeded
		}
	}
	args := ffmpeg.BuildArgs(ffmpeg.Job{
		Src: ffmpeg.Source{URL: srcURL, Codec: codec},
		Dst: ffmpeg.Dest{Scheme: schemeOf(plan.Ingest), URL: plan.URL,
			HeaderName: plan.HeaderName, HeaderValue: plan.HeaderValue},
		Mode: mode,
		Enc:  enc,
	})
	h, err := ffmpeg.Run(ctx, ffmpeg.Options{Args: args})
	if err != nil {
		if gate != nil {
			gate.Release()
		}
		return nil, err
	}
	f := &Fallback{h: h, gate: gate}
	h.OnProgress(func(p ffmpeg.Progress) {
		f.mu.Lock()
		f.last = p
		f.mu.Unlock()
	})
	return f, nil
}

func schemeOf(ingest string) string {
	if ingest == "whip" {
		return "whip"
	}
	return "rtsp"
}

// PID — FFmpeg jarayoni PID'i (guruh bilan).
func (f *Fallback) PID() int { return f.h.PID() }

// Stop — gracieuz to'xtatish (SIGINT→SIGKILL).
func (f *Fallback) Stop() error { return f.h.Stop(3 * time.Second) }

// Wait — jarayon tugashini kutadi (gate'ni qaytaradi).
func (f *Fallback) Wait() error {
	err := f.h.Wait()
	f.mu.Lock()
	if !f.done {
		f.done = true
		if f.gate != nil {
			f.gate.Release()
		}
	}
	f.mu.Unlock()
	return err
}

// Close — Stop + Wait.
func (f *Fallback) Close() error {
	_ = f.Stop()
	return f.Wait()
}

// Stats — oxirgi progress'dan §4.6 Stat'ı.
func (f *Fallback) Stats() Stat {
	f.mu.Lock()
	p := f.last
	f.mu.Unlock()
	return StatFromProgress(p, "tcp")
}

// StatFromProgress — ffmpeg `-progress` → media_stats (§4.6).
// bitrate "780.2kbits/s" → kbps int.
func StatFromProgress(p ffmpeg.Progress, transport string) Stat {
	return Stat{
		BytesSent:   p.OutBytes,
		FPS:         p.FPS,
		BitrateKbps: parseBitrateKbps(p.Bitrate),
		Transport:   transport,
	}
}

func parseBitrateKbps(s string) int {
	s = strings.TrimSuffix(s, "kbits/s")
	s = strings.TrimSpace(s)
	if s == "" || s == "N/A" {
		return 0
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return int(v)
	}
	return 0
}

// whipMuxerAvailable — §8.2: FFmpeg ≥ 7.1 bo'lsa -f whip bor.
func whipMuxerAvailable(versionLine string) bool {
	return ffmpeg.HasWhipMuxer(versionLine)
}

// runFFmpegVersion — `ffmpeg -version` birinchi satri (xato bo'lsa "").
func runFFmpegVersion(ctx context.Context) string {
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "ffmpeg", "-version").Output()
	if err != nil {
		return ""
	}
	if i := strings.IndexByte(string(out), '\n'); i > 0 {
		return string(out[:i])
	}
	return string(out)
}
