// S1.9+: agent WS hub. Har ulanish: bitta o'quvchi + BITTA YOZUVCHI
// goroutine (single-writer, TASKS §0.3) — xabarlar channel orqali.
package wsagent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
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
	RotateWait    time.Duration // secret_rotate tasdig'i kutiladigan vaqt (0 → 10 s)
}

// UIBroadcaster — UI'ga holat yuborish (wsui.Hub; import siklini oldini
// olish uchun interfeys orqali).
type UIBroadcaster interface {
	Broadcast(typ string, data any)
}

// UI event nomlari (wsui bilan kelishilgan).
const (
	EvClientStatus = "client_status"
	EvCamStatus    = "cam_status"
	// EvCamsChanged — agent kamera ro'yxatini yubordi (qo'shish/o'chirish):
	// brauzerlar ro'yxatni qayta o'qiydi (1.0-draft.12).
	EvCamsChanged = "cams_changed"
)

// MediaSink — media_ready/stopped/stats xabarlarini qabul qiluvchi
// (S2 session manager; hub uni o'z navbatiga yo'naltiradi).
type MediaSink interface {
	OnMediaReady(agentID string, m proto.MediaReady)
	OnMediaStopped(agentID string, m proto.MediaStopped)
	OnMediaStats(agentID string, m proto.MediaStats)
}

// Hub — faol agent ulanishlari (bitta egasi: HTTP server goroutine'lari
// Only Register/Unregister orqali kiradi; conn holati conn'ning o'zida).
type Hub struct {
	cfg      Config
	registry *registry.Registry
	lg       *slog.Logger

	mu      sync.Mutex
	conns   map[string]*Conn
	epochs  map[string]int64 // cams_epoch (xotira; DB'ga S2'da yoziladi)
	lastSeq map[string]int64
	// pendingRotate — ikki bosqichli secret_rotate (§5.6).
	pendingRotate map[string]*pendingRotate
	// syncApplied — chunk'lar bo'yicha applied yig'indisi.
	syncApplied map[string]int
	// syncSeen — to'liq cams_sync'da KO'RINGAN kamera ID'lari
	// (chunk'lar bo'yicha to'planadi; §4.2 pruning shu to'plamga
	// qarab qilinadi).
	syncSeen map[string]map[string]bool
	clock    clock     // testlarda fake (idle/ping kuzatuvi)
	sink     MediaSink // S2: media xabarlari session manager'ga
	ui       UIBroadcaster

	// connOK/connFail — /metrics uchun (2026-11-22): muvaffaqiyatli
	// handshake'lar va rad etilganlar (flap yoki kalit qidirish
	// belgisi; alert.sh shu oraliqdan foydalanadi).
	connOK   atomic.Int64
	connFail atomic.Int64
}

// rotateWait — sozlanadigan tasdiq kutish vaqti.
func (h *Hub) rotateWait() time.Duration {
	if h.cfg.RotateWait > 0 {
		return h.cfg.RotateWait
	}
	return rotateWait
}

// SetUI — UI push kanalin o'rnatish (S3.0).
func (h *Hub) SetUI(b UIBroadcaster) { h.mu.Lock(); h.ui = b; h.mu.Unlock() }

// uiEvent — UI'ga hodisa (ui nil bo'lsa jim).
func (h *Hub) uiEvent(typ string, data any) {
	h.mu.Lock()
	u := h.ui
	h.mu.Unlock()
	if u != nil {
		u.Broadcast(typ, data)
	}
}

// SetSink — media xabarlari qabul qiluvchini o'rnatish (S2.4).
func (h *Hub) SetSink(s MediaSink) { h.mu.Lock(); h.sink = s; h.mu.Unlock() }

// deliverMedia — sink'ga uzatish (sink nil bo'lsa jim o'tadi).
func (h *Hub) deliverMedia(f func(MediaSink)) {
	h.mu.Lock()
	sk := h.sink
	h.mu.Unlock()
	if sk != nil {
		f(sk)
	}
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
		syncSeen:    map[string]map[string]bool{},
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

// Kick — agent sessiyasini majburiy yopadi. Admin mijozni o'chirganda
// chaqiriladi: aks holda WS sessiyasi tirik qoladi va o'chirilgan mijoz
// UI'da "online" bo'lib turadi (2026-11-20 kuzatildi). Agent qayta
// ulanishda 4003/4004 oladi va lokal UI'da tiklanish yo'li ko'rsatiladi.
func (h *Hub) Kick(agentID string, code int, reason string) bool {
	h.mu.Lock()
	c := h.conns[agentID]
	h.mu.Unlock()
	if c == nil {
		return false
	}
	c.closeWith(code, reason)
	return true
}

// Send — agentga server xabari (yozuvchi goroutine'ga channel orqali).
// maxAgentFrame — §2: bitta WS xabari ≤1 MiB (chunk'lar ≤900 KiB).
const maxAgentFrame = 1 << 20

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

// pendingRotate — ikki bosqichli sir almashtirish holati.
type pendingRotate struct {
	secret   string
	clientID int64
	done     chan error
}

// rotateWait — agent tasdig'i kutiladigan vaqt.
const rotateWait = 10 * time.Second

// RotateSecret — admin amali (protocol.md §5.6).
//
// ESKISI: DB hash darhol almashtirilardi, yuborish xatosi (`_ = h.Send`)
// yutilardi va agent javobi (`secret_rotated.ok`) umuman o'qilmas edi.
// Agent yangi sirni olmasa keyingi ulanishda 4003 oladi va o'zi
// tiklana olmaydi — operator "Clear connection → Request access" yo'liga
// majbur bo'lardi (2026-11-21 server auditi).
//
// Endi: agentga yuboriladi, DB hash FAQAT agent tasdiqlaganda
// almashtiriladi. Tasdiq yo'qsa eski sir ishlayveradi va admin xato
// oladi.
func (h *Hub) RotateSecret(agentID string, graceS int) (string, error) {
	secret := randToken(32)
	c, err := h.registry.GetClientByAgentID(agentID)
	if err != nil {
		return "", err
	}
	if !h.Online(agentID) {
		return "", ErrNotOnline
	}
	pr := &pendingRotate{secret: secret, clientID: c.ID, done: make(chan error, 1)}
	h.mu.Lock()
	if h.pendingRotate == nil {
		h.pendingRotate = map[string]*pendingRotate{}
	}
	h.pendingRotate[agentID] = pr
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pendingRotate, agentID)
		h.mu.Unlock()
	}()
	if err := h.Send(agentID, proto.MsgSecretRotate, proto.SecretRotate{NewSecret: secret, GraceS: graceS}); err != nil {
		return "", err
	}
	select {
	case err := <-pr.done:
		if err != nil {
			return "", err
		}
		return secret, nil
	case <-time.After(h.rotateWait()):
		return "", fmt.Errorf("agent %s sir yangilanishini %s ichida tasdiqlamadi — eski sir saqlanib qoldi",
			agentID, h.rotateWait())
	}
}

// finishRotate — agent javobi (`secret_rotated`): tasdiq bo'lsa DB hash
// almashtiriladi.
func (h *Hub) finishRotate(agentID string, sr proto.SecretRotated) {
	h.mu.Lock()
	pr := h.pendingRotate[agentID]
	h.mu.Unlock()
	if pr == nil {
		return
	}
	if !sr.OK {
		pr.done <- fmt.Errorf("agent sirni qabul qilmadi")
		return
	}
	if err := h.registry.UpdateClientSecret(pr.clientID, HashSecret(pr.secret)); err != nil {
		pr.done <- err
		return
	}
	h.lg.Info("sir almashtirildi (agent tasdiqladi)", "agent_id", agentID)
	pr.done <- nil
}

// Count — faol agent ulanishlari (metrics).
func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns)
}

// ConnStats — jami muvaffaqiyatli handshake'lar va rad etilganlar
// (/metrics; 2026-11-22). Fail/ok nisbati o'sib borsa — agent flap
// qilmoqda yoki kalit qidirilmoqda degani (alert.sh shuni kuzatadi).
func (h *Hub) ConnStats() (ok, fail int64) {
	return h.connOK.Load(), h.connFail.Load()
}

// PushConfig — admin amali: `config_push` (protocol.md §5.7).
func (h *Hub) PushConfig(agentID string, lp proto.Limits, st proto.Settings) error {
	return h.Send(agentID, proto.MsgConfigPush, proto.ConfigPush{Limits: lp, Settings: st})
}
