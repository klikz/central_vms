package clock

import (
	"testing"
	"time"
)

func base() time.Time { return time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC) }

func TestReal_Interface(t *testing.T) {
	var c Clock = Real{}
	if c.Now().IsZero() {
		t.Fatal("Real.Now() nol")
	}
	tk := c.NewTicker(time.Hour)
	defer tk.Stop()
	if tk.C() == nil {
		t.Fatal("Real ticker kanali nil")
	}
}

func TestFake_After_FiresOnAdvance(t *testing.T) {
	f := NewFake(base())
	ch := f.After(5 * time.Second)

	f.Advance(4 * time.Second)
	select {
	case got := <-ch:
		t.Fatalf("erta o'qildi: %v", got)
	default:
	}

	f.Advance(1 * time.Second)
	select {
	case got := <-ch:
		if !got.Equal(base().Add(5 * time.Second)) {
			t.Fatalf("vaqt noto'g'ri: %v", got)
		}
	default:
		t.Fatal("Advance dan keyin signal kelmadi")
	}

	if p := f.PendingTimers(); p != 0 {
		t.Fatalf("timer tozalanmadi: %d", p)
	}
}

func TestFake_After_OrderAndOnce(t *testing.T) {
	f := NewFake(base())
	a := f.After(9 * time.Second)
	b := f.After(3 * time.Second)

	f.Advance(30 * time.Second)
	if got := <-b; !got.Equal(base().Add(3 * time.Second)) {
		t.Fatalf("birinchi kelishi kerak edi 3s: %v", got)
	}
	if got := <-a; !got.Equal(base().Add(9 * time.Second)) {
		t.Fatalf("ikkinchi 9s bo'lishi kerak edi: %v", got)
	}
	select {
	case got := <-a:
		t.Fatalf("bir martalik timer takrorlandi: %v", got)
	default:
	}
}

func TestFake_Ticker_RepeatsAndStops(t *testing.T) {
	f := NewFake(base())
	tk := f.NewTicker(5 * time.Second)
	defer tk.Stop()

	f.Advance(12 * time.Second)
	for i, want := range []time.Duration{5, 10} {
		select {
		case got := <-tk.C():
			if !got.Equal(base().Add(want * time.Second)) {
				t.Fatalf("tick %d: %v, kutilgan %v", i, got, want)
			}
		default:
			t.Fatalf("tick %d kelmadi", i)
		}
	}

	tk.Stop()
	f.Advance(60 * time.Second)
	select {
	case got := <-tk.C():
		t.Fatalf("Stop dan keyin tick keldi: %v", got)
	default:
	}
}

func TestFake_WatchdogCycle_60sInFewSteps(t *testing.T) {
	// Real holatda 60 s kutish kerak; Fake bilan test ~0 ms'da o'tadi.
	f := NewFake(base())
	deadline := f.Now().Add(60 * time.Second)
	tick := f.NewTicker(5 * time.Second)
	defer tick.Stop()

	expired := false
	for i := 0; i < 20 && !expired; i++ {
		f.Advance(5 * time.Second)
		select {
		case <-tick.C():
		default:
		}
		if !f.Now().Before(deadline) {
			expired = true
		}
	}
	if !expired {
		t.Fatal("watchdog muddati o'tmadi")
	}
	if got := f.Now().Sub(base()); got != 60*time.Second {
		t.Fatalf("umumiy siljish noto'g'ri: %v", got)
	}
}

func TestFake_NonPositiveGuards(t *testing.T) {
	f := NewFake(base())
	f.Advance(0)
	f.Advance(-5 * time.Second)
	if !f.Now().Equal(base()) {
		t.Fatalf("manfiy Advance vaqtni o'zgartirdi: %v", f.Now())
	}
	tk := f.NewTicker(0)
	defer tk.Stop()
	f.Advance(time.Hour)
	select {
	case got := <-tk.C():
		t.Fatalf("interval 0 bo'lgan ticker ishladi: %v", got)
	default:
	}
}

func TestFake_Set(t *testing.T) {
	f := NewFake(base())
	f.Set(base().Add(24 * time.Hour))
	if f.Now() != base().Add(24*time.Hour) {
		t.Fatalf("Set ishlamadi: %v", f.Now())
	}
}
