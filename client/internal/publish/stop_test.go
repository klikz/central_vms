package publish

import (
	"bytes"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"central-vms/client/internal/clock"
)

// C3.10 (§9.1): media_stop → refcount 0 → 1 s grace → yopilish; tezkor
// qayta ochish (1 s ichida) publish'ni uzmaydi.
func TestStop_GraceWindowProtectsQuickReopen(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, nil, ev.onReady, ev.onStopped)
	defer r.Shutdown()

	k := Key{CameraID: "cam1", Profile: "main", Ingest: "whip"}
	first, _ := r.Start("u1", k, "play")
	r.MarkReady(k)
	<-ev.ch

	if rc := r.Stop("u1", k); rc != 0 {
		t.Fatalf("refcount = %d, 0 kutilardi", rc)
	}
	// 1 s ichida boshqa user ochadi — publish uzilmasligi kerak.
	clk.Advance(700 * time.Millisecond)
	again, err := r.Start("u2", k, "play")
	if err != nil {
		t.Fatal(err)
	}
	if again.New || again.PublishID != first.PublishID {
		t.Errorf("grace ichida yangi publish ochildi: %+v", again)
	}
	clk.Advance(2 * time.Second)
	runtime.Gosched()
	if ev.nStopped() != 0 {
		t.Errorf("publish yopildi: %+v", ev.lastStopped())
	}
}

func TestStop_LastViewerClosesAfterOneSecond(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, nil, ev.onReady, ev.onStopped)
	defer r.Shutdown()

	k := Key{CameraID: "cam1", Profile: "main", Ingest: "whip"}
	r.Start("u1", k, "play")
	r.MarkReady(k)
	<-ev.ch
	r.Stop("u1", k)

	for i := 0; i < 20 && ev.nStopped() == 0; i++ {
		clk.Advance(500 * time.Millisecond)
		runtime.Gosched()
	}
	if ev.nStopped() != 1 {
		t.Fatalf("1 s'dan keyin yopilishi kerak edi")
	}
	if st := ev.lastStopped(); st.Reason != "no_viewers" {
		t.Errorf("reason = %q, no_viewers kutilardi", st.Reason)
	}
}

// C3.19 (root §8.2): play + preview → BITTA publish, 2 session —
// qo'shimcha upload yo'q.
func TestPreview_Play_SinglePublishTwoSessions(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, nil, ev.onReady, ev.onStopped)
	defer r.Shutdown()

	k := Key{CameraID: "cam1", Profile: "sub", Ingest: "whip"}
	play, _ := r.Start("se_play", k, "play")
	prev, _ := r.Start("se_prev", k, "preview")
	if prev.New {
		t.Error("preview uchun qo'shimcha publish ochildi (§9.1)")
	}
	if play.PublishID != prev.PublishID {
		t.Errorf("publish_id'lar xil: %q / %q", play.PublishID, prev.PublishID)
	}
	if n := r.Refcount(k); n != 2 {
		t.Errorf("refcount = %d, 2 kutilardi", n)
	}
	r.MarkReady(k)
	select {
	case <-ev.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("media_ready kelmadi")
	}
	if ev.nReady() != 1 {
		t.Errorf("media_ready = %d, 1 kutilardi (bitta upload)", ev.nReady())
	}
}

// §9.4: CloseAll — barcha publish'lar agent_restart bilan yopiladi.
func TestCloseAll_AgentRestart(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, nil, ev.onReady, ev.onStopped)
	defer r.Shutdown()

	r.Start("s1", Key{CameraID: "c1", Profile: "main", Ingest: "whip"}, "play")
	r.Start("s2", Key{CameraID: "c2", Profile: "sub", Ingest: "rtsp_tcp"}, "play")
	r.CloseAll(ReasonAgentRestart)

	if n := len(r.List()); n != 0 {
		t.Errorf("List = %d, 0 kutilardi", n)
	}
	if ev.nStopped() != 2 {
		t.Fatalf("stopped = %d, 2 kutilardi", ev.nStopped())
	}
	for _, want := range []string{ReasonAgentRestart} {
		if st := ev.lastStopped(); st.Reason != want {
			t.Errorf("reason = %q, %q kutilardi", st.Reason, want)
		}
	}
}

// syncBuf — registry goroutine'si yozadi, test o'qiydi (race yo'q).
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) str() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// §10.2/§13: hodisa loglari strukturali maydonlar bilan chiqadi.
func TestRegistry_StructuredLogFields(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	buf := &syncBuf{}
	lg := slog.New(slog.NewTextHandler(buf, nil))
	r := New(clk, lg, nil, nil)
	defer r.Shutdown()

	r.Start("pub-42", Key{CameraID: "cam-7", Profile: "sub", Ingest: "whip"}, "play")
	// TTL muddati o'tsa — media_stopped reason=ttl_expired.
	clk.Advance(61 * time.Second)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.str(), "media_stopped") {
			break
		}
		runtime.Gosched()
	}
	out := buf.str()
	for _, want := range []string{"camera_id=cam-7", "publish_id=pub_", "reason=ttl_expired", "profile=sub"} {
		if !strings.Contains(out, want) {
			t.Errorf("logda %q yo'q: %s", want, out)
		}
	}
}

// §13: media_stop server reason'i grace yopilishga meros bo'ladi.
func TestStopReason_PropagatesToStoppedEvent(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, nil, ev.onReady, ev.onStopped)
	defer r.Shutdown()

	k := Key{CameraID: "c1", Profile: "main", Ingest: "whip"}
	r.Start("u1", k, "play")
	if rc := r.StopReason("u1", k, ReasonUserClose); rc != 0 {
		t.Fatalf("refcount = %d, 0 kutilardi", rc)
	}
	clk.Advance(2 * time.Second) // grace o'tadi
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && ev.nStopped() == 0 {
		runtime.Gosched()
	}
	if st := ev.lastStopped(); st.Reason != ReasonUserClose {
		t.Errorf("reason = %q, %q kutilardi", st.Reason, ReasonUserClose)
	}
}

// §9.3: Remove — transport ochilmaganda entry darhol yopiladi.
func TestRemove_ImmediateWithReason(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	ev := newEvLog()
	r := New(clk, nil, ev.onReady, ev.onStopped)
	defer r.Shutdown()

	k := Key{CameraID: "c1", Profile: "main", Ingest: "whip"}
	r.Start("u1", k, "play")
	r.Remove(k, ReasonPublishFailed)
	if ev.nStopped() != 1 {
		t.Fatalf("stopped = %d, 1 kutilardi", ev.nStopped())
	}
	if st := ev.lastStopped(); st.Reason != ReasonPublishFailed {
		t.Errorf("reason = %q", st.Reason)
	}
	if n := len(r.List()); n != 0 {
		t.Errorf("List = %d, 0 kutilardi", n)
	}
}
