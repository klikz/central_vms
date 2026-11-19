// lanprobe — tarmoqdagi kamerani mijoz kutubxonalari bilan tekshiradi:
// RTSP yo'llarini DESCRIBE qilib, SDP kodek/track xulosasini chiqaradi.
// Ishlatish: go run ./cmd/lanprobe <ip> [rtsp-user [rtsp-pass]]
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/discovery"
	"central-vms/client/internal/rtspclient"
)

// wsdd — WS-Discovery multicast Probe (239.255.255.250:3702) va javoblarn
// chop etadi (§16): qurilma modellari/ONVIF manzillari ro'yxati.
func wsdd() {
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "udp:", err)
		os.Exit(1)
	}
	defer conn.Close()
	pc := conn.(*net.UDPConn)
	probe := []byte(`<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
 xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing"
 xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery">
 <s:Header>
  <a:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</a:Action>
  <a:MessageID>urn:uuid:00000000-0000-0000-0000-000000000001</a:MessageID>
  <a:To>http://schemas.xmlsoap.org/ws/2005/04/discovery/multicast</a:To>
 </s:Header>
 <s:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></s:Body>
</s:Envelope>`)
	dst := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 3702}
	if _, err := pc.WriteTo(probe, dst); err != nil {
		fmt.Fprintln(os.Stderr, "probe yuborib bo'lmadi:", err)
		os.Exit(1)
	}
	deadline := time.Now().Add(4 * time.Second)
	seen := map[string]bool{}
	for time.Now().Before(deadline) {
		_ = pc.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
		buf := make([]byte, 65536)
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			continue
		}
		devs, perr := discovery.ParseProbeMatches(buf[:n])
		if perr != nil || len(devs) == 0 {
			continue
		}
		for _, d := range devs {
			k := d.IP + "|" + d.MAC
			if seen[k] {
				continue
			}
			seen[k] = true
			fmt.Printf("%-16s model=%-22s name=%-18s mac=%-14s xaddrs=%v\n",
				d.IP, d.Model, d.Name, d.MAC, d.XAddrs)
		}
		_ = from
	}
	fmt.Printf("jami: %d qurilma\n", len(seen))
}

var paths = []string{"/", "/live", "/live/main", "/video", "/video1", "/video2", "/media",
	"/11", "/12", "/h264", "/h264preview", "/Streaming/Channels/101", "/Streaming/Channels/201",
	"/cam/realmonitor?channel=1&subtype=0", "/mpeg4?tcpstream=0", "/ONVIF/MEDIA/PREVIEW/AVC",
	"/Live?node=0", "/media/video1", "/ch0_0.h264", "/ps/onvif_media1", "/video.cgi", "/videostream.cgi", "/livestream/0", "/1.h264",
	"/h264stream", "/media/1", "/1", "/2", "/video?2,3", "/ch01", "/ch02", "/av0", "/av1"}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-wsdd" {
		wsdd()
		return
	}
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "ishlatish: lanprobe <ip> [rtsp-user [rtsp-pass]]")
		os.Exit(2)
	}
	host := os.Args[1]
	user, pass := "", ""
	if len(os.Args) > 3 {
		user, pass = os.Args[2], os.Args[3]
	}
	port := "554"
	if strings.Contains(host, ":") {
		host, port, _ = strings.Cut(host, ":")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ok := 0
	for _, p := range paths {
		u := "rtsp://" + host + ":" + port + p
		c, err := rtspclient.Dial(ctx, u, rtspclient.Options{User: user, Pass: pass, Timeout: 3 * time.Second})
		if err != nil {
			fmt.Printf("%-45s dial: %v\n", p, err)
			continue
		}
		sdp, err := c.Describe(ctx)
		if err != nil {
			fmt.Printf("%-45s describe: %v\n", p, err)
			_ = c.Close()
			continue
		}
		sd, perr := cams.ParseSDP(string(sdp))
		note := "sdp parse: " + fmt.Sprint(perr)
		if perr == nil {
			var vs []string
			for _, s := range sd.VideoStreams() {
				vs = append(vs, fmt.Sprintf("%s/%s pt=%d ctrl=%q", s.Codec, s.Profile, s.PayloadType, s.Control))
			}
			note = "video=[" + strings.Join(vs, ", ") + "]"
		}
		fmt.Printf("%-45s OK sdp=%dB %s\n", p, len(sdp), note)
		_ = c.Close()
		ok++
	}
	fmt.Printf("topildi: %d ta ishlaydigan yo'l\n", ok)
}
