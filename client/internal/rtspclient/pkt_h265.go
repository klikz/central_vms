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

// Pack — singleton (NAL ≤ maxPayload) yoki FU (RFC 7798 turi 49) bo'laklash.
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

// fu — RFC 7798 §4.4.2: bayt0 = F|Type(49)|Layer(0);
// bayt1 = NALType(6)|S|E|Tid(0). Payload = nal[2:].
func (p *H265Packetizer) fu(nal []byte, ts uint32, marker bool, maxPayload int) [][]byte {
	nalType := (nal[0] >> 1) & 0x3f
	// NAL boshi 2 baytli va tur byte0'ning 1..6-bitlarida (type<<1) —
	// 62<<2 xato edi: MediaMTX/brauzer "Multi-layer HEVC" deb rad etardi
	// (2026-11-20: H.265 preview dekodlanmas edi).
	// RFC 7798: FU NAL turi = 49 (AP=48, PACI=50). HEVC specida 62 —
	// "unspecified"; uni MediaMTX/FFmpeg qayta-yigmaydi:
	// "Unsupported (HEVC) NAL type (62)" (2026-11-21 kuzatuvi).
	h0 := byte(49 << 1) // type=49 (FU), F=0, layer MSB=0
	body := nal[2:]
	dataPer := maxPayload - 3
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
		// RFC 7798: [NAL boshi 2 bayt (tur=49; bayt1 nusxa)] [FU boshi] [bo'lak]
		pl := make([]byte, 0, 3+len(chunk))
		pl = append(pl, h0, nal[1], h1)
		pl = append(pl, chunk...)
		out = append(out, p.rtp(pl, ts, marker && last))
	}
	return out
}

// --- H.265 DEPAKETIZATOR (RFC 7798) -------------------------------------
//
// 1.0-draft.12: avval faqat H.264 depaketizatori bor edi — H.265 kamera
// oqimi NAL darajasida buzilib, MediaMTX'ga noto'g'ri payload ketardi.

// H265Depacketizer — RTP paketlardan NAL ro'yxatini tiklaydi. AU marker
// paketi bilan tugaydi (H.264 bilan bir xil kelishuv).
type H265Depacketizer struct {
	cur   [][]byte
	fua   []byte
	fuaOn bool
}

// NewH265Depacketizer — bo'sh depaketizator.
func NewH265Depacketizer() *H265Depacketizer { return &H265Depacketizer{} }

// Add — bitta RTP paketi; AU to'lganda NAL'lar qaytadi.
func (d *H265Depacketizer) Add(p RTPPacket) ([][]byte, bool) {
	pl := p.Payload
	if len(pl) < 2 {
		d.reset()
		return nil, false
	}
	typ := (pl[0] >> 1) & 0x3f
	switch {
	case typ == 49: // FU (fragmentation unit) — RFC 7798 turi 49
		if len(pl) < 3 {
			d.reset()
			return nil, false
		}
		fu := pl[2]
		start, end := fu&0x80 != 0, fu&0x40 != 0
		fuType := fu & 0x3f
		if start {
			// NAL boshi qayta tiklanadi: bit0 (F) + bit7 (layer MSB) saqlanadi.
			h0 := (pl[0] & 0x81) | byte(fuType<<1)
			d.fua = append([]byte{h0, pl[1]}, pl[3:]...)
			d.fuaOn = true
		} else if d.fuaOn {
			d.fua = append(d.fua, pl[3:]...)
		}
		if end && d.fuaOn {
			d.cur = append(d.cur, d.fua)
			d.fua, d.fuaOn = nil, false
		}
	case typ == 48: // AP (aggregation packet): [NAL][ST2][NAL][ST2]...
		i := 2
		for i+2 <= len(pl) {
			sz := int(pl[i])<<8 | int(pl[i+1])
			i += 2
			if sz <= 0 || i+sz > len(pl) {
				break
			}
			d.cur = append(d.cur, append([]byte(nil), pl[i:i+sz]...))
			i += sz
		}
	default: // singleton NAL (0..47, shu jumladan VPS/SPS/PPS va IDR)
		d.cur = append(d.cur, append([]byte(nil), pl...))
	}
	if p.Marker {
		if len(d.cur) == 0 {
			return nil, false
		}
		au := d.cur
		d.cur = nil
		return au, true
	}
	return nil, false
}

func (d *H265Depacketizer) reset() { d.cur, d.fua, d.fuaOn = nil, nil, false }
