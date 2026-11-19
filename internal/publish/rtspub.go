package publish

import (
	"context"

	"central-vms/client/internal/rtspclient"
)

// C3.8: H.265 → RTSP/TCP publish — ANNOUNCE/SETUP/RECORD (transport) +
// RFC 7798 packetizer bog'lamasi. §8.1 matritsasi.

// RtspTransport — rtspclient.Conn bilan mos interfeys (test mock uchun).
type RtspTransport interface {
	Announce(ctx context.Context, sdp string) error
	SetupInterleaved(ctx context.Context, trackIdx, rtpCh, rtcpCh int) (int, int, error)
	Record(ctx context.Context, rangeNpt string) error
	WriteInterleaved(ch int, payload []byte) error
	Teardown(ctx context.Context) error
}

// Packetizer — H264/H265 packetizerlar bilan mos interfeys.
type Packetizer interface {
	Pack(nal []byte, ts uint32, marker bool, maxPayload int) [][]byte
}

// RtspPublisher — bitta rtsp_tcp publish: SDP e'loni, interleaved SETUP,
// RECORD, AU→RTP yozish.
type RtspPublisher struct {
	tr      RtspTransport
	pkt     Packetizer
	ch      int
	maxPayl int
}

// DialRtsp — prod ulanish: plan.URL (token query'da) + kreds yo'q (§7.2).
func DialRtsp(ctx context.Context, plan StartPlan) (*rtspclient.Conn, error) {
	return rtspclient.Dial(ctx, plan.URL, rtspclient.Options{})
}

// NewRtspPublisher — transport+packetizer'ni bog'laydi; maxPayload MTU
// (RTP payload; 1200 xavfsiy).
func NewRtspPublisher(tr RtspTransport, pkt Packetizer, maxPayload int) *RtspPublisher {
	if maxPayload <= 0 {
		maxPayload = 1200
	}
	return &RtspPublisher{tr: tr, pkt: pkt, ch: 0, maxPayl: maxPayload}
}

// Start — ANNOUNCE → SETUP(interleaved 0-1) → RECORD.
func (p *RtspPublisher) Start(ctx context.Context, sdp string) error {
	if err := p.tr.Announce(ctx, sdp); err != nil {
		return err
	}
	ch, _, err := p.tr.SetupInterleaved(ctx, 0, 0, 1)
	if err != nil {
		return err
	}
	p.ch = ch
	return p.tr.Record(ctx, "npt=0-")
}

// PublishAU — bitta access unit (NAL'lar ro'yxati) → RTP paketlari.
// Marker AU oxirgi NAL'ining oxirgi pakethu (§8.2 tez ochilish uchun
// ts bir xil: chaqiruvchi 90 kHz hisoblaydi).
func (p *RtspPublisher) PublishAU(nals [][]byte, ts uint32) error {
	for i, nal := range nals {
		last := i == len(nals)-1
		for _, pkt := range p.pkt.Pack(nal, ts, last, p.maxPayl) {
			if err := p.tr.WriteInterleaved(p.ch, pkt); err != nil {
				return err
			}
		}
	}
	return nil
}

// Stop — TEARDOWN.
func (p *RtspPublisher) Stop(ctx context.Context) error { return p.tr.Teardown(ctx) }
