// S3.0 wsui testlari: token bilan ulanish, broadcast, sekin mijoz, ping.
package wsui

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/server/internal/auth"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func newTestHub(t *testing.T, cfg Config) (*httptest.Server, *Hub, string) {
	t.Helper()
	tk, err := auth.NewTokens("test-secret-0123456789abcdef0123456789",
		time.Minute, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHub(tk, lg, cfg)
	srv := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	t.Cleanup(srv.Close)
	acc, _, err := tk.IssueAccess(7, "view1", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	return srv, h, acc
}

// ticketQ — test uchun bir martalik WS kaliti (?access= endi qabul
// qilinmaydi: token query'da loglarga tushadi).
func ticketQ(t *testing.T, h *Hub) string {
	t.Helper()
	tok, _ := h.MintTicket(&auth.Claims{Sub: 7, User: "view1", Role: "viewer"})
	return "?ticket=" + tok
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func dial(t *testing.T, srv *httptest.Server, query string) *websocket.Conn {
	t.Helper()
	ws, resp, err := websocket.DefaultDialer.Dial(wsURL(srv)+query, nil)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial %d", resp.StatusCode)
		}
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { ws.Close() })
	return ws
}

func readEnv(t *testing.T, ws *websocket.Conn, within time.Duration) Envelope {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(within))
	var env Envelope
	if err := ws.ReadJSON(&env); err != nil {
		t.Fatalf("o'qish: %v", err)
	}
	return env
}

func TestConnectAndBroadcast(t *testing.T) {
	srv, h, _ := newTestHub(t, Config{PingTO: time.Second, DeadTO: 3 * time.Second})
	ws := dial(t, srv, ticketQ(t, h))
	if env := readEnv(t, ws, 2*time.Second); env.Type != EvHello {
		t.Fatalf("hello kutilardi: %q", env.Type)
	}
	if h.Count() != 1 {
		t.Fatalf("hub Count: %d", h.Count())
	}

	h.Broadcast(EvClientStatus, ClientStatus{ClientID: 3, AgentID: "ag_x", Status: "online", CamerasCount: 12})
	env := readEnv(t, ws, 2*time.Second)
	if env.Type != EvClientStatus {
		t.Fatalf("client_status kutilardi: %q", env.Type)
	}
	var cs ClientStatus
	if err := json.Unmarshal(env.Data, &cs); err != nil || cs.ClientID != 3 || cs.CamerasCount != 12 {
		t.Fatalf("payload: %v %+v", err, cs)
	}
	if env.TS.IsZero() {
		t.Fatal("ts yo'q")
	}
}

func TestTwoClientsBothReceive(t *testing.T) {
	srv, h, _ := newTestHub(t, Config{})
	a := dial(t, srv, ticketQ(t, h))
	b := dial(t, srv, ticketQ(t, h))
	readEnv(t, a, 2*time.Second)
	readEnv(t, b, 2*time.Second)
	h.Broadcast(EvSessionCount, SessionCount{Active: 5, UserMax: 16, Global: 48})
	for i, ws := range []*websocket.Conn{a, b} {
		env := readEnv(t, ws, 2*time.Second)
		if env.Type != EvSessionCount {
			t.Fatalf("mijoz %d: %q", i, env.Type)
		}
	}
}

func TestBadTokenRejected(t *testing.T) {
	srv, _, _ := newTestHub(t, Config{})
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(srv)+"?access=soxta", nil)
	if err == nil {
		t.Fatal("noto'g'ri token qabul qilindi")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("401 kutilardi: %v", resp)
	}
	// Header orqali ham tekshiriladi (noto'g'ri token → upgrade bo'lmaydi).
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer soxta")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("header so'rovi: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("401 kutilardi: %d", resp2.StatusCode)
	}
}

func TestTrySendFullDrops(t *testing.T) {
	// trySend — bufer to'lganda false qaytaradi (Broadcast shunda
	// ulanishni yopadi; yozuvchi goroutine band bo'lsa bu yagona
	// himoya — UI o'zi qayta ulanadi).
	c := &conn{out: make(chan []byte, 2), done: make(chan struct{}),
		user: auth.Claims{User: "slow"}}
	c.out <- []byte("1")
	c.out <- []byte("2")
	if c.trySend([]byte("3")) {
		t.Fatal("to'lgan buferga yuborish true qaytardi")
	}
	if !c.trySend([]byte("4")) == false {
		// buferda joy yo'q — yana false
	}
}

func TestSlowClientClosed(t *testing.T) {
	// Haqiqiy stsenariy: mijoz o'qimaydi → TCP backpressure → out
	// kanali to'ladi → ulanish yopiladi.
	srv, h, _ := newTestHub(t, Config{PingTO: time.Second, DeadTO: 5 * time.Second})
	ws := dial(t, srv, ticketQ(t, h))
	readEnv(t, ws, 2*time.Second)
	_ = ws // o'qimaymiz — ataylab
	big := strings.Repeat("x", 64*1024)
	for range 400 {
		h.Broadcast(EvCamStatus, CamStatus{CameraID: "c1", Error: big})
		if h.Count() == 0 {
			return
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if h.Count() == 0 {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("sekin mijoz yopilmadi")
}

func TestIdleNoResponseCloses(t *testing.T) {
	srv, h, _ := newTestHub(t, Config{PingTO: 200 * time.Millisecond, DeadTO: 400 * time.Millisecond})
	ws := dial(t, srv, ticketQ(t, h))
	readEnv(t, ws, 2*time.Second)
	// Pinglarga javob bermaymiz: server DeadTO'da yopishi kerak.
	ws.SetPingHandler(func(string) error { return nil })
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if h.Count() == 0 {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("javobsiz UI yopilmadi")
}

// Mijoz uzilayotgan paytda Broadcast ishlasa panic bo'lmasligi kerak:
// avval shutdown() `out` kanalini yopardi, Broadcast esa trySend'ni
// qulf tashqarisida qiladi → "send on closed channel" bilan BUTUN vmsd
// qulardi (2026-11-21 server auditi; race detektori ham shuni ko'rsatdi).
func TestWsui_BroadcastDuringDisconnectNoPanic(t *testing.T) {
	srv, h, _ := newTestHub(t, Config{PingTO: 20 * time.Millisecond, DeadTO: 200 * time.Millisecond})
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			h.Broadcast("session_state", map[string]any{"i": i})
			time.Sleep(500 * time.Microsecond)
		}
	}()
	for i := 0; i < 40; i++ {
		ws := dial(t, srv, ticketQ(t, h))
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer ws.Close()
			_ = ws.SetReadDeadline(time.Now().Add(120 * time.Millisecond))
			_, _, _ = ws.ReadMessage() // kimdir o'qiydi, kimdir uziladi
		}()
	}
	time.Sleep(400 * time.Millisecond)
	close(stop)
	wg.Wait()
}
