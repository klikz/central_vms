// S3.0: UI push kanali orqali hodisalar (agent holati, sessiya holati).
package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/server/internal/wsui"
)

func TestUIPushEvents(t *testing.T) {
	srv, _, _, _, _, ui := newServer(t)
	adminTok := login(t, srv, "admin1")

	// UI ulanadi: avval bir martalik ticket olinadi (?access= endi
	// qabul qilinmaydi — token query'da loglarga tushardi).
	codeTk, tkMap, tkRaw := do(t, http.MethodPost, srv.URL+"/api/ui/ws-ticket", "", adminTok)
	if codeTk != http.StatusOK {
		t.Fatalf("ws-ticket: %d %s", codeTk, tkRaw)
	}
	ticket, _ := tkMap["ticket"].(string)
	if ticket == "" {
		t.Fatalf("ws-ticket javobi: %v", tkMap)
	}
	wsURL := "ws://" + strings.TrimPrefix(srv.URL, "http://") + "/api/ui/ws?ticket=" + ticket
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("UI ulanish: %v", err)
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var env wsui.Envelope
	if err := ws.ReadJSON(&env); err != nil || env.Type != wsui.EvHello {
		t.Fatalf("hello kutilardi: %v %q", err, env.Type)
	}
	if ui.Count() != 1 {
		t.Fatalf("hub Count: %d", ui.Count())
	}

	// Noto'g'ri token bilan UI ulanmaydi.
	if _, resp, err := websocket.DefaultDialer.Dial(
		"ws://"+strings.TrimPrefix(srv.URL, "http://")+"/api/ui/ws?access=soxta", nil); err == nil || resp.StatusCode != 401 {
		t.Fatalf("noto'g'ri UI token: %v %v", err, resp)
	}

	// Agent ulanadi → client_status(online) buferda turadi (out=32).
	enrollAgent(t, srv, adminTok, "ui-kamera")
	deadline := time.Now().Add(6 * time.Second)
	gotOnline := false
	for time.Now().Before(deadline) && !gotOnline {
		_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		var e wsui.Envelope
		if err := ws.ReadJSON(&e); err != nil {
			break
		}
		if e.Type == wsui.EvClientStatus {
			var cs wsui.ClientStatus
			_ = json.Unmarshal(e.Data, &cs)
			if cs.Status == "online" {
				gotOnline = true
			}
		}
	}
	if !gotOnline {
		t.Fatal("client_status(online) kelmadi")
	}

	// Kamera topib play qilamiz → session_state(publishing) keladi.
	var clients []struct {
		ID int64 `json:"id"`
	}
	_, _, raw := do(t, http.MethodGet, srv.URL+"/api/clients", "", adminTok)
	_ = json.Unmarshal([]byte(raw), &clients)
	if len(clients) == 0 {
		t.Fatal("client yo'q")
	}
	_, _, raw = do(t, http.MethodGet, srv.URL+"/api/clients/"+itoa64(clients[0].ID)+"/cameras", "", adminTok)
	var cams []struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(raw), &cams)
	if len(cams) == 0 {
		t.Fatal("kamera yo'q")
	}
	code, _, raw := do(t, http.MethodPost, srv.URL+"/api/cameras/"+cams[0].ID+"/play", `{}`, adminTok)
	if code != 200 {
		t.Fatalf("play: %d %s", code, raw)
	}

	deadline = time.Now().Add(5 * time.Second)
	gotState := false
	for time.Now().Before(deadline) && !gotState {
		_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		var e wsui.Envelope
		if err := ws.ReadJSON(&e); err != nil {
			break
		}
		if e.Type == wsui.EvSessionState {
			var ss wsui.SessionState
			_ = json.Unmarshal(e.Data, &ss)
			if ss.State == "publishing" && ss.CameraID == cams[0].ID {
				gotState = true
			}
		}
	}
	if !gotState {
		t.Fatal("session_state(publishing) kelmadi")
	}
}
