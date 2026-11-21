package media

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/publish"
	"central-vms/client/internal/rtspclient"
)

// C3.20: transport yordamchilari va manager qo'shimcha yo'llari.

func TestAnnexB(t *testing.T) {
	got := annexB([][]byte{{0x67, 1}, {0x65, 2}})
	want := []byte{0, 0, 0, 1, 0x67, 1, 0, 0, 0, 1, 0x65, 2}
	if string(got) != string(want) {
		t.Errorf("annexB: % x", got)
	}
}

func TestAnnounceSDP(t *testing.T) {
	h264 := announceSDP("h264")
	if !strings.Contains(h264, "a=rtpmap:96 H264/90000") || !strings.Contains(h264, "packetization-mode=1") {
		t.Errorf("h264 SDP: %s", h264)
	}
	h265 := announceSDP(cams.CodecH265)
	if !strings.Contains(h265, "a=rtpmap:96 H265/90000") {
		t.Errorf("h265 SDP: %s", h265)
	}
}

func TestCamRTSPURLDefaults(t *testing.T) {
	c := CamRTSPURL(cams.Camera{Host: "10.0.0.7"}, "main")
	if c != "rtsp://10.0.0.7:554/" {
		t.Errorf("got %q", c)
	}
}

// fakeIngest — ANNOUNCE/SETUP/RECORD qabul qiladi va freymlarni sanaydi.
type fakeIngest struct {
	ln     net.Listener
	frames atomic.Int64
	sdp    atomic.Value
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
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		for {
			req, err := rtspclient.ParseRequest(r)
			if err != nil {
				return
			}
			h := map[string]string{"CSeq": fmt.Sprint(req.CSeq)}
			switch strings.ToUpper(req.Method) {
			case "ANNOUNCE":
				fi.sdp.Store(string(req.Body)) // ParseRequest body'ni oldindan o'qiydi
			case "SETUP":
				h["Session"] = "1"
				h["Transport"] = req.Headers["transport"]
			case "RECORD":
				h["Session"] = "1"
			}
			var b strings.Builder
			fmt.Fprintf(&b, "RTSP/1.0 200 OK\r\n")
			for k, v := range h {
				fmt.Fprintf(&b, "%s: %s\r\n", k, v)
			}
			b.WriteString("\r\n")
			if _, err := c.Write([]byte(b.String())); err != nil {
				return
			}
			if strings.ToUpper(req.Method) == "RECORD" {
				for {
					hdr := make([]byte, 4)
					if _, err := io.ReadFull(r, hdr); err != nil {
						return
					}
					buf := make([]byte, int(hdr[2])<<8|int(hdr[3]))
					if _, err := io.ReadFull(r, buf); err != nil {
						return
					}
					fi.frames.Add(1)
				}
			}
		}
	}()
	return fi
}

func TestOpenTransport_RtspE2E(t *testing.T) {
	fi := startFakeIngest(t)
	plan := publish.StartPlan{
		Key:    publish.Key{CameraID: "c1", Profile: "main", Ingest: "rtsp_tcp"},
		Ingest: "rtsp_tcp",
		URL:    "rtsp://" + fi.ln.Addr().String() + "/live?vmsaccesstoken=tk",
	}
	tr, err := OpenTransport(context.Background(), plan, cams.Camera{CodecMain: "h264"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.WriteAU([][]byte{{0x65, 1, 2, 3}}, 90000); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && fi.frames.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if fi.frames.Load() == 0 {
		t.Error("ingest RTP freym ko'rmadi")
	}
	if s, _ := fi.sdp.Load().(string); !strings.Contains(s, "H264/90000") {
		t.Errorf("ANNOUNCE SDP yetmadi: %q", s)
	}
	if tr.Transport() != "tcp" || tr.BytesSent() == 0 {
		t.Errorf("transport: %s %d", tr.Transport(), tr.BytesSent())
	}
	if err := tr.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestOpenTransport_NomaolumIngest(t *testing.T) {
	if _, err := OpenTransport(context.Background(), publish.StartPlan{Ingest: "srt"}, cams.Camera{}, ""); err == nil {
		t.Error("noma'lum ingest qabul qilindi")
	}
}

// Manager qo'shimcha yo'llar: refresh, topilmagan kamera, Send yo'q.
func TestManager_EkYollar(t *testing.T) {
	m := New(Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	// Kamera topilmadi → media_stopped(camera_rejected), panic yo'q.
	m.OnStart(proto.MediaStart{SessionID: "s", CameraID: "yok", Profile: "main",
		Kind: "play", Ingest: "whip",
		Publish: proto.Publish{Token: "t", URLs: proto.PublishURLs{WHIP: "http://x"}}})
	// Refresh/Stop — sessiya yo'q, jim.
	m.OnRefresh(proto.MediaRefresh{SessionID: "s", CameraID: "yok", ExtendS: 60})
	m.OnStop(proto.MediaStop{SessionID: "s", CameraID: "yok", Reason: "user_close"})
	if ev := m.sessionOf(publish.Key{}, "p"); ev != "" {
		t.Error("sessionOf bo'sh bo'lishi kerak")
	}
	if st := m.StatsSource(publish.Key{}); st.BytesSent != 0 {
		t.Error("StatsSource bo'sh bo'lishi kerak")
	}
}
