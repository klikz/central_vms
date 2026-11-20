package publish

import "sync"

// §10.4: FFmpeg jarayonlari soni ≤ max_local_transcodes + 2
// (remux/re-encode hisobga olingan). max_uploads cheklovi registry'da
// (SetMaxUploads + ErrLimitExceeded).

// ffmpegGate — FFmpeg subprocess'lar uchun sanaq (acquire/release).
// Kapasitet = maxLocalTranscodes + 2.
type ffmpegGate struct {
	mu     sync.Mutex
	max    int
	active int
}

// NewFFmpegGate — maxLocalTranscodes bo'yicha gate (§10.4: +2 zaxira).
// max ≤ 0 → default 4 (CPU-only).
func NewFFmpegGate(maxLocalTranscodes int) *ffmpegGate {
	if maxLocalTranscodes <= 0 {
		maxLocalTranscodes = 4
	}
	return &ffmpegGate{max: maxLocalTranscodes + 2}
}

// TryAcquire — joy boro'lsa true; band bo'lsa false (chaqiruvchi
// transcode_failed/limit_exceeded reason'larini tanlaydi).
func (g *ffmpegGate) TryAcquire() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active >= g.max {
		return false
	}
	g.active++
	return true
}

// Release — jarayon tugagach joy qaytaradi.
func (g *ffmpegGate) Release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active > 0 {
		g.active--
	}
}

// Active — hozirgi FFmpeg jarayonlari soni (selftest/status uchun).
func (g *ffmpegGate) Active() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active
}
