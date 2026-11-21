package agent

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/clock"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/statestore"
)

// fakeSender — yuborilgan xabarlarni yozib boradi (§18 mock).
type fakeSender struct {
	mu     sync.Mutex
	sent   []proto.CamsSync
	typs   []string
	sendFn func(typ string, data any) error
}

func (f *fakeSender) Send(typ string, data any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.typs = append(f.typs, typ)
	if cs, ok := data.(proto.CamsSync); ok {
		f.sent = append(f.sent, cs)
	}
	if f.sendFn != nil {
		return f.sendFn(typ, data)
	}
	return nil
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeSender) at(i int) proto.CamsSync {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sent[i]
}

func newTestState(t *testing.T, n int) *statestore.State {
	t.Helper()
	st := statestore.New("")
	for i := 0; i < n; i++ {
		st.Cameras = append(st.Cameras, cams.Camera{
			ID: cams.NewID(), Name: "kamera-" + strings.Repeat("x", 8),
			Host: "192.168.10." + itoa(i%250), Port: 554,
			MainPath: "/cam/realmonitor?channel=1&subtype=0",
			Enabled:  true, Position: i + 1, Status: cams.StatusUnknown,
		})
	}
	st.CamsEpoch = int64(n)
	return st
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// C2.10 tekshiruvi: 250 kamera → 3 chunk (≤100 kamera/xabar).
func TestSyncChunking(t *testing.T) {
	st := newTestState(t, 250)
	fs := &fakeSender{}
	fc := clock.NewFake(time.Now())
	sy := NewSyncer(fs, st, fc, nil)

	// ack'larni avtomatik qaytaramiz: har yuborilgan chunk'dan keyin
	go func() {
		for i := 0; i < 10; i++ {
			// chunk'lar ketma-ket yuboriladi; har biriga javob beramiz
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if fs.count() > i {
					sy.OnCamsAck(proto.CamsAck{Epoch: st.CamsEpoch, OK: true, Applied: 100})
					break
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()

	if err := sy.Push(st.CamsEpoch); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if fs.count() != 3 {
		t.Fatalf("chunk'lar=%d, kutilgan 3", fs.count())
	}
	var total int
	for i := 0; i < 3; i++ {
		c := fs.at(i)
		if c.ChunkCount != 3 || c.SeqNo != i+1 {
			t.Fatalf("chunk %d: seq=%d count=%d", i, c.SeqNo, c.ChunkCount)
		}
		if len(c.Cameras) > MaxCamsPerChunk {
			t.Fatalf("chunk %d hajmi=%d > %d", i, len(c.Cameras), MaxCamsPerChunk)
		}
		if c.Done != (i == 2) {
			t.Fatalf("chunk %d: done=%v", i, c.Done)
		}
		if !c.Full {
			t.Fatalf("chunk %d: full=false", i)
		}
		if c.Epoch != st.CamsEpoch {
			t.Fatalf("epoch=%d", c.Epoch)
		}
		total += len(c.Cameras)
	}
	if total != 250 {
		t.Fatalf("jami kameralar=%d", total)
	}
}

// Bitta chunk ≤900 KiB (§2).
func TestSyncChunkBytes(t *testing.T) {
	st := newTestState(t, 250)
	fs := &fakeSender{}
	sy := NewSyncer(fs, st, clock.NewFake(time.Now()), nil)
	go func() {
		for i := 0; i < 3; i++ {
			for time.Now().Add(2 * time.Second).After(time.Now()) {
				if fs.count() > i {
					sy.OnCamsAck(proto.CamsAck{Epoch: st.CamsEpoch, OK: true})
					break
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()
	if err := sy.Push(st.CamsEpoch); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < fs.count(); i++ {
		b, err := json.Marshal(fs.at(i))
		if err != nil {
			t.Fatal(err)
		}
		if len(b) > MaxChunkBytes {
			t.Fatalf("chunk %d hajmi=%d bayt > %d", i, len(b), MaxChunkBytes)
		}
	}
}

// rtsp_masked: parol yo'q, user=*** (§10.2).
func TestSyncEntryMasked(t *testing.T) {
	st := statestore.New("")
	st.Cameras = []cams.Camera{{ID: "id1", Name: "A", Host: "10.0.0.7", Port: 554,
		MainPath: "/cam/realmonitor?channel=1&subtype=0", Enabled: true, Position: 1}}
	fs := &fakeSender{}
	sy := NewSyncer(fs, st, clock.NewFake(time.Now()), nil)
	go func() {
		for time.Now().Add(2 * time.Second).After(time.Now()) {
			if fs.count() > 0 {
				sy.OnCamsAck(proto.CamsAck{Epoch: 1, OK: true})
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	if err := sy.Push(1); err != nil {
		t.Fatal(err)
	}
	e := fs.at(0).Cameras[0]
	if strings.Contains(e.RTSPMasked, "@") {
		t.Fatalf("rtsp_masked credential bilan: %q", e.RTSPMasked)
	}
	if !strings.Contains(e.RTSPMasked, "10.0.0.7:554") {
		t.Fatalf("rtsp_masked: %q", e.RTSPMasked)
	}
	if e.Op != "upsert" {
		t.Fatalf("op=%q", e.Op)
	}
}

// Ack yo'qolsa: 10 s × 3 qayta urinish, keyin xato (§4.3). Fake clock ishlatiladi.
func TestSyncNoAckRetries(t *testing.T) {
	st := newTestState(t, 1)
	fs := &fakeSender{}
	fc := clock.NewFake(time.Now())
	sy := NewSyncer(fs, st, fc, slog.New(slog.NewTextHandler(discard{}, nil)))

	done := make(chan error, 1)
	go func() { done <- sy.Push(7) }()

	// 4 marta (1 boshlang'ich + 3 qayta) taymaut tugashi kerak.
	// Har Advance'dan oldin yuborilgan xabarlar soni o'sgan bo'lishi shart —
	// shunda taymaut ochilganiga amin bo'lamiz (race yo'q).
	prev := 0
	for i := 0; i < ackRetries; i++ {
		for fs.count() <= prev {
			time.Sleep(time.Millisecond)
		}
		prev = fs.count()
		fc.Advance(ackTimeout)
	}
	for fs.count() <= prev {
		time.Sleep(time.Millisecond)
	}
	fc.Advance(ackTimeout) // oxirgi urinishdan keyin ham ack yo'q → xato
	select {
	case err := <-done:
		if !errors.Is(err, ErrSyncNoAck) {
			t.Fatalf("ErrSyncNoAck kutilmadi: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Push tugamadi — fake clock ishlamadi")
	}
	if fs.count() != ackRetries+1 {
		t.Fatalf("yuborilganlar=%d, kutilgan %d", fs.count(), ackRetries+1)
	}
}

// stale_epoch → darhol xato (§4.2).
func TestSyncStaleEpoch(t *testing.T) {
	st := newTestState(t, 1)
	fs := &fakeSender{}
	sy := NewSyncer(fs, st, clock.NewFake(time.Now()), nil)
	go func() {
		for time.Now().Add(2 * time.Second).After(time.Now()) {
			if fs.count() > 0 {
				r := "stale_epoch"
				sy.OnCamsAck(proto.CamsAck{Epoch: st.CamsEpoch, OK: false, Reason: &r})
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	if err := sy.Push(st.CamsEpoch); !errors.Is(err, ErrSyncStale) {
		t.Fatalf("ErrSyncStale kutilmadi: %v", err)
	}
}

// Eski epoch'li ack e'tiborga olinmaydi — keyingi ack kutiladi.
func TestSyncIgnoresForeignAck(t *testing.T) {
	st := newTestState(t, 1)
	fs := &fakeSender{}
	sy := NewSyncer(fs, st, clock.NewFake(time.Now()), nil)
	sy.OnCamsAck(proto.CamsAck{Epoch: 999, OK: true}) // begona
	go func() {
		for time.Now().Add(2 * time.Second).After(time.Now()) {
			if fs.count() > 0 {
				sy.OnCamsAck(proto.CamsAck{Epoch: st.CamsEpoch, OK: true})
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	if err := sy.Push(st.CamsEpoch); err != nil {
		t.Fatalf("begona ackdan keyin ham sync bo'lishi kerak edi: %v", err)
	}
}

// Delete navbati: cams_sync'da op:"delete" bo'ladi va muvaffaqiyatdan keyin tozalanadi.
func TestSyncPendingDeletes(t *testing.T) {
	st := statestore.New("")
	st.Cameras = []cams.Camera{{ID: "id-1", Host: "10.0.0.1", Port: 554, Enabled: true, Position: 1}}
	st.AddPendingDelete([]string{"id-old-1", "id-old-2", "id-old-1"}) // takrorlanish bir marta
	if len(st.PendingDeletes) != 2 {
		t.Fatalf("navbat: %v", st.PendingDeletes)
	}
	fs := &fakeSender{}
	sy := NewSyncer(fs, st, clock.NewFake(time.Now()), nil)
	go func() {
		for time.Now().Add(2 * time.Second).After(time.Now()) {
			if fs.count() > 0 {
				sy.OnCamsAck(proto.CamsAck{Epoch: 3, OK: true})
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	if err := sy.Push(3); err != nil {
		t.Fatal(err)
	}
	var dels, ups int
	for _, e := range fs.at(0).Cameras {
		if e.Op == "delete" {
			dels++
			if e.ID == "" {
				t.Fatal("delete entry id'siz")
			}
		}
		if e.Op == "upsert" {
			ups++
		}
	}
	if dels != 2 || ups != 1 {
		t.Fatalf("delete=%d upsert=%d", dels, ups)
	}
	if len(st.PendingDeletes) != 0 {
		t.Fatalf("navbat tozalanmadi: %v", st.PendingDeletes)
	}
}

// Bo'sh delta → xabar yuborilmaydi (keraksiz trafik yo'q).
func TestSyncEmptyDelta(t *testing.T) {
	st := statestore.New("")
	fs := &fakeSender{}
	sy := NewSyncer(fs, st, clock.NewFake(time.Now()), nil)
	if err := sy.PushDelta(1, nil); err != nil {
		t.Fatal(err)
	}
	if fs.count() != 0 {
		t.Fatalf("bo'sh delta yuborildi: %d", fs.count())
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// Ikki parallel sync chunk'larni aralashtirmaydi: har bir to'liq
// ketma-ketlik tugagach keyingisi boshlanadi (pushMu, 2026-11-21 audit).
func TestSync_ConcurrentPushSerialized(t *testing.T) {
	st := newTestState(t, 250) // 3 chunk
	fs := &fakeSender{}
	sy := NewSyncer(fs, st, clock.NewFake(time.Now()), nil)

	// Har yuborilgan chunk'ga darhol ack beramiz.
	ackers := make(chan struct{})
	go func() {
		defer close(ackers)
		for i := 0; i < 6; i++ {
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if fs.count() > i {
					sy.OnCamsAck(proto.CamsAck{Epoch: st.CamsEpoch, OK: true, Applied: 100})
					break
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = sy.Push(st.CamsEpoch) }()
	go func() { defer wg.Done(); _ = sy.Push(st.CamsEpoch) }()
	wg.Wait()
	<-ackers

	if fs.count() != 6 {
		t.Fatalf("chunk'lar soni: %d, 6 kutilardi", fs.count())
	}
	want := []int{1, 2, 3, 1, 2, 3}
	for i, w := range want {
		if got := fs.at(i).SeqNo; got != w {
			t.Fatalf("chunk ketma-ketligi arashildi: %d. SeqNo=%d, %d kutilardi", i, got, w)
		}
	}
}
