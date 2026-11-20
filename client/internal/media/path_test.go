package media

import "testing"

// 1.0-draft.12: media_ready.path — MediaMTX yo'li, profil EMAS
// (eski xato: "main" yuborilgani uchun server o'quvchi URL'ini
// rtsp://host/main deb buzardi).
func TestPathFromURL(t *testing.T) {
	cases := map[string]string{
		"rtsp://192.168.0.27:8554/cam/ag_x/cam-1?token=abc": "cam/ag_x/cam-1",
		"rtsp://h:8554/tc/ag_x/cam-1":                       "tc/ag_x/cam-1",
		"https://h:8889/cam/ag_x/cam-1/whip":                "cam/ag_x/cam-1/whip",
		"":                                                  "",
		"not a url":                                         "",
		"rtsp://h":                                          "",
	}
	for in, want := range cases {
		if got := pathFromURL(in); got != want {
			t.Errorf("pathFromURL(%q) = %q, kutilardi %q", in, got, want)
		}
	}
}
