package cams

import (
	"testing"
	"time"

	"central-vms/client/internal/clock"
)

// §12.3: kodek probe uchta trigger bilan ishga tushadi va takrorlanishi
// bloklanadi (bir kamera uchun bir vaqtda bitta probe).
func TestCodecProbeTriggers(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(s *CodecScheduler, clk *clock.Fake)
		trigger Trigger
		want    bool
	}{
		{"ulanganda_ruxsat", func(s *CodecScheduler, clk *clock.Fake) {
			if !s.TryStart("c1", TriggerDaily) {
				t.Fatal("daily boshlanmadi")
			}
			s.Finish("c1") // endi last = t0
		}, TriggerConnect, true},
		{"kuniga1_ichida_rad", func(s *CodecScheduler, clk *clock.Fake) {
			s.TryStart("c1", TriggerConnect)
			s.Finish("c1")
			clk.Advance(23*time.Hour + 59*time.Minute)
		}, TriggerDaily, false},
		{"kuniga1_muddati_tugadi", func(s *CodecScheduler, clk *clock.Fake) {
			s.TryStart("c1", TriggerConnect)
			s.Finish("c1")
			clk.Advance(24 * time.Hour)
		}, TriggerDaily, true},
		{"publish_fail_darhol", func(s *CodecScheduler, clk *clock.Fake) {
			s.TryStart("c1", TriggerDaily)
			s.Finish("c1") // probe endigina bo'ldi
		}, TriggerPublishFail, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clk := clock.NewFake(time.Unix(0, 0))
			s := NewCodecScheduler(clk, 24*time.Hour)
			tc.setup(s, clk)
			if got := s.TryStart("c1", tc.trigger); got != tc.want {
				t.Errorf("TryStart(%s) = %v, xohlangan %v", tc.trigger, got, tc.want)
			}
		})
	}
}

func TestCodecProbeTakrorlanishBloki(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	s := NewCodecScheduler(clk, 24*time.Hour)
	if !s.TryStart("c1", TriggerConnect) {
		t.Fatal("birinchi urinish ruxsat etilmadi")
	}
	// Bir kamera uchun parallel ikkinchi probe — har qanday trigger'da rad.
	for _, tr := range []Trigger{TriggerConnect, TriggerDaily, TriggerPublishFail} {
		if s.TryStart("c1", tr) {
			t.Errorf("%s: running probe ustiga ruxsat chiqdi", tr)
		}
	}
	s.Finish("c1")
	if !s.TryStart("c1", TriggerPublishFail) {
		t.Error("Finish'dan keyin publish_fail ruxsat etilmadi")
	}
	// Boshqa kamera bloklanmaydi.
	if !s.TryStart("c2", TriggerConnect) {
		t.Error("c2 bloklandi (c1 running)")
	}
}

func TestCodecProbeRunningCount(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	s := NewCodecScheduler(clk, 24*time.Hour)
	for i := 0; i < 3; i++ {
		if !s.TryStart(string(rune('a'+i)), TriggerConnect) {
			t.Fatalf("%d ruxsat etilmadi", i)
		}
	}
	if n := s.Running(); n != 3 {
		t.Errorf("Running = %d, xohlangan 3", n)
	}
	s.Finish("a")
	if n := s.Running(); n != 2 {
		t.Errorf("Finish'dan keyin Running = %d, xohlangan 2", n)
	}
}

// §12.4: kamera publish band bo'lsa probe KECHIKTIRILADI (rad),
// publish'ni to'xtatmaydi. Bandlik olingach qayta urinish o'tadi.
func TestCodecProbeKechiktirishBand(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	s := NewCodecScheduler(clk, 24*time.Hour)
	busy := map[string]bool{"c1": true}
	s.IsBusy = func(id string) bool { return busy[id] }
	// Band — hatto publish_fail trigger ham kechiktiriladi.
	for _, tr := range []Trigger{TriggerConnect, TriggerPublishFail} {
		if s.TryStart("c1", tr) {
			t.Errorf("%s: band kameraga probe o'tkazdi (§12.4)", tr)
		}
	}
	if s.Running() != 0 {
		t.Errorf("band holatda Running=0 bo'lishi kerak edi")
	}
	delete(busy, "c1") // publish yopildi
	if !s.TryStart("c1", TriggerPublishFail) {
		t.Error("band olingach publish_fail o'tmadi")
	}
}
