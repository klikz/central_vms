package agent

import (
	"time"
)

// Profile — qayta ulanish sekinlashtirish profili (docs/protocol.md §1, §3.3).
type Profile int

const (
	// ProfileNormal — oddiy uzilish: 1, 2, 4, 8 … 60 s (§2/§14, TASKS C1.5).
	ProfileNormal Profile = iota
	// ProfileVersion — 4001 (protocol_version mos emas): 5, 10, 20 … 300 s (§1).
	ProfileVersion
	// ProfileRevoked — 4003/4004 (secret rad etildi): 60, 120, 240 … 300 s (§3.3).
	ProfileRevoked
)

type profileSpec struct {
	base time.Duration
	cap  time.Duration
}

func spec(p Profile) profileSpec {
	switch p {
	case ProfileVersion:
		return profileSpec{base: 5 * time.Second, cap: 300 * time.Second}
	case ProfileRevoked:
		return profileSpec{base: 60 * time.Second, cap: 300 * time.Second}
	default:
		return profileSpec{base: 1 * time.Second, cap: 60 * time.Second}
	}
}

// jitterFrac — ±20 % (§14: "± jitter"). rnd nil bo'lsa jitter qo'llanilmaydi
// (testlarda aniq ketma-ketlik tekshiriladi).
const jitterFrac = 0.20

// Backoff — urinishlar sonini saqlaydi va keyingi kutishni hisoblaydi.
// Uyqu qilmaydi: faqat davrni qaytaradi; uyqu clock.Clock orqali (fake clock testi).
type Backoff struct {
	prof    Profile
	rnd     func() float64
	attempt int
}

// NewBackoff — rnd [0,1) qiymat qaytarishi kerak; nil → jitter'siz (test rejimi).
func NewBackoff(p Profile, rnd func() float64) *Backoff {
	return &Backoff{prof: p, rnd: rnd}
}

// Next — keyingi qayta urishgacha bo'lgan kutish. Har chaqiruvda attempt o'sadi.
func (b *Backoff) Next() time.Duration {
	s := spec(b.prof)
	attempt := b.attempt
	b.attempt++

	d := s.base
	for i := 0; i < attempt && d < s.cap; i++ {
		d *= 2
	}
	if d > s.cap {
		d = s.cap
	}
	if b.rnd == nil {
		return d
	}
	// jitter: d * (1 ± 0.20) — manfiy bo'lmasligi kafolatlanadi
	delta := (b.rnd()*2 - 1) * jitterFrac
	j := time.Duration(float64(d) * delta)
	if j < 0 && -j > d {
		j = -d
	}
	return d + j
}

// Reset — muvaffaqiyatli ulanishdan keyin urinishlar hisobini nolga qaytaradi.
func (b *Backoff) Reset() { b.attempt = 0 }

// Attempt — hozirgacha bo'lgan urinishlar soni (UI/monitoring uchun).
func (b *Backoff) Attempt() int { return b.attempt }

// Profile — profil qaytadi (log/UI uchun).
func (b *Backoff) Profile() Profile { return b.prof }
