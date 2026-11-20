package publish

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"central-vms/client/internal/clock"
)

// §10.4: max_uploads band → limit_exceeded (server limitidan mustaqil
// ikkinchi himoya); FFmpeg jarayonlari ≤ max_local_transcodes + 2.

func TestLimits_NinthStartRejected(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	r := New(clk, nil, nil, nil)
	defer r.Shutdown()
	r.SetMaxUploads(8) // server config_push max_uploads=8

	for i := 0; i < 8; i++ {
		k := Key{CameraID: string(rune('a' + i)), Profile: "main", Ingest: "whip"}
		if _, err := r.Start("s"+string(rune('0'+i)), k, "play"); err != nil {
			t.Fatalf("%d-chi start: %v", i+1, err)
		}
	}
	k9 := Key{CameraID: "z", Profile: "main", Ingest: "whip"}
	_, err := r.Start("s9", k9, "play")
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("9-chi start: err = %v, ErrLimitExceeded kutilardi", err)
	}
	// §13: reason = limit_exceeded.
	if got := ReasonFor(err, SideServer); got != ReasonLimitExceeded {
		t.Errorf("reason = %q", got)
	}
}

// Mavjud publish'ga qo'shimcha sessiya limitni egallamaydi.
func TestLimits_ExtraSessionNotCounted(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	r := New(clk, nil, nil, nil)
	defer r.Shutdown()
	r.SetMaxUploads(1)

	k := Key{CameraID: "cam1", Profile: "main", Ingest: "whip"}
	if _, err := r.Start("u1", k, "play"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Start("u2", k, "play"); err != nil {
		t.Errorf("mavjud publish'ga 2-user rad etildi: %v", err)
	}
	if _, err := r.Start("u3", Key{CameraID: "cam2", Profile: "main", Ingest: "whip"}, "play"); !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("2-chi kamera uchun err = %v, limit kutilardi", err)
	}
}

// Yopilgandan keyin joy bo'shaydi.
func TestLimits_SlotFreedAfterClose(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	r := New(clk, nil, nil, nil)
	defer r.Shutdown()
	r.SetMaxUploads(1)

	k1 := Key{CameraID: "cam1", Profile: "main", Ingest: "whip"}
	r.Start("u1", k1, "play")
	r.Stop("u1", k1)
	for i := 0; i < 10 && len(r.List()) > 0; i++ {
		clk.Advance(500 * time.Millisecond)
		runtime.Gosched()
	}
	k2 := Key{CameraID: "cam2", Profile: "main", Ingest: "whip"}
	if _, err := r.Start("u2", k2, "play"); err != nil {
		t.Errorf("joy bo'shashi kerak edi: %v", err)
	}
}

// FFmpeg jarayonlari: ≤ max_local_transcodes + 2 (remux/re-encode ham).
func TestLimits_FFmpegCap(t *testing.T) {
	g := NewFFmpegGate(4) // max_local_transcodes=4 → cap 6
	for i := 0; i < 6; i++ {
		if !g.TryAcquire() {
			t.Fatalf("%d-chi acquire rad etildi, 6 tasiga ruxsat bo'lishi kerak", i+1)
		}
	}
	if g.TryAcquire() {
		t.Error("7-chi acquire ruxsat etildi — cap 6 (+2) buzildi")
	}
	g.Release()
	if !g.TryAcquire() {
		t.Error("release'dan keyin acquire rad etildi")
	}
	if g.Active() != 6 {
		t.Errorf("active = %d, 6 kutilardi", g.Active())
	}
}
