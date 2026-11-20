package cams

import (
	"fmt"
	"strconv"
	"strings"
)

// SDP tahlili (docs/protocol.md §8.1, §12.2): kodek, profile, B-frame belgisi.
// Golden testlar: testdata/sdp/*.sdp (4 fixture) — G3 gate.

// Codec nomlari (§8.1).
const (
	CodecH264    = "h264"
	CodecH265    = "h265"
	CodecUnknown = "unknown"
)

// Media — SDP'dagi bitta m=bo'limi.
type Media struct {
	Kind     string
	Port     int
	Proto    string
	Payloads []int
	Rtpmap   map[int]string // payload type → "H264/90000"
	Fmtp     map[int]string // payload type → fmtp qatori
	Control  string         // a=control (media darajasida)
	Attrs    [][2]string    // tartib saqlanadi
}

// SDP — tahlil qilingan SDP hujjati.
type SDP struct {
	Version    int
	HasVersion bool // v= qatori ko'rildi (v=0 ham yaroqli)
	Session    string
	Medias     []Media
	Controls   []string // session darajasidagi a=control
}

// ParseSDP — SDP matnini oddiy, lekin yetarli darajada parse qiladi.
func ParseSDP(text string) (*SDP, error) {
	s := &SDP{}
	var cur *Media
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 2 || line[1] != '=' {
			continue
		}
		typ, val := line[0], strings.TrimSpace(line[2:])
		switch typ {
		case 'v':
			n, _ := strconv.Atoi(val)
			s.Version = n
			s.HasVersion = true
		case 's':
			s.Session = val
		case 'm':
			if cur != nil {
				s.Medias = append(s.Medias, *cur)
			}
			cur = &Media{Rtpmap: map[int]string{}, Fmtp: map[int]string{}}
			fields := strings.Fields(val)
			if len(fields) < 3 {
				return nil, fmt.Errorf("SDP: m=qatori noto'g'ri: %q", val)
			}
			cur.Kind = fields[0]
			cur.Port, _ = strconv.Atoi(fields[1])
			cur.Proto = fields[2]
			for _, p := range fields[3:] {
				if n, err := strconv.Atoi(p); err == nil {
					cur.Payloads = append(cur.Payloads, n)
				}
			}
		case 'a':
			if cur == nil {
				if v, ok := cutAttr(val, "control"); ok {
					s.Controls = append(s.Controls, v)
				}
				continue
			}
			cur.Attrs = append(cur.Attrs, [2]string{"a", val})
			if v, ok := cutAttr(val, "control"); ok {
				cur.Control = v
			}
			if v, ok := cutAttr(val, "rtpmap"); ok {
				if pt, rest, ok2 := strings.Cut(v, " "); ok2 {
					if n, err := strconv.Atoi(pt); err == nil {
						cur.Rtpmap[n] = rest
					}
				}
			}
			if v, ok := cutAttr(val, "fmtp"); ok {
				if pt, rest, ok2 := strings.Cut(v, " "); ok2 {
					if n, err := strconv.Atoi(pt); err == nil {
						cur.Fmtp[n] = rest
					}
				}
			}
		}
	}
	if cur != nil {
		s.Medias = append(s.Medias, *cur)
	}
	if !s.HasVersion {
		return nil, fmt.Errorf("SDP: v= qatori topilmadi")
	}
	return s, nil
}

func cutAttr(val, name string) (string, bool) {
	if !strings.HasPrefix(val, name+":") {
		return "", false
	}
	return strings.TrimSpace(val[len(name)+1:]), true
}

// StreamInfo — bitta video oqimi haqidagi xulosa (§12.2).
type StreamInfo struct {
	Codec       string `json:"codec"`
	Profile     string `json:"profile"`
	BFrames     bool   `json:"bframes"`
	PayloadType int    `json:"payload_type"`
	Control     string `json:"control,omitempty"`
	SPSParsed   bool   `json:"sps_parsed"`
}

// VideoStreams — barcha video oqimlari uchun xulosa chiqaradi.
func (s *SDP) VideoStreams() []StreamInfo {
	var out []StreamInfo
	for _, m := range s.Medias {
		if m.Kind != "video" {
			continue
		}
		for _, pt := range m.Payloads {
			rtpmap := m.Rtpmap[pt]
			codec := codecFromRtpmap(rtpmap)
			if codec == CodecUnknown {
				continue
			}
			info := StreamInfo{Codec: codec, PayloadType: pt, Control: m.Control, Profile: "unknown"}
			fmtp := m.Fmtp[pt]
			switch codec {
			case CodecH264:
				analyzeH264(fmtp, &info)
			case CodecH265:
				analyzeH265(fmtp, &info)
			}
			out = append(out, info)
		}
	}
	return out
}

func codecFromRtpmap(rtpmap string) string {
	encoding := strings.ToLower(strings.TrimSpace(strings.SplitN(rtpmap, "/", 2)[0]))
	switch encoding {
	case "h264", "mp4v-es":
		return CodecH264
	case "h265", "hevc":
		return CodecH265
	default:
		return CodecUnknown
	}
}

// analyzeH264 — avval SPS'ni o'qiymiz (aniq profile + B-frame), bo'lmasa
// profile-level-id hex'ga qaraymiz.
func analyzeH264(fmtp string, info *StreamInfo) {
	params := splitFmtp(fmtp)
	if sp, ok := params["sprop-parameter-sets"]; ok {
		if sps, err := ParseSPSBase64(sp); err == nil {
			info.SPSParsed = true
			info.Profile = sps.ProfileName()
			info.BFrames = sps.HasBFrames()
			return
		}
	}
	if plid, ok := params["profile-level-id"]; ok && len(plid) >= 2 {
		if v, err := strconv.ParseUint(plid[:2], 16, 8); err == nil {
			info.Profile = SPSInfo{ProfileIdc: int(v)}.ProfileName()
		}
	}
	// High profile + SPS o'qilmagan → ehtiyotkorlik bilan B-frame bor deb belgilaymiz
	// (C3 re-encode yo'liga tushadi; xato hisoblash arzonroq).
	if info.Profile == "high" {
		info.BFrames = true
	}
}

// analyzeH265 — profile-id (1=Main, 2=Main10, 3=Main Still). B-frame HEVC SPS'ida
// short_term_ref_pic_set orqali bilindi — MVP uchun false (C3.14 da aniqlashtiriladi).
func analyzeH265(fmtp string, info *StreamInfo) {
	params := splitFmtp(fmtp)
	if v, ok := params["profile-id"]; ok {
		switch v {
		case "1":
			info.Profile = "main"
		case "2":
			info.Profile = "main10"
		case "3":
			info.Profile = "main-still"
		default:
			info.Profile = "unknown"
		}
	}
	if v, ok := params["sprop-sps"]; ok && v != "" {
		info.SPSParsed = true // SPS mavjud, lekin tahlil C3 da
	}
}

// splitFmtp — "profile-level-id=64001f;packetization-mode=1;…" → map.
func splitFmtp(fmtp string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.FieldsFunc(fmtp, func(r rune) bool { return r == ';' || r == ',' }) {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	return out
}
