package media

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/pion/webrtc/v4/pkg/media"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/publish"
	"central-vms/client/internal/rtspclient"
)

// C3.20: haqiqiy transportlar — whip (pion) va rtsp_tcp (o'z rtspclient).
// §8.1: H.264→WHIP; H.265→RTSP/TCP (yoki server/client transcode).

// OpenTransport — ingest turiga qarab transport ochish (§8.1).
func OpenTransport(ctx context.Context, plan publish.StartPlan, cam cams.Camera, pass string) (Transport, error) {
	switch plan.Ingest {
	case "whip":
		return openWhipTransport(ctx, plan)
	case "rtsp_tcp":
		return openRtspTransport(ctx, plan, cam)
	default:
		return nil, fmt.Errorf("noma'lum ingest: %q", plan.Ingest)
	}
}

// --- WHIP (pion/webrtc) ---

type whipT struct {
	w    *publish.Whip
	sent int64
}

func openWhipTransport(ctx context.Context, plan publish.StartPlan) (Transport, error) {
	w, err := publish.NewWhip(ctx, plan)
	if err != nil {
		return nil, err
	}
	if err := w.Publish(ctx); err != nil {
		_ = w.Close()
		return nil, err
	}
	return &whipT{w: w}, nil
}

func (t *whipT) WriteAU(nals [][]byte, ts uint32) error {
	data := annexB(nals)
	t.sent += int64(len(data))
	return t.w.WriteSample(context.Background(), media.Sample{
		Data: data, Duration: 33 * time.Millisecond,
	})
}

func (t *whipT) BytesSent() int64  { return t.sent }
func (t *whipT) Transport() string { return t.w.Transport() }
func (t *whipT) Close() error      { return t.w.Close() }

// annexB — NAL'larni boshlanish kodlari bilan birlashtiradi.
func annexB(nals [][]byte) []byte {
	var b bytes.Buffer
	for _, n := range nals {
		b.Write([]byte{0, 0, 0, 1})
		b.Write(n)
	}
	return b.Bytes()
}

// --- RTSP/TCP publish (o'z rtspclient) ---

type rtspT struct {
	conn *rtspclient.Conn
	pub  *publish.RtspPublisher
	sent int64
}

func openRtspTransport(ctx context.Context, plan publish.StartPlan, cam cams.Camera) (Transport, error) {
	conn, err := publish.DialRtsp(ctx, plan)
	if err != nil {
		return nil, err
	}
	codec := cam.CodecMain
	if plan.Key.Profile == "sub" && cam.CodecSub != "" {
		codec = cam.CodecSub
	}
	var pkt publish.Packetizer
	switch codec {
	case cams.CodecH265:
		pkt = rtspclient.NewH265Packetizer(96, 1)
	default:
		pkt = rtspclient.NewH264Packetizer(96, 1)
	}
	pub := publish.NewRtspPublisher(conn, pkt, 1200)
	if err := pub.Start(ctx, announceSDP(codec)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &rtspT{conn: conn, pub: pub}, nil
}

func (t *rtspT) WriteAU(nals [][]byte, ts uint32) error {
	if err := t.pub.PublishAU(nals, ts); err != nil {
		return err
	}
	for _, n := range nals {
		t.sent += int64(len(n)) + 12
	}
	return nil
}

func (t *rtspT) BytesSent() int64  { return t.sent }
func (t *rtspT) Transport() string { return "tcp" }
func (t *rtspT) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = t.pub.Stop(ctx)
	return t.conn.Close()
}

// announceSDP — ANNOUNCE uchun minimal SDP (§8: MediaMTX qabul qiladi).
func announceSDP(codec string) string {
	pt, rtpmap, fmtp := "96", "H264/90000", "a=fmtp:96 packetization-mode=1\r\n"
	if codec == cams.CodecH265 {
		rtpmap, fmtp = "H265/90000", ""
	}
	return fmt.Sprintf("v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=vmsagent\r\n"+
		"c=IN IP4 0.0.0.0\r\nt=0 0\r\nm=video 0 RTP/AVP %s\r\n"+
		"a=rtpmap:%s %s\r\n%s"+
		"a=control:trackID=0\r\n", pt, pt, rtpmap, fmtp)
}

// CamRTSPURL — kamera modelidan RTSP manzil (parol YO'Q — §10.2).
// CamRTSPURL — kameraning RTSP manzili. profile=="sub" bo'lsa SUB yo'li
// ishlatiladi (bo'sh bo'lsa main'ga qaytamiz). 2026-11-21 auditidagi
// xato: profil qaramasdan doim MAIN tortilardi — masalan 2688x1520 HEVC
// oqimi 640x360 o'rniga o'tardi (kamera sessiyasi, CPU va tarmoq isrofi).
func CamRTSPURL(c cams.Camera, profile string) string {
	port := c.Port
	if port == 0 {
		port = 554
	}
	path := c.MainPath
	if profile == "sub" && c.SubPath != "" {
		path = c.SubPath
	}
	if path == "" {
		path = "/"
	}
	return fmt.Sprintf("rtsp://%s:%d%s", c.Host, port, path)
}
