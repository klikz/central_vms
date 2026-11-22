// contract_matrix_test.go — "eski agent ↔ yangi server" matritsasi
// (2026-11-22 barqarorlik ishlari). Golden fixture'lar (proto_test)
// maydonlarni tekshiradi; bu yerda esa JONLI oqim orqali: eski agent
// yangi maydonlarni YUBORMASA server ularni majburiy deb bilmaskligi,
// yangiroq agent YUBORGAN notanish maydonlarni esa server jim
// yutishini kafolatlaydi — aks holda har upgrade'da maydon qo'shilganda
// eski agentlar offline bo'lib qolardi.
package wsagent

import (
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/server/internal/proto"
)

// TestContract_LegacyAgentMinimalSync — eski (1.0-draft.5 davri) agent:
// hello'da `name` yo'q, cams_sync'da `position/codecs/bframe` yo'q,
// cam_status'da `rtt_ms` yo'q, hb bo'sh. Server hammasini qabul
// qilishi, kamera saqlanishi va ulanish uzilmasligi kerak.
func TestContract_LegacyAgentMinimalSync(t *testing.T) {
	e := newTestEnv(t)
	agentID, secret := e.enroll(t)

	// Eski hello: faqat §4.1'ning birinchi versiya maydonlari.
	ws, w := dialHandshakeWith(t, e, agentID, secret, proto.Hello{
		ProtocolVersion: 1, AgentVersion: "0.9-legacy", OS: "linux",
		Hostname: "joy-eski", CamsEpoch: 0,
	})
	if !w.Compat.OK {
		t.Fatalf("compat: %+v", w.Compat)
	}

	// DIQQAT: startHB ishlatmaymiz — u alohida goroutine'da yozadi va
	// gorilla bitta Conn'ga parallel yozishni taqiqlaydi (race).
	// DeadTO=5s: 2 s lik tekshiruv oynasi hb'siz ham yashaydi.
	send := func(typ string, raw json.RawMessage) {
		if err := ws.WriteJSON(proto.Envelope{Type: typ, Seq: 2, TS: time.Now().UTC(), Data: raw}); err != nil {
			t.Fatalf("send %s: %v", typ, err)
		}
	}

	// cams_sync: ESKI maydonlar to'plami (position/codecs/bframe YO'Q).
	send(proto.MsgCamsSync, json.RawMessage(`{
		"epoch": 7, "full": true, "seq_no": 1, "chunk_count": 1, "done": true,
		"cameras": [{"op":"upsert","id":"cam-legacy-1","name":"Eski hik",
			"rtsp_masked":"rtsp://admin:***@192.168.0.24:554/Streaming/Channels/101",
			"profiles":{"main":"/Streaming/Channels/101","sub":""},
			"status":"offline","enabled":true}]}`))
	ackEnv := readEnvUntil(t, ws, proto.MsgCamsAck)
	var ack proto.CamsAck
	if err := json.Unmarshal(ackEnv.Data, &ack); err != nil {
		t.Fatalf("cams_ack: %v", err)
	}
	if !ack.OK || ack.Applied != 1 || ack.Epoch != 7 {
		t.Fatalf("cams_ack noto'g'ri: %+v", ack)
	}

	// Jonlik belgisi (hb eski ko'rinishi — bo'sh data).
	send(proto.MsgHB, json.RawMessage(`{}`))

	// cam_status: rtt_ms maydoni YO'Q (eski agent yubormaydi).
	send(proto.MsgCamStatus, json.RawMessage(`{
		"camera_id":"cam-legacy-1","status":"online","error":"",
		"checked_at":"2026-11-22T00:00:00Z"}`))

	// Notanish (kelajak versiyasi) maydonlari ham jim yutilishi kerak:
	// yangi agent eski serverga shu tarzda "kelajakdan" xabar yuboradi.
	send(proto.MsgHB, json.RawMessage(`{"rtp_discarded":5,"au_forwarded":10,
		"some_future_field":{"nested":true}}`))

	// Ulanish tirik: 2 s davomida close/error kelmamasini tekshiramiz.
	// (startHB IdleTO=1s ni tirik ushlab turadi — agent o'lik bo'lganda
	// server DeadTO bilan yopar edi.)
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var env proto.Envelope
		if err := ws.ReadJSON(&env); err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				break // yaxshi: 2 s ichida error/close kelmadi
			}
			var ce *websocket.CloseError
			if errorsAs(err, &ce) {
				t.Fatalf("ulanish uzildi: %+v", ce)
			}
			t.Fatalf("o'qish: %v", err)
		}
		if env.Type == proto.MsgError {
			t.Fatalf("server xato qaytardi: %s", env.Data)
		}
	}

	// Kamera saqlanganmi + legacy defaultlar bilan.
	cams, err := e.reg.ListCamerasByAgent(agentID)
	if err != nil || len(cams) != 1 {
		t.Fatalf("kameralar: %v (%v)", cams, err)
	}
	c := cams[0]
	if c.ID != "cam-legacy-1" || c.Name != "Eski hik" || c.Position != 0 {
		t.Fatalf("kamera noto'g'ri saqlandi: %+v", c)
	}
	if c.Status != "online" {
		t.Fatalf("cam_status (rtt_ms'siz) qo'llanilmadi: %+v", c)
	}
}
