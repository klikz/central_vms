// testagent — soxta agent (server REJA §6, TASKS S0/S1/S2).
// S0 skeleti: enroll + auth/welcome handshake + hello + cams_sync (N kamera)
// + cam_status. S2'da haqiqiy WHIP/RTSP publish qo'shiladi (FFmpeg test pattern).
// Client kutilmaydi: `make devserver` (client repodagi mock) ustida ham ishlaydi.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"central-vms/server/internal/proto"
)

func main() {
	var (
		server   = flag.String("server", "ws://127.0.0.1:8081", "vmsd asosi (ws:// yoki wss://)")
		code     = flag.String("code", "", "enrollment kodi (bo'sh bo'lsa -agent-id/-secret ishlatiladi)")
		agentID  = flag.String("agent-id", "", "mavjud agent_id (qayta ulanish)")
		secret   = flag.String("secret", "", "mavjud agent_secret")
		nCams    = flag.Int("cams", 100, "soxta kameralar soni")
		hostname = flag.String("hostname", "testagent", "hostname")
	)
	flag.Parse()

	if err := run(*server, *code, *agentID, *secret, *nCams, *hostname); err != nil {
		fmt.Fprintf(os.Stderr, "testagent: %v\n", err)
		os.Exit(1)
	}
}

func run(server, code, agentID, secret string, nCams int, hostname string) error {
	base := server

	if code != "" {
		id, sec, err := enroll(base, code, hostname)
		if err != nil {
			return err
		}
		agentID, secret = id, sec
		if len(secret) > 6 {
			fmt.Printf("enroll OK: agent_id=%s secret=%s…\n", agentID, secret[:6])
		}
	}
	if agentID == "" || secret == "" {
		return fmt.Errorf("agent_id/secret berilmadi yoki -code orqali enroll qilinmadi")
	}

	wsURL := base + "/api/agent/ws?agent_id=" + agentID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("WS ulanish: %w", err)
	}
	defer conn.Close()

	seq := int64(0)
	send := func(typ string, data any) error {
		raw, err := json.Marshal(data)
		if err != nil {
			return err
		}
		seq++
		return conn.WriteJSON(proto.Envelope{Type: typ, Seq: seq, TS: time.Now().UTC(), Data: raw})
	}

	// protocol.md §18: handshake tartibi (amalda) auth → hello → welcome.
	if err := send(proto.MsgAuth, proto.Auth{AgentID: agentID, Secret: secret}); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if err := send(proto.MsgHello, proto.Hello{
		ProtocolVersion: 1, AgentVersion: "testagent-0", OS: "linux",
		Hostname: hostname, LanIP: "127.0.0.1", BootID: uuid.NewString(),
		StartedAt: time.Now().UTC(), CamsEpoch: time.Now().Unix(),
		Caps: proto.Caps{GPU: "none", CPUCores: 4, RAMMB: 8192, FFmpegVersion: "test"},
	}); err != nil {
		return fmt.Errorf("hello: %w", err)
	}

	var env proto.Envelope
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err := conn.ReadJSON(&env); err != nil {
		return fmt.Errorf("welcome o'qish: %w", err)
	}
	if env.Type != proto.MsgWelcome {
		return fmt.Errorf("kutilmagan birinchi xabar: %q", env.Type)
	}
	fmt.Println("welcome OK")

	cams := make([]proto.CameraEntry, 0, nCams)
	for i := range nCams {
		cams = append(cams, proto.CameraEntry{
			Op: "upsert", ID: uuid.NewString(),
			Name:       fmt.Sprintf("test-cam-%03d", i+1),
			RTSPMasked: fmt.Sprintf("rtsp://***:***@10.0.5.%d:554/cam/realmonitor?channel=1&subtype=1", 20+i%100),
			Codecs:     proto.CameraProfilePair{Main: "h265", Sub: "h264"},
			Status:     "online", Position: i, Enabled: true,
		})
	}
	if err := send(proto.MsgCamsSync, proto.CamsSync{
		Epoch: time.Now().Unix(), Full: true, SeqNo: 1, ChunkCount: 1, Done: true, Cameras: cams,
	}); err != nil {
		return fmt.Errorf("cams_sync: %w", err)
	}
	fmt.Printf("cams_sync yuborildi: %d kamera\n", nCams)

	// cams_ack kutiladi (10 s). Eslatma: devserver mock'ida AutoCamsAck
	// default false — ack kelmasa ogohlantirish bilan davom etamiz
	// (haqiqiy vmsd S1'da albatta javob beradi).
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	gotAck := false
	for {
		if err := conn.ReadJSON(&env); err != nil {
			break
		}
		if env.Type == proto.MsgCamsAck {
			gotAck = true
			break
		}
	}
	if gotAck {
		fmt.Println("cams_ack OK — S0 skeleti tayyor (Ctrl+C bilan chiqish)")
	} else {
		fmt.Println("cams_ack kelmadi (mock rejimi) — pulse rejimiga o'tamiz")
	}

	// cam_status pulse (S1 testlari uchun uzluksiz rejim).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			for _, c := range cams[:min(5, len(cams))] {
				_ = send(proto.MsgCamStatus, proto.CamStatus{
					CameraID: c.ID, Status: "online", CheckedAt: time.Now().UTC(), RTTMs: 12,
				})
			}
		}
	}
}

func enroll(base, code, hostname string) (string, string, error) {
	httpBase := base
	if len(httpBase) > 3 && httpBase[:3] == "ws:" {
		httpBase = "http:" + httpBase[3:]
	}
	body, _ := json.Marshal(map[string]any{
		"code": code, "hostname": hostname, "os": "linux",
		"lan_ip": "127.0.0.1", "agent_version": "testagent-0",
	})
	resp, err := http.Post(httpBase+"/api/agent/enroll", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("enroll: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("enroll %d: %s", resp.StatusCode, b)
	}
	var out struct {
		AgentID     string `json:"agent_id"`
		AgentSecret string `json:"agent_secret"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", "", fmt.Errorf("enroll javob: %w", err)
	}
	return out.AgentID, out.AgentSecret, nil
}
