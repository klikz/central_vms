// S1.9+: agent WS hub. Har ulanish: bitta o'quvchi + BITTA YOZUVCHI
// goroutine (single-writer, TASKS §0.3) — xabarlar channel orqali.
package wsagent

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/server/internal/proto"
	"central-vms/server/internal/registry"
)

// Yopilish kodlari (protocol.md §4.1).
const (
	CloseIncompat  = 4001 // protocol_version mos emas
	CloseTimeout   = 4002 // auth/hello 5 s ichida kelmadi
	CloseBadSecret = 4003 // secret noto'g'ri
	CloseIdle      = 4004 // 30 s javobsiz (ping-pong uzildi)
	CloseReplaced  = 4005 // xuddi shu agent qayta ulandi (eski yopiladi)
)

// Config — hub sozlamalari (config paketi bilan keladi).
type Config struct {
	ServerVersion string
	MinProto      int
	MaxProto      int
	HandshakeTO   time.Duration // 5 s
	IdleTO        time.Duration // 30 s — ping oraliqi (protocol.md §2)
	DeadTO        time.Duration // 90 s — javobsizlikda yopish (4004); IdleTO'dan katta
}

// Hub — faol agent ulanishlari (bitta egasi: HTTP server goroutine'lari
// Only Register/Unregister orqali kiradi; conn holati conn'ning o'zida).
type Hub struct {
	cfg      Config
	registry *registry.Registry
	lg       *slog.Logger

	mu          sync.Mutex
	conns       map[string]*Conn
	epochs      map[string]int64 // cams_epoch (xotira; DB'ga S2'da yoziladi)
	lastSeq     map[string]int64
	syncApplied map[string]int // chunk'lar bo'yicha applied yig'indisi
	clock       clock          // testlarda fake (idle/ping kuzatuvi)
}

func NewHub(cfg Config, reg *registry.Registry, lg *slog.Logger) *Hub {
	if cfg.HandshakeTO == 0 {
		cfg.HandshakeTO = 5 * time.Second
	}
	if cfg.IdleTO == 0 {
		cfg.IdleTO = 30 * time.Second
	}
	if cfg.DeadTO == 0 {
		cfg.DeadTO = 3 * cfg.IdleTO
	}
	return &Hub{
		cfg: cfg, registry: reg, lg: lg, clock: realNow{},
		conns: map[string]*Conn{}, epochs: map[string]int64{}, lastSeq: map[string]int64{},
		syncApplied: map[string]int{},
	}
}

// Conn — bitta agent ulanishi.
type Conn struct {
	hub       *Hub
	clientID  int64
	agentID   string
	ws        *websocket.Conn
	out       chan json.RawMessage
	done      chan struct{}
	closeOnce sync.Once
}

// Online — agent WS orqali ulanganmi (S2 session manager shu orqali tekshiradi).
func (h *Hub) Online(agentID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[agentID] != nil
}

// Send — agentga server xabari (yozuvchi goroutine'ga channel orqali).
func (h *Hub) Send(agentID, typ string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	h.mu.Lock()
	c := h.conns[agentID]
	h.mu.Unlock()
	if c == nil {
		return ErrNotOnline
	}
	env, err := json.Marshal(proto.Envelope{Type: typ, TS: time.Now().UTC(), Data: raw})
	if err != nil {
		return err
	}
	select {
	case c.out <- env:
		return nil
	default:
		return ErrSlowAgent
	}
}

var (
	ErrNotOnline = &hubError{"agent online emas"}
	ErrSlowAgent = &hubError{"agent kanali to'lgan (sekin)"}
)

type hubError struct{ s string }

func (e *hubError) Error() string { return e.s }

// RotateSecret — admin amali: yangi secret DB hash + agentga `secret_rotate`
// (protocol.md §5.6; agent keyingi ulanishdan yangi secret bilan keladi).
func (h *Hub) RotateSecret(agentID string, graceS int) (string, error) {
	secret := randToken(32)
	c, err := h.registry.GetClientByAgentID(agentID)
	if err != nil {
		return "", err
	}
	if err := h.registry.UpdateClientSecret(c.ID, HashSecret(secret)); err != nil {
		return "", err
	}
	_ = h.Send(agentID, proto.MsgSecretRotate, proto.SecretRotate{NewSecret: secret, GraceS: graceS})
	return secret, nil
}

// PushConfig — admin amali: `config_push` (protocol.md §5.7).
func (h *Hub) PushConfig(agentID string, lp proto.Limits, st proto.Settings) error {
	return h.Send(agentID, proto.MsgConfigPush, proto.ConfigPush{Limits: lp, Settings: st})
}
