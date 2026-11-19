package cams

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"central-vms/client/internal/clock"
	"central-vms/client/internal/rtspclient"
)

// C3.5 (§8, §9.3): kamera RTSP pull — DESCRIBE→SETUP(interleaved)→PLAY,
// RTP o'qish, uzilishda 3 retry (2 s), keyin camera_offline.

const pullSDP = "v=0\r\n" +
	"m=video 0 RTP/AVP/UDP 96\r\n" +
	"a=rtpmap:96 H264/90000\r\n" +
	"a=fmtp:96 packetization-mode=1\r\n" +
	"a=control:trackID=1\r\n"

// fakePullServer — DESCRIBE/SETUP/PLAY javoblaydi, so'ng interleaved RTP
// AU'lar yuboradi. stream=false bo'lsa PLAY'dan keyin darhol uzadi.
type fakePullServer struct {
	ln      net.Listener
	stream  bool
	closeAt int // nechta AU'dan keyin uzilsin (0 = uzilmaslik)
}

func newFakePullServer(t *testing.T, stream bool) *fakePullServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakePullServer{ln: ln, stream: stream, closeAt: 2}
	go func() {
		for {
			if !s.serveOnce(t) {
				return
			}
		}
	}()
	return s
}

func (s *fakePullServer) url() string { return "rtsp://" + s.ln.Addr().String() + "/media" }

func (s *fakePullServer) serveOnce(t *testing.T) bool {
	conn, err := s.ln.Accept()
	if err != nil {
		return false
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	send := func(code int, extra map[string]string) bool {
		var b strings.Builder
		fmt.Fprintf(&b, "RTSP/1.0 %d OK\r\n", code)
		for k, v := range extra {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
		b.WriteString("\r\n")
		_, err := conn.Write([]byte(b.String()))
		return err == nil
	}
	for {
		req, err := rtspclient.ParseRequest(r)
		if err != nil {
			return true
		}
		h := map[string]string{"CSeq": fmt.Sprint(req.CSeq)}
		switch strings.ToUpper(req.Method) {
		case "SETUP":
			h["Session"] = "42"
			h["Transport"] = req.Headers["transport"]
		case "DESCRIBE":
			body := []byte(pullSDP)
			h["Content-Type"] = "application/sdp"
			h["Content-Length"] = fmt.Sprint(len(body))
			if !send(200, h) {
				return true
			}
			conn.Write(body)
			continue
		case "PLAY":
			h["Session"] = "42"
			if !send(200, h) {
				return true
			}
			if s.stream {
				s.streamAUs(conn)
			}
			return true
		}
		if !send(200, h) {
			return true
		}
	}
}

// streamAUs — packetizer orqali 2 AU yuboradi, so'ng ulanishni yopadi.
func (s *fakePullServer) streamAUs(conn net.Conn) {
	pk := rtspclient.NewH264Packetizer(96, 7)
	nals := [][]byte{
		{0x67, 1, 2, 3}, // SPS
		append([]byte{0x65}, bytesRepeat(0xEE, 1500)...), // IDR (FU-A bo'ladi)
	}
	au1 := pk.Pack(nals[0], 0, false, 1200)
	au2 := pk.Pack(nals[1], 3600, true, 1200)
	for _, raw := range append(au1, au2...) {
		hdr := []byte{'$', 0, byte(len(raw) >> 8), byte(len(raw))}
		if _, err := conn.Write(append(hdr, raw...)); err != nil {
			return
		}
	}
}

func nalTypes(nals [][]byte) []byte {
	out := make([]byte, len(nals))
	for i, n := range nals {
		out[i] = n[0]
	}
	return out
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestPull_ReadsAUs(t *testing.T) {
	srv := newFakePullServer(t, true)
	defer srv.ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var aus []AU
	errCh := make(chan error, 1)
	go func() {
		errCh <- Pull(ctx, PullOptions{URL: srv.url()}, func(a AU) error {
			mu.Lock()
			aus = append(aus, a)
			n := len(aus)
			mu.Unlock()
			if n >= 1 {
				cancel() // SPS+IDR bitta AU — yetarli
			}
			return nil
		})
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Pull: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Pull tugamadi")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(aus) < 1 {
		t.Fatalf("AU soni = %d, kamida 1 kutilardi", len(aus))
	}
	au := aus[0]
	if len(au.Nals) != 2 || au.Nals[0][0] != 0x67 || au.Nals[1][0] != 0x65 {
		t.Fatalf("AU NAL'lari: %d (turlar %q)", len(au.Nals), nalTypes(au.Nals))
	}
	if len(au.Nals[1]) != 1501 {
		t.Errorf("IDR (FU-A qayta yig'ish) uzunligi = %d, 1501 kutilardi", len(au.Nals[1]))
	}
}

// Uzilish: PLAY'dan keyin server yopadi → 3 retry (2 s) → camera_offline.
func TestPull_RetryThenCameraOffline(t *testing.T) {
	srv := newFakePullServer(t, false) // PLAY'dan keyin darhol uziladi
	defer srv.ln.Close()

	fc := clock.NewFake(time.Unix(0, 0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Pull(ctx, PullOptions{URL: srv.url(), Clock: fc, Retries: 3, RetryDelay: 2 * time.Second},
			func(AU) error { return nil })
	}()

	// Fake soatni retry'lar yechilguncha suramiz.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		fc.Advance(500 * time.Millisecond)
		select {
		case err := <-errCh:
			if !errors.Is(err, ErrCameraOffline) {
				t.Fatalf("err = %v, ErrCameraOffline kutilardi", err)
			}
			return
		default:
		}
	}
	t.Fatal("Pull camera_offline bilan tugamadi")
}

// O'chirish: ctx bekor qilinsa — darhol chiqish (retry yo'q).
func TestPull_CtxCancelStops(t *testing.T) {
	srv := newFakePullServer(t, true)
	defer srv.ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Pull(ctx, PullOptions{URL: srv.url()}, func(AU) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, context.Canceled kutilardi", err)
	}
}
