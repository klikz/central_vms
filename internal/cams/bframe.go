package cams

// B-frame siyosati (docs/protocol.md §8.1, C2.14).
//
// H.264 oqimida B-frame bo'lsa, past-kechikishli WebRTC (WHIP) brauzerda
// yomon qayta tiklanadi — shuning uchun B-frame'lar olib tashlanishi kerak.
// Ikki yo'l:
//   1. ONVIF bor bo'lsa — kamera sub profilini baseline/-bf 0 ga o'giramiz
//      (C2.13 codec_fix), re-encode shart emas.
//   2. ONVIF yo'q bo'lsa — C3 FFmpeg re-encode (`-profile:v baseline -bf 0`)
//      orqali siqadi. Bu holat uchun needs_reencode=true.
//
// H.265 alohida: u allaqachon transcode zanjiriga tushadi (HEVC brauzerda
// yo'q), shuning uchun B-frame siyosati H.264'ga xos.

// NeedsReencode — kamera oqimi C3'da re-encode qilinishi kerakmi.
// onvifOK — kamera ONVIF orqali profil/siqish sozlamasiga ruxsat beradimi.
func NeedsReencode(info StreamInfo, onvifOK bool) bool {
	if info.Codec != CodecH264 {
		return false // H.265 transcode zanjirida; unknown — keyingi bosqichda
	}
	if !info.BFrames {
		return false // B-frame yo'q → `-c copy` remux yetarli (§8.1 1-qator)
	}
	// B-frame bor: ONVIF o'chira olmasa, FFmpeg siqadi.
	return !onvifOK
}
