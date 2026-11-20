package rtspclient

// RFC 6184 H.264 RTP packetizer (C3.7/C3.8 publish yo'li uchun).
// mediacommon stub bo'lgani uchun minimal o'z implementatsiyamiz.

// H264Packetizer — bitta oqim uchun seq sanagichi bilan paketlash.
type H264Packetizer struct {
	pt   byte
	ssrc uint32
	seq  uint16
}

// NewH264Packetizer — payload type (SDP'dan) va SSRC.
func NewH264Packetizer(pt byte, ssrc uint32) *H264Packetizer {
	return &H264Packetizer{pt: pt, ssrc: ssrc, seq: 1}
}

// Pack — bitta NAL unitni RTP paket(lar)iga bo'ladi. maxPayload — MTU
// chegarasi (RTP payload). marker AU oxirgi paketi uchun.
func (p *H264Packetizer) Pack(nal []byte, ts uint32, marker bool, maxPayload int) [][]byte {
	if len(nal) == 0 {
		return nil
	}
	if maxPayload < 8 {
		maxPayload = 8
	}
	if len(nal) <= maxPayload {
		return [][]byte{p.rtp(nal, ts, marker)}
	}
	return p.fua(nal, ts, marker, maxPayload)
}

// rtp — RTP sarlavhasi (V=2, CC=1, X=0) + payload.
func (p *H264Packetizer) rtp(payload []byte, ts uint32, marker bool) []byte {
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
	copy(b[8:12], []byte{byte(p.ssrc >> 24), byte(p.ssrc >> 16), byte(p.ssrc >> 8), byte(p.ssrc)})
	copy(b[12:], payload)
	return b
}

// fua — FU-A bo'laklash (type 28): indicator NRI'ni saqlaydi,
// birinchi bo'lak S=1, oxirgisi E=1.
func (p *H264Packetizer) fua(nal []byte, ts uint32, marker bool, maxPayload int) [][]byte {
	ind := (nal[0] & 0xE0) | 28
	body := nal[1:]
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
		fu := nal[0] & 0x1f // FU header: S|E|R|Type (Type = original NAL turi)
		if i == 0 {
			fu |= 0x80 // S
		}
		if last {
			fu |= 0x40 // E
		}
		pl := make([]byte, 0, 2+len(chunk))
		pl = append(pl, ind, fu)
		pl = append(pl, chunk...)
		out = append(out, p.rtp(pl, ts, marker && last))
	}
	return out
}
