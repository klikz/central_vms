package rtspclient

import (
	"encoding/binary"
	"fmt"
)

// RTP (RFC 3550) minimal parse + H.264 depacketizer (C3.5 pull yo'li).
// Packetizer bilan simmetrik: yuborilgan AU olingan AU bo'lishi kerak.

// RTPPacket — sarlavha maydonlari + payload.
type RTPPacket struct {
	PT        byte
	Marker    bool
	Seq       uint16
	Timestamp uint32
	SSRC      uint32
	Payload   []byte
}

// ParseRTP — bitta RTP paketi (V=2, X/CC hisobga olinadi).
func ParseRTP(b []byte) (RTPPacket, error) {
	if len(b) < 12 {
		return RTPPacket{}, fmt.Errorf("rtp: juda qisqa: %d bayt", len(b))
	}
	if b[0]>>6 != 2 {
		return RTPPacket{}, fmt.Errorf("rtp: versiya %d (2 kutilardi)", b[0]>>6)
	}
	p := RTPPacket{
		PT:        b[1] & 0x7f,
		Marker:    b[1]&0x80 != 0,
		Seq:       binary.BigEndian.Uint16(b[2:4]),
		Timestamp: binary.BigEndian.Uint32(b[4:8]),
		SSRC:      binary.BigEndian.Uint32(b[8:12]),
	}
	off := 12
	cc := int(b[0] & 0x0f)
	off += 4 * cc
	if off > len(b) {
		return RTPPacket{}, fmt.Errorf("rtp: CC chegaradan tashqari")
	}
	if b[0]&0x10 != 0 { // X — kengaytma
		if off+4 > len(b) {
			return RTPPacket{}, fmt.Errorf("rtp: extension truncated")
		}
		extLen := int(binary.BigEndian.Uint16(b[off+2 : off+4]))
		off += 4 + 4*extLen
		if off > len(b) {
			return RTPPacket{}, fmt.Errorf("rtp: extension chegaradan tashqari")
		}
	}
	p.Payload = b[off:]
	return p, nil
}

// H264Depacketizer — RTP paketlarini NAL unitlarga yig'adi (RFC 6184).
// Marker bit AU chegarasini bildiradi: AU → [][]byte (NAL'lar).
type H264Depacketizer struct {
	cur   [][]byte
	fua   []byte
	fuaOn bool
	ts    uint32
}

// Add — bitta RTP paketi. AU to'lganda (marker) NAL ro'yxati qaytadi.
func (d *H264Depacketizer) Add(p RTPPacket) ([][]byte, bool) {
	pl := p.Payload
	if len(pl) == 0 {
		return nil, false
	}
	typ := pl[0] & 0x1f
	switch {
	case typ == 28: // FU-A
		if len(pl) < 2 {
			d.reset()
			return nil, false
		}
		start := pl[1]&0x80 != 0
		end := pl[1]&0x40 != 0
		nalType := pl[0] & 0xE0
		if start {
			d.fua = append([]byte{nalType | (pl[1] & 0x1f)}, pl[2:]...)
			d.fuaOn = true
		} else if d.fuaOn {
			d.fua = append(d.fua, pl[2:]...)
		}
		if end && d.fuaOn {
			d.cur = append(d.cur, d.fua)
			d.fua, d.fuaOn = nil, false
		}
	case typ > 0 && typ < 24: // singleton NAL
		d.cur = append(d.cur, append([]byte(nil), pl...))
	default: // 24..27 aggsutellar — MVP: oddiy qo'shamiz (STAP/AU: ichki NAL'lar)
		d.cur = append(d.cur, append([]byte(nil), pl...))
	}
	d.ts = p.Timestamp

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

func (d *H264Depacketizer) reset() {
	d.cur, d.fua, d.fuaOn = nil, nil, false
}
