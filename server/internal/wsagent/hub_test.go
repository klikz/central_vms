// Hub testlari: haqiqiy WS (httptest) + migratsiyadan o'tgan DB.
// Idle/ping testi fake clock bilan (TASKS §0.2).
package wsagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/server/internal/proto"
	"central-vms/server/internal/registry"
	"central-vms/server/internal/store"
)

func errorsAs(err error, target any) bool { return errors.As(err, target) }

type testEnv struct {
	hub *Hub
	reg *registry.Registry
	srv *httptest.Server
	fc  *fakeClock
	db  interface{ Close() error }
}

func newTestEnv(t *testing.T) *testEnv { return newTestEnvWith(t, Config{}) }

// newTestEnvWith — Config'ning ba'zi maydonlarini test uchun
// qisqartirish imkoni (masalan RotateWait).
func newTestEnvWith(t *testing.T, extra Config) *testEnv {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "ws.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
	if _, err := store.MigrateUp(db, dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	reg := registry.New(db)
	fc := &fakeClock{t: time.Date(2026, 11, 20, 5, 0, 0, 0, time.UTC)}
	lg := slog.Default()
	if os.Getenv("VMS_TESTLOG") != "" {
		lg = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	cfg := Config{ServerVersion: "test", MinProto: 1, MaxProto: 1, HandshakeTO: 2 * time.Second, IdleTO: time.Second, DeadTO: 5 * time.Second}
	if extra.RotateWait > 0 {
		cfg.RotateWait = extra.RotateWait
	}
	hub := NewHub(cfg, reg, lg)
	hub.clock = fc
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeHTTP))
	t.Cleanup(func() { srv.Close(); db.Close() })
	return &testEnv{hub: hub, reg: reg, srv: srv, fc: fc, db: db}
}

// enroll — test yordamchisi: §3 bo'yicha so'rov yuboriladi va admin
// tasdiqlaydi (kod oqimi 1.0-draft.10 bilan bekor qilindi).
func (e *testEnv) enroll(t *testing.T) (agentID, secret string) {
	t.Helper()
	j := NewJoiner(e.hub.registry.DB(), e.reg, slog.Default(), 100, e.fc)
	secret = "test-secret-" + randHexLower(24)
	out, err := j.Request(JoinInput{
		JoinKey: randHexLower(32), AgentSecret: secret,
		Hostname: "joy", OS: "ubuntu", LanIP: "10.0.0.9", AgentVersion: "0.1.0",
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if out.Status != "pending" {
		t.Fatalf("so'rov holati: %+v", out)
	}
	ap, err := j.Approve(out.RequestID, "", 0)
	if err != nil {
		t.Fatalf("tasdiq: %v", err)
	}
	return ap.AgentID, secret
}

func wsURL(s string) string { return "ws://" + strings.TrimPrefix(s, "http://") }

// dialHandshake — auth → hello → welcome (o'qib qaytaradi).
// startHB — test agentining "jonli" ekanligini ko'rsatish (hb, 500ms).
func startHB(t *testing.T, ws *websocket.Conn) {
	t.Helper()
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		tk := time.NewTicker(500 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-done:
				return
			case <-tk.C:
				_ = ws.WriteJSON(proto.Envelope{Type: proto.MsgHB, Seq: 9, TS: time.Now().UTC(), Data: json.RawMessage(`{}`)})
			}
		}
	}()
}

func dialHandshake(t *testing.T, e *testEnv, agentID, secret string, protoVer int) (*websocket.Conn, proto.Welcome) {
	t.Helper()
	return dialHandshakeWith(t, e, agentID, secret, proto.Hello{
		ProtocolVersion: protoVer, AgentVersion: "t", OS: "linux",
		Hostname: "h", CamsEpoch: 0, Caps: proto.Caps{GPU: "none"},
	})
}

// dialHandshakeWith — hello maydonlarini to'liq nazorat qilish uchun
// (masalan `name` bilan qayta ulanishni sinash).
func dialHandshakeWith(t *testing.T, e *testEnv, agentID, secret string, hello proto.Hello) (*websocket.Conn, proto.Welcome) {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial(wsURL(e.srv.URL)+"?agent_id="+agentID, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { ws.Close() })
	send := func(typ string, data any) {
		raw, _ := json.Marshal(data)
		if err := ws.WriteJSON(proto.Envelope{Type: typ, Seq: 1, TS: time.Now().UTC(), Data: raw}); err != nil {
			t.Fatalf("send %s: %v", typ, err)
		}
	}
	send(proto.MsgAuth, proto.Auth{AgentID: agentID, Secret: secret})
	send(proto.MsgHello, hello)
	env := readEnv(t, ws)
	if env.Type != proto.MsgWelcome {
		t.Fatalf("birinchi xabar %q", env.Type)
	}
	var w proto.Welcome
	if err := json.Unmarshal(env.Data, &w); err != nil {
		t.Fatalf("welcome: %v", err)
	}
	return ws, w
}

func readEnvUntil(t *testing.T, ws *websocket.Conn, typ string) proto.Envelope {
	t.Helper()
	for range 20 {
		env := readEnv(t, ws)
		if env.Type == typ {
			return env
		}
	}
	t.Fatalf("%q topilmadi", typ)
	return proto.Envelope{}
}

func readEnv(t *testing.T, ws *websocket.Conn) proto.Envelope {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var env proto.Envelope
	if err := ws.ReadJSON(&env); err != nil {
		t.Fatalf("read: %v", err)
	}
	return env
}

func TestHandshakeAndWelcome(t *testing.T) {
	e := newTestEnv(t)
	agentID, secret := e.enroll(t)
	ws, w := dialHandshake(t, e, agentID, secret, 1)
	if !w.Compat.OK || w.ServerVersion != "test" || !w.NeedsFullSync {
		t.Fatalf("welcome: %+v", w)
	}
	if w.Limits.MaxUploads == nil || *w.Limits.MaxUploads != 8 {
		t.Fatalf("limits: %+v", w.Limits)
	}
	if !e.hub.Online(agentID) {
		t.Fatal("hub online emas")
	}
	c, err := e.reg.GetClientByAgentID(agentID)
	if err != nil || c.Status != "online" || c.Version != "t" {
		t.Fatalf("hello DB'ga tushmadi: %+v %v", c, err)
	}
	_ = ws
}

func TestHandshakeErrors(t *testing.T) {
	e := newTestEnv(t)
	agentID, secret := e.enroll(t)

	// Noto'g'ri secret → 4003.
	ws, _, err := websocket.DefaultDialer.Dial(wsURL(e.srv.URL)+"?agent_id="+agentID, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(proto.Auth{AgentID: agentID, Secret: "yomoq"})
	_ = ws.WriteJSON(proto.Envelope{Type: proto.MsgAuth, Data: raw})
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err = ws.ReadMessage()
	var ce *websocket.CloseError
	if err == nil || !errorsAs(err, &ce) || ce.Code != CloseBadSecret {
		t.Fatalf("secret xato: %v", err)
	}
	ws.Close()

	// Protocol mos emas → welcome{compat:false} + 4001.
	ws2, _, err := websocket.DefaultDialer.Dial(wsURL(e.srv.URL)+"?agent_id="+agentID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws2.Close()
	rawA, _ := json.Marshal(proto.Auth{AgentID: agentID, Secret: secret})
	rawH, _ := json.Marshal(proto.Hello{ProtocolVersion: 9})
	_ = ws2.WriteJSON(proto.Envelope{Type: proto.MsgAuth, Data: rawA})
	_ = ws2.WriteJSON(proto.Envelope{Type: proto.MsgHello, Data: rawH})
	env := readEnv(t, ws2)
	var w proto.Welcome
	_ = json.Unmarshal(env.Data, &w)
	if w.Compat.OK || w.Compat.MinSupported != 1 || w.Compat.MaxSupported != 1 || w.Compat.ServerVersion != "test" {
		t.Fatalf("compat xato kutilardi: %+v", w)
	}
	_ = ws2.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err = ws2.ReadMessage()
	if err == nil || !errorsAs(err, &ce) || ce.Code != CloseIncompat {
		t.Fatalf("4001 kutilardi: %v", err)
	}
}

func TestCamsSyncAckDeleteCascade(t *testing.T) {
	e := newTestEnv(t)
	agentID, secret := e.enroll(t)
	ws, _ := dialHandshake(t, e, agentID, secret, 1)
	startHB(t, ws)

	send := func(typ string, data any) {
		raw, _ := json.Marshal(data)
		_ = ws.WriteJSON(proto.Envelope{Type: typ, Seq: 2, TS: time.Now().UTC(), Data: raw})
	}
	cam := func(id, name string) proto.CameraEntry {
		return proto.CameraEntry{Op: "upsert", ID: id, Name: name,
			Codecs: proto.CameraProfilePair{Main: "h265", Sub: "h264"}, Status: "online", Position: 1}
	}
	send(proto.MsgCamsSync, proto.CamsSync{Epoch: 100, Full: true, SeqNo: 1, ChunkCount: 2, Done: false,
		Cameras: []proto.CameraEntry{cam("k1", "B1"), cam("k2", "B2")}})
	send(proto.MsgCamsSync, proto.CamsSync{Epoch: 100, Full: true, SeqNo: 2, ChunkCount: 2, Done: true,
		Cameras: []proto.CameraEntry{cam("k3", "B3")}})
	var ack proto.CamsAck
	for {
		env := readEnv(t, ws)
		if env.Type == proto.MsgCamsAck {
			_ = json.Unmarshal(env.Data, &ack)
			break
		}
	}
	if !ack.OK || ack.Applied != 3 || ack.Epoch != 100 {
		t.Fatalf("ack: %+v", ack)
	}
	c, err := e.reg.GetClientByAgentID(agentID)
	if err != nil {
		t.Fatal(err)
	}
	cams, _ := e.reg.ListCamerasByClient(c.ID)
	if len(cams) != 3 {
		t.Fatalf("kamera soni: %d", len(cams))
	}

	// Eski epoch → stale_epoch.
	send(proto.MsgCamsSync, proto.CamsSync{Epoch: 99, Full: true, SeqNo: 1, ChunkCount: 1, Done: true})
	for {
		env := readEnv(t, ws)
		if env.Type == proto.MsgCamsAck {
			_ = json.Unmarshal(env.Data, &ack)
			break
		}
	}
	if ack.OK || ack.Reason == nil || *ack.Reason != "stale_epoch" {
		t.Fatalf("stale kutilardi: %+v", ack)
	}

	// delete + cascade (favorites va layout sloti).
	u, _ := e.reg.CreateUser("v", "h", "viewer", "")
	_ = e.reg.AddFavorite(u, "k2", 0)
	lid, _ := e.reg.CreateLayout(u, "L", 2, fmt.Sprintf(`["k1","k2"]`))
	_ = lid
	send(proto.MsgCamsSync, proto.CamsSync{Epoch: 101, Full: false, SeqNo: 1, ChunkCount: 1, Done: true,
		Cameras: []proto.CameraEntry{{Op: "delete", ID: "k2"}}})
	for {
		env := readEnv(t, ws)
		if env.Type == proto.MsgCamsAck {
			_ = json.Unmarshal(env.Data, &ack)
			break
		}
	}
	if !ack.OK {
		t.Fatalf("delete ack: %+v", ack)
	}
	if _, err := e.reg.GetCamera("k2"); err == nil {
		t.Fatal("k2 o'chirilmadi")
	}
	ids, _ := e.reg.ListFavoriteCameraIDs(u)
	if len(ids) != 0 {
		t.Fatalf("favorites cascade: %v", ids)
	}
	ls, _ := e.reg.ListLayouts(u)
	if ls[0].SlotsJSON != `["k1",null]` {
		t.Fatalf("layout slot: %s", ls[0].SlotsJSON)
	}

	// cam_status + codec_probe.
	send(proto.MsgCamStatus, proto.CamStatus{CameraID: "k1", Status: "offline"})
	send(proto.MsgCodecProbe, proto.CodecProbe{CameraID: "k1",
		Codecs: proto.CameraProfilePair{Main: "h265", Sub: "h264"}})
	deadline := time.Now().Add(3 * time.Second)
	for {
		k, err := e.reg.GetCamera("k1")
		if err == nil && k.Status == "offline" && k.CodecProbeAt != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status/probe: %+v err=%v", k, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSecretRotate(t *testing.T) {
	e := newTestEnv(t)
	agentID, secret := e.enroll(t)
	ws, _ := dialHandshake(t, e, agentID, secret, 1)
	startHB(t, ws)

	// Ikki bosqichli rotate: DB hash FAQAT agent tasdig'idan keyin
	// almashtiriladi (2026-11-21 server auditi).
	type rot struct {
		s   string
		err error
	}
	ch := make(chan rot, 1)
	go func() {
		s, err := e.hub.RotateSecret(agentID, 30)
		ch <- rot{s, err}
	}()
	env := readEnvUntil(t, ws, proto.MsgSecretRotate)
	if env.Type != proto.MsgSecretRotate {
		t.Fatalf("xabar: %q", env.Type)
	}
	var sr proto.SecretRotate
	_ = json.Unmarshal(env.Data, &sr)
	if sr.GraceS != 30 {
		t.Fatalf("secret_rotate: %+v", sr)
	}
	rawAck, _ := json.Marshal(proto.SecretRotated{OK: true})
	_ = ws.WriteJSON(proto.Envelope{Type: proto.MsgSecretRotated, Seq: 3, TS: time.Now().UTC(), Data: rawAck})
	got := <-ch
	if got.err != nil {
		t.Fatalf("rotate: %v", got.err)
	}
	newSecret := got.s
	if newSecret == "" || sr.NewSecret != newSecret {
		t.Fatalf("secret mos emas: %+v", sr)
	}
	// Eski secret endi ishlamaydi: eski secret bilan yangi ulanish 4003.
	if ws3, _, err := websocket.DefaultDialer.Dial(wsURL(e.srv.URL)+"?agent_id="+agentID, nil); err == nil {
		rawOld, _ := json.Marshal(proto.Auth{AgentID: agentID, Secret: secret})
		_ = ws3.WriteJSON(proto.Envelope{Type: proto.MsgAuth, Data: rawOld})
		_ = ws3.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, _, err := ws3.ReadMessage(); err == nil {
			ws3.Close()
			t.Fatal("eski secret qabul qilindi")
		}
		ws3.Close()
	}

	// Yangi secret bilan qayta ulanish — config_push endi shu conn'ga boradi.
	ws2, _ := dialHandshake(t, e, agentID, newSecret, 1)
	startHB(t, ws2)
	mu := 5
	if err := e.hub.PushConfig(agentID, proto.Limits{MaxUploads: &mu}, proto.Settings{}); err != nil {
		t.Fatalf("push: %v", err)
	}
	env = readEnvUntil(t, ws2, proto.MsgConfigPush)
	var cp proto.ConfigPush
	_ = json.Unmarshal(env.Data, &cp)
	if cp.Limits.MaxUploads == nil || *cp.Limits.MaxUploads != 5 {
		t.Fatalf("config_push: %+v", cp)
	}
}

func TestIdlePingKeepsAlive(t *testing.T) {
	e := newTestEnv(t)
	agentID, secret := e.enroll(t)
	ws, _ := dialHandshake(t, e, agentID, secret, 1)
	pinged := make(chan string, 8)
	ws.SetPingHandler(func(appData string) error {
		pinged <- appData // pongni test goroutine'si yuboradi (handler ichida yozish deadlock xavfi)
		return nil
	})
	// Kontrol frame'lari ishlashi uchun client tomonda uzluksiz o'quvchi kerak.
	go func() {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()
	// Server IdleTO=3s da WS control ping yuboradi; pong qaytargan agent tirik qoladi.
	select {
	case p := <-pinged:
		if err := ws.WriteMessage(websocket.PongMessage, []byte(p)); err != nil {
			t.Fatalf("pong: %v", err)
		}
		startHB(t, ws) // hb ham faollikni uzaytiradi (ikkala yo'l tekshiriladi)
	case <-time.After(5 * time.Second):
		t.Fatal("server ping yubormadi")
	}
	time.Sleep(4 * time.Second) // IdleTO dan ko'proq — pong tufayli uzilmasligi kerak
	if !e.hub.Online(agentID) {
		t.Fatal("tirik agent offline bo'lib qoldi")
	}
}

func TestIdleNoResponseCloses(t *testing.T) {
	e := newTestEnv(t)
	agentID, secret := e.enroll(t)
	ws, _ := dialHandshake(t, e, agentID, secret, 1)
	ws.SetPingHandler(func(string) error { return nil }) // pong YO'Q (o'lik agent)
	ws.SetPongHandler(func(string) error { return nil })
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	closed := false
	for range 20 {
		var env proto.Envelope
		err := ws.ReadJSON(&env)
		if err != nil {
			var ce *websocket.CloseError
			if errorsAs(err, &ce) && ce.Code == CloseIdle {
				closed = true
			}
			break
		}
	}
	if !closed {
		t.Fatal("javobsiz agent 4004 bilan yopilmadi")
	}
	// Offline belgisi DB'ga tushishi kerak.
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, err := e.reg.GetClientByAgentID(agentID)
		if err == nil && c.Status == "offline" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("offline belgilanmadi: %+v", c)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Agent UI'da nom o'zgarsa (masalan "Oxangaron") serverda ham darhol
// ko'rinishi kerak: hello.name mijoz nomini yangilaydi.
func TestHelloNameRenamesClient(t *testing.T) {
	e := newTestEnv(t)
	agentID, secret := e.enroll(t)
	before, err := e.reg.GetClientByAgentID(agentID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(before.Name, "agent-") {
		t.Fatalf("enroll avtomatik nom bermadi: %q", before.Name)
	}
	ws, _ := dialHandshake(t, e, agentID, secret, 1)
	_ = ws
	// Ikkinchi ulanish (qayta ulanish) yangi nom bilan keladi.
	agentID2, secret2 := agentID, secret
	ws2, _ := dialHandshakeNamed(t, e, agentID2, secret2, 1, "Oxangaron  ")
	_ = ws2
	after, err := e.reg.GetClientByAgentID(agentID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Name != "Oxangaron" {
		t.Fatalf("nom yangilanmadi: %q (avvalgi %q)", after.Name, before.Name)
	}
}

func dialHandshakeNamed(t *testing.T, e *testEnv, agentID, secret string, ver int, name string) (*websocket.Conn, proto.Welcome) {
	t.Helper()
	return dialHandshakeWith(t, e, agentID, secret, proto.Hello{
		ProtocolVersion: ver, AgentVersion: "t", OS: "linux", Hostname: "h", Name: name,
		Caps: proto.Caps{GPU: "none"},
	})
}

// Admin mijozni o'chirganda sessiya majburiy yopilishi kerak: aks holda
// o'chirilgan mijoz UI'da "online" bo'lib turadi (2026-11-20 kuzatildi).
func TestKickClosesSession(t *testing.T) {
	e := newTestEnv(t)
	agentID, secret := e.enroll(t)
	_, _ = dialHandshake(t, e, agentID, secret, 1)
	if !e.hub.Online(agentID) {
		t.Fatal("kick oldidan online emas")
	}
	if !e.hub.Kick(agentID, 4004, "mijoz_ochirildi") {
		t.Fatal("Kick false qaytardi")
	}
	deadline := time.Now().Add(3 * time.Second)
	for e.hub.Online(agentID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if e.hub.Online(agentID) {
		t.Fatal("kick dan keyin ham online")
	}
	if e.hub.Kick("ag_yo'qagent", 4004, "x") {
		t.Fatal("yo'q agentga Kick true qaytardi")
	}
}

// 1.0-draft.12: agent tez qayta ulangsa, ESKI sessiyaning yopilishi
// mijozni offline qilib qo'ymasligi kerak (aks holda yangi sessiyaning
// `online` holati bosilib qolardi — operator kuzatgan "doim offline").
func TestOldSessionCloseKeepsClientOnline(t *testing.T) {
	e := newTestEnv(t)
	agentID, secret := e.enroll(t)
	oldWS, _ := dialHandshake(t, e, agentID, secret, 1)
	newWS, _ := dialHandshake(t, e, agentID, secret, 1) // hub eskinisini siqadi
	t.Cleanup(func() { _ = newWS.Close() })

	if err := oldWS.Close(); err != nil {
		t.Fatalf("eski ws yopilmadi: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := e.hub.registry.GetClientByAgentID(agentID)
		if err == nil && c.Status == "online" && e.hub.Online(agentID) {
			return // online saqlandi — to'g'ri
		}
		time.Sleep(20 * time.Millisecond)
	}
	c, err := e.hub.registry.GetClientByAgentID(agentID)
	t.Fatalf("mijoz online bo'lib qolmadi: status=%q err=%v hubOnline=%v",
		c.Status, err, e.hub.Online(agentID))
}

// §2/§5.8: server har IdleTO da agentga `ping` xabarini (envelope)
// yuborishi shart — WS control ping agentning o'qish muddatini
// uzamaydi (2026-11-21: agent har 60 s'da uzilar edi).
func TestServerSendsPingEnvelope(t *testing.T) {
	e := newTestEnv(t)
	agentID, secret := e.enroll(t)
	ws, _ := dialHandshake(t, e, agentID, secret, 1)
	t.Cleanup(func() { _ = ws.Close() })

	// Gorilla: o'qish xatosidan keyin ulanish yopiladi — shuning uchun
	// bitta keng muddat bilan o'qiymiz (IdleTO=1s, sekin mashinada ham
	// 10 s ichida ping kelishi kerak).
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	for i := 0; i < 20; i++ {
		var env proto.Envelope
		if err := ws.ReadJSON(&env); err != nil {
			t.Fatalf("ping kutilmadi, o'qish xatosi: %v", err)
		}
		if env.Type == proto.MsgPing {
			// Agent javobi (pong) server tomonidan qabul qilinadi:
			if err := ws.WriteJSON(proto.Envelope{Type: proto.MsgPong, TS: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("server `ping` yubormadi")
}

// 1.0-draft.12: `position` sync'da keladi va server bazasiga yoziladi
// (avval bu maydon bu yerga yetkazilmasdi — UI tartibi doim 0 bo'lardi).
func TestCamsSyncStoresPosition(t *testing.T) {
	e := newTestEnv(t)
	agentID, secret := e.enroll(t)
	ws, _ := dialHandshake(t, e, agentID, secret, 1)
	startHB(t, ws)

	raw, _ := json.Marshal(proto.CamsSync{Epoch: 5, Full: true, SeqNo: 1, ChunkCount: 1, Done: true,
		Cameras: []proto.CameraEntry{{Op: "upsert", ID: "pos-1", Name: "Tartib",
			Codecs: proto.CameraProfilePair{Main: "h264"}, Position: 7}}})
	_ = ws.WriteJSON(proto.Envelope{Type: proto.MsgCamsSync, Seq: 2, TS: time.Now().UTC(), Data: raw})
	for {
		env := readEnv(t, ws)
		if env.Type == proto.MsgCamsAck {
			break
		}
	}
	cl, err := e.reg.GetClientByAgentID(agentID)
	if err != nil {
		t.Fatal(err)
	}
	kam, err := e.reg.ListCamerasByClient(cl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(kam) != 1 || kam[0].Position != 7 {
		t.Fatalf("position saqlanmadi: %+v", kam)
	}
}

// §4.2: sync'dagi status bazaga yoziladi; keyingi statussiz sync uni
// bosmaydi (server qayta boshlanganda kameralar offline qolmasligi uchun).
func TestCamsSyncCarriesStatus(t *testing.T) {
	e := newTestEnv(t)
	agentID, secret := e.enroll(t)
	ws, _ := dialHandshake(t, e, agentID, secret, 1)
	startHB(t, ws)
	sendSync := func(epoch int64, st string) {
		raw, _ := json.Marshal(proto.CamsSync{Epoch: epoch, Full: true, SeqNo: 1, ChunkCount: 1, Done: true,
			Cameras: []proto.CameraEntry{{Op: "upsert", ID: "st-1", Name: "Holat",
				Codecs: proto.CameraProfilePair{Main: "h265"}, Status: st}}})
		_ = ws.WriteJSON(proto.Envelope{Type: proto.MsgCamsSync, Seq: 2, TS: time.Now().UTC(), Data: raw})
		for {
			if env := readEnv(t, ws); env.Type == proto.MsgCamsAck {
				break
			}
		}
	}
	sendSync(1, "online")
	sendSync(2, "")
	cl, err := e.reg.GetClientByAgentID(agentID)
	if err != nil {
		t.Fatal(err)
	}
	kam, err := e.reg.ListCamerasByClient(cl.ID)
	if err != nil || len(kam) != 1 {
		t.Fatalf("kameralar: %v %v", kam, err)
	}
	if kam[0].Status != "online" {
		t.Fatalf("status yo'qoldi: %+v", kam[0])
	}
}

// cross-tenant: bitta agent boshqa mijozning kamerasini o'chira olmasligi
// kerak (cams_sync `client_id` bo'yicha cheklanmagan edi; 2026-11-21 auditi).
func TestCamsSync_CrossTenantDeleteIgnored(t *testing.T) {
	e := newTestEnv(t)
	agentA, secretA := e.enroll(t)
	wsA, _ := dialHandshake(t, e, agentA, secretA, 1)
	startHB(t, wsA)
	sendA := func(typ string, data any) {
		raw, _ := json.Marshal(data)
		_ = wsA.WriteJSON(proto.Envelope{Type: typ, Seq: 2, TS: time.Now().UTC(), Data: raw})
	}
	sendA(proto.MsgCamsSync, proto.CamsSync{Epoch: 100, Full: true, SeqNo: 1, ChunkCount: 1, Done: true,
		Cameras: []proto.CameraEntry{{Op: "upsert", ID: "k-shared", Name: "A-nomi", Status: "online"}}})
	waitAck(t, wsA)

	agentB, secretB := e.enroll(t)
	wsB, _ := dialHandshake(t, e, agentB, secretB, 1)
	startHB(t, wsB)
	raw, _ := json.Marshal(proto.CamsSync{Epoch: 100, Full: true, SeqNo: 1, ChunkCount: 1, Done: true,
		Cameras: []proto.CameraEntry{{Op: "delete", ID: "k-shared"}}})
	_ = wsB.WriteJSON(proto.Envelope{Type: proto.MsgCamsSync, Seq: 2, TS: time.Now().UTC(), Data: raw})
	waitAck(t, wsB)

	c, err := e.reg.GetCamera("k-shared")
	if err != nil {
		t.Fatalf("begona kamera o'chirib yuborildi: %v", err)
	}
	if c.Name != "A-nomi" {
		t.Fatalf("kamera buzildi: %+v", c)
	}
}

// §4.2: `full:true` to'liq ro'yxat — agentda o'chirilgan kamera serverda
// ham o'chirilishi kerak (avval `full` umuman o'qilmas edi, serverda
// "o'lik kamera" abadiy qolardi).
func TestCamsSync_FullPrunesMissing(t *testing.T) {
	e := newTestEnv(t)
	agentID, secret := e.enroll(t)
	ws, _ := dialHandshake(t, e, agentID, secret, 1)
	startHB(t, ws)
	send := func(cs proto.CamsSync) {
		raw, _ := json.Marshal(cs)
		_ = ws.WriteJSON(proto.Envelope{Type: proto.MsgCamsSync, Seq: 2, TS: time.Now().UTC(), Data: raw})
		waitAck(t, ws)
	}
	up := func(id string) proto.CameraEntry {
		return proto.CameraEntry{Op: "upsert", ID: id, Name: id, Status: "online"}
	}
	send(proto.CamsSync{Epoch: 100, Full: true, SeqNo: 1, ChunkCount: 1, Done: true,
		Cameras: []proto.CameraEntry{up("k1"), up("k2")}})
	c, err := e.reg.GetClientByAgentID(agentID)
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := e.reg.ListCamerasByClient(c.ID); len(list) != 2 {
		t.Fatalf("boshida 2 kamera: %d", len(list))
	}
	// Keyingi to'liq sync'da k2 yo'q — u agentda o'chirilgan.
	send(proto.CamsSync{Epoch: 101, Full: true, SeqNo: 1, ChunkCount: 1, Done: true,
		Cameras: []proto.CameraEntry{up("k1")}})
	list, _ := e.reg.ListCamerasByClient(c.ID)
	if len(list) != 1 || list[0].ID != "k1" {
		t.Fatalf("pruning ishlamadi: %+v", list)
	}
}

func waitAck(t *testing.T, ws *websocket.Conn) {
	t.Helper()
	for i := 0; i < 20; i++ {
		env := readEnv(t, ws)
		if env.Type == proto.MsgCamsAck {
			var ack proto.CamsAck
			_ = json.Unmarshal(env.Data, &ack)
			if !ack.OK {
				t.Fatalf("cams_ack ok=false: %+v", ack)
			}
			return
		}
	}
	t.Fatal("cams_ack kelmadi")
}

// Agent tasdiqlamasa: sir O'ZGARMASLIGI qolishi va admin xato olishi
// kerak (aks holda agent 4003 bilan qulflanib qolardi).
func TestSecretRotateUnconfirmedKeepsOld(t *testing.T) {
	e := newTestEnvWith(t, Config{RotateWait: 300 * time.Millisecond})
	agentID, secret := e.enroll(t)
	ws, _ := dialHandshake(t, e, agentID, secret, 1)
	startHB(t, ws)
	if _, err := e.hub.RotateSecret(agentID, 30); err == nil {
		t.Fatal("tasdiqlanmagan rotate muvaffaqiyatli qaytdi")
	}
	// Eski secret bilan qayta ulanish ISHLASHI kerak.
	ws2, _ := dialHandshake(t, e, agentID, secret, 1)
	startHB(t, ws2)
	ws2.Close()
}
