package cams

import (
	"sync"
	"time"

	"central-vms/client/internal/clock"
)

// Kodek probe jadvali (docs/protocol.md §12.3–12.4).
// Uch trigger: kamera ulanganda + kuniga 1 marta + publish fail'da darhol.
// Takrorlanish bloki: bir kamera uchun bir vaqtda bitta probe.
// §12.4: kamera publish band bo'lsa probe KECHIKTIRILADI (TryStart rad etadi
// — chaqiruvchi keyinroq qayta urinishi mumkin), probe publish'ni to'xtatmaydi.

// Trigger — kodek probe ishga tushish sababi.
type Trigger string

const (
	TriggerConnect     Trigger = "connect"      // kamera ulanganda
	TriggerDaily       Trigger = "daily"        // kuniga 1 marta
	TriggerPublishFail Trigger = "publish_fail" // darhol (full_probe_on_publish_fail)
)

// CodecScheduler — kim qachon kodek probe o'tkazishi mumkinligini hal qiladi.
// Holat xotirada (state.json'ga yozish chaqiruvchining ishi).
type CodecScheduler struct {
	clk   clock.Clock
	daily time.Duration

	// IsBusy — kamera hozir publish holatida bo'lsa true (§12.4).
	// nil bo'lsa bandlik hisobga olinmaydi.
	IsBusy func(id string) bool

	mu      sync.Mutex
	running map[string]bool
	last    map[string]time.Time // oxirgi tugagan probe vaqti
}

// NewCodecScheduler — daily=0 bo'lsa 24 soat (§12.3 "kuniga 1").
func NewCodecScheduler(clk clock.Clock, daily time.Duration) *CodecScheduler {
	if clk == nil {
		clk = clock.Real{}
	}
	if daily <= 0 {
		daily = 24 * time.Hour
	}
	return &CodecScheduler{clk: clk, daily: daily,
		running: map[string]bool{}, last: map[string]time.Time{}}
}

// TryStart — ruxsat so'raydi. Haqiqiy probe boshlanmasa Finish chaqirilmaydi.
func (s *CodecScheduler) TryStart(id string, trig Trigger) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Takrorlanish bloki: bir kamera uchun bir vaqtda bitta probe.
	if s.running[id] {
		return false
	}
	// §12.4: publish band — kechiktiramiz (rad), to'xtatmaymiz.
	if s.IsBusy != nil && s.IsBusy(id) {
		return false
	}
	now := s.clk.Now()
	switch trig {
	case TriggerDaily:
		// Kuniga 1 marta: oxirgi probe'dan daily o'tmagan bo'lsa rad.
		if last, ok := s.last[id]; ok && now.Sub(last) < s.daily {
			return false
		}
	case TriggerConnect, TriggerPublishFail:
		// ulanganda va publish fail'da darhol — daily oynasiga qaramaydi.
	default:
		return false // noma'lum trigger — ehtiyotkorlik bilan rad
	}
	s.running[id] = true
	return true
}

// Finish — probe tugadi: running tozalanadi, last yangilanadi.
func (s *CodecScheduler) Finish(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, id)
	s.last[id] = s.clk.Now()
}

// Running — faol probe'lar soni (kuzatuv/test).
func (s *CodecScheduler) Running() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.running)
}
