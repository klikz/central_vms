package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/client/internal/clock"
	"central-vms/client/internal/proto"
)

// Sessiya taymautlari (§2, §3.3)
const (
	dialTimeout     = 10 * time.Second
	handshakeWait   = 5 * time.Second  // auth/hello javobi
	readTimeout     = 60 * time.Second // ping 30 s + zaxira
	hbInterval      = 30 * time.Second // idle heartbeat (§2)
	writeTimeout    = 10 * time.Second
	maxMessageBytes = 1 << 20 // 1 MiB (§2)
)

// ConnError — server yopish kodi (§1, §3.3: 4001/4002/4003/4004).
type ConnError struct {
	Code   int
	Reason string
}

func (e *ConnError) Error() string {
	return fmt.Sprintf("server yopdi: kod=%d sabab=%s", e.Code, e.Reason)
}

// ErrCompat — protocol_version mos emas (§1).
var ErrCompat = errors.New("protocol_version mos emas — server yangilanishi yoki agent yangilanishi kerak")

// SessionConfig — sessiya kirish ma'lumotlari.
type SessionConfig struct {
	ServerURL    string // ws://host:8081
	AgentID      string
	Secret       string
	AgentVersion string
	OS           string
	Hostname     string
	LanIP        string
	BootID       string
	StartedAt    time.Time
	CamsEpoch    int64
	Caps         proto.Caps

	Clock clock.Clock  // nil → Real
	Log   *slog.Logger // nil → Discard
	// HbInterval — idle heartbeat davri (§2 default 30 s; testlarda qisqartiriladi).
	HbInterval time.Duration
	// OnDisconnect — ulanish uzilganda chaqiriladi (UI status chizig'i uchun, §17).
	OnDisconnect func(error)
	// OnMessage — serverdan kelgan har bir xabar (dispatch uchun).
	OnMessage func(proto.Envelope)
	// OnWelcome — welcome qabul qilinganda chaqiriladi (epoch/limits saqlash uchun).
	OnWelcome func(proto.Welcome)
}

func (c *SessionConfig) withDefaults() {
	if c.Clock == nil {
		c.Clock = clock.Real{}
	}
	if c.Log == nil {
		c.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if c.StartedAt.IsZero() {
		c.StartedAt = c.Clock.Now()
	}
	if c.HbInterval <= 0 {
		c.HbInterval = hbInterval
	}
}

// wsURL — ?agent_id= qo'shiladi (§2).
func wsURL(serverURL, agentID string) (string, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", fmt.Errorf("server_url: %w", err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return "", fmt.Errorf("nodirab sxema %q", u.Scheme)
	}
	// Yo'l §2 bo'yicha belgilangan; config faqat host:port beradi
	if u.Path == "" || u.Path == "/" {
		u.Path = "/api/agent/ws"
	}
	q := u.Query()
	q.Set("agent_id", agentID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Session — bitta WS ulanishi + qayta ulanish sikli.
type Session struct {
	cfg SessionConfig

	// seq — xabar ketma-ketligi (§2). Run goroutine'si va dispatch goroutine'si
	// bir vaqtda o'qiydi/yozadi → atomik (race detektori topdi).
	seq atomic.Int64

	// sendCh — yagona yozuvchi goroutine navbati (serve ishlaganda to'ldiriladi).
	// sendMu: Send (dispatch goroutine'si) va serve (Run goroutine'si) uchun.
	sendMu sync.Mutex
	sendCh chan proto.Envelope
}

// setSendCh — serve boshlanishida/yopilishida chaqiriladi.
func (s *Session) setSendCh(ch chan proto.Envelope) {
	s.sendMu.Lock()
	s.sendCh = ch
	s.sendMu.Unlock()
}

// trySend — faol kanalga yuborish (bor bo'lsa). Kanal topish va yuborish
// bitta mutex ostida: serve yopilayotganda "send on closed channel"
// poydeq'idan himoya (race detektori topgan uzil shu edi).
func (s *Session) trySend(env proto.Envelope) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if s.sendCh == nil {
		return ErrChannelClosed
	}
	select {
	case s.sendCh <- env:
		return nil
	default: // navbat to'lgan — uzilish deb hisoblaymiz
		return ErrChannelClosed
	}
}

// closeSendCh — yopilish: avval maydonni nol qilamiz (keyingi trySend'lar
// darhol ErrChannelClosed oladi), so'ng kanalni yopamiz (yozuvchi
// goroutine'ning `range` shu yerda tugaydi).
func (s *Session) closeSendCh(ch chan proto.Envelope) {
	s.sendMu.Lock()
	if s.sendCh == ch {
		s.sendCh = nil
	}
	s.sendMu.Unlock()
	close(ch)
}

// ErrChannelClosed — sessiya yopilgan, xabar yuborib bo'lmadi.
var ErrChannelClosed = errors.New("sessiya yopiq — xabar yuborib bo'lmadi")

func NewSession(cfg SessionConfig) *Session {
	cfg.withDefaults()
	return &Session{cfg: cfg}
}

// SetOnMessage — dispatch bog'lash (Run boshlanishidan oldin chaqiriladi).
func (s *Session) SetOnMessage(fn func(proto.Envelope)) { s.cfg.OnMessage = fn }

// nextSeq — agent tomonidan 1 dan boshlab o'sadi (§2).
func (s *Session) nextSeq() int64 { return s.seq.Add(1) }

// RunOnce — bitta to'liq ulanish: dial → auth → hello → welcome.
// Muvaffaqiyatda welcome'ni qaytaradi; ctx bekor qilinsa ctx.Err().
func (s *Session) RunOnce(ctx context.Context) (*proto.Welcome, error) {
	endpoint, err := wsURL(s.cfg.ServerURL, s.cfg.AgentID)
	if err != nil {
		return nil, err
	}

	dialer := websocket.Dialer{HandshakeTimeout: dialTimeout}
	conn, _, err := dialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("ulanib bo'lmadi: %w", err)
	}
	conn.SetReadLimit(maxMessageBytes)

	defer conn.Close()
	if err := s.handshake(ctx, conn); err != nil {
		return nil, err
	}
	wel, err := s.readWelcome(conn)
	if err != nil {
		return nil, err
	}
	if !wel.Compat.OK {
		// §1: server 4001 bilan yopadi — biz ham darhol chiqamiz
		return &wel, ErrCompat
	}
	if s.cfg.OnWelcome != nil {
		s.cfg.OnWelcome(wel)
	}
	s.cfg.Log.Info("serverga ulandi", "server_version", wel.ServerVersion,
		"last_seq", wel.LastSeq, "needs_full_sync", wel.NeedsFullSync)
	return &wel, nil
}

// handshake — auth (§3.3) va hello (§4.1) yuboradi.
func (s *Session) handshake(ctx context.Context, conn *websocket.Conn) error {
	_ = conn.SetWriteDeadline(s.cfg.Clock.Now().Add(writeTimeout))
	authEnv, err := proto.NewEnvelope(proto.MsgAuth, s.nextSeq(), proto.Auth{
		AgentID: s.cfg.AgentID, Secret: s.cfg.Secret,
	})
	if err != nil {
		return err
	}
	if err := conn.WriteJSON(authEnv); err != nil {
		return fmt.Errorf("auth yuborib bo'lmadi: %w", err)
	}

	hello := proto.Hello{
		ProtocolVersion: proto.ProtocolVersion,
		AgentVersion:    s.cfg.AgentVersion,
		OS:              s.cfg.OS,
		Hostname:        s.cfg.Hostname,
		LanIP:           s.cfg.LanIP,
		BootID:          s.cfg.BootID,
		StartedAt:       s.cfg.StartedAt.UTC(),
		CamsEpoch:       s.cfg.CamsEpoch,
		Caps:            s.cfg.Caps,
	}
	helloEnv, err := proto.NewEnvelope(proto.MsgHello, s.nextSeq(), hello)
	if err != nil {
		return err
	}
	if err := conn.WriteJSON(helloEnv); err != nil {
		return fmt.Errorf("hello yuborib bo'lmadi: %w", err)
	}
	return nil
}

// readWelcome — welcome'ni o'qiydi; server yopsa ConnError qaytadi.
func (s *Session) readWelcome(conn *websocket.Conn) (proto.Welcome, error) {
	_ = conn.SetReadDeadline(s.cfg.Clock.Now().Add(handshakeWait))
	var env proto.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		var ce *websocket.CloseError
		if errors.As(err, &ce) {
			return proto.Welcome{}, &ConnError{Code: ce.Code, Reason: ce.Text}
		}
		return proto.Welcome{}, fmt.Errorf("welcome kutilmadi: %w", err)
	}
	if env.Type != proto.MsgWelcome {
		return proto.Welcome{}, fmt.Errorf("%w: birinchi javob %q", ErrBadResponse, env.Type)
	}
	return proto.Decode[proto.Welcome](env.Data)
}
