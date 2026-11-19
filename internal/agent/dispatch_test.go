package agent

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"central-vms/client/internal/keystore"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/statestore"
)

// testHandlers — vaqtinchalik keystore/statestore bilan Handlers va yuborish kanalini ushlash
func testHandlers(t *testing.T) (*Handlers, chan proto.Envelope) {
	t.Helper()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "agent.key")
	if _, err := keystore.NewKeyFile(keyPath); err != nil {
		t.Fatal(err)
	}
	ks, err := keystore.Open(keyPath, filepath.Join(dir, "secrets.bin"))
	if err != nil {
		t.Fatal(err)
	}
	st := statestore.New(filepath.Join(dir, "state.json"))

	sendCh := make(chan proto.Envelope, 8)
	sess := NewSession(SessionConfig{ServerURL: "ws://x", AgentID: "ag_1", Secret: "s"})
	sess.setSendCh(sendCh)

	h := &Handlers{
		KS: ks, St: st, Sess: sess,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return h, sendCh
}

func envOf(t *testing.T, typ string, data any) proto.Envelope {
	t.Helper()
	env, err := proto.NewEnvelope(typ, 1, data)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestHandle_SecretRotate_OK(t *testing.T) {
	h, sendCh := testHandlers(t)
	h.Handle(envOf(t, proto.MsgSecretRotate, proto.SecretRotate{NewSecret: "YANGI-SECRET", GraceS: 30}))

	// keystore'da saqlanganmi
	got, ok := h.KS.Get(keystore.RefAgentSecret)
	if !ok || got != "YANGI-SECRET" {
		t.Fatalf("keystore: %q ok=%v", got, ok)
	}
	env := readEnv(t, sendCh)
	if env.Type != proto.MsgSecretRotated {
		t.Fatalf("javob turi=%q", env.Type)
	}
	ack, err := proto.Decode[proto.SecretRotated](env.Data)
	if err != nil || !ack.OK {
		t.Fatalf("ack: %+v err=%v", ack, err)
	}
}

func TestHandle_SecretRotate_NotoGriData(t *testing.T) {
	h, sendCh := testHandlers(t)
	h.Handle(envOf(t, proto.MsgSecretRotate, map[string]any{"grace_s": 5})) // new_secret yo'q
	env := readEnv(t, sendCh)
	ack, _ := proto.Decode[proto.SecretRotated](env.Data)
	if ack.OK {
		t.Fatal("ok=true bo'lishi mumkin emas")
	}
}

func TestHandle_SecretRotate_SaveXato(t *testing.T) {
	h, sendCh := testHandlers(t)
	// keystore store yo'lini yozib bo'lmadigan qilib qo'yamiz
	roDir := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(roDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "agent.key")
	if _, err := keystore.NewKeyFile(keyPath); err != nil {
		t.Fatal(err)
	}
	ks, err := keystore.Open(keyPath, filepath.Join(roDir, "secrets.bin"))
	if err != nil {
		t.Fatal(err)
	}
	h.KS = ks
	h.Handle(envOf(t, proto.MsgSecretRotate, proto.SecretRotate{NewSecret: "X", GraceS: 10}))
	env := readEnv(t, sendCh)
	ack, _ := proto.Decode[proto.SecretRotated](env.Data)
	if ack.OK {
		t.Fatal("saqlanmagan bo'lsa ok=false bo'lishi kerak")
	}
}

func TestHandle_ConfigPush(t *testing.T) {
	h, _ := testHandlers(t)
	var gotLevel string
	h.SetLogLevel = func(l string) error { gotLevel = l; return nil }

	lvl := "debug"
	h.Handle(envOf(t, proto.MsgConfigPush, proto.ConfigPush{
		Settings: proto.Settings{LogLevel: &lvl, ProbePacingParallel: intptr(5)},
	}))
	if gotLevel != "debug" {
		t.Fatalf("log_level=%q", gotLevel)
	}
	// SetLogLevel xato bersa ham panic bo'lmasligi kerak
	h.SetLogLevel = func(string) error { return errors.New("xato") }
	h.Handle(envOf(t, proto.MsgConfigPush, proto.ConfigPush{
		Settings: proto.Settings{LogLevel: &lvl},
	}))
}

func TestHandle_MediaRefresh(t *testing.T) {
	h, _ := testHandlers(t)
	var got proto.MediaRefresh
	h.RefreshMedia = func(m proto.MediaRefresh) error { got = m; return nil }
	h.Handle(envOf(t, proto.MsgMediaRefresh, proto.MediaRefresh{
		SessionID: "ses_1", CameraID: "cam-1", ExtendS: 60,
	}))
	if got.SessionID != "ses_1" || got.ExtendS != 60 {
		t.Fatalf("refresh: %+v", got)
	}
	// hook bo'lmasa ham xato bermasin (C3 gacha)
	h.RefreshMedia = nil
	h.Handle(envOf(t, proto.MsgMediaRefresh, proto.MediaRefresh{SessionID: "s"}))
}

func TestHandle_PingPong(t *testing.T) {
	h, sendCh := testHandlers(t)
	h.Handle(envOf(t, proto.MsgPing, map[string]any{}))
	env := readEnv(t, sendCh)
	if env.Type != proto.MsgPong {
		t.Fatalf("javob=%q, kutilgan pong", env.Type)
	}
}

func TestHandle_NomaolumTurShovqinsiz(t *testing.T) {
	h, sendCh := testHandlers(t)
	h.Handle(envOf(t, "kelajak_xabari", map[string]any{"x": 1}))
	select {
	case e := <-sendCh:
		t.Fatalf("noma'lum turga javob berildi: %q", e.Type)
	default:
	}
}

// C3.20: media_start/stop/codec_fix dispatch routing.
func TestHandle_MediaRouting(t *testing.T) {
	h, _ := testHandlers(t)
	var gotStart proto.MediaStart
	var gotStop proto.MediaStop
	var gotFix proto.CodecFix
	h.MediaStart = func(m proto.MediaStart) { gotStart = m }
	h.MediaStop = func(m proto.MediaStop) { gotStop = m }
	h.MediaCodecFix = func(m proto.CodecFix) { gotFix = m }

	h.Handle(envOf(t, proto.MsgMediaStart, proto.MediaStart{
		SessionID: "se_1", CameraID: "cam-7", Profile: "main", Kind: "play", TTLS: 60, Ingest: "whip"}))
	h.Handle(envOf(t, proto.MsgMediaStop, proto.MediaStop{SessionID: "se_1", CameraID: "cam-7", Reason: "user_close"}))
	h.Handle(envOf(t, proto.MsgCodecFix, proto.CodecFix{CameraID: "cam-7", Target: proto.CodecFixTarget{Codec: "h264"}}))

	if gotStart.SessionID != "se_1" || gotStart.TTLS != 60 || gotStart.Ingest != "whip" {
		t.Errorf("media_start: %+v", gotStart)
	}
	if gotStop.Reason != "user_close" {
		t.Errorf("media_stop: %+v", gotStop)
	}
	if gotFix.CameraID != "cam-7" || gotFix.Target.Codec != "h264" {
		t.Errorf("codec_fix: %+v", gotFix)
	}
}

// Handler nil bo'lsa xabar shovqinsiz o'tkaziladi (§1 forward-compat).
func TestHandle_MediaNilHandler(t *testing.T) {
	h, sendCh := testHandlers(t)
	h.Handle(envOf(t, proto.MsgMediaStart, proto.MediaStart{SessionID: "se_1"}))
	select {
	case e := <-sendCh:
		t.Fatalf("nil handler javob yubordi: %q", e.Type)
	default:
	}
}

func TestSend_SessiyaYopiq(t *testing.T) {
	s := NewSession(SessionConfig{ServerURL: "ws://x"})
	if err := s.Send(proto.MsgHB, map[string]any{}); !errors.Is(err, ErrChannelClosed) {
		t.Fatalf("xato: %v", err)
	}
}

func readEnv(t *testing.T, ch chan proto.Envelope) proto.Envelope {
	t.Helper()
	select {
	case e := <-ch:
		return e
	default:
		t.Fatal("javob yuborilmadi")
		return proto.Envelope{}
	}
}

func intptr(v int) *int { return &v }
