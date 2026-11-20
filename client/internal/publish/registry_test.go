package publish

import (
	"bytes"
	"log/slog"
	"runtime"
	"sync"
	"testing"
	"time"

	"central-vms/client/internal/clock"
)

// §9.1: publish_key = camera_id+profile+ingest; birinchi media_start →
// publish ochiladi (media_ready BIR marta); qo'shimcha start'lar refcount'ni
// oshiradi, yangi publish OCHILMAYDI.

// evLog — hodisalarni yig'adi. Writer goroutine yozadi, test o'qiydi →
// mutex bilan himoyalangan (race detektori topgan uzil).
type evLog struct {
	mu      sync.Mutex
	ready   []ReadyEvent
	stopped []StoppedEvent
	ch      chan struct{} // har hodisada signal
}

func newEvLog() *evLog { return &evLog{ch: make(chan struct{}, 32)} }
func (e *evLog) onReady(s ReadyEvent) {
	e.mu.Lock()
	e.ready = append(e.ready, s)
	e.mu.Unlock()
	e.ch <- struct{}{}
}
func (e *evLog) onStopped(s StoppedEvent) {
	e.mu.Lock()
	e.stopped = append(e.stopped, s)
	e.mu.Unlock()
	e.ch <- struct{}{}
}
func (e *evLog) nReady() int   { e.mu.Lock(); defer e.mu.Unlock(); return len(e.ready) }
func (e *evLog) nStopped() int { e.mu.Lock(); defer e.mu.Unlock(); return len(e.stopped) }
func (e *evLog) lastStopped() StoppedEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.stopped) == 0 {
		return StoppedEvent{}
	}
	return e.stopped[len(e.stopped)-1]
}
func (e *evLog) firstReady() ReadyEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.ready) == 0 {
		return ReadyEvent{}
	}
	return e.ready[0]
}

func TestRegistry_TwoUsersPlusPreview_OnePublish(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), ev.onReady, ev.onStopped)
	defer r.Shutdown()

	k := Key{CameraID: "cam1", Profile: "main", Ingest: "whip"}

	res1, err := r.Start("sess-A", k, "play")
	if err != nil {
		t.Fatalf("start A: %v", err)
	}
	if !res1.New || res1.Refcount != 1 {
		t.Fatalf("A: %+v, yangi publish kutilardi", res1)
	}
	r.MarkReady(k)

	res2, _ := r.Start("sess-B", k, "play") // 2-user
	if res2.New {
		t.Error("2-user uchun yangi publish ochildi (§9.1)")
	}
	if res2.PublishID != res1.PublishID {
		t.Errorf("publish_id o'zgardi: %q → %q", res1.PublishID, res2.PublishID)
	}
	res3, _ := r.Start("sess-C", k, "preview") // preview + play → bir publish
	if res3.New {
		t.Error("preview uchun yangi publish ochildi (§9.1)")
	}
	if res3.Refcount != 3 {
		t.Errorf("refcount = %d, 3 kutilardi", res3.Refcount)
	}

	// media_ready FAQAT bir marta: bitta hodisa keladi, ikkinchisi kelmaydi.
	select {
	case <-ev.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("media_ready hodisasi kelmadi")
	}
	select {
	case <-ev.ch:
		t.Errorf("media_ready takrorlandi: n=%d", ev.nReady())
	case <-time.After(300 * time.Millisecond):
	}
	if ev.nReady() != 1 {
		t.Errorf("media_ready = %d marta, 1 kutilardi", len(ev.ready))
	}
	if fr := ev.firstReady(); fr.PublishID != res1.PublishID || fr.Key != k {
		t.Errorf("ready hodisasi: %+v", ev.firstReady())
	}
}

// §9.1: profile har xil bo'lsa — har xil publish (sub va main alohida).
func TestRegistry_DifferentProfileSeparatePublish(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, nil, ev.onReady, ev.onStopped)
	defer r.Shutdown()

	a, _ := r.Start("s1", Key{CameraID: "cam1", Profile: "main", Ingest: "whip"}, "play")
	b, _ := r.Start("s2", Key{CameraID: "cam1", Profile: "sub", Ingest: "whip"}, "play")
	if a.PublishID == b.PublishID {
		t.Error("main va sub bitta publish bo'ldi (§9.1: alohida bo'lishi kerak)")
	}
}

// §9.1: refcount 0 → 1 s kutiladi; shu orada qayta ochilsa — publish uzilmaydi,
// media_ready takrorlanmaydi.
func TestRegistry_GraceWindowKeepsPublish(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, nil, ev.onReady, ev.onStopped)
	defer r.Shutdown()

	k := Key{CameraID: "cam1", Profile: "main", Ingest: "whip"}
	first, _ := r.Start("s1", k, "play")
	r.MarkReady(k)
	<-ev.ch

	rc := r.Stop("s1", k)
	if rc != 0 {
		t.Fatalf("refcount = %d, 0 kutilardi", rc)
	}
	// Grace ichida (0.5 s) qayta ochish — eski publish davom etadi.
	clk.Advance(500 * time.Millisecond)
	again, err := r.Start("s2", k, "play")
	if err != nil {
		t.Fatalf("qayta start: %v", err)
	}
	if again.New {
		t.Error("grace ichida qayta ochilganda yangi publish yaratildi")
	}
	if again.PublishID != first.PublishID {
		t.Errorf("publish_id o'zgardi: %q → %q", first.PublishID, again.PublishID)
	}
	// media_ready takrorlanmasligi kerak: 500 ms grace'dan keyin ham yopish bo'lmasin.
	clk.Advance(2 * time.Second)
	select {
	case <-ev.ch:
		t.Errorf("publish yopildi (grace bekor qilinishi kerak edi): stopped=%d", ev.nStopped())
	case <-time.After(300 * time.Millisecond):
	}
}

// §9.1: refcount 0 + 1 s o'tsa → yopiladi, media_stopped{no_viewers}.
func TestRegistry_CloseAfterGrace(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, nil, ev.onReady, ev.onStopped)
	defer r.Shutdown()

	k := Key{CameraID: "cam1", Profile: "main", Ingest: "whip"}
	res, _ := r.Start("s1", k, "play")
	r.MarkReady(k)
	<-ev.ch
	r.Stop("s1", k)

	// Fake soatni oldinga suramiz (grace=1 s) — sleep yo'q, Advance+Gosched (§0.2).
	deadline := time.Now().Add(3 * time.Second)
	for ev.nStopped() == 0 && time.Now().Before(deadline) {
		clk.Advance(500 * time.Millisecond)
		runtime.Gosched()
	}
	if ev.nStopped() != 1 {
		t.Fatalf("media_stopped kelmadi: %d", ev.nStopped())
	}
	st := ev.lastStopped()
	if st.Reason != "no_viewers" || st.PublishID != res.PublishID {
		t.Errorf("stopped hodisasi: %+v", st)
	}
}

// Yozuvchi goroutine Shutdown'dan keyin qolmaydi (goleak o'rniga).
func TestRegistry_NoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, nil, ev.onReady, ev.onStopped)
	r.Start("s1", Key{CameraID: "c", Profile: "main", Ingest: "whip"}, "play")
	r.Shutdown()
	// Goroutine'lar tugashi uchun imtiyoz (sleep yo'q).
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("goroutine sizib chiqdi: oldin %d, keyin %d", before, n)
	}
	// Shutdown'dan keyingi amallar aniq xato qaytaradi (panic emas).
	if _, err := r.Start("s2", Key{CameraID: "c", Profile: "main", Ingest: "whip"}, "play"); err == nil {
		t.Error("Shutdown'dan keyin Start xato qaytarmadi")
	}
}
