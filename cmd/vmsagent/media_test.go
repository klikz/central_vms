package main

import (
	"context"
	"errors"
	"log/slog"
	goruntime "runtime"
	"sync/atomic"
	"testing"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/clock"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/publish"
	"central-vms/client/internal/statestore"
)

// C3.20: mediaMgr — fake transport/pull bilan boshqaruv mantiqi.

type fakeTr struct {
	written   atomic.Int64
	closed    atomic.Bool
	transport string
	werr      error
}

func (t *fakeTr) WriteAU(nals [][]byte, ts uint32) error {
	if t.werr != nil {
		return t.werr
	}
	t.written.Add(1)
	return nil
}
func (t *fakeTr) BytesSent() int64  { return t.written.Load() * 100 }
func (t *fakeTr) Transport() string { return t.transport }
func (t *fakeTr) Close() error      { t.closed.Store(true); return nil }

func testMediaMgr(t *testing.T) (*mediaMgr, *publish.Registry, *fakeTr, chan publish.StoppedEvent) {
	t.Helper()
	rt := &runtime{
		log: slog.New(slog.NewTextHandler(discard{}, nil)),
		st: &statestore.State{Cameras: []cams.Camera{{
			ID: "cam-7", Host: "10.0.0.7", MainPath: "/media", CodecMain: "h264",
		}}},
	}
	m := newMediaMgr(rt, nil)
	tr := &fakeTr{transport: "udp"}
	m.openFn = func(context.Context, publish.StartPlan, cams.Camera, string) (transport, error) {
		return tr, nil
	}
	m.pullFn = func(ctx context.Context, _ cams.PullOptions, sink func(cams.AU) error) error {
		for i := 0; i < 2; i++ {
			if err := sink(cams.AU{Nals: [][]byte{{0x65, 1}}, Timestamp: uint32(i)}); err != nil {
				return err
			}
		}
		<-ctx.Done() // oqim ochiq turadi
		return ctx.Err()
	}
	stopped := make(chan publish.StoppedEvent, 4)
	reg := publish.New(clock.Real{}, nil, m.onReady, func(ev publish.StoppedEvent) {
		m.onStopped(ev) // avval transport yopilsin, keyin signal
		stopped <- ev
	})
	m.reg = reg
	t.Cleanup(reg.Shutdown)
	return m, reg, tr, stopped
}

// waitStopped — evetni kutish (uyqusiz: channel select).
func waitStopped(t *testing.T, ch chan publish.StoppedEvent) publish.StoppedEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("media_stopped kelmadi")
		return publish.StoppedEvent{}
	}
}

// waitWritten — transportga AU'lar yetib kelishini kutish.
func waitWritten(t *testing.T, tr *fakeTr, n int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && tr.written.Load() < n {
		goruntime.Gosched()
	}
}

func msStart() proto.MediaStart {
	return proto.MediaStart{
		SessionID: "se_1", CameraID: "cam-7", Profile: "main", Kind: "play",
		TTLS: 60, Ingest: "whip",
		Publish: proto.Publish{Token: "tk", URLs: proto.PublishURLs{WHIP: "http://x/w"}},
	}
}

func TestMediaMgr_StartFeedsTransport(t *testing.T) {
	m, reg, tr, _ := testMediaMgr(t)
	m.OnStart(msStart())
	waitWritten(t, tr, 2)
	if tr.written.Load() < 2 {
		t.Fatalf("AU yozilmadi: %d", tr.written.Load())
	}
	if n := len(reg.List()); n != 1 {
		t.Errorf("registry = %d, 1 kutilardi", n)
	}
}

func TestMediaMgr_OpenFailPublishFailed(t *testing.T) {
	m, reg, _, stopped := testMediaMgr(t)
	m.openFn = func(context.Context, publish.StartPlan, cams.Camera, string) (transport, error) {
		return nil, errors.New("whip 401")
	}
	m.OnStart(msStart())
	if ev := waitStopped(t, stopped); ev.Reason != publish.ReasonPublishFailed {
		t.Fatalf("stopped: %+v", ev)
	}
	if n := len(reg.List()); n != 0 {
		t.Errorf("registry = %d, 0 kutilardi", n)
	}
}

func TestMediaMgr_PullFailCameraOffline(t *testing.T) {
	m, _, _, stopped := testMediaMgr(t)
	m.pullFn = func(context.Context, cams.PullOptions, func(cams.AU) error) error {
		return cams.ErrCameraOffline
	}
	m.OnStart(msStart())
	if ev := waitStopped(t, stopped); ev.Reason != publish.ReasonCameraOffline {
		t.Fatalf("stopped: %+v", ev)
	}
}

func TestMediaMgr_StopClosesTransport(t *testing.T) {
	m, _, tr, stopped := testMediaMgr(t)
	m.OnStart(msStart())
	waitWritten(t, tr, 1)
	m.OnStop(proto.MediaStop{SessionID: "se_1", CameraID: "cam-7", Reason: publish.ReasonUserClose})
	if ev := waitStopped(t, stopped); ev.Reason != publish.ReasonUserClose {
		t.Fatalf("stopped: %+v", ev)
	}
	if !tr.closed.Load() {
		t.Error("transport yopilmadi")
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
