// S1.9-13: o'quvchi sikli, cams_sync/cams_ack, cam_status/codec_probe,
// ping/hb (idle kuzatuvi), secret amallari.
package wsagent

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/server/internal/proto"
)

// runConn — yozuvchi goroutine + o'quvchi sikl (conn tugaganda ikkalasi ham
// tugaydi; goroutine sizib chiqishi goleak bilan tekshiriladi).
func (h *Hub) runConn(c *Conn) {
	go c.writeLoop(c.done)
	go c.pingLoop(c.done)

	h.lg.Info("agent ulandi", "agent_id", c.agentID, "client_id", c.clientID)
	// Pong (bizning ping'ga javob) — faollik belgisi: o'quvchi sikl
	// deadline'ini uzaytiramiz (kontrol frame ReadJSON'ni qaytarmaydi).
	c.ws.SetPongHandler(func(string) error {
		_ = c.ws.SetReadDeadline(time.Now().Add(c.hub.cfg.DeadTO))
		return nil
	})
	err := c.readLoop()

	h.mu.Lock()
	if h.conns[c.agentID] == c {
		delete(h.conns, c.agentID)
	}
	h.mu.Unlock()
	c.shutdown() // out+done bir marta yopiladi: writeLoop/pingLoop tugaydi
	if err := h.registry.MarkClientOffline(c.clientID); err != nil {
		h.lg.Warn("offline belgilash", "agent_id", c.agentID, "err", err)
	}
	var ce *websocket.CloseError
	if errors.As(err, &ce) && ce.Code == CloseIdle {
		h.lg.Warn("agent uzildi: 30 s javobsiz", "agent_id", c.agentID)
	} else {
		h.lg.Info("agent uzildi", "agent_id", c.agentID, "err", err)
	}
	c.ws.Close()
}

// pingLoop — har IdleTO da `ping` (protocol.md §2: server 30 sda ping,
// agent pong/hb bilan javob beradi; javobsizlik readLoop'da 4004 bilan yopiladi).
func (c *Conn) pingLoop(done chan struct{}) {
	interval := c.hub.cfg.IdleTO
	for {
		select {
		case <-done:
			return
		case <-time.After(interval):
			// WS control ping (protocol.md §2 "ping/pong"); agent kutubxonalari
			// avtomatik pong qaytaradi, qo'shimcha envelope yuborilmaydi.
			if err := c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}
}

// writeLoop — yagona yozuvchi (single-writer).
func (c *Conn) writeLoop(done chan struct{}) {
	// done'ni shutdown() yopadi (writeLoop chiqishda emas — ikki marta
	// close panic bo'lardi).
	for raw := range c.out {
		_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := c.ws.WriteMessage(websocket.TextMessage, raw); err != nil {
			return
		}
	}
}

// readLoop — envelope'lar; idle: IdleTO ichida hech qanday xabar kelmasa
// 4004 bilan yopamiz (server ping yuboradi, agent pong/hb bilan javob beradi).
func (c *Conn) readLoop() error {
	for {
		_ = c.ws.SetReadDeadline(time.Now().Add(c.hub.cfg.DeadTO))
		var env proto.Envelope
		if err := c.ws.ReadJSON(&env); err != nil {
			var ce *websocket.CloseError
			if errors.As(err, &ce) {
				return err
			}
			var ne interface{ Timeout() bool }
			if errors.As(err, &ne) && ne.Timeout() {
				_ = c.ws.WriteControl(websocket.CloseMessage,
					closeFrame(CloseIdle, "idle"), time.Now().Add(2*time.Second))
				return &websocket.CloseError{Code: CloseIdle, Text: "idle"}
			}
			return err
		}
		c.trackSeq(env.Seq)
		if err := c.dispatch(env); err != nil {
			c.hub.lg.Warn("xabar bajarilmadi", "agent_id", c.agentID, "type", env.Type, "err", err)
		}
	}
}

func (c *Conn) trackSeq(seq int64) {
	h := c.hub
	h.mu.Lock()
	if seq > h.lastSeq[c.agentID] {
		h.lastSeq[c.agentID] = seq
	}
	h.mu.Unlock()
}

// dispatch — agent → server xabarlari (noma'lum type jimgina o'tkaziladi —
// forward-compatible, protocol.md §1).
func (c *Conn) dispatch(env proto.Envelope) error {
	switch env.Type {
	case proto.MsgCamsSync:
		var cs proto.CamsSync
		if err := json.Unmarshal(env.Data, &cs); err != nil {
			return err
		}
		return c.handleCamsSync(cs)
	case proto.MsgCamStatus:
		var st proto.CamStatus
		if err := json.Unmarshal(env.Data, &st); err != nil {
			return err
		}
		return c.hub.registry.SetCameraStatus(st.CameraID, st.Status, st.Error, st.RTTMs)
	case proto.MsgCodecProbe:
		var cp proto.CodecProbe
		if err := json.Unmarshal(env.Data, &cp); err != nil {
			return err
		}
		return c.hub.registry.SaveCodecProbe(cp.CameraID, cp.Codecs.Main, cp.Codecs.Sub,
			cp.BFrame.Main, cp.BFrame.Sub)
	case proto.MsgHB, proto.MsgPong, proto.MsgSecretRotated, proto.MsgCodecFixDone,
		proto.MsgDiscoveryDone, proto.MsgSelftestDone:
		return c.hub.registry.MarkClientSeen(c.clientID)
	case proto.MsgMediaReady, proto.MsgMediaStopped, proto.MsgMediaStats:
		// S2: session manager qabul qiladi (hozircha idle hisobi uchun seen).
		return c.hub.registry.MarkClientSeen(c.clientID)
	default:
		return nil
	}
}

// sendDirect — faqat handshake bosqichida (writeLoop boshlanmadan oldin).
func (c *Conn) sendDirect(typ string, data any) error {
	raw, err := json.Marshal(proto.Envelope{Type: typ, TS: time.Now().UTC(), Data: mustRaw(data)})
	if err != nil {
		return err
	}
	_ = c.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.ws.WriteMessage(websocket.TextMessage, raw)
}

// sendEnvelope — server xabarini yozuvchi kanalga qo'yish.
func (c *Conn) sendEnvelope(typ string, data any) error {
	raw, err := json.Marshal(proto.Envelope{Type: typ, TS: time.Now().UTC(), Data: mustRaw(data)})
	if err != nil {
		return err
	}
	select {
	case c.out <- raw:
		return nil
	default:
		return ErrSlowAgent
	}
}

// closeWith — eski ulanishni siqib chiqarish: close frame + kanallarni
// yopish (sync.Once: runConn ham yopadi, ikki marta close panic bo'lardi).
func (c *Conn) closeWith(code int, reason string) {
	_ = c.ws.WriteControl(websocket.CloseMessage,
		closeFrame(code, reason), time.Now().Add(2*time.Second))
	c.shutdown()
}

func (c *Conn) shutdown() {
	c.closeOnce.Do(func() {
		close(c.out)
		close(c.done)
	})
}
