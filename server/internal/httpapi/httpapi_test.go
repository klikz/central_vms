// httpapi testlari: to'liq oqim — login → JWT → enroll → WS cams_sync →
// API orqali kameralarni ko'rish; rol izolatsiyasi; rate-limit.
package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/server/internal/audit"
	"central-vms/server/internal/auth"
	"central-vms/server/internal/config"
	"central-vms/server/internal/gpu"
	"central-vms/server/internal/media"
	"central-vms/server/internal/proto"
	"central-vms/server/internal/registry"
	"central-vms/server/internal/store"
	"central-vms/server/internal/wsagent"
	"central-vms/server/internal/wsui"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// fakeMTX/fakeAgent — media.Manager uchun soddalashtirilgan dublyorlar.
type fakeMTX struct {
	mu      sync.Mutex
	paths   map[string]bool
	deleted []string
	alive   bool
	readers int
}

// PathConfigured — false: patternlar yo'q deb hisoblaymiz, EnsurePath
// chaqiruvi sinovda ko'rinishi uchun.
func (f *fakeMTX) PathConfigured(string) (bool, error) { return false, nil }

func (f *fakeMTX) EnsurePath(n string, _ media.PathConf) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths[n] = true
	return nil
}
func (f *fakeMTX) DeletePath(n string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, n)
	delete(f.paths, n)
	return nil
}
func (f *fakeMTX) Readers(string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readers, nil
}
func (f *fakeMTX) setReaders(n int) { f.mu.Lock(); f.readers = n; f.mu.Unlock() }
func (f *fakeMTX) Alive() bool      { f.mu.Lock(); defer f.mu.Unlock(); return f.alive }
func (f *fakeMTX) URLs(h string, wp, rp int) (string, string) {
	return fmt.Sprintf("http://%s:%d", h, wp), fmt.Sprintf("rtsp://%s:%d", h, rp)
}

type fakeAgent struct {
	mu   sync.Mutex
	sent []string
}

func (f *fakeAgent) Send(_, typ string, _ any) error {
	f.mu.Lock()
	f.sent = append(f.sent, typ)
	f.mu.Unlock()
	return nil
}
func (f *fakeAgent) Online(string) bool { return true }

func newServer(t *testing.T) (*httptest.Server, *registry.Registry, *wsagent.Hub, *fakeAgent, *fakeMTX, *wsui.Hub) {
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
	if os.Getenv("VMS_TESTLOG") == "1" {
		lg = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	hub := wsagent.NewHub(wsagent.Config{ServerVersion: "t", MinProto: 1, MaxProto: 1, HandshakeTO: time.Second, IdleTO: time.Second, DeadTO: 2 * time.Second}, reg, lg)
	uiHub := wsui.NewHub(tokens, lg, wsui.Config{})
	hub.SetUI(uiHub) // agent hodisalari UI'ga chiqadi (S3.0)
	agentF := &fakeAgent{}
	mtxF := &fakeMTX{paths: map[string]bool{}, alive: true, readers: 1}
	mgr := media.NewManager(media.MgrConfig{IdleStop: 10 * time.Millisecond, Tick: 10 * time.Millisecond}, media.Deps{
		Reg: reg, Store: reg, Agent: agentF, MTX: mtxF,
		Tokens: media.NewTokens(db, nil), Lg: lg, PublicHost: "10.0.0.5", WHEPPort: 8889, RTSPPort: 8554,
		UI: uiHub, GPU: gpu.Caps{Backend: gpu.BackendNone}, // transcode yo'q mashina simulyatsiyasi
	})
	t.Cleanup(mgr.Close)
	d := &Deps{
		Cfg: config.Default(), Reg: reg, Tokens: tokens, Media: mgr, UI: uiHub, MTXHook: media.NewHook(media.NewTokens(db, nil), lg),
		Login: auth.NewRate(5, 5*time.Minute, nil),
		Audit: audit.New(db),
		Join:  wsagent.NewJoiner(db, reg, lg, 20, nil),
		Agent: hub, Lg: lg,
	}
	d.Mux = Routes(d)
	srv := httptest.NewServer(d.Mux)
	t.Cleanup(srv.Close)
	return srv, reg, hub, agentF, mtxF, uiHub
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
	srv, _, _, _, _, _ := newServer(t)

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
	srv, _, _, _, _, _ := newServer(t)
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
	srv, _, _, _, _, _ := newServer(t)
	view := login(t, srv, "view1")
	if code, _, _ := do(t, http.MethodGet, srv.URL+"/api/admin/users", "", view); code != 403 {
		t.Fatalf("viewer admin endpoint'da: %d", code)
	}
	if code, _, _ := do(t, http.MethodGet, srv.URL+"/api/admin/joins", "", view); code != 403 {
		t.Fatalf("viewer so‘rovlar ro‘yxatiga kirdi: %d", code)
	}
	if code, _, _ := do(t, http.MethodPost, srv.URL+"/api/admin/preauth", `{"ip":"10.0.0.0/8"}`, view); code != 403 {
		t.Fatalf("viewer preauth qo‘shdi: %d", code)
	}
}

// TestJoinToCamerasFlow — S1 qabul mezonining yadrosi (1.0-draft.10
// oqimi bilan): agent so‘rov yuboradi → admin tasdiqlaydi → agent
// polling orqali agent_id oladi → WS cams_sync → API'da kameralar.
func TestJoinToCamerasFlow(t *testing.T) {
	srv, reg, hub, _, _, _ := newServer(t)
	adminTok := login(t, srv, "admin1")

	joinKey := "ab" + strings.Repeat("cd", 31) // 64 belgi hex
	secret := "joy-sir-" + strings.Repeat("9f", 24)

	// 1) Ochiq endpoint: so‘rov pending, mijoz hali yaratilmagan.
	code, m, raw := do(t, http.MethodPost, srv.URL+"/api/agent/join",
		`{"join_key":"`+joinKey+`","agent_secret":"`+secret+`","hostname":"joy-1",`+
			`"os":"ubuntu","lan_ip":"10.0.5.9","agent_version":"0.1.0","name":"Joy-1"}`, "")
	if code != 200 {
		t.Fatalf("agent join: %d %s", code, raw)
	}
	if m["status"] != "pending" {
		t.Fatalf("so‘rov holati: %v", m)
	}
	reqID := int64(m["request_id"].(float64))
	if _, err := reg.GetClientByAgentID(""); err == nil {
		t.Fatal("tasdiqdan oldin tekshiruv kutilmagan")
	}

	// 2) Admin so‘rovni ko‘radi va tasdiqlaydi.
	code, m, raw = do(t, http.MethodGet, srv.URL+"/api/admin/joins?status=pending", "", adminTok)
	if code != 200 {
		t.Fatalf("joins list: %d %s", code, raw)
	}
	reqs := m["requests"].([]any)
	if len(reqs) != 1 {
		t.Fatalf("so‘rovlar soni: %v", m)
	}
	if rq := reqs[0].(map[string]any); rq["hostname"] != "joy-1" || rq["name"] != "Joy-1" ||
		rq["source_ip"] == "" || rq["join_key_hash"] != nil {
		t.Fatalf("so‘rov maydonlari: %v", rq)
	}
	code, m, raw = do(t, http.MethodPost,
		srv.URL+"/api/admin/joins/"+itoaTest(reqID)+"/approve", `{"name":"Joy-1"}`, adminTok)
	if code != 200 {
		t.Fatalf("approve: %d %s", code, raw)
	}
	agentID := m["agent_id"].(string)

	// 3) Agent polling: shu kalit bilan agent_id oladi (sir qaytmaydi).
	code, m, raw = do(t, http.MethodPost, srv.URL+"/api/agent/join",
		`{"join_key":"`+joinKey+`","agent_secret":"`+secret+`"}`, "")
	if code != 200 || m["status"] != "approved" || m["agent_id"] != agentID {
		t.Fatalf("polling: %d %s", code, raw)
	}
	if _, ok := m["agent_secret"]; ok {
		t.Fatal("javobda sir qaytdi")
	}

	// 4) Tasdiqlangan so‘rovni qayta tasdiqlab bo'lmaydi.
	if code, _, _ = do(t, http.MethodPost,
		srv.URL+"/api/admin/joins/"+itoaTest(reqID)+"/approve", `{}`, adminTok); code != 409 {
		t.Fatalf("takroriy tasdiq: %d", code)
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

func itoaTest(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
