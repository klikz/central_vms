package cams

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeOnvif — OnvifApplier soxtasi; GetProfiles/Set natijalarini boshqaradi.
type fakeOnvif struct {
	profiles []Profile
	profErr  error
	setErr   error
	setCalls []EncoderParams
	setToks  []string
}

func (f *fakeOnvif) GetProfiles(_ context.Context) ([]Profile, error) {
	if f.profErr != nil {
		return nil, f.profErr
	}
	return f.profiles, nil
}
func (f *fakeOnvif) SetVideoEncoderConfiguration(_ context.Context, tok string, p EncoderParams) error {
	f.setToks = append(f.setToks, tok)
	f.setCalls = append(f.setCalls, p)
	return f.setErr
}

func subProfiles() []Profile {
	return []Profile{
		{Token: "P0", Name: "Profile_0_Media1", VideoEncoderToken: "V0"},
		{Token: "P1", Name: "Profile_1_Media2", VideoEncoderToken: "V1"},
	}
}

func TestCodecFix_SingleOK(t *testing.T) {
	ap := &fakeOnvif{profiles: subProfiles()}
	fix := func(_, _, _ string) OnvifApplier { return ap }
	cam := Camera{ID: "c1", Name: "Kirish", Host: "10.0.0.1", OnvifXAddr: "http://x/onvif", OnvifOK: true}
	target := FixTarget{Profile: "sub", Codec: "h264", NoBFrames: true, Width: 1280, Height: 720, FPS: 10, BitrateKbps: 700, GopS: 2}

	out := ApplyCodecFix(context.Background(), cam, "admin", "parol", target, fix)
	if !out.OK || out.Reason != "" {
		t.Fatalf("natija = %+v, ok kutilardi", out)
	}
	if len(ap.setToks) != 1 || ap.setToks[0] != "V1" {
		t.Errorf("sub profil encoder tokeni V1 bo'lishi kerak edi: %v", ap.setToks)
	}
	p := ap.setCalls[0]
	if p.Width != 1280 || p.Height != 720 || p.FPS != 10 || p.BitrateKbps != 700 || p.GovLengthSec != 2 {
		t.Errorf("encoder parametrlari xato: %+v", p)
	}
}

func TestCodecFix_Reasons(t *testing.T) {
	target := FixTarget{Profile: "sub", Codec: "h264", Width: 1280, Height: 720, FPS: 10, BitrateKbps: 700, GopS: 2}
	cam := Camera{ID: "c1", OnvifXAddr: "http://x/onvif", OnvifOK: true}

	// ONVIF manzili yo'q.
	fix := func(_, _, _ string) OnvifApplier { return &fakeOnvif{profiles: subProfiles()} }
	if out := ApplyCodecFix(context.Background(), Camera{ID: "c1"}, "admin", "p", target, fix); out.OK || out.Reason != "onvif_unavailable" {
		t.Errorf("onvif_unavailable kutilardi: %+v", out)
	}
	// GetProfiles xatosi.
	fixBad := func(_, _, _ string) OnvifApplier { return &fakeOnvif{profErr: errors.New("auth fail")} }
	if out := ApplyCodecFix(context.Background(), cam, "admin", "p", target, fixBad); out.OK || !strings.Contains(out.Reason, "onvif") {
		t.Errorf("onvif xatosi kutilardi: %+v", out)
	}
	// Profil topilmadi.
	fixEmpty := func(_, _, _ string) OnvifApplier { return &fakeOnvif{profiles: nil} }
	if out := ApplyCodecFix(context.Background(), cam, "admin", "p", target, fixEmpty); out.OK || out.Reason != "profile_not_found" {
		t.Errorf("profile_not_found kutilardi: %+v", out)
	}
	// Set xatosi.
	fixSet := func(_, _, _ string) OnvifApplier {
		return &fakeOnvif{profiles: subProfiles(), setErr: errors.New("read-only")}
	}
	if out := ApplyCodecFix(context.Background(), cam, "admin", "p", target, fixSet); out.OK || !strings.Contains(out.Reason, "set_failed") {
		t.Errorf("set_failed kutilardi: %+v", out)
	}
}

// apply_all: 12 kamera, 3 tasida ONVIF yo'q → qisman hisobot.
func TestCodecFix_ApplyAllPartial(t *testing.T) {
	target := FixTarget{Profile: "sub", Codec: "h264", Width: 1280, Height: 720, FPS: 10, BitrateKbps: 700, GopS: 2}
	var camsList []Camera
	for i := 0; i < 12; i++ {
		c := Camera{ID: "id" + itoaC(i), Name: "m", Host: "10.0.0.1", OnvifXAddr: "http://x/onvif", OnvifOK: true}
		if i%4 == 0 { // 0,4,8 → ONVIF yo'q (3 ta)
			c.OnvifXAddr = ""
			c.OnvifOK = false
		}
		camsList = append(camsList, c)
	}
	fix := func(_, _, _ string) OnvifApplier { return &fakeOnvif{profiles: subProfiles()} }
	outs := ApplyCodecFixAll(context.Background(), camsList, func(Camera) (string, string) { return "admin", "p" }, target, fix)
	if len(outs) != 12 {
		t.Fatalf("hisobot = %d, 12 kutilardi", len(outs))
	}
	var okCnt, failCnt int
	for _, o := range outs {
		if o.OK {
			okCnt++
		} else {
			failCnt++
			if o.Reason != "onvif_unavailable" {
				t.Errorf("kutilmagan reason: %+v", o)
			}
		}
	}
	if okCnt != 9 || failCnt != 3 {
		t.Errorf("ok=%d fail=%d, xohlangan ok=9 fail=3", okCnt, failCnt)
	}
}

// Interface mosligi: *OnvifClient OnvifApplier'ni qanoatlantiradi.
var _ OnvifApplier = (*OnvifClient)(nil)

func itoaC(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
