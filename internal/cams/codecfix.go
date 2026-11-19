package cams

import (
	"context"
	"fmt"
	"strings"
)

// codec_fix bajarish (docs/protocol.md §5.5, C2.13).
// Server `codec_fix` yuboradi → agent ONVIF orqali sub profilni maqsadli
// parametrlarga o'giradi (720p/10fps/700kbps CBR/GOP 2s/B-frame'siz) va
// `codec_fix_done {camera_id, ok, reason}` bilan javob beradi.
// `apply_all: true` → bir xil modeldagi barcha kameralarga (bulk).

// OnvifApplier — ONVIF sozlash qobiliyati (*OnvifClient mos keladi).
type OnvifApplier interface {
	GetProfiles(ctx context.Context) ([]Profile, error)
	SetVideoEncoderConfiguration(ctx context.Context, configToken string, p EncoderParams) error
}

// OnvifFactory — kamera uchun ONVIF applier yasaydi (test fake uzatadi).
type OnvifFactory func(xaddr, user, pass string) OnvifApplier

// FixTarget — qo'llanadigan maqsad (proto.CodecFixTarget'dan mustaqil,
// aks holda cams→proto bog'liqligi paydo bo'lardi).
type FixTarget struct {
	Profile     string
	Codec       string
	NoBFrames   bool
	Width       int
	Height      int
	FPS         int
	BitrateKbps int
	GopS        int
}

// FixOutcome — bitta kamera natijasi (§5.5 codec_fix_done maydonlari).
type FixOutcome struct {
	CameraID string
	OK       bool
	Reason   string // bo'sh = ok; onvif_unavailable | profile_not_found | set_failed:…
}

// ApplyCodecFix — bitta kameraga qo'llaydi. ONVIF bo'lmasa/xato bo'lsa
// ok=false va aniq reason (jimgina yutilmaydi — chaqiruvchi hisobot qiladi).
// user/secret — ONVIF kreds (chaqiruvchi keystore/config'dan oladi; Camera
// modelida username saqlanmaydi).
func ApplyCodecFix(ctx context.Context, cam Camera, user, secret string, t FixTarget, newOnvif OnvifFactory) FixOutcome {
	if !cam.OnvifOK || cam.OnvifXAddr == "" {
		return FixOutcome{CameraID: cam.ID, Reason: "onvif_unavailable"}
	}
	ap := newOnvif(cam.OnvifXAddr, user, secret)
	profiles, err := ap.GetProfiles(ctx)
	if err != nil {
		return FixOutcome{CameraID: cam.ID, Reason: "onvif: " + err.Error()}
	}
	prof := selectProfile(profiles, t.Profile)
	if prof == nil || prof.VideoEncoderToken == "" {
		return FixOutcome{CameraID: cam.ID, Reason: "profile_not_found"}
	}
	params := EncoderParams{
		Encoding:     strings.ToUpper(t.Codec),
		Width:        t.Width,
		Height:       t.Height,
		FPS:          t.FPS,
		BitrateKbps:  t.BitrateKbps,
		RateControl:  "CBR",
		GovLengthSec: t.GopS,
	}
	if err := ap.SetVideoEncoderConfiguration(ctx, prof.VideoEncoderToken, params); err != nil {
		return FixOutcome{CameraID: cam.ID, Reason: fmt.Sprintf("set_failed: %v", err)}
	}
	return FixOutcome{CameraID: cam.ID, OK: true}
}

// ApplyCodecFixAll — bulk: har kameraga qo'llaydi, barcha natijalarni
// qaytaradi (bitta muvaffaqiyatsizlik qolganlarini to'xtatmaydi, §5.5).
// credsFor kamera uchun (user, pass) qaytaradi.
func ApplyCodecFixAll(ctx context.Context, camList []Camera, credsFor func(Camera) (string, string), t FixTarget, newOnvif OnvifFactory) []FixOutcome {
	outs := make([]FixOutcome, 0, len(camList))
	for _, c := range camList {
		user, sec := "", ""
		if credsFor != nil {
			user, sec = credsFor(c)
		}
		outs = append(outs, ApplyCodecFix(ctx, c, user, sec, t, newOnvif))
	}
	return outs
}

// selectProfile — maqsad profili (sub/main) bo'yicha tanlash.
// Nom belgisiga (Media2/sub) qarab, bo'lmasa indeks bo'yicha (sub→ikkinchi).
func selectProfile(profiles []Profile, want string) *Profile {
	if len(profiles) == 0 {
		return nil
	}
	want = strings.ToLower(strings.TrimSpace(want))
	sub := want == "sub" || want == "media2" || want == "2"
	// Nom belgisini qidirish.
	for i := range profiles {
		n := strings.ToLower(profiles[i].Name)
		if sub && (strings.Contains(n, "media2") || strings.Contains(n, "sub")) {
			return &profiles[i]
		}
		if !sub && (strings.Contains(n, "media1") || strings.Contains(n, "main")) {
			return &profiles[i]
		}
	}
	// Indeksga qarab: sub → oxirgisi, main → birinchisi.
	if sub && len(profiles) >= 2 {
		return &profiles[len(profiles)-1]
	}
	return &profiles[0]
}
