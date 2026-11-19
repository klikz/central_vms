package publish

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"central-vms/client/internal/rtspclient"
)

// §5.2/§8.1: H.264 → WHIP publish (pion/webrtc v4).
// POST {urls.whip} + Authorization: Bearer; SDP offer/answer; ICE/UDP.

// whipGatherMax — ICE gathering to'lishini kutish ustki chegarasi
// (host kandidatlari lokal darhol yig'iladi).
const whipGatherMax = 5 * time.Second

// h264Baseline — §8.2 baseline profil (B-frame yo'q), zerolatency uchun
// transport-cc feedback.
var h264Baseline = webrtc.RTPCodecCapability{
	MimeType:     webrtc.MimeTypeH264,
	ClockRate:    90000,
	SDPFmtpLine:  "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
	RTCPFeedback: []webrtc.RTCPFeedback{{Type: "goog-remb"}, {Type: "transport-cc"}},
}

// Whip — bitta WHIP publish sessiyasi.
type Whip struct {
	pc    *webrtc.PeerConnection
	track *webrtc.TrackLocalStaticSample
	offer webrtc.SessionDescription
	plan  StartPlan

	gathered  chan struct{} // ICE gathering complete
	transport string        // birinchi kandidattan: udp|tcp
}

// NewWhip — PC yaratadi, H.264 sendonly track qo'shadi, offer tuzadi va
// ICE gathering'ni kutadi (lokal muhitda tezkor).
func NewWhip(ctx context.Context, plan StartPlan) (*Whip, error) {
	if plan.Ingest != "whip" {
		return nil, fmt.Errorf("%w: whip kutilardi, %q", ErrCodecMismatch, plan.Ingest)
	}
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}
	w := &Whip{pc: pc, gathered: make(chan struct{}), transport: "udp", plan: plan}

	pc.OnICEGatheringStateChange(func(state webrtc.ICEGatheringState) {
		if state == webrtc.ICEGatheringStateComplete {
			select {
			case <-w.gathered:
			default:
				close(w.gathered)
			}
		}
	})
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil && w.transport == "udp" && c.Protocol.String() != "udp" {
			w.transport = c.Protocol.String()
		}
	})

	track, err := webrtc.NewTrackLocalStaticSample(h264Baseline, "video", "vmsagent")
	if err != nil {
		pc.Close()
		return nil, err
	}
	w.track = track
	if _, err := pc.AddTransceiverFromTrack(track,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
		pc.Close()
		return nil, err
	}

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		pc.Close()
		return nil, err
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		pc.Close()
		return nil, err
	}
	w.offer = offer

	select {
	case <-w.gathered:
	case <-time.After(whipGatherMax): // qisman kandidtlar bilan davom etamiz
	case <-ctx.Done():
		pc.Close()
		return nil, ctx.Err()
	}
	return w, nil
}

// Publish — offer'ni POST qiladi, answer'ni qo'llaydi (§7.2: Bearer header).
// Noma'lum statuslar *rtspclient.StatusError orqali (§13 reason matritsasi).
func (w *Whip) Publish(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.plan.URL,
		bytes.NewReader([]byte(w.offer.SDP)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/sdp")
	if w.plan.HeaderName != "" {
		req.Header.Set(w.plan.HeaderName, w.plan.HeaderValue)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		return &rtspclient.StatusError{Method: "POST", Code: res.StatusCode,
			Reason: res.Status}
	}
	return w.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer, SDP: string(body)})
}

// WriteSample — H.264 AU namunalarini yuboradi (C3.5/FFmpeg manbati keyin).
func (w *Whip) WriteSample(ctx context.Context, sample media.Sample) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return w.track.WriteSample(sample)
}

// SignalingState — SDP muzokara holati (test/telemetriya).
func (w *Whip) SignalingState() webrtc.SignalingState { return w.pc.SignalingState() }

// Transport — ICE kandidati tarmog'i: udp (asosiy) yoki tcp (fallback).
func (w *Whip) Transport() string { return w.transport }

// Close — PC'ni yopadi (idempotent).
func (w *Whip) Close() error {
	err := w.pc.Close()
	if errors.Is(err, webrtc.ErrConnectionClosed) {
		return nil
	}
	return err
}
