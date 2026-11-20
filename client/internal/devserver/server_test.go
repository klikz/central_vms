package devserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/client/internal/proto"
)

func newTestServer(t *testing.T, o Options) (*Server, *httptest.Server) {
	t.Helper()
	if o.EnrollCode == "" {
		o.EnrollCode = "TEST-CODE"
	}
	s := New(o)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func wsURL(ts *httptest.Server, agentID string) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/agent/ws?agent_id=" + agentID
}

// enroll — bir martalik kod orqali agent olish
func enroll(t *testing.T, ts *httptest.Server, code string) (string, string, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"code": code, "hostname": "test", "os": "Ubuntu", "lan_ip": "10.0.0.5"})
	res, err := ts.Client().Post(ts.URL+"/api/agent/enroll", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		AgentID     string `json:"agent_id"`
		AgentSecret string `json:"agent_secret"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return out.AgentID, out.AgentSecret, res.StatusCode
}

func TestEnroll_Flow(t *testing.T) {
	_, ts := newTestServer(t, Options{})

	id1, secret1, code := enroll(t, ts, "TEST-CODE")
	if code != http.StatusOK || id1 == "" || secret1 == "" {
		t.Fatalf("birinchi enroll: code=%d id=%q", code, id1)
	}
	if !strings.HasPrefix(id1, "ag_") {
		t.Fatalf("agent_id formati: %q", id1)
	}
	// Takroriy: 409 + bir xil agent_id qaytadi
	id2, _, code := enroll(t, ts, "TEST-CODE")
	if code != http.StatusConflict || id2 != id1 {
		t.Fatalf("takroriy enroll: code=%d id=%q", code, id2)
	}
	// Yolg'on kod: 401
	if _, _, code = enroll(t, ts, "YOLGON"); code != http.StatusUnauthorized {
		t.Fatalf("yolg'on kod: code=%d", code)
	}
	// Rate limit: 10 urinishdan keyin 429
	last := 0
	for i := 0; i < 12; i++ {
		_, _, last = enroll(t, ts, "YOLGON")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("rate limit kutilmadi, oxirgi code=%d", last)
	}
}

func TestWS_AuthWelcome(t *testing.T) {
	s, ts := newTestServer(t, Options{})
	id, secret, _ := enroll(t, ts, "TEST-CODE")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(ts, id), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	writeMsg(t, conn, proto.MsgAuth, proto.Auth{AgentID: id, Secret: secret})
	writeMsg(t, conn, proto.MsgHello, proto.Hello{
		ProtocolVersion: 1, AgentVersion: "0.3.1", Hostname: "test", CamsEpoch: 7,
	})

	var env proto.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		t.Fatal(err)
	}
	if env.Type != proto.MsgWelcome {
		t.Fatalf("xabar turi: %q, kutilgan welcome", env.Type)
	}
	wel, err := proto.Decode[proto.Welcome](env.Data)
	if err != nil {
		t.Fatal(err)
	}
	if !wel.Compat.OK {
		t.Fatalf("compat: %+v", wel.Compat)
	}
	if wel.CamsEpoch != 7 {
		t.Fatalf("cams_epoch=%d, kutilgan 7", wel.CamsEpoch)
	}
	if wel.Limits.MaxUploads == nil || *wel.Limits.MaxUploads != 8 {
		t.Fatalf("limits: %+v", wel.Limits)
	}
	if !wel.NeedsFullSync {
		t.Fatal("mock needs_full_sync=true berishi kerak")
	}
	if got := s.OnlineAgents(); len(got) != 1 || got[0] != id {
		t.Fatalf("online agentlar: %v", got)
	}
}

func TestWS_BadSecret_Close4003(t *testing.T) {
	_, ts := newTestServer(t, Options{})
	id, _, _ := enroll(t, ts, "TEST-CODE")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(ts, id), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	writeMsg(t, conn, proto.MsgAuth, proto.Auth{AgentID: id, Secret: "NOTO'G'RI"})
	if code := readCloseCode(t, conn); code != CloseBadSecret {
		t.Fatalf("yopinish kodi=%d, kutilgan %d", code, CloseBadSecret)
	}
}

func TestWS_AuthTimeout_Close4002(t *testing.T) {
	_, ts := newTestServer(t, Options{AuthTimeout: 200 * time.Millisecond})
	id, _, _ := enroll(t, ts, "TEST-CODE")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(ts, id), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// auth yuborilmaydi → server 4002 bilan yopishi kerak
	if code := readCloseCode(t, conn); code != CloseAuthTimeout {
		t.Fatalf("yopinish kodi=%d, kutilgan %d", code, CloseAuthTimeout)
	}
}

func TestWS_WrongFirstMessage_Close4002(t *testing.T) {
	_, ts := newTestServer(t, Options{})
	id, _, _ := enroll(t, ts, "TEST-CODE")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(ts, id), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	writeMsg(t, conn, proto.MsgHB, map[string]any{})
	if code := readCloseCode(t, conn); code != CloseAuthTimeout {
		t.Fatalf("kodi=%d, kutilgan %d", code, CloseAuthTimeout)
	}
}

func TestWS_VersionMismatch_WelcomeThen4001(t *testing.T) {
	_, ts := newTestServer(t, Options{MinVersion: 1, MaxVersion: 1})
	id, secret, _ := enroll(t, ts, "TEST-CODE")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(ts, id), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	writeMsg(t, conn, proto.MsgAuth, proto.Auth{AgentID: id, Secret: secret})
	writeMsg(t, conn, proto.MsgHello, proto.Hello{ProtocolVersion: 99, CamsEpoch: 1})

	var env proto.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		t.Fatal(err)
	}
	if env.Type != proto.MsgWelcome {
		t.Fatalf("turi=%q", env.Type)
	}
	wel, _ := proto.Decode[proto.Welcome](env.Data)
	if wel.Compat.OK {
		t.Fatal("compat.ok=false bo'lishi kerak edi")
	}
	if wel.Compat.MinSupported == nil || *wel.Compat.MinSupported != 1 {
		t.Fatalf("supported maydonlari: %+v", wel.Compat)
	}
	if code := readCloseCode(t, conn); code != CloseVersionMismatch {
		t.Fatalf("kodi=%d, kutilgan %d", code, CloseVersionMismatch)
	}
}

func TestWS_UnknownAgent_Close4003(t *testing.T) {
	_, ts := newTestServer(t, Options{})
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(ts, "ag_yoq"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	writeMsg(t, conn, proto.MsgAuth, proto.Auth{AgentID: "ag_yoq", Secret: "x"})
	if code := readCloseCode(t, conn); code != CloseBadSecret {
		t.Fatalf("kodi=%d", code)
	}
}

// --- yordamchilar ---

func writeMsg(t *testing.T, conn *websocket.Conn, typ string, data any) {
	t.Helper()
	env, err := proto.NewEnvelope(typ, 1, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(env); err != nil {
		t.Fatal(err)
	}
}

func readCloseCode(t *testing.T, conn *websocket.Conn) int {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err := conn.ReadMessage()
		if err == nil {
			continue // boshqa xabarlarni o'tkazib yuboramiz
		}
		var ce *websocket.CloseError
		if errors.As(err, &ce) {
			return ce.Code
		}
		t.Fatalf("o'qish xatosi: %v", err)
	}
}

// C1.14: mock qayta boshlansa ham agentlar yo'qolmasligi kerak (dev persistensiyasi)
func TestState_Persistentsiya(t *testing.T) {
	dir := t.TempDir()
	stateFile := dir + "/devserver.state.json"

	o := Options{EnrollCode: "KOD", StateFile: stateFile}
	s1 := New(o)
	ts1 := httptest.NewServer(s1.Handler())
	id, secret, code := enroll(t, ts1, "KOD")
	ts1.Close()
	if code != http.StatusOK {
		t.Fatalf("enroll: %d", code)
	}
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("state fayli yozilmadi: %v", err)
	}

	// "Qayta boshlash": yangi Server, xuddi shu fayl
	s2 := New(o)
	ts2 := httptest.NewServer(s2.Handler())
	defer ts2.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(ts2, id), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	writeMsg(t, conn, proto.MsgAuth, proto.Auth{AgentID: id, Secret: secret})
	writeMsg(t, conn, proto.MsgHello, proto.Hello{ProtocolVersion: 1})

	var env proto.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		t.Fatalf("welcome o'qilmadi (agent tiklanmagan): %v", err)
	}
	if env.Type != proto.MsgWelcome {
		t.Fatalf("turi=%q", env.Type)
	}
	// Eski kod ham tiklanadi: takroriy enroll 409 qaytishi kerak
	if _, _, c := enroll(t, ts2, "KOD"); c != http.StatusConflict {
		t.Fatalf("takroriy enroll: %d, kutilgan 409", c)
	}
}
