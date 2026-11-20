// Package wsui — server UI (brauzer) uchun WS push kanali: holat
// o'zgarishlarini so'rov-siz yuboradi (client_status, cam_status,
// session_state, session_count, session_revoked). Kontrakt: docs/api.md.
//
// Ulanish: GET /api/ui/ws?access=<JWT> — brauzer WS header qo'yolmaydi,
// shuning uchun access token query orqali olinadi (faqat qisqa muddatli).
package wsui

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/server/internal/auth"
)

// Event turlari (server → UI).
const (
	EvClientStatus   = "client_status"
	EvCamStatus      = "cam_status"
	EvSessionState   = "session_state"
	EvSessionCount   = "session_count"
	EvSessionRevoked = "session_revoked"
	EvHello          = "hello"
)

// Envelope — barcha UI xabarlari shu qobiqda yuboriladi.
type Envelope struct {
	Type string          `json:"type"`
	TS   time.Time       `json:"ts"`
	Data json.RawMessage `json:"data"`
}

// --- payloadlar (docs/api.md bilan kelishilgan) ---

type ClientStatus struct {
	ClientID     int64  `json:"client_id"`
	AgentID      string `json:"agent_id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Version      string `json:"version,omitempty"`
	CamerasCount int    `json:"cameras_count"`
	OnlineCount  int    `json:"online_count"`
}

type CamStatus struct {
	CameraID string `json:"camera_id"`
	ClientID int64  `json:"client_id"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
	RTTMs    int64  `json:"rtt_ms,omitempty"`
}

type SessionState struct {
	SessionID  string `json:"session_id"`
	CameraID   string `json:"camera_id"`
	UserID     int64  `json:"user_id"`
	State      string `json:"state"`
	Reason     string `json:"reason,omitempty"`
	Transcoded bool   `json:"transcoded"`
	Backend    string `json:"backend,omitempty"`
}

type SessionCount struct {
	Active  int `json:"active"`
	UserMax int `json:"user_max"`
	Global  int `json:"global_max"`
}

// Hub — barcha UI ulanishlari (bitta egasi).
type Hub struct {
	tokens *auth.Tokens
	lg     *slog.Logger
	upgr   websocket.Upgrader

	mu     sync.Mutex
	conns  map[*conn]struct{}
	pingTO time.Duration
	deadTO time.Duration
	// seq — har bir broadcast uchun tartib raqami (UI eski xabarni
	// e'tiborsiz qoldirishi uchun).
	seq uint64
}

type conn struct {
	hub  *Hub
	ws   *websocket.Conn
	out  chan []byte
	user auth.Claims
	done chan struct{}
	once sync.Once
}

// Config — idle/ping parametrlari (testlar qisqartiradi).
type Config struct {
	PingTO time.Duration // ping oraliqi
	DeadTO time.Duration // javobsizlikda yopish
}

func NewHub(tokens *auth.Tokens, lg *slog.Logger, cfg Config) *Hub {
	if cfg.PingTO <= 0 {
		cfg.PingTO = 30 * time.Second
	}
	if cfg.DeadTO <= 0 {
		cfg.DeadTO = 3 * cfg.PingTO
	}
	return &Hub{
		tokens: tokens, lg: lg, conns: map[*conn]struct{}{},
		pingTO: cfg.PingTO, deadTO: cfg.DeadTO,
		upgr: websocket.Upgrader{
			ReadBufferSize: 1024, WriteBufferSize: 4096,
			// UI bitta origin'dan (vmsd o'zi statikani ham beradi);
			// dev rejimda Vite 5173 — ruxsat beriladi (faqat token bilan).
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
	}
}

// Broadcast — barcha UI mijozlariga xabar (sekin mijoz tushib qoladi).
func (h *Hub) Broadcast(typ string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		h.lg.Warn("wsui marshal", "typ", typ, "err", err)
		return
	}
	h.mu.Lock()
	h.seq++
	env, err := json.Marshal(Envelope{Type: typ, TS: time.Now().UTC(), Data: raw})
	conns := make([]*conn, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	if err != nil {
		return
	}
	for _, c := range conns {
		if !c.trySend(env) {
			h.lg.Warn("wsui sekin mijoz — yopildi", "user", c.user.User)
			c.close()
		}
	}
}

// trySend — xabarni yozuv kanaliga qo'yish; bufer to'lsa false
// (chaqiruvchi tomonda ulanish yopiladi — UI o'zi qayta ulanadi).
func (c *conn) trySend(env []byte) bool {
	select {
	case c.out <- env:
		return true
	default:
		return false
	}
}

// Count — faol UI ulanishlari (metrics).
func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns)
}

// ServeHTTP — tokenni query'dan oladi (brauzer WS header qo'yolmaydi).
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("access")
	if tok == "" {
		tok = r.Header.Get("Authorization")
		tok = authBearer(tok)
	}
	cl, err := h.tokens.Verify(tok, "access")
	if err != nil {
		http.Error(w, `{"error":"bad_token"}`, http.StatusUnauthorized)
		return
	}
	ws, err := h.upgr.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &conn{hub: h, ws: ws, out: make(chan []byte, 32), user: *cl, done: make(chan struct{})}
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.mu.Unlock()
	h.lg.Info("UI ulandi", "user", cl.User, "role", cl.Role)

	// Birinchi xabar — salom (UI ulanishni tasdiqlashi uchun).
	h.welcome(c)
	go c.writeLoop()
	go c.pingLoop()
	c.readLoop()
	h.mu.Lock()
	delete(h.conns, c)
	h.mu.Unlock()
	c.shutdown()
	h.lg.Info("UI uzildi", "user", cl.User)
}

func (h *Hub) welcome(c *conn) {
	env, _ := json.Marshal(Envelope{Type: EvHello, TS: time.Now().UTC(),
		Data: json.RawMessage(`{"ok":true}`)})
	select {
	case c.out <- env:
	default:
	}
}

func authBearer(s string) string {
	if len(s) > 7 && s[:7] == "Bearer " {
		return s[7:]
	}
	return s
}
