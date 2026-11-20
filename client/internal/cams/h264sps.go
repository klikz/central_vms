package cams

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// H.264 SPS tahlili (docs/protocol.md §8.1): B-frame'ni aniq bilish uchun
// profile-level-id yetarli emas — VUI ichidagi max_num_reorder_frames kerak.
// Spec: ITU-T H.264 §7.3.2.1.1 (seq_parameter_set_rb sp).

// SPSInfo — SPS'dan olingan muhim maydonlar.
type SPSInfo struct {
	ProfileIdc       int
	LevelIdc         int
	PicOrderCntType  int
	NumReorderFrames int
	MaxNumRefFrames  int
	FrameMbsOnly     bool
	WidthInMBs       int
	HeightInMapUnits int
	HasVUI           bool
}

// HasBFrames — B-frame'lar mavjudmi (§8.1).
// Faqat VUI'dagi max_num_reorder_frames asosida: POC tipi 2 Baseline'ga xos va
// B-frame belgisi EMAS (xato qoida test orqali aniqlandi).
func (s SPSInfo) HasBFrames() bool {
	return s.NumReorderFrames > 0
}

// ProfileName — profile_idc → oddiy nom (§12.2).
func (s SPSInfo) ProfileName() string {
	switch s.ProfileIdc {
	case 66:
		return "baseline"
	case 77:
		return "main"
	case 88:
		return "extended"
	case 100, 110, 122, 244, 44, 83, 86, 118, 128:
		return "high"
	default:
		return "unknown"
	}
}

var (
	ErrNoSPS  = errors.New("SDP: SPS topilmadi")
	ErrBadSPS = errors.New("SDP: SPS parse qilinmadi")
)

// ParseSPSAnnexB — Annex-B oqimidagi birinchi SPS'ni (NAL type 7) parse qiladi.
func ParseSPSAnnexB(annexb []byte) (SPSInfo, error) {
	for _, nal := range splitAnnexBNALs(annexb) {
		if len(nal) < 4 || nal[0]&0x1F != 7 {
			continue
		}
		return parseSPS(nal[1:])
	}
	return SPSInfo{}, ErrNoSPS
}

// ParseSPSBase64 — SDP'dagi `sprop-parameter-sets` (base64, vergul bilan ajratilgan).
func ParseSPSBase64(sprop string) (SPSInfo, error) {
	for _, part := range strings.Split(strings.TrimSpace(sprop), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(part)
		if err != nil {
			return SPSInfo{}, fmt.Errorf("%w: base64: %v", ErrBadSPS, err)
		}
		if len(raw) >= 4 && raw[0]&0x1F == 7 {
			return parseSPS(raw[1:])
		}
	}
	return SPSInfo{}, ErrNoSPS
}

// parseSPS — SPS RBSP (NAL header'siz).
func parseSPS(rbsp []byte) (SPSInfo, error) {
	var s SPSInfo
	r := newBitReader(stripEmulationPrevention(rbsp))

	s.ProfileIdc = int(r.u(8))
	r.u(8) // constraint_set0..5 + reserved
	s.LevelIdc = int(r.u(8))

	_ = r.ue() // seq_parameter_set_id
	if isHighProfile(s.ProfileIdc) {
		chroma := r.ue()
		if chroma == 3 {
			r.u(1) // separate_colour_plane_flag
		}
		_ = r.ue() // bit_depth_luma_minus8
		_ = r.ue() // bit_depth_chroma_minus8
		r.u(1)     // qpprime_y_zero_transform_bypass_flag
		if r.u1() == 1 {
			n := 8
			if chroma == 3 {
				n = 12
			}
			for i := 0; i < n && r.err == nil; i++ {
				if r.u1() == 1 {
					size := 16
					if i >= 6 {
						size = 64
					}
					skipScalingList(r, size)
				}
			}
		}
	}
	_ = r.ue() // log2_max_frame_num_minus4

	s.PicOrderCntType = int(r.ue())
	switch s.PicOrderCntType {
	case 0:
		_ = r.ue() // log2_max_pic_order_cnt_lsb_minus4
	case 1:
		r.u(1) // delta_pic_order_always_zero_flag
		_ = r.se()
		_ = r.se()
		n := r.ue()
		for i := uint32(0); i < n && r.err == nil; i++ {
			_ = r.se()
		}
	}
	s.MaxNumRefFrames = int(r.ue())
	r.u(1) // gaps_in_frame_num_value_allowed_flag
	s.WidthInMBs = int(r.ue())
	s.HeightInMapUnits = int(r.ue())
	s.FrameMbsOnly = r.u1() == 1
	if !s.FrameMbsOnly {
		r.u(1) // mb_adaptive_frame_field_flag
	}
	r.u(1)           // direct_8x8_inference_flag
	if r.u1() == 1 { // frame_cropping_flag
		_ = r.ue()
		_ = r.ue()
		_ = r.ue()
		_ = r.ue()
	}
	s.HasVUI = r.u1() == 1
	if s.HasVUI {
		parseVUI(r, &s)
	}
	if r.err != nil {
		return s, fmt.Errorf("%w: %v", ErrBadSPS, r.err)
	}
	return s, nil
}

// skipScalingList — scaling_list (spec 7.3.2.1.1.1) bitlarni to'g'ri o'tkazish uchun.
func skipScalingList(r *bitReader, size int) {
	last, next := int32(8), int32(8)
	for j := 0; j < size && r.err == nil; j++ {
		if next != 0 {
			if r.u1() == 1 {
				d := r.se()
				next = (last + d + 256) % 256
			}
			if next != 0 {
				last = next
			}
		}
	}
}

// parseVUI — VUI maydonlari (spec 7.4.3.1); maqsad: bitstream_restriction_flag
// ostidagi max_num_reorder_frames.
func parseVUI(r *bitReader, s *SPSInfo) {
	if r.u1() == 1 { // aspect_ratio_info_present_flag
		if r.u(8) == 255 {
			r.u(16)
			r.u(16)
		}
	}
	if r.u1() == 1 { // overscan_info_present_flag
		r.u(1)
	}
	if r.u1() == 1 { // video_signal_type_present_flag
		r.u(3) // video_format
		r.u(1) // video_full_range_flag
		if r.u1() == 1 {
			r.u(8)
			r.u(8)
			r.u(8)
		}
	}
	if r.u1() == 1 { // chroma_loc_info_present_flag
		_ = r.ue()
		_ = r.ue()
	}
	if r.u1() == 1 { // timing_info_present_flag
		r.u(32) // num_units_in_tick
		r.u(32) // time_scale
		r.u(1)  // fixed_frame_rate_flag
	}
	nalHRD := r.u1() == 1
	if nalHRD {
		skipHRD(r)
	}
	vclHRD := r.u1() == 1
	if vclHRD {
		skipHRD(r)
	}
	if nalHRD || vclHRD {
		r.u(1) // low_delay_hrd_flag
	}
	r.u(1)           // pic_struct_present_flag
	if r.u1() == 1 { // bitstream_restriction_flag
		r.u(1) // motion_vectors_over_pic_boundaries_flag
		_ = r.ue()
		_ = r.ue()
		_ = r.ue()
		_ = r.ue()
		s.NumReorderFrames = int(r.ue()) // max_num_reorder_frames
		_ = r.ue()                       // max_dec_frame_buffering
	}
}

// skipHRD — hrd_parameters (spec 7.4.3.2).
func skipHRD(r *bitReader) {
	n := r.ue() // cpb_cnt_minus1
	r.u(4)      // bit_rate_scale
	r.u(4)      // cpb_size_scale
	for i := uint32(0); i <= n && r.err == nil; i++ {
		_ = r.ue()
		_ = r.ue()
		r.u(1)
	}
	r.u(4) // time_offset_scale
	for i := uint32(0); i <= n && r.err == nil; i++ {
		_ = r.ue()
	}
}

func isHighProfile(p int) bool {
	switch p {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128:
		return true
	}
	return false
}

// splitAnnexBNALs — Annex-B boshlanish belgilariga qarab NAL larni ajratadi
// (boshlang'ich 00 00 01 / 00 00 00 01 baytlari olib tashlanadi).
func splitAnnexBNALs(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i+2 < len(b); i++ {
		is3 := b[i] == 0 && b[i+1] == 0 && b[i+2] == 1
		is4 := i+3 < len(b) && b[i] == 0 && b[i+1] == 0 && b[i+2] == 0 && b[i+3] == 1
		if !is3 && !is4 {
			continue
		}
		skip := 3
		if is4 {
			skip = 4
		}
		if i > start {
			out = append(out, b[start:i])
		}
		start = i + skip
		i += skip - 1
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}
