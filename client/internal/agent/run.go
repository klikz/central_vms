package agent

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/client/internal/proto"
	"central-vms/client/internal/redact"
)

// profileForCloseCode — qayta ulanish profilini tanlash (§1, §3.3).
//
//	4001 → 5→10→20…300 s   (§1)
//	4003/4004 → 60→120…300 s (§3.3)
//	boshqalar → 1→2→4…60 s
func profileForCloseCode(code int) Profile {
	switch code {
	case 4001:
		return ProfileVersion
	case 4003, 4004:
		return ProfileRevoked
	default:
		return ProfileNormal
	}
}

// profileForError — xatodan profil aniqlaydi.
func profileForError(err error) Profile {
	var ce *ConnError
	if errors.As(err, &ce) {
		return profileForCloseCode(ce.Code)
	}
	if errors.Is(err, ErrCompat) {
		return ProfileVersion
	}
	return ProfileNormal
}

// serve — welcome'dan keyingi holat: idle heartbeat (§2) va o'qish halqasi.
// Yopinish/xato bo'lsa ConnError yoki boshqa xato qaytadi.
func (s *Session) serve(ctx context.Context, conn *websocket.Conn) error {
	sendCh := make(chan proto.Envelope, 64)
	pingTick := s.cfg.Clock.NewTicker(s.cfg.PingEvery)
	defer pingTick.Stop()
	errCh := make(chan error, 2)
	s.setSendCh(sendCh)
	s.setConn(conn)
	defer func() { s.setSendCh(nil); s.setConn(nil) }()

	// §2 + §15 izoh: WS control ping kelganda o'qish muddati uzatiladi.
	// Gorilla control frame ReadJSON'ni qaytarmaydi — deadline o'z
	// o'rnida qolsa, faqat ping/pong bilan yashayotgan ulanish 60 s'da
	// "i/o timeout" bilan uzilardi (2026-11-21 kuzatildi: agent har
	// daqiqada qayta ulanar edi). Default pong handler overwrite
	// qilingani uchun pong o'zimiz yuboriladi.
	conn.SetPingHandler(func(appData string) error {
		_ = conn.SetReadDeadline(s.cfg.Clock.Now().Add(s.cfg.ReadTO))
		return conn.WriteControl(websocket.PongMessage, []byte(appData),
			s.cfg.Clock.Now().Add(writeTimeout))
	})
	// Bizning ping'imizga pong ham faollik belgisi (kontrol frame ReadJSON'ni
	// qaytarmaydi — deadline'ni shu yerda uzaytiramiz).
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(s.cfg.Clock.Now().Add(s.cfg.ReadTO))
		return nil
	})

	// Yagona yozuvchi goroutine (TASKS §0.3: murakkab mutex'lar yo'q)
	go func() {
		for env := range sendCh {
			_ = conn.SetWriteDeadline(s.cfg.Clock.Now().Add(writeTimeout))
			if err := conn.WriteJSON(env); err != nil {
				errCh <- err
				return
			}
		}
	}()

	// O'quvchi: ping/pong gorilla avtomatik javob beradi; qolgan xabarlar
	// C1.14/C2 da ishlatiladi — hozircha noma'lum turlar jimgina o'tkaziladi (§1).
	go func() {
		for {
			_ = conn.SetReadDeadline(s.cfg.Clock.Now().Add(s.cfg.ReadTO))
			var env proto.Envelope
			if err := conn.ReadJSON(&env); err != nil {
				var ce *websocket.CloseError
				if errors.As(err, &ce) {
					errCh <- &ConnError{Code: ce.Code, Reason: ce.Text}
				} else {
					errCh <- err
				}
				return
			}
			// Noma'lum type jimgina o'tkaziladi (§1 forward-compat), ma'lum bo'lsa dispatch.
			if s.cfg.OnMessage != nil {
				s.cfg.OnMessage(env)
			}
		}
	}()

	tick := s.cfg.Clock.NewTicker(s.cfg.HbInterval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			s.closeSendCh(sendCh)
			_ = conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseGoingAway, "agent ishonchli to'xtadi"),
				time.Now().Add(2*time.Second))
			return context.Cause(ctx)
		case err := <-errCh:
			s.closeSendCh(sendCh)
			return err
		case <-pingTick.C():
			// Agent ham WS ping yuboradi: server eski bo'lsa yoki TCP
			// yarim-ochiq bo'lsa, ulanishning o'lganini erta bilamiz.
			if err := conn.WriteControl(websocket.PingMessage, []byte("agent"),
				s.cfg.Clock.Now().Add(writeTimeout)); err != nil {
				s.closeSendCh(sendCh)
				return err
			}
		case <-tick.C():
			now := s.cfg.Clock.Now()
			data := map[string]any{"uptime_s": int64(now.Sub(s.cfg.StartedAt).Seconds())}
			if s.cfg.Stats != nil { // §4.10: mem_mb, publishes — hb ichida
				for k, v := range s.cfg.Stats() {
					data[k] = v
				}
			}
			env, err := proto.NewEnvelope(proto.MsgHB, s.nextSeq(), data)
			if err != nil {
				s.closeSendCh(sendCh)
				return err
			}
			if err := s.trySend(env); err != nil { // navbat to'lgan — uzilish deb hisoblaymiz
				s.closeSendCh(sendCh)
				return errors.New("heartbeat navbati to'ldi")
			}
			if s.cfg.OnHeartbeat != nil {
				s.cfg.OnHeartbeat(now)
			}
		}
	}
}

// Run — uzluksiz qayta ulanish sikli (backoff clock.Clock orqali: fake clock testi).
// ctx bekor qilinsa nil qaytaradi.
func (s *Session) Run(ctx context.Context) error {
	// server_url sozlanmagan (birinchi ishga tushirish — config avtomatik
	// yaratilganda bo'sh bo'ladi): backoff bilan xato loglamasdan TINIM.
	// Lokal UI "Save" sessiyani qayta boshlaydi (main.go reload), shuning
	// uchun bu yerda faqat ctx kutiladi. 2026-11-23 talabi.
	if s.cfg.ServerURL == "" {
		s.cfg.Log.Info("server_url sozlanmagan",
			"izoh", "lokal UI'dan server manzilini kiriting — sessiya avtomatik qayta boshlanadi")
		t := s.cfg.Clock.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-t.C():
			}
		}
	}

	bo := NewBackoff(ProfileNormal, nil)
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		start := s.cfg.Clock.Now()
		err := s.connectAndServe(ctx)
		// Sog'lom sessiya bo'lgan bo'lsa kutish bazasini qaytamiz —
		// aks holda hisob kunlar davomida o'sib boradi va 5 sekundlik
		// tarmoq uzilishidan keyin agent 60 s (ba'zan 300 s) kutadi
		// (2026-11-21 audit topilmasi).
		resetBackoff(bo, s.cfg.Clock.Now().Sub(start))
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		if r := s.cfg.OnDisconnect; r != nil && !errors.Is(err, context.Canceled) {
			r(err)
		}
		if p := profileForError(err); p != bo.Profile() {
			bo = NewBackoff(p, nil)
		}
		d := bo.Next()
		att := bo.Attempt()
		// Talab (2026-11-19): barcha xatolar ko'rinishi kerak — 3 ta
		// ketma-ket muvaffaqiyatsiz urinishda holat ERROR darajasiga
		// ko'tariladi (spam qilmaslik uchun bir marta), keyin WARN.
		msg, lvl := "server bilan aloqa uzildi, qayta uriniladi", slog.LevelWarn
		if att == 3 {
			msg, lvl = "server bilan aloqa uzildi — 3 urinish muvaffaqiyatsiz", slog.LevelError
		}
		s.cfg.Log.Log(ctx, lvl, msg,
			"kutish", d.String(), "urinish", att, "err", redact.String(err.Error()))

		select {
		case <-ctx.Done():
			return nil
		case <-s.cfg.Clock.After(d):
		}
	}
}

// connectAndServe — bitta to'liq ulanish: dial → auth → hello → welcome → serve.
func (s *Session) connectAndServe(ctx context.Context) error {
	endpoint, err := wsURL(s.cfg.ServerURL, s.cfg.AgentID)
	if err != nil {
		return err
	}
	dialer := websocket.Dialer{HandshakeTimeout: dialTimeout}
	conn, _, err := dialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		return fmtDialErr(err)
	}
	defer conn.Close()
	conn.SetReadLimit(maxMessageBytes)

	if err := s.handshake(ctx, conn); err != nil {
		return err
	}
	wel, err := s.readWelcome(conn)
	if err != nil {
		return err
	}
	if !wel.Compat.OK {
		// §1/C4.7: versiya mos emas — UI banneri uchun ma'lumot beramiz.
		if s.cfg.OnCompat != nil {
			s.cfg.OnCompat(wel)
		}
		return ErrCompat
	}
	s.seq.Store(wel.LastSeq) // seq serverda qayerda qolgani bilan davom etadi (§2)
	if s.cfg.OnWelcome != nil {
		s.cfg.OnWelcome(wel)
	}
	return s.serve(ctx, conn)
}

// fmtDialErr — gorilla xatolarini bizning ConnError/AError ko'rinishiga keltiradi.
func fmtDialErr(err error) error {
	if errors.Is(err, websocket.ErrBadHandshake) {
		return &ConnError{Code: 0, Reason: "WS upgrade rad etildi (server /api/agent/ws yo'q yoki boshqa javob berdi)"}
	}
	return err
}

// resetBackoff — sessiya davomiyligi bo'yicha urinishlar hisobini
// tiklash qarori (deterministik test uchun ajratilgan).
func resetBackoff(bo *Backoff, dur time.Duration) {
	if dur >= healthySession {
		bo.Reset()
	}
}
