package rtspclient

// RFC 7798 H.265 RTP packetizer (C3.8 rtsp_tcp publish yo'li).

// H265Packetizer — H.265 NAL unitlarini RTP paketlariga bo'ladi.
type H265Packetizer struct {
	pt   byte
	ssrc uint32
	seq  uint16
}

// NewH265Packetizer — payload type (SDP rtpmap) va SSRC.
func NewH265Packetizer(pt byte, ssrc uint32) *H265Packetizer {
	return &H265Packetizer{pt: pt, ssrc: ssrc, seq: 1}
}

// Pack — singleton (NAL ≤ maxPayload) yoki FU (type 62) bo'laklash.
func (p *H265Packetizer) Pack(nal []byte, ts uint32, marker bool, maxPayload int) [][]byte {
	if len(nal) < 2 {
		return nil
	}
	if maxPayload < 8 {
		maxPayload = 8
	}
	if len(nal) <= maxPayload {
		return [][]byte{p.rtp(nal, ts, marker)}
	}
	return p.fu(nal, ts, marker, maxPayload)
}

func (p *H265Packetizer) rtp(payload []byte, ts uint32, marker bool) []byte {
	b := make([]byte, 12+len(payload))
	b[0] = 0x80
	if marker {
		b[1] = 0x80 | p.pt
	} else {
		b[1] = p.pt
	}
	b[2], b[3] = byte(p.seq>>8), byte(p.seq)
	p.seq++
	b[4], b[5], b[6], b[7] = byte(ts>>24), byte(ts>>16), byte(ts>>8), byte(ts)
	b[8], b[9], b[10], b[11] = byte(p.ssrc>>24), byte(p.ssrc>>16), byte(p.ssrc>>8), byte(p.ssrc)
	copy(b[12:], payload)
	return b
}

// fu — RFC 7798 §4.4.2: bayt0 = F|Type(62)|Layer(0);
// bayt1 = NALType(6)|S|E|Tid(0). Payload = nal[2:].
func (p *H265Packetizer) fu(nal []byte, ts uint32, marker bool, maxPayload int) [][]byte {
	nalType := (nal[0] >> 1) & 0x3f
	h0 := byte(62 << 2) // type=62, F=0, Layer=0
	body := nal[2:]
	dataPer := maxPayload - 2
	if dataPer < 1 {
		dataPer = 1
	}
	var out [][]byte
	for i := 0; i < len(body); i += dataPer {
		end := i + dataPer
		if end > len(body) {
			end = len(body)
		}
		chunk := body[i:end]
		last := end == len(body)
		h1 := nalType
		if i == 0 {
			h1 |= 0x80 // S
		}
		if last {
			h1 |= 0x40 // E
		}
		pl := make([]byte, 0, 2+len(chunk))
		pl = append(pl, h0, h1)
		pl = append(pl, chunk...)
		out = append(out, p.rtp(pl, ts, marker && last))
	}
	return out
}
