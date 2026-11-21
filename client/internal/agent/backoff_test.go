package agent

import (
	"testing"
	"time"
)

func seq(b *Backoff, n int) []time.Duration {
	out := make([]time.Duration, n)
	for i := range out {
		out[i] = b.Next()
	}
	return out
}

func eqSeq(got []time.Duration, want ...time.Duration) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestBackoff_NormalSequence(t *testing.T) {
	b := NewBackoff(ProfileNormal, nil)
	got := seq(b, 9)
	want := []time.Duration{
		1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second, 60 * time.Second, 60 * time.Second, 60 * time.Second,
	}
	if !eqSeq(got, want...) {
		t.Fatalf("normal ketma-ketlik:\n  oldi=%v\n  kutilgan=%v", got, want)
	}
	if b.Attempt() != 9 {
		t.Fatalf("attempt=%d", b.Attempt())
	}
	b.Reset()
	if b.Attempt() != 0 || b.Next() != time.Second {
		t.Fatal("Reset ishlamadi")
	}
}

func TestBackoff_VersionProfile(t *testing.T) {
	b := NewBackoff(ProfileVersion, nil)
	got := seq(b, 8)
	want := []time.Duration{
		5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second,
		80 * time.Second, 160 * time.Second, 300 * time.Second, 300 * time.Second,
	}
	if !eqSeq(got, want...) {
		t.Fatalf("4001 profili:\n  oldi=%v\n  kutilgan=%v", got, want)
	}
}

func TestBackoff_RevokedProfile(t *testing.T) {
	b := NewBackoff(ProfileRevoked, nil)
	got := seq(b, 5)
	want := []time.Duration{
		60 * time.Second, 120 * time.Second, 240 * time.Second,
		300 * time.Second, 300 * time.Second,
	}
	if !eqSeq(got, want...) {
		t.Fatalf("4003 profili:\n  oldi=%v\n  kutilgan=%v", got, want)
	}
}

func TestBackoff_JitterChegarasi(t *testing.T) {
	// rnd=0 → -20 %, rnd=1 → +20 %, rnd=0.5 → 0
	cases := []struct {
		rnd  float64
		want time.Duration
	}{
		{0.0, 800 * time.Millisecond},
		{0.5, 1000 * time.Millisecond},
		{0.999999, 1200 * time.Millisecond},
	}
	for _, c := range cases {
		b := NewBackoff(ProfileNormal, func() float64 { return c.rnd })
		got := b.Next()
		// suzuvchi hisob tufayli 1 ms lik moslikka ruxsat beriladi
		if got < c.want-time.Millisecond || got > c.want+time.Millisecond {
			t.Fatalf("rnd=%v: got=%v, want=%v±1ms", c.rnd, got, c.want)
		}
	}
	// Cap + jitter ham cap'dan oshmasligi kerak
	b := NewBackoff(ProfileNormal, func() float64 { return 0.999999 })
	for i := 0; i < 12; i++ {
		if d := b.Next(); d > 60*time.Second*121/100 {
			t.Fatalf("cap+jitter juda katta: %v", d)
		}
	}
}

func TestBackoff_MusobaqagaKarshiEMAS(t *testing.T) {
	// Backoff bir goroutine'da ishlatiladi (single-writer qoidasi, TASKS §0.3).
	// Shuning uchun lock yo'q — hujjatlashtirilgan xatti-harakatni tekshiramiz.
	b := NewBackoff(ProfileNormal, nil)
	if b.Profile() != ProfileNormal {
		t.Fatal("Profile() noto'g'ri")
	}
}

// Sog'lom sessiyadan keyin kutish bazasi tiklanadi; qisqa sessiyadan
// keyin yo'q (aks holda hisob kunlar davomida o'sib borardi).
func TestResetBackoff_AfterHealthySession(t *testing.T) {
	bo := NewBackoff(ProfileNormal, nil)
	for i := 0; i < 6; i++ {
		bo.Next()
	}
	if d := bo.Next(); d < 30*time.Second {
		t.Fatalf("o'sish kutilardi, %v", d)
	}
	resetBackoff(bo, 2*time.Hour)
	if d := bo.Next(); d != time.Second {
		t.Fatalf("reset ishlamadi: keyingi kutish %v, 1s kutilardi", d)
	}
	// Qisqa (uzilish) sessiya reset qilmaydi.
	bo.Next()
	bo.Next()
	resetBackoff(bo, 3*time.Second)
	if d := bo.Next(); d < 4*time.Second {
		t.Fatalf("qisqa sessiya reset qildi: %v", d)
	}
}
