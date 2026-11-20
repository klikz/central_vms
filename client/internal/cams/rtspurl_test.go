package cams

import (
	"strings"
	"testing"
)

func TestRTSPURLBuild(t *testing.T) {
	u := NewRTSPURL("192.168.10.20", 0, "")
	if u.Port != 554 {
		t.Fatalf("port defaulti=%d", u.Port)
	}
	if !strings.Contains(u.Path, "subtype=0") {
		t.Fatalf("default yo'l: %q", u.Path)
	}
	sub := u.SubURL()
	if !strings.Contains(sub.Path, "subtype=1") {
		t.Fatalf("sub yo'l: %q", sub.Path)
	}
	// Asosiy o'zgarmasligi kerak
	if strings.Contains(u.Path, "subtype=1") {
		t.Fatal("SubURL asosiy obyektni o'zgartirdi")
	}

	a := u.WithAuth("admin", "parol 123")
	full := a.String()
	if !strings.Contains(full, "rtsp://admin:") || !strings.Contains(full, "192.168.10.20:554") {
		t.Fatalf("to'liq URL: %q", full)
	}
	// Parol bo'shliq bilan escape'lanadi
	if !strings.Contains(full, "parol%20123") && !strings.Contains(full, "parol+123") {
		t.Fatalf("parol escape'lanmagan: %q", full)
	}
}

func TestRTSPURLMasked(t *testing.T) {
	u := NewRTSPURL("10.0.0.5", 554, "/cam/realmonitor?channel=1&subtype=0").WithAuth("admin", "JIZNIQ")
	m := u.Masked()
	if strings.Contains(m, "JIZNIQ") {
		t.Fatalf("mask'langan URL'da parol qoldi: %q", m)
	}
	if !strings.Contains(m, "admin:****@10.0.0.5") {
		t.Fatalf("mask formati: %q", m)
	}
	// Yo'l (query) o'zgarmaydi
	if !strings.Contains(m, "subtype=0") {
		t.Fatalf("yo'l buzildi: %q", m)
	}
	// Parolsiz: userinfo yo'q
	plain := NewRTSPURL("h", 554, "/x").Masked()
	if strings.Contains(plain, "@") || strings.Contains(plain, "****") {
		t.Fatalf("parolsiz URL: %q", plain)
	}
}

func TestParseRTSPURL(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
		host    string
		port    int
		path    string
		user    string
		pass    string
	}{
		{in: "rtsp://admin:secret@192.168.10.20:554/cam/realmonitor?channel=1&subtype=0",
			host: "192.168.10.20", port: 554, path: "/cam/realmonitor?channel=1&subtype=0", user: "admin", pass: "secret"},
		{in: "rtsp://10.0.0.5/live", host: "10.0.0.5", port: 554, path: "/live"},
		{in: "rtsp://h:8554/x", host: "h", port: 8554, path: "/x"},
		{in: "http://h/x", wantErr: true},
		{in: "not a url", wantErr: true},
		{in: "rtsp://h:abc/x", wantErr: true},
	}
	for _, c := range cases {
		u, err := ParseRTSPURL(c.in)
		if c.wantErr {
			if err == nil {
				t.Fatalf("%q uchun xato kutilmadi", c.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if u.Host != c.host || u.Port != c.port || u.Path != c.path || u.User != c.user || u.Pass != c.pass {
			t.Fatalf("%q → host=%q port=%d path=%q user=%q pass=%q", c.in, u.Host, u.Port, u.Path, u.User, u.Pass)
		}
	}
}

func TestParseRTSPURLRoundTrip(t *testing.T) {
	orig := "rtsp://user:pass@192.168.1.50:554/cam/realmonitor?channel=2&subtype=1"
	u, err := ParseRTSPURL(orig)
	if err != nil {
		t.Fatal(err)
	}
	back := u.String()
	u2, err := ParseRTSPURL(back)
	if err != nil {
		t.Fatalf("qayta parse (%s): %v", back, err)
	}
	if u2.Host != u.Host || u2.Port != u.Port || u2.Path != u.Path || u2.User != u.User || u2.Pass != u.Pass {
		t.Fatalf("round-trip buzildi: %q → %q", orig, back)
	}
}

func TestMaskRawURL(t *testing.T) {
	got := MaskRawURL("rtsp://admin:parol@10.0.0.1/cam")
	if strings.Contains(got, "parol") || !strings.Contains(got, "****") {
		t.Fatalf("mask: %q", got)
	}
	// parse bo'lmagan satr o'zgarishsiz qaytadi
	if s := MaskRawURL("rtsp:// h"); s != "rtsp:// h" {
		t.Fatalf("noto'g'ri URL o'zgardi: %q", s)
	}
	// parolsiz foydalanuvchi
	if s := MaskRawURL("rtsp://user@host/x"); strings.Contains(s, "****") {
		t.Fatalf("parol bo'lmasa **** qo'yilmasin: %q", s)
	}
}
