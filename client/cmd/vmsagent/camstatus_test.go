package main

import (
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"central-vms/client/internal/agent"
	"central-vms/client/internal/cams"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/statestore"
)

type fakeSender struct {
	mu   sync.Mutex
	typs []string
	msgs []proto.CamStatus
}

func (f *fakeSender) Send(typ string, data any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.typs = append(f.typs, typ)
	if cs, ok := data.(proto.CamStatus); ok {
		f.msgs = append(f.msgs, cs)
	}
	return nil
}

func (f *fakeSender) all() ([]string, []proto.CamStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.typs...), append([]proto.CamStatus(nil), f.msgs...)
}

// §4.4: sessiya ochilganda barcha kamera holatlari serverga qayta
// yuboriladi — aks holda serverda "offline" qolib qolardi (2026-11-20).
func TestReportAllCamStatus(t *testing.T) {
	st, err := statestore.Load(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.Cameras = []cams.Camera{
		{ID: "cam-1", Status: "online", Enabled: true},
		{ID: "cam-2", Status: "offline", Enabled: true},
		{ID: "cam-3", Enabled: true}, // holati yo'q → unknown
	}
	fs := &fakeSender{}
	r := &runtime{log: slog.New(slog.NewTextHandler(discardW{}, nil)), st: st}
	r.rep = agent.NewStatusReporter(fs, nil, nil)

	r.reportAllCamStatus()

	typs, msgs := fs.all()
	if len(typs) != 3 {
		t.Fatalf("yuborilgan xabarlar: %d (3 kutilgan): %v", len(typs), typs)
	}
	for _, ty := range typs {
		if ty != proto.MsgCamStatus {
			t.Fatalf("xabar turi: %s", ty)
		}
	}
	want := map[string]string{"cam-1": "online", "cam-2": "offline", "cam-3": "unknown"}
	for _, m := range msgs {
		if want[m.CameraID] != m.Status {
			t.Errorf("%s: %s, %s kutilgan", m.CameraID, m.Status, want[m.CameraID])
		}
		if m.CheckedAt.IsZero() {
			t.Errorf("%s: checked_at yo'q", m.CameraID)
		}
	}
}

// §4.4: probe natijasi xato matni bilan birga yuboriladi (server UI'da
// sabab ko'rinishi uchun).
func TestReportCamStatusWithError(t *testing.T) {
	fs := &fakeSender{}
	r := &runtime{log: slog.New(slog.NewTextHandler(discardW{}, nil))}
	r.rep = agent.NewStatusReporter(fs, nil, nil)

	r.reportCamStatus(cams.ProbeResult{ID: "cam-9", Status: cams.StatusOffline,
		Method: "dial", Err: "unreachable"})

	_, msgs := fs.all()
	if len(msgs) != 1 {
		t.Fatalf("xabarlar: %d", len(msgs))
	}
	if msgs[0].CameraID != "cam-9" || msgs[0].Status != "offline" || msgs[0].Error != "unreachable" {
		t.Fatalf("xabar: %+v", msgs[0])
	}
	// Reporter dedup: bir xil holat takrorlanmaydi.
	before := len(msgs)
	r.reportCamStatus(cams.ProbeResult{ID: "cam-9", Status: cams.StatusOffline, Err: "unreachable"})
	if _, again := fs.all(); len(again) != before {
		t.Fatal("o'zgarmagan holat takror yuborildi")
	}
	// O'zgarsa yuboriladi.
	r.reportCamStatus(cams.ProbeResult{ID: "cam-9", Status: cams.StatusOnline})
	if _, after := fs.all(); len(after) != before+1 {
		t.Fatal("o'zgargan holat yuborilmadi")
	}
	_ = time.Now
}

// Sessiya bo'lmasa (reporter nil) — xato bermaydi, jim o'tadi.
func TestReportCamStatusNoSession(t *testing.T) {
	r := &runtime{log: slog.New(slog.NewTextHandler(discardW{}, nil))}
	r.reportCamStatus(cams.ProbeResult{ID: "cam-x", Status: cams.StatusOnline})
	r.reportAllCamStatus()
}
