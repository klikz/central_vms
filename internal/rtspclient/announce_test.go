package rtspclient

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// C3.7/C3.8: RTSP publish zanjiri — ANNOUNCE (SDP) → SETUP (interleaved)
// → RECORD → RTP/TCP karg'ama ($-freym) → TEARDOWN (§8.1 rtsp_tcp yo'li).

type fakeRTSPServer struct {
	ln       net.Listener
	mu       sync.Mutex
	gotSDP   string
	gotSetup []string
	frames   []interleavedFrame
}

func (s *fakeRTSPServer) snapshot() (string, []string, []interleavedFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gotSDP, append([]string(nil), s.gotSetup...), append([]interleavedFrame(nil), s.frames...)
}

type interleavedFrame struct {
	ch      byte
	payload []byte
}

func newFakeRTSPServer(t *testing.T) *fakeRTSPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeRTSPServer{ln: ln}
	go s.serve(t)
	return s
}

func (s *fakeRTSPServer) serve(t *testing.T) {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		req, err := ParseRequest(r)
		if err != nil {
			return // EOF — mijoz yopdi
		}
		h := Headers{"CSeq": fmt.Sprint(req.CSeq)}
		switch strings.ToUpper(req.Method) {
		case "ANNOUNCE":
			s.mu.Lock()
			s.gotSDP = string(req.Body)
			s.mu.Unlock()
		case "SETUP":
			tr := req.Headers["transport"]
			if tr == "" {
				tr = req.Headers["Transport"]
			}
			s.mu.Lock()
			s.gotSetup = append(s.gotSetup, tr)
			s.mu.Unlock()
			h["Session"] = "12345678"
			h["Transport"] = tr
		case "RECORD":
			h["Session"] = "12345678"
			h["RTP-Info"] = "url=rtsp://x/trackID=0;seq=0;rtptime=0"
		case "TEARDOWN":
			writeRTSPResp(conn, 200, "OK", h)
			return
		}
		if !writeRTSPResp(conn, 200, "OK", h) {
			return
		}
		if strings.ToUpper(req.Method) == "RECORD" {
			if f, ok := readInterleaved(r); ok {
				s.mu.Lock()
				s.frames = append(s.frames, f)
				s.mu.Unlock()
			}
		}
	}
}

func writeRTSPResp(conn net.Conn, code int, reason string, h Headers) bool {
	var b bytes.Buffer
	fmt.Fprintf(&b, "RTSP/1.0 %d %s\r\n", code, reason)
	for k, v := range h {
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	b.WriteString("\r\n")
	_, err := conn.Write(b.Bytes())
	return err == nil
}

// readInterleaved — RFC 2326 $-freym: '$' ch len(2 bayt BE).
func readInterleaved(r *bufio.Reader) (interleavedFrame, bool) {
	b0, err := r.ReadByte()
	if err != nil {
		return interleavedFrame{}, false
	}
	if b0 != '$' {
		return interleavedFrame{}, false
	}
	hdr := make([]byte, 3)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return interleavedFrame{}, false
	}
	n := int(hdr[1])<<8 | int(hdr[2])
	p := make([]byte, n)
	if _, err := io.ReadFull(r, p); err != nil {
		return interleavedFrame{}, false
	}
	return interleavedFrame{ch: hdr[0], payload: p}, true
}

func TestAnnounceSetupRecordFlow(t *testing.T) {
	srv := newFakeRTSPServer(t)
	defer srv.ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := Dial(ctx, "rtsp://"+srv.ln.Addr().String()+"/media", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	sdp := "v=0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\n"
	if err := c.Announce(ctx, sdp); err != nil {
		t.Fatalf("Announce: %v", err)
	}
	gotSDP, gotSetup, _ := srv.snapshot()
	if !strings.Contains(gotSDP, "H264") {
		t.Errorf("server SDP olmadi: %q", gotSDP)
	}

	ch0, ch1, err := c.SetupInterleaved(ctx, 0, 0, 1)
	if err != nil {
		t.Fatalf("SetupInterleaved: %v", err)
	}
	if ch0 != 0 || ch1 != 1 {
		t.Errorf("kanallar = %d,%d, 0,1 kutilardi", ch0, ch1)
	}
	_, gotSetup, _ = srv.snapshot()
	if len(gotSetup) != 1 || !strings.Contains(gotSetup[0], "interleaved=0-1") {
		t.Errorf("Transport boshliigi: %q", gotSetup)
	}

	if err := c.Record(ctx, "npt=0-"); err != nil {
		t.Fatalf("Record: %v", err)
	}

	rtp := []byte{0x80, 0x60, 0, 1, 0, 0, 0, 100, 0, 0, 0, 0, 0xAA, 0xBB}
	if err := c.WriteInterleaved(ch0, rtp); err != nil {
		t.Fatalf("WriteInterleaved: %v", err)
	}

	var frames []interleavedFrame
	for i := 0; i < 20000; i++ {
		_, _, frames = srv.snapshot()
		if len(frames) > 0 {
			break
		}
		runtime.Gosched() // sleep yo'q (§0.2)
	}
	if len(frames) == 0 {
		t.Fatal("server interleaved karg'ama olmadi")
	}
	f := frames[0]
	if int(f.ch) != ch0 {
		t.Errorf("kanal = %d, %d kutilardi", f.ch, ch0)
	}
	if !bytes.Equal(f.payload, rtp) {
		t.Errorf("payload farq: % x", f.payload)
	}

	if err := c.Teardown(ctx); err != nil {
		t.Errorf("Teardown: %v", err)
	}
}

// 4xx → StatusError (§13 token_rejected yo'li).
func TestAnnounceRejected(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		req, err := ParseRequest(r)
		if err != nil {
			return
		}
		writeRTSPResp(conn, 401, "Unauthorized", Headers{"CSeq": fmt.Sprint(req.CSeq)})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, "rtsp://"+ln.Addr().String()+"/media", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = c.Announce(ctx, "v=0\r\n")
	se, ok := err.(*StatusError)
	if !ok || se.Code != 401 {
		t.Fatalf("err = %v, 401 StatusError kutilardi", err)
	}
}
