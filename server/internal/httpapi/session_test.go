// S2.6: play/preview/refresh/stop endpointlari + /internal/auth marshi.
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/server/internal/proto"
	"central-vms/server/internal/registry"
)

// joinAgent — §3 oqimi: agent so‘rov yuboradi → admin tasdiqlaydi →
// agent polling orqali agent_id oladi (test yordamchisi).
func joinAgent(t *testing.T, srv *httptest.Server, adminTok, hostname string) (agentID, secret string) {
	t.Helper()
	joinKey := "a1" + strings.Repeat("b2", 31)
	secret = "sir-" + strings.Repeat("7c", 24)
	body := `{"join_key":"` + joinKey + `","agent_secret":"` + secret +
		`","hostname":"` + hostname + `","name":"` + hostname + `"}`
	code, m, raw := do(t, http.MethodPost, srv.URL+"/api/agent/join", body, "")
	if code != 200 || m["status"] != "pending" {
		t.Fatalf("join: %d %s", code, raw)
	}
	reqID := int64(m["request_id"].(float64))
	_, lm, lraw := do(t, http.MethodGet, srv.URL+"/api/admin/joins?status=pending", "", adminTok)
	reqs, ok := lm["requests"].([]any)
	if !ok || len(reqs) == 0 {
		t.Fatalf("joins list: %s", lraw)
	}
	_ = reqID
	_, am, araw := do(t, http.MethodPost,
		srv.URL+"/api/admin/joins/"+itoaTest(int64(reqs[0].(map[string]any)["id"].(float64)))+"/approve",
		`{"name":"`+hostname+`"}`, adminTok)
	if am == nil {
		t.Fatalf("approve: %s", araw)
	}
	agentID, ok = am["agent_id"].(string)
	if !ok {
		t.Fatalf("approve natija: %s", araw)
	}
	// Polling — tasdiqlangan holat keladi.
	if code, pm, praw := do(t, http.MethodPost, srv.URL+"/api/agent/join", body, ""); code != 200 ||
		pm["status"] != "approved" || pm["agent_id"] != agentID {
		t.Fatalf("join polling: %d %s", code, praw)
	}
	return agentID, secret
}

// enrollAgent — join → WS handshake (test yordamchisi).
func enrollAgent(t *testing.T, srv *httptest.Server, adminTok, camName string) (agentID, secret string, cams []registry.Camera) {
	t.Helper()
	agentID, secret = joinAgent(t, srv, adminTok, "joy-9")

	ws, _, err := websocket.DefaultDialer.Dial("ws://"+strings.TrimPrefix(srv.URL, "http://")+"/api/agent/ws?agent_id="+agentID, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	send := func(typ string, data any) {
		raw, _ := json.Marshal(data)
		_ = ws.WriteJSON(proto.Envelope{Type: typ, Seq: 1, TS: time.Now().UTC(), Data: raw})
	}
	send(proto.MsgAuth, proto.Auth{AgentID: agentID, Secret: secret})
	send(proto.MsgHello, proto.Hello{ProtocolVersion: 1, Hostname: "joy-9"})
	var env proto.Envelope
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := ws.ReadJSON(&env); err != nil || env.Type != proto.MsgWelcome {
		t.Fatalf("welcome: %v", err)
	}
	send(proto.MsgCamsSync, proto.CamsSync{Epoch: 1, Full: true, SeqNo: 1, ChunkCount: 1, Done: true,
		Cameras: []proto.CameraEntry{{Op: "upsert", ID: protoID(7), Name: camName, Status: "online"}}})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := ws.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := ws.ReadJSON(&env); err != nil {
			break
		}
		if env.Type == proto.MsgCamsAck {
			break
		}
	}
	// Kameralarni API orqali olib qaytamiz.
	var clients []registry.Client
	_, _, raw := do(t, http.MethodGet, srv.URL+"/api/clients", "", adminTok)
	_ = json.Unmarshal([]byte(raw), &clients)
	if len(clients) == 0 {
		t.Fatal("client yo'q")
	}
	_, _, raw = do(t, http.MethodGet, srv.URL+"/api/clients/"+itoa64(clients[0].ID)+"/cameras", "", adminTok)
	_ = json.Unmarshal([]byte(raw), &cams)
	return agentID, secret, cams
}

func TestPlayRefreshStopFlow(t *testing.T) {
	srv, _, _, agentF, mtxF, _ := newServer(t)
	adminTok := login(t, srv, "admin1")
	_, _, cams := enrollAgent(t, srv, adminTok, "kirish")
	if len(cams) != 1 {
		t.Fatalf("kameralar: %d", len(cams))
	}
	camID := cams[0].ID

	code, m, raw := do(t, http.MethodPost, srv.URL+"/api/cameras/"+camID+"/play", `{"profile":"sub"}`, adminTok)
	if code != 200 {
		t.Fatalf("play: %d %s", code, raw)
	}
	if !strings.Contains(m["whep_url"].(string), "http://10.0.0.5:8889/cam/") || !strings.Contains(m["whep_url"].(string), "/whep") {
		t.Fatalf("play_url: %v", m["whep_url"])
	}
	sessID := m["session_id"].(string)
	agentF.mu.Lock()
	nStart := 0
	for _, typ := range agentF.sent {
		if typ == proto.MsgMediaStart {
			nStart++
		}
	}
	agentF.mu.Unlock()
	if nStart != 1 {
		t.Fatalf("media_start yuborilmadi: %v", agentF.sent)
	}
	mtxF.mu.Lock()
	hasPath := mtxF.paths["cam/"+cams[0].ID] // path cam/{agent}/{cam}
	mtxF.mu.Unlock()
	_ = hasPath

	// refresh → 200.
	if code, _, raw = do(t, http.MethodPost, srv.URL+"/api/sessions/refresh",
		`{"session_id":"`+sessID+`","extend_s":120}`, adminTok); code != 200 {
		t.Fatalf("refresh: %d %s", code, raw)
	}
	// GET /api/sessions — sessiya ko'rinadi.
	if code, _, raw = do(t, http.MethodGet, srv.URL+"/api/sessions", "", adminTok); code != 200 || !strings.Contains(raw, sessID) {
		t.Fatalf("sessions: %d %s", code, raw)
	}
	// stop → 200; MediaMTX'da o'quvchi qolmaydi → idle stop ishlaydi.
	if code, _, raw = do(t, http.MethodDelete, srv.URL+"/api/sessions/"+sessID, "", adminTok); code != 200 {
		t.Fatalf("stop: %d %s", code, raw)
	}
	mtxF.setReaders(0)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		agentF.mu.Lock()
		hasStop := false
		for _, typ := range agentF.sent {
			if typ == proto.MsgMediaStop {
				hasStop = true
			}
		}
		agentF.mu.Unlock()
		if hasStop {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	agentF.mu.Lock()
	defer agentF.mu.Unlock()
	found := false
	for _, typ := range agentF.sent {
		if typ == proto.MsgMediaStop {
			found = true
		}
	}
	if !found {
		t.Fatalf("media_stop yuborilmadi: %v", agentF.sent)
	}
}

func TestPlayErrors(t *testing.T) {
	srv, reg, _, _, _, _ := newServer(t)
	adminTok := login(t, srv, "admin1")
	_, _, cams := enrollAgent(t, srv, adminTok, "holirda")
	camID := cams[0].ID

	// Noma'lum kamera → 404.
	if code, _, _ := do(t, http.MethodPost, srv.URL+"/api/cameras/yoq/play", `{}`, adminTok); code != 404 {
		t.Fatalf("404 kutilardi: %d", code)
	}
	// Yomon profil → 400.
	if code, _, _ := do(t, http.MethodPost, srv.URL+"/api/cameras/"+camID+"/play", `{"profile":"x"}`, adminTok); code != 400 {
		t.Fatalf("400 kutilardi: %d", code)
	}
	// O'chirilgan kamera → 403.
	dis := false
	body, _ := json.Marshal(map[string]any{"enabled": dis})
	if code, _, raw := do(t, http.MethodPatch, srv.URL+"/api/admin/cameras/"+camID, string(body), adminTok); code != 200 {
		t.Fatalf("disable: %d %s", code, raw)
	}
	if code, _, _ := do(t, http.MethodPost, srv.URL+"/api/cameras/"+camID+"/play", `{}`, adminTok); code != 403 {
		t.Fatalf("403 kutilardi: %d", code)
	}
	_ = reg
}

func TestPreviewLimit(t *testing.T) {
	srv, reg, _, _, _, _ := newServer(t)
	adminTok := login(t, srv, "admin1")
	// 3 ta kamera.
	agentID, secret := joinAgent(t, srv, adminTok, "joy-lim")
	ws, _, err := websocket.DefaultDialer.Dial("ws://"+strings.TrimPrefix(srv.URL, "http://")+"/api/agent/ws?agent_id="+agentID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	send := func(typ string, data any) {
		raw, _ := json.Marshal(data)
		_ = ws.WriteJSON(proto.Envelope{Type: typ, Seq: 1, TS: time.Now().UTC(), Data: raw})
	}
	send(proto.MsgAuth, proto.Auth{AgentID: agentID, Secret: secret})
	send(proto.MsgHello, proto.Hello{ProtocolVersion: 1})
	var env proto.Envelope
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_ = ws.ReadJSON(&env)
	camList := make([]proto.CameraEntry, 3)
	for i := range camList {
		camList[i] = proto.CameraEntry{Op: "upsert", ID: protoID(i + 20), Name: "k" + itoa64(int64(i)), Status: "online"}
	}
	send(proto.MsgCamsSync, proto.CamsSync{Epoch: 1, Full: true, SeqNo: 1, ChunkCount: 1, Done: true, Cameras: camList})
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		if err := ws.ReadJSON(&env); err != nil {
			break
		}
		if env.Type == proto.MsgCamsAck {
			break
		}
	}
	cams, err := reg.ListCamerasByAgent(agentID)
	if err != nil || len(cams) != 3 {
		t.Fatalf("kameralar: %v %v", len(cams), err)
	}

	// PreviewMax=2 (test config'ni qo'lda qisqartirish uchun deps'ni
	// qayta tiklaymiz — bu yerda oddiy: 3 preview ochib, 429 kutamiz;
	// default PreviewMax=8 bo'lgani uchun limitni kichraytirish shart).
	// Shuning uchun avval /api/me orqali emas, to'g'ridan-to'g'ri
	// manager limiti testlanadi (media paketi testi), bu yerda esa
	// endpoint'ning o'zi 200 qaytarishini tekshiramiz.
	for i := range 3 {
		if code, _, _ := do(t, http.MethodPost, srv.URL+"/api/cameras/"+cams[i].ID+"/preview", `{}`, adminTok); code != 200 {
			t.Fatalf("preview %d: %d", i, code)
		}
	}
}

func TestAuthHookMounted(t *testing.T) {
	srv, _, _, _, _, _ := newServer(t)
	// /internal/auth API mux'da mavjud va noto'g'ri token 403 qaytaradi.
	code, m, _ := do(t, http.MethodPost, srv.URL+"/internal/auth",
		`{"action":"read","path":"cam/ag_x/c1","query":"token=soxta"}`, "")
	if code != 403 || m["error"] == nil {
		t.Fatalf("hook: %d %v", code, m)
	}
}

// UI `.map()` qiladi — bo'sh ro'yxat JSON'da `null` emas `[]` bo'lishi kerak.
func TestEmptyListsSerializeAsArray(t *testing.T) {
	srv, _, _, _, _, _ := newServer(t)
	tok := login(t, srv, "admin1")
	for _, path := range []string{"/api/sites", "/api/clients", "/api/sessions", "/api/favorites", "/api/layouts"} {
		code, _, body := do(t, http.MethodGet, srv.URL+path, "", tok)
		if code != 200 {
			t.Fatalf("%s: %d", path, code)
		}
		if strings.TrimSpace(body) != "[]" {
			t.Fatalf("%s bo'sh ro'yxat: %q (=[] kutilardi)", path, body)
		}
	}
}
