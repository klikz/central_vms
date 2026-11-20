// C3.20 qabul mezoni (mock server bilan): media_start → kamera pull →
// RTSP/TCP publish → media_ready; media_stop → media_stopped{user_close}.
// MediaMTX o'rniga fake RTSP ingest (ANNOUNCE/SETUP/RECORD o'qiydi).
//
// Ishga tushirish: make itest SCENARIO=c3
package integration

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"central-vms/client/internal/agent"
	"central-vms/client/internal/cams"
	"central-vms/client/internal/clock"
	"central-vms/client/internal/devserver"
	"central-vms/client/internal/keystore"
	"central-vms/client/internal/media"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/publish"
	"central-vms/client/internal/rtspclient"
	"central-vms/client/internal/statestore"
)

// --- fake kamera: DESCRIBE/SETUP/PLAY + uzluksiz interleaved AU ---

func startFakeCamera(t *testing.T) (url string, auCount *atomic.Int64) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	count := &atomic.Int64{}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveCameraConn(c, count)
		}
	}()
	return "rtsp://" + ln.Addr().String() + "/media", count
}

func serveCameraConn(c net.Conn, count *atomic.Int64) {
	defer c.Close()
	r := bufio.NewReader(c)
	send := func(code int, h map[string]string) bool {
		var b strings.Builder
		fmt.Fprintf(&b, "RTSP/1.0 %d OK\r\n", code)
		for k, v := range h {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
		b.WriteString("\r\n")
		_, err := c.Write([]byte(b.String()))
		return err == nil
	}
	for {
		req, err := rtspclient.ParseRequest(r)
		if err != nil {
			return
		}
		h := map[string]string{"CSeq": fmt.Sprint(req.CSeq)}
		switch strings.ToUpper(req.Method) {
		case "DESCRIBE":
			body := "v=0\r\nm=video 0 RTP/AVP/UDP 96\r\na=rtpmap:96 H264/90000\r\n" +
				"a=fmtp:96 packetization-mode=1\r\na=control:trackID=1\r\n"
			h["Content-Type"] = "application/sdp"
			h["Content-Length"] = fmt.Sprint(len(body))
			if !send(200, h) {
				return
			}
			c.Write([]byte(body))
		case "SETUP":
			h["Session"] = "7"
			h["Transport"] = req.Headers["transport"]
			if !send(200, h) {
				return
			}
		case "PLAY":
			h["Session"] = "7"
			if !send(200, h) {
				return
			}
			streamCameraAUs(c, count)
			return
		default:
			if !send(200, h) {
				return
			}
		}
	}
}

// streamCameraAUs — 100 ms'da bitta H.264 AU (ticker, uyqu yo'q).
func streamCameraAUs(c net.Conn, count *atomic.Int64) {
	pk := rtspclient.NewH264Packetizer(96, 5)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	ts := uint32(0)
	for range tick.C {
		nal := []byte{0x61, byte(ts), byte(ts >> 8)}
		for _, raw := range pk.Pack(nal, ts, true, 1200) {
			hdr := []byte{'$', 0, byte(len(raw) >> 8), byte(len(raw))}
			if _, err := c.Write(append(hdr, raw...)); err != nil {
				return
			}
		}
		count.Add(1)
		ts += 3000
	}
}

// --- fake ingest: ANNOUNCE/SETUP/RECORD + interleaved o'qish ---

type fakeIngest struct {
	ln        net.Listener
	announced atomic.Bool
	frames    atomic.Int64
	auBytes   atomic.Int64
	torn      atomic.Bool
}

func startFakeIngest(t *testing.T) *fakeIngest {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fi := &fakeIngest{ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go fi.serve(c)
		}
	}()
	return fi
}

func (fi *fakeIngest) url() string { return "rtsp://" + fi.ln.Addr().String() + "/live" }

func (fi *fakeIngest) serve(c net.Conn) {
	defer func() { fi.torn.Store(true); c.Close() }()
	r := bufio.NewReader(c)
	send := func(code int, h map[string]string) bool {
		var b strings.Builder
		fmt.Fprintf(&b, "RTSP/1.0 %d OK\r\n", code)
		for k, v := range h {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
		b.WriteString("\r\n")
		_, err := c.Write([]byte(b.String()))
		return err == nil
	}
	for {
		req, err := rtspclient.ParseRequest(r)
		if err != nil {
			return
		}
		h := map[string]string{"CSeq": fmt.Sprint(req.CSeq)}
		switch strings.ToUpper(req.Method) {
		case "ANNOUNCE":
			fi.announced.Store(true)
			if !send(200, h) {
				return
			}
		case "SETUP":
			h["Session"] = "9"
			h["Transport"] = req.Headers["transport"]
			if !send(200, h) {
				return
			}
		case "RECORD":
			h["Session"] = "9"
			if !send(200, h) {
				return
			}
			fi.readFrames(r)
			return
		default:
			if !send(200, h) {
				return
			}
		}
	}
}

func (fi *fakeIngest) readFrames(r *bufio.Reader) {
	for {
		hdr := make([]byte, 4)
		if _, err := io.ReadFull(r, hdr); err != nil {
			return
		}
		if hdr[0] != '$' {
			return
		}
		n := int(hdr[2])<<8 | int(hdr[3])
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return
		}
		fi.frames.Add(1)
		fi.auBytes.Add(int64(n))
	}
}

// --- asosiy ssenariy ---

func TestC3_MediaStartToReadyAndStop(t *testing.T) {
	camURL, camAUs := startFakeCamera(t)
	ing := startFakeIngest(t)

	var mu sync.Mutex
	var gotReady proto.MediaReady
	var gotStopped proto.MediaStopped
	h := newHarness(t, devserver.Options{OnMessage: func(_ string, env proto.Envelope) {
		switch env.Type {
		case proto.MsgMediaReady:
			if m, err := proto.Decode[proto.MediaReady](env.Data); err == nil {
				mu.Lock()
				gotReady = m
				mu.Unlock()
			}
		case proto.MsgMediaStopped:
			if m, err := proto.Decode[proto.MediaStopped](env.Data); err == nil {
				mu.Lock()
				gotStopped = m
				mu.Unlock()
			}
		}
	}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	res, err := agent.Enroll(ctx, h.srv.URL, agent.EnrollInfo{
		Code: "KOD-1", Hostname: "joy-c3", OS: "Ubuntu 24.04", LanIP: "10.0.0.9", AgentVersion: "0.4.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(h.tempDir, "agent.key")
	if _, err := keystore.NewKeyFile(keyPath); err != nil {
		t.Fatal(err)
	}
	ks, err := keystore.Open(keyPath, filepath.Join(h.tempDir, "secrets.bin"))
	if err != nil {
		t.Fatal(err)
	}
	ks.Put(keystore.RefAgentSecret, res.AgentSecret)
	if err := ks.Save(); err != nil {
		t.Fatal(err)
	}
	st, err := statestore.Load(filepath.Join(h.tempDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.AgentID, st.ServerURL = res.AgentID, h.wsURL
	cam := cams.Camera{ID: "cam-7", Host: "10.0.0.7", Port: 554, MainPath: "/media",
		CodecMain: "h264", Enabled: true}
	// Kamera manzilini fake kameraga qo'yamiz (Host:Port = fake).
	cu, err := url.Parse(camURL)
	if err != nil {
		t.Fatal(err)
	}
	cam.Host = cu.Hostname()
	pn, _ := strconv.Atoi(cu.Port())
	cam.Port = pn
	cam.MainPath = cu.Path
	st.Cameras = []cams.Camera{cam}

	// WS sessiyasi avval yaratiladi — media hodisalari shu orqali yuboriladi.
	cfg := agent.SessionConfig{
		ServerURL: h.wsURL, AgentID: res.AgentID, Secret: res.AgentSecret,
		AgentVersion: "0.4.0", Hostname: "joy-c3", BootID: "b-c3",
		HbInterval: 200 * time.Millisecond, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	sess := agent.NewSession(cfg)

	// media.Manager + registry (haqiqiy transportlar bilan).
	mgr := media.New(media.Config{
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		LookupCamera: func(id string) (cams.Camera, bool) { return cam, id == cam.ID },
		LookupSecret: func(ref string) (string, bool) { return ks.Get(ref) },
		Send:         func(typ string, data any) error { return sess.Send(typ, data) },
	})
	reg := publish.New(clock.Real{}, nil, mgr.OnReady, mgr.OnStopped)
	mgr.SetRegistry(reg)
	defer reg.Shutdown()

	// Dispatch: server media buyruqlari mgr'ga (§5.2).
	hnd := &agent.Handlers{KS: ks, St: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sess: sess, MediaStart: mgr.OnStart, MediaStop: mgr.OnStop}
	sess.SetOnMessage(hnd.Handle)

	go func() { _ = sess.Run(ctx) }()
	if !h.dev.WaitConnected(res.AgentID, 5*time.Second) {
		t.Fatal("agent ulanmadi")
	}

	// Server media_start yuboradi (§5.2: rtsp_tcp ingest).
	if err := h.dev.SendMediaStart(res.AgentID, proto.MediaStart{
		SessionID: "se_c3", CameraID: "cam-7", Profile: "main", Kind: "play",
		TTLS: 60, Ingest: "rtsp_tcp",
		Publish: proto.Publish{Token: "tk-c3", URLs: proto.PublishURLs{RTSP: ing.url()}, QueryName: "vmsaccesstoken"},
	}); err != nil {
		t.Fatal(err)
	}

	// media_ready serverga yetib kelishi kerak (≤10 s; real tarmoq).
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := gotReady.PublishID
		mu.Unlock()
		if got != "" {
			break
		}
		time.Sleep(20 * time.Millisecond) // tashqi tarmoq hodisasi kuzatuvi (qabul mezoni)
	}
	mu.Lock()
	if gotReady.PublishID == "" {
		mu.Unlock()
		t.Fatal("media_ready serverga yetmadi")
	}
	if gotReady.Ingest != "rtsp_tcp" || gotReady.CameraID != "cam-7" || gotReady.Transport != "tcp" {
		t.Errorf("media_ready: %+v", gotReady)
	}
	t.Logf("media_ready: publish_id=%s ready_ms=%d transport=%s", gotReady.PublishID, gotReady.ReadyMs, gotReady.Transport)
	mu.Unlock()

	if !ing.announced.Load() {
		t.Fatal("ANNOUNCE ingest'ga yetmadi")
	}
	if ing.frames.Load() == 0 {
		t.Fatal("ingest RTP freym ko'rmadi")
	}
	if camAUs.Load() == 0 {
		t.Fatal("kamera o'qilmadi")
	}
	t.Logf("OK: ANNOUNCE + %d RTP freym ingest'da, kamera AULARI=%d", ing.frames.Load(), camAUs.Load())

	// media_stop → media_stopped{user_close} + transport yopiladi.
	if err := h.dev.SendMediaStop(res.AgentID, proto.MediaStop{
		SessionID: "se_c3", CameraID: "cam-7", Reason: "user_close"}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !ing.torn.Load() {
		time.Sleep(20 * time.Millisecond)
	}
	if !ing.torn.Load() {
		t.Error("TEARDOWN ingest'ga yetmadi")
	}
	if n := len(reg.List()); n != 0 {
		t.Errorf("registry = %d, 0 kutilardi", n)
	}
	mu.Lock()
	if gotStopped.Reason != "user_close" || gotStopped.CameraID != "cam-7" {
		t.Errorf("media_stopped: %+v", gotStopped)
	}
	mu.Unlock()
}
