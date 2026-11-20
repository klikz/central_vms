// testagent — soxta agent (server REJA §6, TASKS S0/S1/S2).
// S0: enroll + handshake + cams_sync. S1: cam_status pulse.
// S2: -publish rejimi — media_start kelganda FFmpeg test pattern'ini
// MediaMTX'ga RTSP bilan yuboradi (haqiqiy oqim; e2e qabul uchun).
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"central-vms/server/internal/proto"
)

func main() {
	var (
		server   = flag.String("server", "ws://127.0.0.1:8081", "vmsd asosi (ws:// yoki wss://)")
		join     = flag.Bool("join", false, "ulanish so'rovi yuborish (§3.1) va admin tasdig'ini kutish")
		name     = flag.String("name", "", "serverda ko'rinadigan nom (join so'rovida)")
		agentID  = flag.String("agent-id", "", "mavjud agent_id (qayta ulanish)")
		secret   = flag.String("secret", "", "mavjud agent_secret")
		nCams    = flag.Int("cams", 100, "soxta kameralar soni")
		hostname = flag.String("hostname", "testagent", "hostname")
		once     = flag.Bool("once", false, "cams_ackdan keyin chiqish (S1 e2e testlari uchun)")
		publish  = flag.Bool("publish", false, "media_start'da FFmpeg test pattern'ini chiqaradi (S2)")
		ffmpeg   = flag.String("ffmpeg", "ffmpeg", "ffmpeg yo'li (-publish uchun)")
		size     = flag.String("size", "1280x720", "test pattern o'lchami")
		fps      = flag.Int("fps", 15, "test pattern fps")
	)
	flag.Parse()

	err := run(*server, *join, *name, *agentID, *secret, *nCams, *hostname, *once, *publish, *ffmpeg, *size, *fps)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testagent: %v\n", err)
		os.Exit(1)
	}
}

// publisher — bitta kamera uchun FFmpeg test pattern jarayoni.
type publisher struct {
	mu     sync.Mutex
	cmds   map[string]*pubProc // session_id → jarayon
	ffmpeg string
	size   string
	fps    int
}

// pubProc — bitta FFmpeg jarayoni; done kanali BIR marta Wait() bilan yopiladi
// (ikki marta Wait() Go'da xato beradi — jarayon o'lmagan holda "o'ldi" deb
// o'ylanardi, amalda topilib tuzatildi).
type pubProc struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (p *publisher) start(ms proto.MediaStart) (int64, error) {
	url := ms.Publish.URLs.RTSP
	if url == "" {
		return 0, fmt.Errorf("rtsp publish URL yo'q")
	}
	if ms.Publish.Token != "" {
		url += "?token=" + ms.Publish.Token
	}
	t0 := time.Now()
	// testsrc → libx264 → RTSP (TCP). Haqiqiy agentda manba kameradan oladi.
	cmd := exec.Command(p.ffmpeg, "-hide_banner", "-loglevel", "error",
		"-re", "-f", "lavfi", "-i", fmt.Sprintf("testsrc=size=%s:rate=%d", p.size, p.fps),
		"-an", "-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency",
		// Qisqa GOP (2 s): yangi o'quvchi birinchi keyframe'ni tez
		// kutib oladi — aks holda MediaMTX'da o'qish ~15 s kechikadi
		// (x264 default gop=250 kadr; 2026-11-20 o'lchandi).
		"-pix_fmt", "yuv420p", "-g", "30", "-keyint_min", "30", "-sc_threshold", "0",
		"-f", "rtsp", "-rtsp_transport", "tcp", url)
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("ffmpeg start: %w", err)
	}
	pr := &pubProc{cmd: cmd, done: make(chan struct{})}
	p.mu.Lock()
	p.cmds[ms.SessionID] = pr
	p.mu.Unlock()
	go func() { _ = cmd.Wait(); close(pr.done) }()
	return time.Since(t0).Milliseconds(), nil
}

func (p *publisher) stop(sessionID string) {
	p.mu.Lock()
	pr, ok := p.cmds[sessionID]
	if ok {
		delete(p.cmds, sessionID)
	}
	p.mu.Unlock()
	if !ok || pr.cmd.Process == nil {
		return
	}
	_ = pr.cmd.Process.Signal(syscall.SIGINT)
	select {
	case <-pr.done: // Wait() goroutine'si yopadi
	case <-time.After(3 * time.Second):
		_ = pr.cmd.Process.Kill()
		<-pr.done
	}
}

func run(server string, join bool, name, agentID, secret string, nCams int, hostname string, once, publish bool, ffmpegPath, size string, fps int) error {
	base := server

	if join {
		id, sec, err := joinAndWait(base, hostname, name)
		if err != nil {
			return err
		}
		agentID, secret = id, sec
		fmt.Printf("join OK: agent_id=%s\n", agentID)
	}
	if agentID == "" || secret == "" {
		return fmt.Errorf("agent_id/secret berilmadi yoki -join bilan so'rov yuborilmadi")
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
		ProtocolVersion: 1, AgentVersion: "testagent-1.0", OS: "linux",
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
		main, sub := "h265", "h264"
		if publish && i == 0 {
			// Test pattern H.264 chiqariladi — kodek ma'lumoti shunga mos.
			main, sub = "h264", "h264"
		}
		cams = append(cams, proto.CameraEntry{
			Op: "upsert", ID: uuid.NewString(),
			Name:       fmt.Sprintf("test-cam-%03d", i+1),
			RTSPMasked: fmt.Sprintf("rtsp://***:***@10.0.5.%d:554/cam/realmonitor?channel=1&subtype=1", 20+i%100),
			Codecs:     proto.CameraProfilePair{Main: main, Sub: sub},
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
	// default false — ack kelmasa ogohlantirish bilan davom etamiz.
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
		fmt.Println("cams_ack OK")
		if once {
			return nil
		}
	} else {
		fmt.Println("cams_ack kelmadi (mock rejimi) — pulse rejimiga o'tamiz")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pub := &publisher{cmds: map[string]*pubProc{}, ffmpeg: ffmpegPath, size: size, fps: fps}
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()

	// O'qish alohida goroutine'da: media_start/media_stop darhol javob berish
	// kerak (ready taymauti 5 s).
	msgs := make(chan proto.Envelope, 16)
	// Pong — server ping'iga javob. Read deadline faqat DATA frame'lar bilan
	// uzaradi (kontrol frame'lar bilan uzarmaydi): shuning uchun pong'da
	// qo'lda uzatamiz, aks holda 90 sdan keyin jim uziladi (2026-11-20 dev
	// stack'da kuzatildi — haqiqiy client ham shuni qilishi shart).
	// Server ping yuboradi, agent pong qaytaradi — agent o'z pong'ini
	// OLMAAYDI, shuning uchun faollik belgisi sifatida KELGAN PING'ni
	// hisoblaymiz (PingHandler default: pong yuboradi + deadline uzaytiradi).
	// Aks holda 90 sda read timeout bo'lib jim uziladi (2026-11-20 kuzatildi).
	conn.SetPingHandler(func(appData string) error {
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(5*time.Second))
	})
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	})
	go func() {
		for {
			var e proto.Envelope
			_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			if err := conn.ReadJSON(&e); err != nil {
				close(msgs)
				return
			}
			msgs <- e
		}
	}()

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
		case e, ok := <-msgs:
			if !ok {
				fmt.Println("ulanish uzildi")
				return nil
			}
			switch e.Type {
			case proto.MsgMediaStart:
				var ms proto.MediaStart
				if err := json.Unmarshal(e.Data, &ms); err != nil {
					fmt.Println("media_start parse:", err)
					continue
				}
				if err := validateMediaStart(ms); err != nil {
					// Kontrakt tekshiruvi (1.0-draft.12): bundan oldin
					// testagent `ingest` kabi maydonlarni umuman
					// o'qimasdi — server noto'g'ri qiymat yuborgani
					// e2e'da sezilmay qolgan edi.
					fmt.Printf("KONTRAKT XATOSI media_start: %v\n", err)
					_ = send(proto.MsgMediaStopped, proto.MediaStopped{
						SessionID: ms.SessionID, CameraID: ms.CameraID, Reason: "publish_failed"})
					continue
				}
				if !publish {
					fmt.Printf("media_start (publish rejimi yopiq): %s\n", ms.SessionID)
					continue
				}
				readyMs, err := pub.start(ms)
				if err == nil {
					// Test agent: RTSP publish o'rnatilishini kutamiz
					// (haqiqiy agentda kamera javobi bilan aniq bo'ladi).
					time.Sleep(1500 * time.Millisecond)
				}
				if err != nil {
					fmt.Println("publish boshlanmadi:", err)
					_ = send(proto.MsgMediaStopped, proto.MediaStopped{
						SessionID: ms.SessionID, CameraID: ms.CameraID, Reason: "ffmpeg_failed"})
					continue
				}
				path := ""
				if u := ms.Publish.URLs.RTSP; u != "" {
					if i := bytes.Index([]byte(u), []byte("554/")); i >= 0 {
						path = string(u[i+4:])
					}
				}
				_ = send(proto.MsgMediaReady, proto.MediaReady{
					SessionID: ms.SessionID, CameraID: ms.CameraID, Profile: ms.Profile,
					Kind: ms.Kind, PublishID: "pub-" + ms.SessionID, Path: path,
					Ingest: ms.Ingest, Transport: "rtsp/tcp", ReadyMs: readyMs,
				})
				fmt.Printf("publish boshlandi: session=%s path=%s\n", ms.SessionID, path)
			case proto.MsgMediaStop:
				var ms proto.MediaStop
				_ = json.Unmarshal(e.Data, &ms)
				pub.stop(ms.SessionID)
				_ = send(proto.MsgMediaStopped, proto.MediaStopped{
					SessionID: ms.SessionID, CameraID: ms.CameraID, Reason: ms.Reason})
				fmt.Printf("publish to'xtatildi: session=%s reason=%s\n", ms.SessionID, ms.Reason)
			case proto.MsgSecretRotate:
				var sr proto.SecretRotate
				_ = json.Unmarshal(e.Data, &sr)
				fmt.Printf("secret_rotate olindi (grace %d s)\n", sr.GraceS)
			case proto.MsgConfigPush:
				fmt.Println("config_push olindi")
			default:
				// welcome/ping/pong va boshqalar — e'tiborsiz
			}
		}
	}
}

// joinAndWait — §3.1/§3.2: so'rov yuboriladi va admin tasdig'i
// polling orqali kutiladi. Sirni agent o'zi yaratadi (serverda hash saqlanadi).
func joinAndWait(base, hostname, name string) (agentID, secret string, err error) {
	key := randHex(32)
	secret = randHex(32)
	for {
		id, status, err := joinOnce(base, hostname, name, key, secret)
		if err != nil {
			// Tarmoq xatosi (server restart, connection refused) — davom
			// etamiz: tasdiqni kutish normal holat (2026-11-20: server
			// qayta boshlansa testagent shu xatoda chiqib ketardi).
			var he *joinHTTPError
			if !errors.As(err, &he) {
				fmt.Println("join: vaqtinchalik xato (qayta uriladi):", err)
				time.Sleep(3 * time.Second)
				continue
			}
			if he.code == http.StatusForbidden {
				return "", "", err // rad etilgan
			}
			return "", "", err
		}
		switch status {
		case "approved":
			return id, secret, nil
		case "rejected":
			return "", "", fmt.Errorf("join: so'rov rad etildi (admin izohini so'rang)")
		default:
			fmt.Println("join: so'rov yuborildi — server UI'da tasdiqlang…")
			time.Sleep(3 * time.Second)
		}
	}
}

func joinOnce(base, hostname, name, key, secret string) (agentID, status string, err error) {
	httpBase := base
	if len(httpBase) > 3 && httpBase[:3] == "ws:" {
		httpBase = "http:" + httpBase[3:]
	}
	body, _ := json.Marshal(map[string]any{
		"join_key": key, "agent_secret": secret, "hostname": hostname, "name": name,
		"os": "linux", "lan_ip": "127.0.0.1", "agent_version": "testagent-0",
	})
	resp, err := http.Post(httpBase+"/api/agent/join", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("join: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return "", "", &joinHTTPError{code: resp.StatusCode, body: string(b)}
	}
	var out struct {
		Status  string `json:"status"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", "", fmt.Errorf("join javob: %w", err)
	}
	return out.AgentID, out.Status, nil
}

// joinHTTPError — server javob berdi (status kodi bilan); boshqa xatolar
// tarmoq darajasida (server o'chik / restart) va ular qayta urinish kerak.
type joinHTTPError struct {
	code int
	body string
}

func (e *joinHTTPError) Error() string {
	return fmt.Sprintf("join %d: %s", e.code, e.body)
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// validateMediaStart — §5.2 maydonlarining kontraktga mosligi.
func validateMediaStart(ms proto.MediaStart) error {
	if ms.SessionID == "" || ms.CameraID == "" {
		return fmt.Errorf("session_id/camera_id bo'sh")
	}
	switch ms.Ingest {
	case "whip", "rtsp_tcp":
	default:
		return fmt.Errorf("noma'lum ingest %q (kontrakt: whip|rtsp_tcp)", ms.Ingest)
	}
	if ms.Publish.Token == "" {
		return fmt.Errorf("publish.token bo'sh")
	}
	if ms.Ingest == "whip" && ms.Publish.URLs.WHIP == "" {
		return fmt.Errorf("whip uchun urls.whip bo'sh")
	}
	if ms.Ingest == "rtsp_tcp" && ms.Publish.URLs.RTSP == "" {
		return fmt.Errorf("rtsp_tcp uchun urls.rtsp bo'sh")
	}
	if ms.Profile != "main" && ms.Profile != "sub" {
		return fmt.Errorf("noma'lum profil %q", ms.Profile)
	}
	return nil
}
