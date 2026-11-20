package cams

import "testing"

// streamFromFixture — SDP fixture'idan birinchi video oqim xulosasi.
func streamFromFixture(t *testing.T, name string) StreamInfo {
	t.Helper()
	sdp := readFile(t, fixturePath(t, "sdp", name))
	parsed, err := ParseSDP(sdp)
	if err != nil {
		t.Fatalf("ParseSDP: %v", err)
	}
	streams := parsed.VideoStreams()
	if len(streams) == 0 {
		t.Fatal("video oqim topilmadi")
	}
	// B-frame'li oqimni tanlaymiz (bo'lsa), bo'lmasa birinchisini.
	for _, s := range streams {
		if s.BFrames {
			return s
		}
	}
	return streams[0]
}

// C2.14: B-frame siyosati — ONVIF bo'lmasa C3 re-encode belgisi.
// Qaror matritsasi §8.1: H.264 `High` + B-frame + ONVIF yo'q → re-encode
// (kamera baseline/-bf 0 ga ONVIF orqali o'girmasa, FFmpeg siqadi).

func TestBFramePolicy_Matrix(t *testing.T) {
	tests := []struct {
		name    string
		info    StreamInfo
		onvifOK bool
		want    bool
	}{
		{"h264_high_bframe_onvif_yoq", StreamInfo{Codec: CodecH264, Profile: "high", BFrames: true}, false, true},
		{"h264_high_bframe_onvif_bor", StreamInfo{Codec: CodecH264, Profile: "high", BFrames: true}, true, false},
		{"h264_high_bframe_yoq", StreamInfo{Codec: CodecH264, Profile: "high", BFrames: false}, false, false},
		{"h264_main_bframe", StreamInfo{Codec: CodecH264, Profile: "main", BFrames: true}, false, true},
		{"h264_main_bframe_onvif", StreamInfo{Codec: CodecH264, Profile: "main", BFrames: true}, true, false},
		{"h264_baseline", StreamInfo{Codec: CodecH264, Profile: "baseline", BFrames: false}, false, false},
		{"h264_profile_nomaalum_bframe", StreamInfo{Codec: CodecH264, Profile: "unknown", BFrames: true}, false, true},
		{"h265_reencode_yoq", StreamInfo{Codec: CodecH265, Profile: "main", BFrames: true}, false, false},
		{"unknown_kodek", StreamInfo{Codec: CodecUnknown, Profile: "unknown", BFrames: true}, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NeedsReencode(tc.info, tc.onvifOK); got != tc.want {
				t.Errorf("NeedsReencode(%+v, onvif=%v) = %v, xohlangan %v",
					tc.info, tc.onvifOK, got, tc.want)
			}
		})
	}
}

// Golden fixture orakoli: h264-high-bframes.sdp → flag true (ONVIF yo'q).
func TestBFramePolicy_GoldenFixture(t *testing.T) {
	info := streamFromFixture(t, "h264-high-bframes.sdp")
	if info.Codec != CodecH264 {
		t.Fatalf("kodek = %q, h264 kutilardi", info.Codec)
	}
	if !info.BFrames {
		t.Fatal("fixture'da B-frame topilmadi — fixture noto'g'ri yoki parse buzuq")
	}
	if !NeedsReencode(info, false) {
		t.Error("h264-high-bframes + ONVIF yo'q → needs_reencode=true bo'lishi kerak edi")
	}
	if NeedsReencode(info, true) {
		t.Error("ONVIF bo'lsa re-encode kerak emas (kamera o'gira oladi)")
	}
}
