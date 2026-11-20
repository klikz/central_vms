// C3.20 qismi: JONLI MediaMTX bilan tekshiruv (S2 serveri shart emas).
// Sintetik H.264 kamera → media.Manager → RTSP/TCP ANNOUNCE publish →
// MediaMTX path'da publisher ko'rinadi → MediaMTX'dan DESCRIBE qaytadi.
//
// Ishga tushirish: MTX=1 go test ./tests/integration -run Mtx -v
// (MediaMTX 127.0.0.1:8554 + API 9997 bo'lishi kerak; bo'lmasa skip.)
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/clock"
	"central-vms/client/internal/media"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/publish"
	"central-vms/client/internal/rtspclient"
)

func mtxAvailable(t *testing.T) bool {
	if os.Getenv("MTX") == "" {
		t.Skip("MTX=1 qo'yilsa jonli MediaMTX bilan ishlaydi")
	}
	c, err := net.DialTimeout("tcp", "127.0.0.1:8554", 2*time.Second)
	if err != nil {
		t.Skip("MediaMTX 8554 da emas")
	}
	_ = c.Close()
	return true
}

func TestC3_Mtx_RtspPublishReal(t *testing.T) {
	if !mtxAvailable(t) {
		return
	}
	camURL, camAUs := startFakeCamera(t)

	const path = "agenttest"
	pubURL := "rtsp://127.0.0.1:8554/" + path

	cu, err := parseRTSP(camURL)
	if err != nil {
		t.Fatal(err)
	}
	cam := cams.Camera{ID: "cam-mtx", Host: cu.Hostname(), Port: cu.Port(),
		MainPath: cu.Path(), CodecMain: "h264", Enabled: true}

	mgr := media.New(media.Config{
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		LookupCamera: func(id string) (cams.Camera, bool) { return cam, id == cam.ID },
		Send:         func(string, any) error { return nil },
	})
	var readyCh = make(chan proto.MediaReady, 1)
	reg := publish.New(clock.Real{}, nil, func(ev publish.ReadyEvent) {
		readyCh <- proto.MediaReady{PublishID: ev.PublishID, CameraID: ev.Key.CameraID}
	}, mgr.OnStopped)
	mgr.SetRegistry(reg)
	defer reg.Shutdown()

	mgr.OnStart(proto.MediaStart{
		SessionID: "se_mtx", CameraID: cam.ID, Profile: "main", Kind: "play",
		TTLS: 60, Ingest: "rtsp_tcp",
		Publish: proto.Publish{Token: "tk", URLs: proto.PublishURLs{RTSP: pubURL}, QueryName: "vmsaccesstoken"},
	})

	select {
	case r := <-readyCh:
		t.Logf("media_ready: %s (kamera AULARI=%d)", r.PublishID, camAUs.Load())
	case <-time.After(10 * time.Second):
		t.Fatal("media_ready kelmadi (MediaMTX ANNOUNCE/RECORD rad etgan bo'lishi mumkin)")
	}

	// MediaMTX API: dinamik path faol o'qiluvchilar ro'yxatida bo'lishi kerak.
	api, err := http.Get("http://127.0.0.1:9997/v3/paths/list")
	if err == nil {
		defer api.Body.Close()
		var v struct {
			Items []struct {
				Name   string   `json:"name"`
				Ready  bool     `json:"ready"`
				Tracks []string `json:"tracks"`
			} `json:"items"`
		}
		body, _ := io.ReadAll(api.Body)
		found := false
		if json.Unmarshal(body, &v) == nil {
			for _, it := range v.Items {
				if it.Name == path {
					found = true
					if !it.Ready || len(it.Tracks) == 0 {
						t.Errorf("MediaMTX path %q ready=%v tracks=%v", it.Name, it.Ready, it.Tracks)
					}
					t.Logf("MediaMTX faol path %q ready=%v tracks=%v", it.Name, it.Ready, it.Tracks)
				}
			}
		}
		if !found {
			t.Logf("MediaMTX paths/list'da %q topilmadi: %s", path, body)
		}
	}

	// MediaMTX'dan qayta DESCRIBE: oqim haqiqatan mavjud.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c, err := rtspclient.Dial(ctx, pubURL, rtspclient.Options{})
	if err != nil {
		t.Fatalf("MediaMTX DESCRIBE: %v", err)
	}
	defer c.Close()
	sdp, err := c.Describe(ctx)
	if err != nil {
		t.Fatalf("MediaMTX DESCRIBE javob: %v", err)
	}
	sd, err := cams.ParseSDP(string(sdp))
	if err != nil {
		t.Fatalf("SDP: %v", err)
	}
	vs := sd.VideoStreams()
	if len(vs) == 0 || vs[0].Codec != cams.CodecH264 {
		t.Fatalf("MediaMTX'dagi oqim kodek: %+v", vs)
	}
	fmt.Printf("OK: jonli MediaMTX — %s kodek=%s profil=%s\n", path, vs[0].Codec, vs[0].Profile)

	// To'xtatish: media_stop -> grace (1 s) -> registry yopiladi.
	mgr.OnStop(proto.MediaStop{SessionID: "se_mtx", CameraID: cam.ID, Reason: "user_close"})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(reg.List()) > 0 {
		time.Sleep(50 * time.Millisecond) // tashqi jarayon kuzatuvi (qabul mezoni)
	}
	if n := len(reg.List()); n != 0 {
		t.Errorf("registry = %d, 0 kutilardi", n)
	}
}
