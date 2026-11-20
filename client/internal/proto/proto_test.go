package proto

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Golden testlar — testdata/protocol/*.json (docs/protocol.md misollari) bizning
// strukturalariga mos kelishini tekshiradi. Kontrakt o'zgarsa test darhol qoladi.
//
// Tekshiriladi:
//  1. fixture struktura bilan o'qiladi (noma'lum maydon = xato);
//  2. qayta kodlanib yana o'qiladi (round-trip);
//  3. ikki o'qish natijasi bir xil (maydon yo'qolmaydi).

func fixtureDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "protocol")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("fixture papkasi topilmadi (%v) — testdata/protocol bo'lishi kerak", err)
	}
	return dir
}

func TestGolden_AllFixtures(t *testing.T) {
	dir := fixtureDir(t)

	// fixture fayl nomi → struktura turi (§ raqami izohda)
	types := map[string]func() any{
		"auth.json":               func() any { return &Auth{} },          // §3.3
		"hello.json":              func() any { return &Hello{} },         // §4.1
		"welcome.json":            func() any { return &Welcome{} },       // §4.1
		"cams_sync.json":          func() any { return &CamsSync{} },      // §4.2
		"cams_ack.json":           func() any { return &CamsAck{} },       // §4.3
		"cam_status.json":         func() any { return &CamStatus{} },     // §4.4
		"codec_probe.json":        func() any { return &CodecProbe{} },    // §4.5
		"media_ready.json":        func() any { return &MediaReady{} },    // §4.6
		"media_stopped.json":      func() any { return &MediaStopped{} },  // §4.6
		"media_stats.json":        func() any { return &MediaStats{} },    // §4.6
		"hb.json":                 func() any { return &HB{} },            // §4.8
		"discovery_done.json":     func() any { return &DiscoveryDone{} }, // §4.7
		"selftest_done.json":      func() any { return &SelftestDone{} },  // §4.7
		"media_start.json":        func() any { return &MediaStart{} },    // §5.2
		"media_stop.json":         func() any { return &MediaStop{} },     // §5.3
		"media_refresh.json":      func() any { return &MediaRefresh{} },  // §5.4
		"codec_fix.json":          func() any { return &CodecFix{} },      // §5.5
		"codec_fix_done.json":     func() any { return &CodecFixDone{} },  // §5.5
		"secret_rotate.json":      func() any { return &SecretRotate{} },  // §5.6
		"secret_rotated.json":     func() any { return &SecretRotated{} }, // §5.6
		"config_push.json":        func() any { return &ConfigPush{} },    // §5.7
		"cams_request.json":       func() any { return &CamsRequest{} },   // §5.1
		"cams_request_delta.json": func() any { return &CamsRequest{} },   // §5.1
		"error.json":              func() any { return &ErrorMsg{} },      // §5.8
	}

	// Har bir .json fayl ro'yxatda bo'lishi shart — yangi fixture qo'shilsa,
	// test uni ham qamrab olishni talab qiladi.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("fixture o'qib bo'lmadi: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" || e.Name() == "envelope.json" {
			continue
		}
		if _, ok := types[e.Name()]; !ok {
			t.Errorf("fixture %s uchun struktura belgilanmagan (proto_test.go)", e.Name())
		}
	}

	for name, ctor := range types {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("o'qib bo'lmadi: %v", err)
			}

			first := ctor()
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(first); err != nil {
				t.Fatalf("fixture %s o'qilmadi (noma'lum maydon yoki tur xatosi): %v", name, err)
			}

			out, err := Encode(first)
			if err != nil {
				t.Fatalf("qayta kodlab bo'lmadi: %v", err)
			}

			second := ctor()
			dec2 := json.NewDecoder(bytes.NewReader(out))
			dec2.DisallowUnknownFields()
			if err := dec2.Decode(second); err != nil {
				t.Fatalf("round-trip o'qilmadi: %v (kodlangan: %s)", err, out)
			}

			if !reflect.DeepEqual(first, second) {
				t.Fatalf("round-trip bir xil emas:\n  1: %+v\n  2: %+v", first, second)
			}
		})
	}
}

func TestGolden_Envelope(t *testing.T) {
	dir := fixtureDir(t)
	raw, err := os.ReadFile(filepath.Join(dir, "envelope.json"))
	if err != nil {
		t.Fatalf("o'qib bo'lmadi: %v", err)
	}

	env, err := Decode[Envelope](raw)
	if err != nil {
		t.Fatalf("envelope o'qilmadi: %v", err)
	}
	if env.Type != MsgMediaReady {
		t.Fatalf("type noto'g'ri: %q", env.Type)
	}
	if env.Seq != 42 {
		t.Fatalf("seq noto'g'ri: %d", env.Seq)
	}
	if env.TS.Year() != 2026 {
		t.Fatalf("ts noto'g'ri: %v", env.TS)
	}

	mr, err := Decode[MediaReady](env.Data)
	if err != nil {
		t.Fatalf("media_ready o'qilmadi: %v", err)
	}
	if mr.PublishID != "pu_1" || mr.Ingest != "whip" || mr.ReadyMs != 640 {
		t.Fatalf("maydonlar noto'g'ri: %+v", mr)
	}
}

// Decode noma'lum maydonni rad etishi kerak — kontrakt drift'i shu yerda ushlanadi.
func TestDecode_RejectsUnknownField(t *testing.T) {
	raw := []byte(`{"uptime_s":1,"publishes":0,"mem_mb":1,"cams_total":0,"cams_online":0,"kelajak_maydon":7}`)
	if _, err := Decode[HB](raw); err == nil {
		t.Fatal("noma'lum maydon qabul qilindi — DisallowUnknownFields ishlamayapti")
	}
}

// Bo'sh data — nol qiymatli struktura qaytariishi kerak (pong/ping kabi).
func TestDecode_EmptyData(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, []byte(""), []byte("null"), []byte("{}")} {
		v, err := Decode[SecretRotated](raw)
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if v.OK {
			t.Fatalf("%q: OK false bo'lishi kerak edi", raw)
		}
	}
}

func TestDecode_BadJSON(t *testing.T) {
	if _, err := Decode[HB](json.RawMessage(`{`)); err == nil {
		t.Fatal("buzilgan JSON qabul qilindi")
	}
}

// NewEnvelope — envelope yig'uvchi yordamchi (§2)
func TestNewEnvelope(t *testing.T) {
	env, err := NewEnvelope(MsgHB, 42, map[string]int64{"uptime_s": 120})
	if err != nil {
		t.Fatal(err)
	}
	if env.Type != MsgHB || env.Seq != 42 {
		t.Fatalf("envelope: %+v", env)
	}
	if env.TS.Location() != time.UTC {
		t.Fatalf("ts UTC bo'lishi kerak: %v", env.TS)
	}
	// data JSON'ga kodlangan bo'lishi kerak
	var back map[string]int64
	if err := json.Unmarshal(env.Data, &back); err != nil {
		t.Fatalf("data JSON emas: %v", err)
	}
	if back["uptime_s"] != 120 {
		t.Fatalf("data: %s", env.Data)
	}
	// struct ham ishlaydi
	env2, err := NewEnvelope(MsgAuth, 1, Auth{AgentID: "ag_x", Secret: "s"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := Decode[Auth](env2.Data)
	if err != nil || a.AgentID != "ag_x" {
		t.Fatalf("round-trip: %v %+v", err, a)
	}
	// nil data → "{}" (xabarlar data'siz ham yuborilishi mumkin)
	env3, err := NewEnvelope(MsgPong, 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(env3.Data) != "null" {
		t.Logf("nil data → %s (Decode bunga yo'l qo'yadi)", env3.Data)
	}
	if _, err := Decode[Auth](env3.Data); err != nil {
		t.Fatalf("nil data decode: %v", err)
	}
	// kodlab bo'lmagan data → xato
	if _, err := NewEnvelope(MsgHB, 1, func() {}); err == nil {
		t.Fatal("funktsiya JSON'ga kodlanmasligi kerak edi")
	}
}

func TestProtocolVersionConstant(t *testing.T) {
	if ProtocolVersion != 1 {
		t.Fatalf("ProtocolVersion=%d, kontrakt §1 bo'yicha 1 bo'lishi kerak", ProtocolVersion)
	}
}
