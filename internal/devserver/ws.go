package devserver

import (
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/client/internal/proto"
)

// Yopinish kodlari (§1, §3.3)
const (
	CloseVersionMismatch = 4001
	CloseAuthTimeout     = 4002
	CloseBadSecret       = 4003
	CloseRevoked         = 4004
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Lokal mock: Origin tekshiruvi ma'noga ega emas (mijoz brauzer emas).
	CheckOrigin: func(*http.Request) bool { return true },
}

func ptr[T any](v T) *T { return &v }

// handleWS — §3.3 + §4.1: auth → hello → welcome.
// Xato holatlar: auth/hello kechiksa 4002, secret noto'g'ri 4003, versiya mos emas 4001.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // upgrade xato javobi yozilgan
	}
	defer conn.Close()

	// 1) auth (§3.3)
	env, err := s.readEnvelope(conn, s.o.AuthTimeout)
	if err != nil {
		s.closeConn(conn, CloseAuthTimeout, "auth_kutilmoqda")
		return
	}
	if env.Type != proto.MsgAuth {
		s.closeConn(conn, CloseAuthTimeout, "auth_kutilmoqda")
		return
	}
	auth, err := proto.Decode[proto.Auth](env.Data)
	if err != nil || auth.AgentID == "" {
		s.closeConn(conn, CloseBadSecret, "auth_noto'g'ri")
		return
	}
	want, ok := s.secretFor(auth.AgentID)
	if !ok || want != auth.Secret {
		s.closeConn(conn, CloseBadSecret, "secret_rad_etildi")
		return
	}
	s.markOnline(auth.AgentID)

	// 2) hello (§4.1)
	env, err = s.readEnvelope(conn, s.o.AuthTimeout)
	if err != nil || env.Type != proto.MsgHello {
		s.closeConn(conn, CloseAuthTimeout, "hello_kutilmoqda")
		return
	}
	hello, err := proto.Decode[proto.Hello](env.Data)
	if err == nil && s.o.OnHello != nil {
		s.o.OnHello(hello)
	}
	if err != nil {
		s.closeConn(conn, CloseVersionMismatch, "hello_noto'g'ri")
		return
	}
	if hello.ProtocolVersion < s.o.MinVersion || hello.ProtocolVersion > s.o.MaxVersion {
		// §1: avval compat ok:false yuboriladi, keyin 4001 bilan yopiladi
		wel := s.welcome(hello)
		wel.Compat = proto.Compat{OK: false, ServerVersion: ptr(s.o.ServerVer),
			MinSupported: ptr(s.o.MinVersion), MaxSupported: ptr(s.o.MaxVersion)}
		_ = s.writeEnvelope(conn, proto.MsgWelcome, wel)
		s.closeConn(conn, CloseVersionMismatch, "protocol_version mos emas")
		return
	}

	// 3) welcome — SETTL'dan keyin, lekin yagona yozuvchi goroutine ishga
	// tushguncha NAVBATGA qo'yiladi. Shunda keyinchalik yuboriladigan
	// cams_ack/ping welcome'dan oldin chiqib ketmaydi (§3.3 tartibi).
	ac := &agentConn{conn: conn, out: make(chan outMsg, 64), done: make(chan struct{})}
	ac.aliveFlag.Store(true)
	welEnv, err := proto.NewEnvelope(proto.MsgWelcome, 0, s.welcome(hello))
	if err != nil {
		return
	}
	ac.out <- outMsg{env: &welEnv}
	s.setConn(auth.AgentID, ac)
	defer func() {
		ac.aliveFlag.Store(false)
		close(ac.done)
		s.clearConn(auth.AgentID)
	}()

	writerErr := make(chan error, 1)
	go func() {
		for m := range ac.out {
			deadline := time.Now().Add(5 * time.Second)
			_ = conn.SetWriteDeadline(deadline)
			if m.ping {
				if err := conn.WriteControl(websocket.PingMessage, []byte("hb"), deadline); err != nil {
					writerErr <- err
					return
				}
				continue
			}
			if err := conn.WriteJSON(*m.env); err != nil {
				writerErr <- err
				return
			}
		}
	}()
	defer close(ac.out)

	// Mock: ulanish saqlanadi; kelgan xabarlar kuzatiladi (C1.14 da to'liq ishlanadi).
	// §2: server har 30 s ping yuboradi — agent avtomatik pong qaytaradi.
	code := websocket.CloseNormalClosure
	ping := time.NewTicker(s.o.PingInterval)
	defer ping.Stop()
	forceClose := make(chan struct{})
	go func() {
		for range ping.C {
			select {
			case ac.out <- outMsg{ping: true}:
			case <-forceClose:
				return
			}
		}
	}()
	defer close(forceClose)
	_ = writerErr

	for {
		// handshake'da qo'yilgan 5 s deadline'ni uzatamiz (aks holda 5 s'da uziladi)
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		var env proto.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			var ce *websocket.CloseError
			if errors.As(err, &ce) {
				code = ce.Code
			}
			break
		}
		if env.Type == proto.MsgCamsSync {
			s.handleCamsSync(auth.AgentID, env)
		}
		if s.o.OnMessage != nil {
			s.o.OnMessage(auth.AgentID, env)
		}
	}
	if s.o.OnClose != nil {
		s.o.OnClose(auth.AgentID, code)
	}
}

func (s *Server) welcome(hello proto.Hello) proto.Welcome {
	return proto.Welcome{
		ServerVersion:   s.o.ServerVer,
		ProtocolVersion: s.o.MaxVersion,
		Compat: proto.Compat{OK: true, ServerVersion: ptr(s.o.ServerVer),
			MinSupported: ptr(s.o.MinVersion), MaxSupported: ptr(s.o.MaxVersion)},
		CamsEpoch:     hello.CamsEpoch,
		Limits:        proto.Limits{MaxUploads: ptr(8), MaxLocalTranscodes: ptr(4), MediaTTLS: ptr(60)},
		NeedsFullSync: true, // mock: har safar to'liq sync so'raydi (C1.6 testi uchun qulay)
	}
}

func (s *Server) readEnvelope(conn *websocket.Conn, d time.Duration) (proto.Envelope, error) {
	_ = conn.SetReadDeadline(time.Now().Add(d))
	var env proto.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		return proto.Envelope{}, err
	}
	return env, nil
}

func (s *Server) writeEnvelope(conn *websocket.Conn, typ string, data any) error {
	env, err := proto.NewEnvelope(typ, 0, data)
	if err != nil {
		return err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteJSON(env)
}

func (s *Server) closeConn(conn *websocket.Conn, code int, reason string) {
	_ = conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason), time.Now().Add(2*time.Second))
}
