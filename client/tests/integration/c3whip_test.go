// C3.20 o'lchovlari (mock): H.264 → WHIP ochilishi ≤ 1.5 s (§14 P3).
// MediaMTX o'rniga pion asosidagi fake WHIP server (to'liq SDP + ICE loopback).
//
// Ishga tushirish: make itest SCENARIO=c3_h264_whip
package integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"central-vms/client/internal/agent"
	"central-vms/client/internal/cams"
	"central-vms/client/internal/clock"
	"central-vms/client/internal/devserver"
	"central-vms/client/internal/keystore"
	"central-vms/client/internal/media"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/publish"
	"central-vms/client/internal/statestore"
)

// fakeWhipServer — Bearer tekshiradi, offer→answer, ICE loopback, RTP sanaydi.
type fakeWhipServer struct {
	ts        *httptest.Server
	packets   atomic.Int64
	connected atomic.Bool
	authOK    atomic.Bool
}

func startFakeWhip(t *testing.T, token string) *fakeWhipServer {
	t.Helper()
	fs := &fakeWhipServer{}
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		t.Fatal(err)
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m))
	fs.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fs.authOK.Store(true)
		body, _ := io.ReadAll(r.Body)
		pc, err := api.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		pc.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			fs.connected.Store(true)
			for {
				if _, _, err := tr.ReadRTP(); err != nil {
					return
				}
				fs.packets.Add(1)
			}
		})
		pc.OnICEConnectionStateChange(func(st webrtc.ICEConnectionState) {
			if st == webrtc.ICEConnectionStateConnected {
				fs.connected.Store(true)
			}
		})
		if err := pc.SetRemoteDescription(webrtc.SessionDescription{
			Type: webrtc.SDPTypeOffer, SDP: string(body)}); err != nil {
			t.Logf("whip server offer: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		gathered := make(chan struct{})
		pc.OnICEGatheringStateChange(func(st webrtc.ICEGatheringState) {
			if st == webrtc.ICEGatheringStateComplete {
				close(gathered)
			}
		})
		ans, err := pc.CreateAnswer(nil)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := pc.SetLocalDescription(ans); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		select {
		case <-gathered:
		case <-time.After(3 * time.Second):
		}
		w.Header().Set("Content-Type", "application/sdp")
		w.Header().Set("Link-Signal", "<"+fs.ts.URL+"/ice>; rel=\"ice-server\"")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, pc.LocalDescription().SDP)
		t.Cleanup(func() { _ = pc.Close() })
	}))
	t.Cleanup(fs.ts.Close)
	return fs
}

type logTr struct{ inner media.Transport }

func (l logTr) WriteAU(nals [][]byte, ts uint32) error {
	fmt.Printf("WriteAU ts=%d nals=%d\n", ts, len(nals))
	return l.inner.WriteAU(nals, ts)
}
func (l logTr) BytesSent() int64  { return l.inner.BytesSent() }
func (l logTr) Transport() string { return l.inner.Transport() }
func (l logTr) Close() error      { return l.inner.Close() }

func TestC3_H264WhipOpenUnder1500ms(t *testing.T) {
	camURL, camAUs := startFakeCamera(t)
	whip := startFakeWhip(t, "tk-whip")

	var mu sync.Mutex
	var gotReady proto.MediaReady
	h := newHarness(t, devserver.Options{OnMessage: func(_ string, env proto.Envelope) {
		if env.Type == proto.MsgMediaReady {
			if m, err := proto.Decode[proto.MediaReady](env.Data); err == nil {
				mu.Lock()
				gotReady = m
				mu.Unlock()
			}
		}
	}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agentID, secret := joinAgent(t, h.srv.URL, "KEY-joy-whip", "joy-whip")
	keyPath := filepath.Join(h.tempDir, "agent.key")
	if _, err := keystore.NewKeyFile(keyPath); err != nil {
		t.Fatal(err)
	}
	ks, err := keystore.Open(keyPath, filepath.Join(h.tempDir, "secrets.bin"))
	if err != nil {
		t.Fatal(err)
	}
	ks.Put(keystore.RefAgentSecret, secret)
	if err := ks.Save(); err != nil {
		t.Fatal(err)
	}
	st, err := statestore.Load(filepath.Join(h.tempDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.AgentID, st.ServerURL = agentID, h.wsURL

	cu, err := parseRTSP(camURL)
	if err != nil {
		t.Fatal(err)
	}
	cam := cams.Camera{ID: "cam-w", Host: cu.Hostname(), Port: cu.Port(), MainPath: cu.Path(),
		CodecMain: "h264", Enabled: true}
	st.Cameras = []cams.Camera{cam}

	cfg := agent.SessionConfig{
		ServerURL: h.wsURL, AgentID: agentID, Secret: secret,
		AgentVersion: "0.4.0", Hostname: "joy-whip", BootID: "b-whip",
		HbInterval: 200 * time.Millisecond, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	sess := agent.NewSession(cfg)

	mgr := media.New(media.Config{
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		LookupCamera: func(id string) (cams.Camera, bool) { return cam, id == cam.ID },
		LookupSecret: func(ref string) (string, bool) { return ks.Get(ref) },
		Send:         func(typ string, data any) error { return sess.Send(typ, data) },
	})
	reg := publish.New(clock.Real{}, nil, mgr.OnReady, mgr.OnStopped)
	mgr.SetRegistry(reg)
	defer reg.Shutdown()

	hnd := &agent.Handlers{KS: ks, St: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sess: sess, MediaStart: mgr.OnStart, MediaStop: mgr.OnStop}
	sess.SetOnMessage(hnd.Handle)
	go func() { _ = sess.Run(ctx) }()
	if !h.dev.WaitConnected(agentID, 5*time.Second) {
		t.Fatal("agent ulanmadi")
	}

	start := time.Now()
	if err := h.dev.SendMediaStart(agentID, proto.MediaStart{
		SessionID: "se_whip", CameraID: "cam-w", Profile: "main", Kind: "play",
		TTLS: 60, Ingest: "whip",
		Publish: proto.Publish{Token: "tk-whip", URLs: proto.PublishURLs{WHIP: whip.ts.URL + "/whip"}},
	}); err != nil {
		t.Fatal(err)
	}

	deadline := start.Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := gotReady.PublishID
		mu.Unlock()
		if got != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	elapsed := time.Since(start)

	mu.Lock()
	defer mu.Unlock()
	if gotReady.PublishID == "" {
		t.Fatal("media_ready (whip) yetmadi")
	}
	if !whip.authOK.Load() {
		t.Error("WHIP Bearer token tekshiruvidan o'tmadi")
	}
	if gotReady.Transport != "udp" {
		t.Errorf("transport = %q, udp kutilardi (§8.1)", gotReady.Transport)
	}
	// §14 P3 me'yori: H.264 ≤1.5 s ochilish (mock ICE loopback bilan).
	if gotReady.ReadyMs > 1500 {
		t.Errorf("ready_ms = %d, ≤1500 kutilardi", gotReady.ReadyMs)
	}
	t.Logf("WHIP ochilishi: ready_ms=%d (wall %v), kamera AULARI=%d",
		gotReady.ReadyMs, elapsed.Round(10*time.Millisecond), camAUs.Load())
}

func parseRTSP(u string) (urlParts, error) {
	var p urlParts
	i := strings.Index(u, "://")
	if i < 0 {
		return p, fmt.Errorf("rtsp:// kutildi: %q", u)
	}
	rest := u[i+3:]
	j := strings.Index(rest, "/")
	hostport, path := rest, "/"
	if j >= 0 {
		hostport, path = rest[:j], rest[j:]
	}
	k := strings.LastIndex(hostport, ":")
	if k < 0 {
		return p, fmt.Errorf("port kutildi: %q", u)
	}
	port := 0
	for _, ch := range hostport[k+1:] {
		port = port*10 + int(ch-'0')
	}
	return urlParts{host: hostport[:k], port: port, path: path}, nil
}

type urlParts struct {
	host string
	port int
	path string
}

func (u urlParts) Hostname() string { return u.host }
func (u urlParts) Port() int        { return u.port }
func (u urlParts) Path() string     { return u.path }
