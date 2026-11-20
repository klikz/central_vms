package rtspclient

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// RTSP publish tomoni (C3.7/C3.8, §8.1 rtsp_tcp yo'li):
// ANNOUNCE → SETUP (interleaved) → RECORD → $-freym RTP → TEARDOWN.

// Announce — SDP'ni serverga e'lon qiladi (Content-Type: application/sdp).
func (c *Conn) Announce(ctx context.Context, sdp string) error {
	resp, err := c.Do(ctx, "ANNOUNCE", Headers{"content-type": "application/sdp"}, []byte(sdp))
	if err != nil {
		return err
	}
	return check("ANNOUNCE", resp)
}

// SetupInterleaved — RTP'ni shu TCP ulanish orqali yuborish (RFC 2326 §3.2,
// interleaved=a-b). trackIdx URL yo'liga qo'shiladi (/trackID=N).
// Qaytgan (rtpCh, rtcpCh) — $-freym kanal raqamlari.
func (c *Conn) SetupInterleaved(ctx context.Context, trackIdx int, rtpCh, rtcpCh int) (int, int, error) {
	url := c.uri
	if trackIdx >= 0 {
		url = fmt.Sprintf("%s/trackID=%d", c.uri, trackIdx)
	}
	// To'liq transport specifikatsiyasi + mode=record — gortsplib/MediaMTX
	// publish SETUP'ni shu ko'rinishda qabul qiladi (jonli MediaMTX bilan
	// tekshirildi: mode'siz 400 qaytadi).
	tr := fmt.Sprintf("RTP/AVP/TCP;unicast;interleaved=%d-%d;mode=record", rtpCh, rtcpCh)
	resp, err := c.doAt(ctx, "SETUP", url, Headers{"transport": tr, "session": c.session}, nil)
	if err != nil {
		return 0, 0, err
	}
	if err := check("SETUP", resp); err != nil {
		return 0, 0, err
	}
	if s := resp.Headers["session"]; s != "" {
		c.session = s
	}
	// Server transportni qaytardi — kanal raqamlarini shundan olamiz.
	if got := resp.Headers["transport"]; got != "" {
		if a, b, ok := parseInterleaved(got); ok {
			return a, b, nil
		}
	}
	return rtpCh, rtcpCh, nil
}

// parseInterleaved — "interleaved=0-1" → (0,1).
func parseInterleaved(s string) (int, int, bool) {
	i := 0
	for i < len(s) {
		if s[i:i+12] == "interleaved=" {
			j := i + 12
			k := j
			for k < len(s) && s[k] != '-' && s[k] != ';' && s[k] != ',' {
				k++
			}
			a, err1 := strconv.Atoi(s[j:k])
			if err1 != nil || k >= len(s) || s[k] != '-' {
				return 0, 0, false
			}
			m := k + 1
			n := m
			for n < len(s) && s[n] != ';' && s[n] != ',' {
				n++
			}
			b, err2 := strconv.Atoi(s[m:n])
			if err2 != nil {
				return 0, 0, false
			}
			return a, b, true
		}
		i++
	}
	return 0, 0, false
}

// SetupTrack — SDP'dagi a=control bo'yicha SETUP (trackID=1, video, …).
func (c *Conn) SetupTrack(ctx context.Context, control string, rtpCh, rtcpCh int) (int, int, error) {
	url := c.uri
	if control != "" {
		url = joinRTSP(c.uri, control)
	}
	tr := fmt.Sprintf("RTP/AVP/TCP;unicast;interleaved=%d-%d", rtpCh, rtcpCh)
	resp, err := c.doAt(ctx, "SETUP", url, Headers{"transport": tr, "session": c.session}, nil)
	if err != nil {
		return 0, 0, err
	}
	if err := check("SETUP", resp); err != nil {
		return 0, 0, err
	}
	if s := resp.Headers["session"]; s != "" {
		c.session = s
	}
	if got := resp.Headers["transport"]; got != "" {
		if a, b, ok := parseInterleaved(got); ok {
			return a, b, nil
		}
	}
	return rtpCh, rtcpCh, nil
}

// joinRTSP — base URL + control (nisbiy yoki to'liq rtsp:// bo'lishi mumkin).
func joinRTSP(base, control string) string {
	if strings.HasPrefix(control, "rtsp://") {
		return control
	}
	if strings.HasPrefix(base, "rtsp://") {
		return strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(control, "/")
	}
	return control
}

// Play — oqimni boshlash (pull). rangeNpt "npt=0.0-" ko'rinishida.
func (c *Conn) Play(ctx context.Context, rangeNpt string) error {
	h := Headers{"session": c.session}
	if rangeNpt != "" {
		h["range"] = rangeNpt
	}
	resp, err := c.Do(ctx, "PLAY", h, nil)
	if err != nil {
		return err
	}
	return check("PLAY", resp)
}

// Record — oqimni boshlash. rangeNpt "npt=0-" (chegara yo'q) ko'rinishida.
func (c *Conn) Record(ctx context.Context, rangeNpt string) error {
	h := Headers{"session": c.session}
	if rangeNpt != "" {
		h["range"] = rangeNpt
	}
	resp, err := c.Do(ctx, "RECORD", h, nil)
	if err != nil {
		return err
	}
	return check("RECORD", resp)
}

// Teardown — sessiyani yopadi.
func (c *Conn) Teardown(ctx context.Context) error {
	resp, err := c.Do(ctx, "TEARDOWN", Headers{"session": c.session}, nil)
	if err != nil {
		return err
	}
	return check("TEARDOWN", resp)
}

// WriteInterleaved — RTP/RTCP paketini $-freym bilan yuboradi
// (RFC 2326 §10.12): '$' channel len(2B BE) payload.
func (c *Conn) WriteInterleaved(ch int, payload []byte) error {
	if ch < 0 || ch > 255 {
		return fmt.Errorf("rtsp: kanal %d 0..255 tashqarisida", ch)
	}
	// Yozish muddati har freymda yangilanadi: so'rov (RECORD) deadline'i
	// oqimga meros bo'lib qolsa, publish ~5 s'da "i/o timeout" bilan
	// uzilardi (2026-11-20: H.265 kamera oqimi shu sababli uzlanar edi).
	_ = c.conn.SetWriteDeadline(time.Now().Add(interleavedWriteTimeout))
	hdr := make([]byte, 4)
	hdr[0] = '$'
	hdr[1] = byte(ch)
	binary.BigEndian.PutUint16(hdr[2:4], uint16(len(payload)))
	if _, err := c.conn.Write(hdr); err != nil {
		return fmt.Errorf("rtsp: interleaved header: %w", err)
	}
	_, err := c.conn.Write(payload)
	return err
}

// ReadInterleaved — o'qish tomoni (C3.5 pull) uchun: $-freym yoki
// boshqa satr (RTSP javob aralash oqimda).
// interleavedIdleTimeout — ReadInterleaved uchun idle o'qish chegarasi
// (so'rov dediti oqimga meros bo'lmasin, §8).
const interleavedIdleTimeout = 10 * time.Second

// interleavedWriteTimeout — publish tomonidagi yozish muddati (har freymda
// qayta qo'yiladi).
const interleavedWriteTimeout = 10 * time.Second

func (c *Conn) ReadInterleaved() (byte, []byte, error) {
	// Streaming o'qish: har chaqiruvda idle deditini yangilaymiz — aks
	// holda so'rov (PLAY) dediti butun oqimga meros bo'lib 5 s'da uzadi.
	_ = c.conn.SetReadDeadline(time.Now().Add(interleavedIdleTimeout))
	b0, err := c.br.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	if b0 != '$' {
		return 0, nil, fmt.Errorf("rtsp: interleaved kutilardi, topildi %q", b0)
	}
	hdr := make([]byte, 3)
	if _, err := io.ReadFull(c.br, hdr); err != nil {
		return 0, nil, err
	}
	n := int(hdr[1])<<8 | int(hdr[2])
	if n <= 0 || n > maxBodyLen {
		return 0, nil, fmt.Errorf("rtsp: interleaved uzunlik %d chekdan tashqari", n)
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(c.br, p); err != nil {
		return 0, nil, err
	}
	return hdr[0], p, nil
}
