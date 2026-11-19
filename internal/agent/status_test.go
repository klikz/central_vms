package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"central-vms/client/internal/clock"
	"central-vms/client/internal/proto"
)

// capSender — yuborilgan xabarlarni (typ + data) ushlab turadi.
type capSender struct {
	mu    sync.Mutex
	typs  []string
	datas []any
	err   error
}

func (c *capSender) Send(typ string, data any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.typs = append(c.typs, typ)
	c.datas = append(c.datas, data)
	return c.err
}

func (c *capSender) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.typs)
}

func (c *capSender) lastData() any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.datas) == 0 {
		return nil
	}
	return c.datas[len(c.datas)-1]
}

// goldenEq — yuborilgan strukturaning JSON'i fixture bilan bir xil miydor
// (maydon tartibi ahamiyatsiz).
func goldenEq(t *testing.T, fixture string, got any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "protocol", fixture))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	out, err := proto.Encode(got)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var a, b any
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatalf("fixture parse: %v", err)
	}
	if err := json.Unmarshal(out, &b); err != nil {
		t.Fatalf("got parse: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("JSON fixture bilan mos emas:\n  fixture: %s\n  yuborildi: %s", raw, out)
	}
}

// §4.4: cam_status chiqishi fixture (golden JSON) bilan bir xil.
func TestStatus_CamStatusGolden(t *testing.T) {
	s := capSender{}
	r := NewStatusReporter(&s, clock.NewFake(time.Unix(0, 0)), nil)
	cs := proto.CamStatus{
		CameraID:  "6f0f1f2a-0000-4000-8000-000000000001",
		Status:    "online",
		Error:     "",
		CheckedAt: time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC),
		RTTMs:     42,
	}
	if err := r.SendCamStatus(cs); err != nil {
		t.Fatalf("send: %v", err)
	}
	if s.count() != 1 || s.typs[0] != proto.MsgCamStatus {
		t.Fatalf("typ/son = %v, cam_status bitta kutilardi", s.typs)
	}
	goldenEq(t, "cam_status.json", s.lastData())
}

// §4.5: codec_probe chiqishi fixture bilan bir xil.
func TestStatus_CodecProbeGolden(t *testing.T) {
	s := capSender{}
	r := NewStatusReporter(&s, clock.NewFake(time.Unix(0, 0)), nil)
	p := proto.CodecProbe{
		CameraID:    "6f0f1f2a-0000-4000-8000-000000000001",
		Codecs:      proto.CameraProfilePair{Main: "h264", Sub: "h265"},
		BFrame:      proto.CameraBoolPair{Main: true, Sub: false},
		ProfilesSDP: proto.CameraProfilePair{Main: "High", Sub: "Baseline"},
		OnvifSwitch: false,
		Reason:      nil,
	}
	if err := r.SendCodecProbe(p); err != nil {
		t.Fatalf("send: %v", err)
	}
	if s.count() != 1 || s.typs[0] != proto.MsgCodecProbe {
		t.Fatalf("typ/son = %v, codec_probe bitta kutilardi", s.typs)
	}
	goldenEq(t, "codec_probe.json", s.lastData())
}

// §4.4: bir xil online holat takrorlanmaydi; xato holati 5 min'da bir takrorlanadi.
func TestStatus_DedupAndErrorRepeat(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	s := capSender{}
	r := NewStatusReporter(&s, clk, nil)
	base := proto.CamStatus{CameraID: "c1", Status: "online", CheckedAt: clk.Now()}

	// online → online: ikkinchisi siqiladi.
	_ = r.SendCamStatus(base)
	_ = r.SendCamStatus(base)
	if s.count() != 1 {
		t.Fatalf("online takrorlandi: %d, 1 kutilardi", s.count())
	}

	// online → offline: yuboriladi (o'zgardi).
	off := base
	off.Status = "offline"
	off.Error = "connection_refused"
	_ = r.SendCamStatus(off)
	if s.count() != 2 {
		t.Fatalf("o'zgarish yuborilmadi: %d, 2 kutilardi", s.count())
	}

	// offline → offline: 5 min'gacha siqiladi.
	clk.Advance(4 * time.Minute)
	_ = r.SendCamStatus(off)
	if s.count() != 2 {
		t.Fatalf("4 min'da takrorlandi: %d, 2 kutilardi", s.count())
	}
	// 5 min'dan keyin takrorlanadi.
	clk.Advance(1 * time.Minute)
	_ = r.SendCamStatus(off)
	if s.count() != 3 {
		t.Fatalf("5 min'da takrorlanmadi: %d, 3 kutilardi", s.count())
	}

	// offline → online: darhol (o'zgardi), taymer bekor.
	_ = r.SendCamStatus(base)
	if s.count() != 4 {
		t.Fatalf("online'ga qaytish yuborilmadi: %d, 4 kutilardi", s.count())
	}
}

// CameraID bo'sh bo'lsa xato (server bu xabarni qabul qilmaydi).
func TestStatus_BoshCameraID(t *testing.T) {
	s := capSender{}
	r := NewStatusReporter(&s, clock.NewFake(time.Unix(0, 0)), nil)
	if err := r.SendCamStatus(proto.CamStatus{}); err == nil {
		t.Error("cam_status bo'sh camera_id'da xato berishi kerak")
	}
	if err := r.SendCodecProbe(proto.CodecProbe{}); err == nil {
		t.Error("codec_probe bo'sh camera_id'da xato berishi kerak")
	}
	if s.count() != 0 {
		t.Errorf("xato holatida sender chaqirildi: %d", s.count())
	}
}

// codec_probe reason matni redaktasiyadan o'tadi (parol tushib qolmasin, §10.2).
func TestStatus_CodecProbeReasonRedaktasiya(t *testing.T) {
	s := capSender{}
	r := NewStatusReporter(&s, clock.NewFake(time.Unix(0, 0)), nil)
	reason := "rtsp://admin:superxato@10.0.0.1:554/cam"
	_ = r.SendCodecProbe(proto.CodecProbe{CameraID: "c1", Reason: &reason})
	got, ok := s.lastData().(proto.CodecProbe)
	if !ok || got.Reason == nil {
		t.Fatalf("kutilmagan: %+v", s.lastData())
	}
	if *got.Reason == reason {
		t.Errorf("reason redaktasiya qilinmadi: %q", *got.Reason)
	}
}
