package publish

import (
	"runtime"
	"testing"
	"time"

	"central-vms/client/internal/clock"
)

// §9.2 TTL watchdog: deadline = now + ttl_s; media_refresh siljitadi;
// muddat o'tsa → media_stopped{ttl_expired}. Watchdog agent ichida — WS
// uzilganda ham ishlaydi (tashqi kiritim talab qilmaydi).

// advanceTo — fake soatni hodisa bo'lguncha yoki chekgacha suradi (sleep yo'q).
func advanceTo(t *testing.T, clk *clock.Fake, ev *evLog, want func() bool, fakeLimit time.Time) bool {
	t.Helper()
	for i := 0; i < 200; i++ {
		if want() {
			return true
		}
		clk.Advance(5 * time.Second) // §9.2: ~5 s tekshiruv ritmi
		runtime.Gosched()            // yozuvchi goroutine'ga navbat beramiz
		if clk.Now().After(fakeLimit) {
			break
		}
	}
	return want()
}

func TestWatchdog_TTLExpiresWithViewers(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, nil, ev.onReady, ev.onStopped)
	defer r.Shutdown()

	k := Key{CameraID: "cam1", Profile: "main", Ingest: "whip"}
	res, err := r.StartWithTTL("s1", k, "play", 60)
	if err != nil {
		t.Fatal(err)
	}
	r.MarkReady(k)
	<-ev.ch

	// Tomoshibi bor — lekin TTL tugadi (60 s + tekshiruv ritmi) → yopiladi.
	if !advanceTo(t, clk, ev, func() bool { return ev.nStopped() > 0 }, clk.Now().Add(5*time.Minute)) {
		t.Fatalf("ttl_expired yopilishi kerak edi; stopped=%d", ev.nStopped())
	}
	st := ev.lastStopped()
	if st.Reason != "ttl_expired" || st.PublishID != res.PublishID {
		t.Errorf("hodisa: %+v, ttl_expired kutilardi", st)
	}
}

func TestWatchdog_RefreshExtendsDeadline(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, nil, ev.onReady, ev.onStopped)
	defer r.Shutdown()

	k := Key{CameraID: "cam1", Profile: "main", Ingest: "whip"}
	r.StartWithTTL("s1", k, "play", 60)
	r.MarkReady(k)
	<-ev.ch

	// 40 s o'tadi, refresh → deadline = now+60 (jami 100 s).
	clk.Advance(40 * time.Second)
	r.Refresh(k, 60)
	// 80 s'ga yetamiz — hali yopilmasligi kerak.
	for clk.Now().Unix() < 80 {
		clk.Advance(5 * time.Second)
	}
	if ev.nStopped() != 0 {
		t.Fatalf("80 s'da noto'g'ri yopildi: %+v", ev.lastStopped())
	}
	// 100 s'dan o'tamiz → yopiladi.
	if !advanceTo(t, clk, ev, func() bool { return ev.nStopped() > 0 }, clk.Now().Add(3*time.Minute)) {
		t.Fatal("refresh'dan keyin TTL yopilishi kerak edi")
	}
	if st := ev.lastStopped(); st.Reason != "ttl_expired" {
		t.Errorf("reason = %q, ttl_expired kutilardi", st.Reason)
	}
}

// Default TTL = 60 s (§9.2), Start (TTLsiz) shu qiymatni oladi.
func TestWatchdog_DefaultTTL60(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, nil, ev.onReady, ev.onStopped)
	defer r.Shutdown()

	k := Key{CameraID: "cam1", Profile: "main", Ingest: "whip"}
	r.Start("s1", k, "play")
	r.MarkReady(k)
	<-ev.ch
	// 55 s — ochiq.
	for clk.Now().Unix() < 55 {
		clk.Advance(5 * time.Second)
	}
	if ev.nStopped() != 0 {
		t.Fatalf("55 s'da yopildi: %+v", ev.lastStopped())
	}
	if !advanceTo(t, clk, ev, func() bool { return ev.nStopped() > 0 }, clk.Now().Add(2*time.Minute)) {
		t.Fatal("60 s default TTL yopishi kerak edi")
	}
	if st := ev.lastStopped(); st.Reason != "ttl_expired" {
		t.Errorf("reason = %q", st.Reason)
	}
}

// Ikki sessiyadan birining refresh'i publish'ni tirik qiladi.
func TestWatchdog_AnySessionRefreshKeepsAlive(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, nil, ev.onReady, ev.onStopped)
	defer r.Shutdown()

	k := Key{CameraID: "cam1", Profile: "main", Ingest: "whip"}
	r.StartWithTTL("s1", k, "play", 60)
	r.StartWithTTL("s2", k, "play", 60)
	r.MarkReady(k)
	<-ev.ch

	for i := 0; i < 10; i++ { // 50 s davomida s2 har 10 s'da refresh qiladi
		clk.Advance(10 * time.Second)
		r.Refresh(k, 60)
	}
	if ev.nStopped() != 0 {
		t.Fatalf("refresh'lar orasida yopildi: %+v", ev.lastStopped())
	}
}

// Grace (no_viewers) va TTL birga: tomoshobi ketgan payt TTL ham yetgan
// bo'lsa — yopish sodir bo'ladi (reason no_viewers birinchi, grace aniqroq).
func TestWatchdog_NoViewersBeatsTTLWhenBothDue(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, nil, ev.onReady, ev.onStopped)
	defer r.Shutdown()

	k := Key{CameraID: "cam1", Profile: "main", Ingest: "whip"}
	r.StartWithTTL("s1", k, "play", 10)
	r.MarkReady(k)
	<-ev.ch
	r.Stop("s1", k) // grace 1 s (t=1 s), TTL t=10 s — grace oldin yetadi

	if !advanceTo(t, clk, ev, func() bool { return ev.nStopped() > 0 }, clk.Now().Add(2*time.Minute)) {
		t.Fatal("yopilishi kerak edi")
	}
	if st := ev.lastStopped(); st.Reason != "no_viewers" {
		t.Errorf("reason = %q, no_viewers kutilardi (grace oldin)", st.Reason)
	}
}
