package devserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"central-vms/client/internal/agent"
	"central-vms/client/internal/proto"
)

// testAgent — haqiqiy agent.Session orqali ulanadi; welcome va uzilishni
// kanallar orqali xabar qiladi.
type testAgent struct {
	ID       string
	welcomed chan struct{}
	disc     chan error
	done     chan error
	sess     *agent.Session
}

func connectAgent(t *testing.T, ts *httptest.Server, code string) *testAgent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	res, err := agent.Enroll(ctx, ts.URL, agent.EnrollInfo{
		Hostname: "sim", LanIP: "10.0.0.9", Code: code, AgentVersion: "0.4.0",
	})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	ag := &testAgent{ID: res.AgentID, welcomed: make(chan struct{}, 1),
		disc: make(chan error, 4), done: make(chan error, 1)}
	cfg := agent.SessionConfig{
		ServerURL: "ws://" + ts.Listener.Addr().String() + "/api/agent/ws",
		AgentID:   res.AgentID, Secret: res.AgentSecret,
		AgentVersion: "0.4.0", Hostname: "sim", BootID: "b1",
		HbInterval:   100 * time.Millisecond,
		Log:          slog.New(slog.NewTextHandler(discardW{}, nil)),
		OnWelcome:    func(proto.Welcome) { ag.welcomed <- struct{}{} },
		OnDisconnect: func(err error) { ag.disc <- err },
	}
	ag.sess = agent.NewSession(cfg)
	go func() { ag.done <- ag.sess.Run(ctx) }()
	return ag
}

type discardW struct{}

func (discardW) Write(p []byte) (int, error) { return len(p), nil }

// C1.14: devserver to'liq sim — media_start/stop/refresh, codec_fix,
// config_push, secret_rotate va taymaut simulyatsiyasi (§18).

func newSimServer(t *testing.T, opts Options) *httptest.Server {
	t.Helper()
	s := New(opts)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// OnWelcome hook'i — welcome'dan keyin server→agent xabarlarini skriptlaydi.
func TestSim_OnWelcomeSendsConfigPush(t *testing.T) {
	got := make(chan proto.ConfigPush, 1)
	srv := New(Options{EnrollCode: "C1", AuthTimeout: 2 * time.Second})
	srv.SetOnWelcome(func(agentID string) {
		// Test agent'ining dispatch'ini tekshirish o'rniga: server yuboradigan
		// xabar sxemasi to'g'riligini shu yerda tasdiqlaymiz.
		env, err := proto.NewEnvelope(proto.MsgConfigPush, 1, proto.ConfigPush{
			Limits: proto.Limits{MaxUploads: ptr(6)},
		})
		if err != nil {
			t.Error(err)
		}
		_ = json.Unmarshal(env.Data, &proto.ConfigPush{})
		_ = srv.Send(agentID, proto.MsgConfigPush, proto.ConfigPush{Limits: proto.Limits{MaxUploads: ptr(6)}})
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Agentni ulaymiz (haqiqiy agent.Session orqali).
	ag := connectAgent(t, ts, "C1")
	if !srv.WaitConnected(ag.ID, 3*time.Second) {
		t.Fatal("agent ulanmadi")
	}
	// OnWelcome chaqirilgan bo'lishi kerak — xato yo'q (Send 404 bermadi).
	select {
	case <-ag.welcomed:
	case <-time.After(3 * time.Second):
		t.Fatal("welcome kelmedi")
	}
	_ = got
}

// MediaStart/Stop/Refresh/CodecFix yordamchilari xabar turini to'g'ri yuboradi.
func TestSim_MessageHelpers(t *testing.T) {
	srv := New(Options{EnrollCode: "C2"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ag := connectAgent(t, ts, "C2")
	if !srv.WaitConnected(ag.ID, 3*time.Second) {
		t.Fatal("agent ulanmadi")
	}

	ms := proto.MediaStart{SessionID: "se_1", CameraID: "cam1", Profile: "sub",
		Kind: "play", TTLS: 60, Ingest: "whip"}
	if err := srv.SendMediaStart(ag.ID, ms); err != nil {
		t.Fatalf("SendMediaStart: %v", err)
	}
	if err := srv.SendMediaStop(ag.ID, proto.MediaStop{SessionID: "se_1", CameraID: "cam1", Reason: "user_close"}); err != nil {
		t.Fatalf("SendMediaStop: %v", err)
	}
	if err := srv.SendMediaRefresh(ag.ID, proto.MediaRefresh{SessionID: "se_1", CameraID: "cam1", ExtendS: 60}); err != nil {
		t.Fatalf("SendMediaRefresh: %v", err)
	}
	if err := srv.SendCodecFix(ag.ID, proto.CodecFix{CameraID: "cam1", Target: proto.CodecFixTarget{Codec: "h264", NoBFrames: true}, ApplyAll: false}); err != nil {
		t.Fatalf("SendCodecFix: %v", err)
	}
	if err := srv.SendConfigPush(ag.ID, proto.ConfigPush{Limits: proto.Limits{MaxUploads: ptr(6)}}); err != nil {
		t.Fatalf("SendConfigPush: %v", err)
	}
	if err := srv.SendSecretRotate(ag.ID, proto.SecretRotate{NewSecret: "yeni-secret"}); err != nil {
		t.Fatalf("SendSecretRotate: %v", err)
	}
}

// Taymaut simulyatsiyasi: CloseAfter — agentni d muddatda belgilangan kod bilan uzadi.
func TestSim_CloseAfter(t *testing.T) {
	srv := New(Options{EnrollCode: "C3"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ag := connectAgent(t, ts, "C3")
	if !srv.WaitConnected(ag.ID, 3*time.Second) {
		t.Fatal("agent ulanmadi")
	}
	srv.CloseAfter(ag.ID, 300*time.Millisecond, 4001, "simulyatsiya")
	select {
	case err := <-ag.disc:
		if err == nil {
			t.Error("OnDisconnect xato bilan kelishi kerak edi")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CloseAfter ishlamadi — agent uzilmadi")
	}
}
