package publish

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"central-vms/client/internal/clock"
	"central-vms/client/internal/proto"
)

// §4.6: media_stats har 10 s (faqat faol publish'lar bo'lsa);
// media_ready maydonlari to'liq bo'ladi.

func TestStats_GoldenMediaStats(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	r := New(clk, nil, nil, nil)
	defer r.Shutdown()

	k := Key{CameraID: "6f0f1f2a-0000-4000-8000-000000000001", Profile: "sub", Ingest: "whip"}
	res, err := r.Start("se_1", k, "play")
	if err != nil {
		t.Fatal(err)
	}
	// Fixture'dagi publish_id bilan solishtirish uchun ref'ni almashtiramiz.
	refs := r.List()
	if len(refs) != 1 || refs[0].PublishID != res.PublishID {
		t.Fatalf("List: %+v", refs)
	}

	src := func(Key) Stat {
		return Stat{BytesSent: 1048576, FPS: 9.8, BitrateKbps: 690, RTTMs: 18, PacketLossPct: 0.2, Transport: "udp"}
	}
	sc := NewStats(r, clk, src, nil)
	got := sc.Snapshot()

	// publish_id'ni fixture qiymatiga qo'yib, JSON orqali taqqoslaymiz.
	if len(got.Publishes) == 1 {
		got.Publishes[0].PublishID = "pu_1"
	}
	want := mustFixtureStats(t, "../../testdata/protocol/media_stats.json")
	if !reflect.DeepEqual(got, want) {
		gj, _ := json.Marshal(got)
		wj, _ := json.Marshal(want)
		t.Errorf("media_stats farq:\n got %s\nwant %s", gj, wj)
	}
}

func mustFixtureStats(t *testing.T, path string) proto.MediaStats {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m proto.MediaStats
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// 10 s ticker: faol publish bo'lsa uzatiladi; yopilsa — to'xtaydi.
func TestStats_TickerEmitsAndStops(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	r := New(clk, nil, nil, nil)
	defer r.Shutdown()

	k := Key{CameraID: "cam1", Profile: "main", Ingest: "whip"}
	r.Start("s1", k, "play")

	var mu sync.Mutex
	var emits []proto.MediaStats
	nEmits := func() int { mu.Lock(); defer mu.Unlock(); return len(emits) }
	sc := NewStats(r, clk, func(Key) Stat { return Stat{BytesSent: 10, Transport: "udp"} },
		func(m proto.MediaStats) { mu.Lock(); emits = append(emits, m); mu.Unlock() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sc.Run(ctx)
	<-sc.Started() // ticker o'rnatilgachina Advance qilamiz

	// Kamida 2 tick yig'ilguncha Advance (goroutine navbati kechikishi mumkin).
	for i := 0; i < 10 && nEmits() < 2; i++ {
		clk.Advance(10 * time.Second)
		runtime.Gosched()
	}
	if n := nEmits(); n < 2 {
		t.Fatalf("emits = %d, kamida 2 kutilardi", n)
	}

	// Publish yopilsa — keyingi tick'larda uzatmaydi (bo'sh stats yo'q).
	r.Stop("s1", k)
	for i := 0; i < 10 && len(r.List()) > 0; i++ { // grace (1 s) tugashini kutamiz
		clk.Advance(500 * time.Millisecond)
		runtime.Gosched()
	}
	if len(r.List()) != 0 {
		t.Fatal("publish yopilmadi")
	}
	n1 := nEmits()

	// Yopilgandan keyin tick'lar — uzatmasligi kerak (bo'sh stats yo'q).
	for i := 0; i < 4; i++ {
		clk.Advance(10 * time.Second)
		runtime.Gosched()
	}
	if extra := nEmits() - n1; extra != 0 {
		t.Errorf("yopilgandan keyin %d qo'shimcha emit bo'ldi", extra)
	}
}

// media_ready: §4.6 fixture bilan golden (maydonlar to'liq).
func TestStats_GoldenMediaReady(t *testing.T) {
	b, err := os.ReadFile("../../testdata/protocol/media_ready.json")
	if err != nil {
		t.Fatal(err)
	}
	var want proto.MediaReady
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}

	plan := StartPlan{
		Key:    Key{CameraID: want.CameraID, Profile: "sub", Ingest: "whip"},
		Ingest: "whip",
	}
	st := Stat{Transport: "udp", FPS: 10.0, BitrateKbps: 700, BytesSent: 0}
	got := BuildReady(want.SessionID, want.Kind, "pu_1", "cam/ag_9f2c1a7e/6f0f", plan, st, false, "none", 640)

	if !reflect.DeepEqual(got, want) {
		gj, _ := json.Marshal(got)
		wj, _ := json.Marshal(want)
		t.Errorf("media_ready farq:\n got %s\nwant %s", gj, wj)
	}
}
