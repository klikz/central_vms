// S3.0: UI ulanishining o'quv/yozuv/ping sikllari (single-writer —
// barcha yozuvlar out kanali orqali, TASKS §0.3).
package wsui

import (
	"time"

	"github.com/gorilla/websocket"
)

func (c *conn) writeLoop() {
	// `out` HECH QACHON yopilmaydi: Broadcast conns nusxasini qulf
	// ostida oladi, trySend'ni esa qulf TASHQARISIDA qiladi (hub.go).
	// shutdown'da channel yopilsa, parallel trySend "send on closed
	// channel" panic qiladi va butun vmsd qulaydi (2026-11-21 auditi).
	// Chiqish `done` orqali belgilanadi.
	for {
		select {
		case <-c.done:
			return
		case raw := <-c.out:
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteMessage(websocket.TextMessage, raw); err != nil {
				c.close()
				return
			}
		}
	}
}

func (c *conn) pingLoop() {
	tk := time.NewTicker(c.hubPingInterval())
	defer tk.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-tk.C:
			// WriteControl — yagona yozuvchi qoidasini buzmaydi
			// (gorilla: WriteControl boshqa metodlar bilan parallel
			// chaqirilishi mumkin, WriteMessage EMAS).
			if err := c.ws.WriteControl(websocket.PingMessage, nil,
				time.Now().Add(5*time.Second)); err != nil {
				c.close()
				return
			}
		}
	}
}

// readLoop — UI dan faqat kontrol frame'lar va ixtiyoriy "hb" keladi;
// o'qish xatosi ulanishni yopadi. Pong — faollik belgisi.
func (c *conn) readLoop() {
	dead := c.hubDeadTimeout()
	_ = c.ws.SetReadDeadline(time.Now().Add(dead))
	c.ws.SetPongHandler(func(string) error {
		return c.ws.SetReadDeadline(time.Now().Add(dead))
	})
	for {
		var env Envelope
		if err := c.ws.ReadJSON(&env); err != nil {
			return
		}
		// Har qanday xabar faollik hisoblanadi.
		_ = c.ws.SetReadDeadline(time.Now().Add(dead))
	}
}

func (c *conn) close() {
	_ = c.ws.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(2*time.Second))
	c.shutdown()
}

func (c *conn) shutdown() {
	c.once.Do(func() {
		// ESKISI: close(c.out) — yuqoridagi panic sababi.
		close(c.done)
		_ = c.ws.Close()
	})
}

func (c *conn) hubPingInterval() time.Duration {
	return c.hub.pingTO
}

func (c *conn) hubDeadTimeout() time.Duration {
	return c.hub.deadTO
}
