// httpapi testlari: to'liq oqim — login → JWT → enroll → WS cams_sync →
// API orqali kameralarni ko'rish; rol izolatsiyasi; rate-limit.
package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/server/internal/audit"
	"central-vms/server/internal/auth"
	"central-vms/server/internal/config"
	"central-vms/server/internal/proto"
	"central-vms/server/internal/registry"
	"central-vms/server/internal/store"
	"central-vms/server/internal/wsagent"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func newServer(t *testing.T) (*httptest.Server, *registry.Registry, *wsagent.Hub) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
	if _, err := store.MigrateUp(db, dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	reg := registry.New(db)
	hash, err := auth.HashPassword("Parol-Salom-123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.CreateUser("admin1", hash, "admin", "Admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.CreateUser("view1", hash, "viewer", ""); err != nil {
		t.Fatal(err)
	}

	tokens, err := auth.NewTokens("test-secret-0123456789abcdef0123456789", 15*time.Minute, 7*24*time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := wsagent.NewHub(wsagent.Config{ServerVersion: "t", MinProto: 1, MaxProto: 1, HandshakeTO: time.Second, IdleTO: time.Second, DeadTO: 2 * time.Second}, reg, lg)
	d := &Deps{
		Cfg: config.Default(), Reg: reg, Tokens: tokens,
		Login:  auth.NewRate(5, 5*time.Minute, nil),
		Enroll: auth.NewRate(5, 5*time.Minute, nil),
		Audit:  audit.New(db),
		Enr:    wsagent.NewEnrollment(db, reg, 24*time.Hour, 5, 5*time.Minute, nil),
		Agent:  hub, Lg: lg,
	}
	d.Mux = Routes(d)
	srv := httptest.NewServer(d.Mux)
	t.Cleanup(srv.Close)
	return srv, reg, hub
}

func do(t *testing.T, method, url, body string, token string) (int, map[string]any, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return resp.StatusCode, m, string(b)
}

func login(t *testing.T, srv *httptest.Server, user string) string {
	t.Helper()
	code, m, raw := do(t, http.MethodPost, srv.URL+"/api/auth/login",
		`{"username":"`+user+`","password":"Parol-Salom-123"}`, "")
	if code != 200 {
		t.Fatalf("login %s: %d %s", user, code, raw)
	}
	return m["access"].(string)
}

func TestLoginMeRefreshAudit(t *testing.T) {
	srv, _, _ := newServer(t)

	if code, _, _ := do(t, http.MethodPost, srv.URL+"/api/auth/login", `{"username":"admin1","password":"x"}`, ""); code != 401 {
		t.Fatalf("noto'g'ri parol: %d", code)
	}
	adminTok := login(t, srv, "admin1")

	code, m, _ := do(t, http.MethodGet, srv.URL+"/api/me", "", adminTok)
	if code != 200 || m["role"] != "admin" {
		t.Fatalf("me: %d %v", code, m)
	}
	if c2, _, raw2 := do(t, http.MethodGet, srv.URL+"/api/me", "", "soxta"); c2 != 401 {
		t.Fatalf("soxta token qabul qilindi: code=%d codeOuter=%d raw=%s tok=%q", c2, code, raw2, adminTok[:12])
	}

	// Refresh cookie: login javobida Set-Cookie bo'lishi kerak.
	resp, err := http.Post(srv.URL+"/api/auth/login", "application/json",
		strings.NewReader(`{"username":"admin1","password":"Parol-Salom-123"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var rc *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == refreshCookie {
			rc = c
		}
	}
	if rc == nil || !rc.HttpOnly {
		t.Fatalf("refresh cookie yo'q yoki httpOnly emas: %v", resp.Cookies())
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/refresh", nil)
	req.AddCookie(rc)
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r2.Body)
	r2.Body.Close()
	if r2.StatusCode != 200 || !strings.Contains(string(b), "access") {
		t.Fatalf("refresh: %d %s", r2.StatusCode, b)
	}

	// Audit: login yozuvi ko'rinishi kerak.
	code, _, raw := do(t, http.MethodGet, srv.URL+"/api/admin/audit", "", adminTok)
	if code != 200 || !strings.Contains(raw, "login") {
		t.Fatalf("audit: %d %s", code, raw)
	}
}

func TestLoginRateLimit(t *testing.T) {
	srv, _, _ := newServer(t)
	for i := range 5 {
		if code, _, _ := do(t, http.MethodPost, srv.URL+"/api/auth/login", `{"username":"admin1","password":"yomoq"}`, ""); code != 401 {
			t.Fatalf("urinish %d: %d", i+1, code)
		}
	}
	code, m, _ := do(t, http.MethodPost, srv.URL+"/api/auth/login", `{"username":"admin1","password":"Parol-Salom-123"}`, "")
	if code != 429 || m["error"] != "rate_limited" {
		t.Fatalf("rate-limit: %d %v", code, m)
	}
}

func TestViewerIsolation(t *testing.T) {
	srv, _, _ := newServer(t)
	view := login(t, srv, "view1")
	if code, _, _ := do(t, http.MethodGet, srv.URL+"/api/admin/users", "", view); code != 403 {
		t.Fatalf("viewer admin endpoint'da: %d", code)
	}
	if code, _, _ := do(t, http.MethodPost, srv.URL+"/api/admin/enrollments", `{}`, view); code != 403 {
		t.Fatalf("viewer enrollment yaratdi: %d", code)
	}
}

// TestEnrollToCamerasFlow — S1 qabul mezonining yadrosi:
// admin kod yaratadi → agent enroll → WS cams_sync → API'da kameralar.
func TestEnrollToCamerasFlow(t *testing.T) {
	srv, reg, hub := newServer(t)
	adminTok := login(t, srv, "admin1")

	code, m, raw := do(t, http.MethodPost, srv.URL+"/api/admin/enrollments", `{"site_name":""}`, adminTok)
	if code != 201 {
		t.Fatalf("enroll create: %d %s", code, raw)
	}
	enrollCode := m["code"].(string)

	// Ochiq endpoint orqali enroll (bitta urinish).
	code, m, raw = do(t, http.MethodPost, srv.URL+"/api/agent/enroll",
		`{"code":"`+enrollCode+`","hostname":"joy-1","os":"ubuntu","lan_ip":"10.0.5.9","agent_version":"0.1.0"}`, "")
	if code != 200 {
		t.Fatalf("agent enroll: %d %s", code, raw)
	}
	agentID := m["agent_id"].(string)
	secret := m["agent_secret"].(string)

	// Takror kod → 409.
	if code, _, _ = do(t, http.MethodPost, srv.URL+"/api/agent/enroll", `{"code":"`+enrollCode+`"}`, ""); code != 409 {
		t.Fatalf("takror kod: %d", code)
	}

	// WS orqali cams_sync (100 kamera — S1 qabul mezoni).
	ws, _, err := websocket.DefaultDialer.Dial("ws://"+strings.TrimPrefix(srv.URL, "http://")+"/api/agent/ws?agent_id="+agentID, nil)
	if err != nil {
		// srv.Mux /api/agent/ws'ni bilmaydi — handler alohida ulanadi:
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()
	send := func(typ string, data any) {
		raw, _ := json.Marshal(data)
		_ = ws.WriteJSON(proto.Envelope{Type: typ, Seq: 1, TS: time.Now().UTC(), Data: raw})
	}
	send(proto.MsgAuth, proto.Auth{AgentID: agentID, Secret: secret})
	send(proto.MsgHello, proto.Hello{ProtocolVersion: 1, Hostname: "joy-1", Caps: proto.Caps{GPU: "nvenc"}})
	var env proto.Envelope
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := ws.ReadJSON(&env); err != nil || env.Type != proto.MsgWelcome {
		t.Fatalf("welcome: %v", err)
	}
	cams := make([]proto.CameraEntry, 100)
	for i := range cams {
		cams[i] = proto.CameraEntry{Op: "upsert", ID: protoID(i), Name: protoName(i),
			Codecs: proto.CameraProfilePair{Main: "h265", Sub: "h264"}, Status: "online", Position: i}
	}
	send(proto.MsgCamsSync, proto.CamsSync{Epoch: 1, Full: true, SeqNo: 1, ChunkCount: 1, Done: true, Cameras: cams})
	ackDeadline := time.Now().Add(5 * time.Second)
	var ack proto.CamsAck
	for time.Now().Before(ackDeadline) {
		if err := ws.ReadJSON(&env); err != nil {
			break
		}
		if env.Type == proto.MsgCamsAck {
			_ = json.Unmarshal(env.Data, &ack)
			break
		}
	}
	if !ack.OK || ack.Applied != 100 {
		t.Fatalf("cams_ack: %+v", ack)
	}

	// API orqali ko'rish.
	code, _, raw = do(t, http.MethodGet, srv.URL+"/api/clients", "", adminTok)
	if code != 200 || !strings.Contains(raw, agentID) {
		t.Fatalf("clients: %d %s", code, raw)
	}
	var clients []registry.Client
	_ = json.Unmarshal([]byte(raw), &clients)
	if len(clients) != 1 {
		t.Fatalf("client soni: %d", len(clients))
	}
	if clients[0].Status != "online" || clients[0].CapsJSON == "" || clients[0].CapsJSON == "{}" {
		t.Fatalf("client holati: %+v", clients[0])
	}
	code, _, raw = do(t, http.MethodGet, srv.URL+"/api/clients/"+itoa64(clients[0].ID)+"/cameras", "", adminTok)
	if code != 200 || !strings.Contains(raw, "cam-050") {
		t.Fatalf("cameras: %d %s", code, raw)
	}
	var list []registry.Camera
	_ = json.Unmarshal([]byte(raw), &list)
	if len(list) != 100 {
		t.Fatalf("kamera soni API'da: %d", len(list))
	}

	// Admin PATCH policy → keyin qayta sync'da SAQLANADI.
	tm := "off"
	body, _ := json.Marshal(map[string]any{"transcode_mode": tm})
	code, _, raw = do(t, http.MethodPatch, srv.URL+"/api/admin/cameras/"+list[0].ID, string(body), adminTok)
	if code != 200 {
		t.Fatalf("policy patch: %d %s", code, raw)
	}
	send(proto.MsgCamsSync, proto.CamsSync{Epoch: 2, Full: true, SeqNo: 1, ChunkCount: 1, Done: true,
		Cameras: []proto.CameraEntry{{Op: "upsert", ID: list[0].ID, Name: "yangi-nom", Status: "online"}}})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, err := reg.GetCamera(list[0].ID)
		if err == nil && c.Name == "yangi-nom" && c.TranscodeMode == "off" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("policy sync'da saqlanmadi: %+v", c)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Secret rotate (admin) — DB hash o'zgaradi.
	code, m, raw = do(t, http.MethodPost, srv.URL+"/api/admin/clients/"+itoa64(clients[0].ID)+"/secret-rotate", "", adminTok)
	if code != 200 {
		t.Fatalf("rotate: %d %s", code, raw)
	}
	_ = hub
}

func protoID(i int) string {
	// Testlar uchun statik UUIDsimon (har xil, o'zgarmas).
	const hexd = "0123456789abcdef"
	b := []byte("00000000-0000-4000-8000-000000000000")
	copy(b[24:], []byte(hexd[i%16:][:1]))
	copy(b[22:], []byte(hexd[(i/16)%16:][:1]))
	copy(b[20:], []byte(hexd[i%4:][:1]))
	return string(b)
}

func protoName(i int) string { return "cam-" + pad3(i) }

func pad3(i int) string {
	s := itoa64(int64(i))
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
