// S1.9: handshake — protocol.md §18: auth → hello → welcome (kodlar 4001–4003).
package wsagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/server/internal/auth"
	"central-vms/server/internal/proto"
	"central-vms/server/internal/registry"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Agent — mobil/joy tarmog'idan keladi, Origin tekshiruvi ma'nosiz
	// (TLS yo'q ichki tarmoq; xavfsizlik agent_secret orqali).
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// ServeHTTP — `/api/agent/ws?agent_id=…` (handler query'ni tekshiradi).
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	if agentID == "" {
		http.Error(w, `{"error":"missing_agent_id"}`, http.StatusBadRequest)
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.lg.Warn("ws upgrade", "err", err)
		return
	}

	conn, err := h.handshake(ws, agentID)
	if err != nil {
		h.lg.Info("handshake rad etildi", "agent_id", agentID, "err", err)
		return
	}
	h.runConn(conn)
}

// handshake — auth → hello → tekshiruv → welcome. Xato holatda ws yopilgan
// holda qaytadi (nil, err).
func (h *Hub) handshake(ws *websocket.Conn, agentID string) (*Conn, error) {
	_ = ws.SetReadDeadline(time.Now().Add(h.cfg.HandshakeTO))
	var env proto.Envelope
	if err := ws.ReadJSON(&env); err != nil || env.Type != proto.MsgAuth {
		_ = ws.WriteControl(websocket.CloseMessage,
			closeFrame(4002, "auth_kutilmoqda"), time.Now().Add(2*time.Second))
		_ = ws.Close()
		return nil, fmt.Errorf("auth kutilmadi: %w", err)
	}
	var a proto.Auth
	if err := json.Unmarshal(env.Data, &a); err != nil || a.AgentID == "" {
		_ = ws.WriteControl(websocket.CloseMessage, closeFrame(4002, "auth_yaroqsiz"), time.Now().Add(2*time.Second))
		_ = ws.Close()
		return nil, errors.New("auth parse")
	}
	if a.AgentID != agentID {
		_ = ws.WriteControl(websocket.CloseMessage, closeFrame(4003, "agent_id_motmatch"), time.Now().Add(2*time.Second))
		_ = ws.Close()
		return nil, fmt.Errorf("agent_id mos emas: URL=%s auth=%s", agentID, a.AgentID)
	}

	client, err := h.registry.GetClientByAgentID(agentID)
	if err != nil {
		_ = ws.WriteControl(websocket.CloseMessage, closeFrame(4003, "agent_noma'lum"), time.Now().Add(2*time.Second))
		_ = ws.Close()
		return nil, fmt.Errorf("client topilmadi: %w", err)
	}
	if !auth.SecretEqual(HashSecret(a.Secret), client.AgentSecretHash) {
		_ = ws.WriteControl(websocket.CloseMessage, closeFrame(4003, "secret_noto'g'ri"), time.Now().Add(2*time.Second))
		_ = ws.Close()
		return nil, errors.New("secret noto'g'ri")
	}

	_ = ws.SetReadDeadline(time.Now().Add(h.cfg.HandshakeTO))
	if err := ws.ReadJSON(&env); err != nil || env.Type != proto.MsgHello {
		_ = ws.WriteControl(websocket.CloseMessage, closeFrame(4002, "hello_kutilmoqda"), time.Now().Add(2*time.Second))
		_ = ws.Close()
		return nil, fmt.Errorf("hello kutilmadi: %w", err)
	}
	var hello proto.Hello
	if err := json.Unmarshal(env.Data, &hello); err != nil {
		_ = ws.WriteControl(websocket.CloseMessage, closeFrame(4002, "hello_yaroqsiz"), time.Now().Add(2*time.Second))
		_ = ws.Close()
		return nil, fmt.Errorf("hello parse: %w", err)
	}

	conn := &Conn{hub: h, clientID: client.ID, agentID: agentID, ws: ws,
		out: make(chan json.RawMessage, 64), done: make(chan struct{})}

	compat := proto.Compat{OK: true}
	if hello.ProtocolVersion < h.cfg.MinProto || hello.ProtocolVersion > h.cfg.MaxProto {
		compat.OK = false
		compat.ServerVersion, compat.MinSupported, compat.MaxSupported =
			h.cfg.ServerVersion, h.cfg.MinProto, h.cfg.MaxProto
		// §1: avval welcome{compat.ok:false}, keyin 5 s ichida 4001.
		w := h.buildWelcome(conn, hello, compat)
		if raw, err := json.Marshal(proto.Envelope{Type: proto.MsgWelcome, TS: time.Now().UTC(), Data: mustRaw(w)}); err == nil {
			// Write deadline'siz yozuv: mijoz o'qimasa goroutine cheksiz
			// bloklanib qolardi.
			_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_ = ws.WriteMessage(websocket.TextMessage, raw)
			_ = ws.SetWriteDeadline(time.Time{})
		}
		_ = ws.WriteControl(websocket.CloseMessage, closeFrame(CloseIncompat, "compat"), time.Now().Add(2*time.Second))
		_ = ws.Close()
		return nil, fmt.Errorf("protocol %d oralikda emas (%d..%d)", hello.ProtocolVersion, h.cfg.MinProto, h.cfg.MaxProto)
	}

	// §2: bitta xabar ≤1 MiB. Limit qo'yilmasa bitta (hatto auth'siz)
	// ulanish ulkan JSON bilan vmsd xotirasini yey olardi (DoS).
	ws.SetReadLimit(maxAgentFrame)

	// Bir agent — bitta ulanish: eski conn'ni mayin yopamiz (4005).
	h.mu.Lock()
	if old := h.conns[agentID]; old != nil && old != conn {
		old.closeWith(CloseReplaced, "qayta_ulanish")
	}
	h.conns[agentID] = conn
	h.mu.Unlock()

	if err := h.registry.UpdateClientHello(client.ID, hello.AgentVersion, hello.OS, hello.LanIP, mustJSON(hello.Caps)); err != nil {
		h.lg.Warn("hello saqlash", "agent_id", agentID, "err", err)
	}
	// Operator nomi (`hello.name`): agent UI'da nomni o'zgartirsa, shu
	// yerda serverga yetib keladi va interfeysda darhol ko'rinadi.
	if nm := registry.SanitizeName(hello.Name); nm != "" && nm != client.Name {
		if err := h.registry.UpdateClientName(client.ID, nm); err != nil {
			h.lg.Warn("client nomi yangilanmadi", "agent_id", agentID, "err", err)
		} else {
			h.lg.Info("client nomi yangilandi", "agent_id", agentID, "name", nm)
			h.uiEvent(EvClientStatus, map[string]any{
				"client_id": client.ID, "agent_id": agentID,
				"status": "online", "name": nm,
			})
		}
	}
	// welcome — handshake bosqichida writeLoop hali yo'q: to'g'ridan-to'g'ri yozamiz.
	w := h.buildWelcome(conn, hello, proto.Compat{OK: true})
	if err := conn.sendDirect(proto.MsgWelcome, w); err != nil {
		// Ro'yxatga OLINGAN conn: xatoda tozalanmasa `Online()` abadiy
		// true qolardi (UI'da yo'q sessiya "online", MarkClientOffline
		// hech qachon chaqirilmaydi) va fd/out kanali sizib chiqardi.
		h.mu.Lock()
		if h.conns[agentID] == conn {
			delete(h.conns, agentID)
		}
		h.mu.Unlock()
		conn.shutdown()
		return nil, err
	}
	return conn, nil
}

func (h *Hub) buildWelcome(c *Conn, hello proto.Hello, compat proto.Compat) proto.Welcome {
	h.mu.Lock()
	lastSeq, epoch := h.lastSeq[c.agentID], h.epochs[c.agentID]
	h.mu.Unlock()
	client, _ := h.registry.GetClientByAgentID(c.agentID)

	w := proto.Welcome{
		ServerVersion: h.cfg.ServerVersion, ProtocolVersion: h.cfg.MaxProto,
		Compat: compat, LastSeq: lastSeq, CamsEpoch: epoch,
		NeedsFullSync: epoch == 0 || hello.CamsEpoch != epoch,
	}
	mu, mt, ttl := client.MaxUploads, client.MaxTranscodes, 60
	w.Limits = proto.Limits{MaxUploads: &mu, MaxLocalTranscodes: &mt, MediaTTLS: &ttl}
	return w
}

func closeFrame(code int, reason string) []byte {
	return websocket.FormatCloseMessage(code, reason)
}

func mustRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func mustJSON(v any) string { return string(mustRaw(v)) }
