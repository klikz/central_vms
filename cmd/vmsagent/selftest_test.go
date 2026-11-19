package main

import (
	"context"
	"net"
	"testing"
	"time"

	"central-vms/client/internal/cams"
)

func TestMediaHost(t *testing.T) {
	if got := mediaHost("ws://192.168.1.10:8081"); got != "192.168.1.10" {
		t.Errorf("got %q", got)
	}
	if got := mediaHost("not a url"); got != "" {
		t.Errorf("yaroqsiz URL: got %q", got)
	}
}

func TestCamRTSPURL(t *testing.T) {
	c := cams.Camera{Host: "10.0.0.7", Port: 0, MainPath: "/Streaming/Channels/101"}
	if got := camRTSPURL(c); got != "rtsp://10.0.0.7:554/Streaming/Channels/101" {
		t.Errorf("got %q", got)
	}
	c2 := cams.Camera{Host: "h", Port: 8554, MainPath: ""}
	if got := camRTSPURL(c2); got != "rtsp://h:8554/" {
		t.Errorf("got %q", got)
	}
}

// stunProbe — fake STUN server javob bersa udp=ok.
func TestStunProbe(t *testing.T) {
	udpAddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	ln, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		buf := make([]byte, 512)
		n, addr, err := ln.ReadFromUDP(buf)
		if err == nil && n >= 20 {
			resp := make([]byte, 20)
			resp[0], resp[1] = 0x01, 0x01 // Binding Response
			copy(resp[4:8], []byte{0x21, 0x12, 0xa4, 0x42})
			_, _ = ln.WriteToUDP(resp, addr)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if got := stunProbe(ctx, ln.LocalAddr().String()); got != "udp=ok" {
		t.Errorf("stunProbe = %q", got)
	}
}

func TestFfProcs_SafeWhenNone(t *testing.T) {
	// FFmpeg yo'q muhitda ham panika qilmaslik kerak.
	_ = ffProcs()
}
