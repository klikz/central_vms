package gpu

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func fixture(t *testing.T, name string) []string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	p := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "encoders", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return ParseCodecList(string(b))
}

func TestParseCodecList_Fixtures(t *testing.T) {
	enc := fixture(t, "enc_nvenc.txt")
	if len(enc) < 50 {
		t.Fatalf("encoder ro'yxati juda qisqa: %d", len(enc))
	}
	if !contains(enc, "h264_nvenc") || !contains(enc, "libx264") {
		t.Fatalf("kutilgan encoderlar yo'q: %v", enc[:10])
	}
	// Header satrlari ( " V..... = Video" ) nom sifatida kirmasligi kerak.
	if contains(enc, "=") {
		t.Fatal("header satri parse qilindi")
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestSelectBackend_Matrix(t *testing.T) {
	cases := []struct {
		encF, decF string
		want       Backend
		enc, dec   string
	}{
		{"enc_nvenc.txt", "dec_nvenc.txt", BackendNVENC, "h264_nvenc", "hevc_cuvid"},
		{"enc_vaapi.txt", "dec_vaapi.txt", BackendVAAPI, "h264_vaapi", "hevc_vaapi"},
		{"enc_none.txt", "dec_none.txt", BackendSoft, "libx264", ""},
	}
	for _, c := range cases {
		b, e, d := SelectBackend(fixture(t, c.encF), fixture(t, c.decF))
		if b != c.want || e != c.enc || d != c.dec {
			t.Errorf("%s/%s: got %s/%s/%s want %s/%s/%s", c.encF, c.decF, b, e, d, c.want, c.enc, c.dec)
		}
	}
	// Butunlay yo'q.
	if b, _, _ := SelectBackend(nil, nil); b != BackendNone {
		t.Errorf("bo'sh ro'yxat: %s", b)
	}
	// AMF (AMD): nvenc/vaapi yo'q, amf bor.
	b, e, _ := SelectBackend([]string{"h264_amf", "libx264"}, []string{"hevc"})
	if b != BackendAMF || e != "h264_amf" {
		t.Errorf("amf: %s/%s", b, e)
	}
	// NVENC encoder boru-decoder yo'q → baribir nvenc (decode ffmpeg o'zi hal qiladi).
	b, _, d := SelectBackend([]string{"h264_nvenc"}, nil)
	if b != BackendNVENC || d != "" {
		t.Errorf("nvenc-decodesiz: %s/%q", b, d)
	}
}

func TestOverride(t *testing.T) {
	c := Caps{Backend: BackendNVENC, Encoder: "h264_nvenc", Encoders: fixture(t, "enc_nvenc.txt")}
	if !c.Override("cpu") || c.Backend != BackendSoft || c.Encoder != "libx264" {
		t.Fatalf("cpu override: %+v", c)
	}
	if !c.Override("") || c.Backend != BackendSoft {
		t.Fatal("bo'sh override o'zgartirmasligi kerak")
	}
	if !c.Override("auto") {
		t.Fatal("auto qabul qilinishi kerak")
	}
	// Imkoniyat yo'q → false (aniqlangan qiymat saqlanadi).
	c2 := Caps{Backend: BackendSoft, Encoder: "libx264", Encoders: fixture(t, "enc_none.txt")}
	if c2.Override("nvenc") {
		t.Fatal("nvenc yo'q mashinada override qabul qilindi")
	}
	if c2.Backend != BackendSoft {
		t.Fatalf("override rad etilganda o'zgardi: %+v", c2)
	}
	if c2.Override("nima-dir") {
		t.Fatal("noto'g'ri nom qabul qilindi")
	}
}

func TestDetect_RealFFmpeg(t *testing.T) {
	if _, err := os.Stat("/usr/bin/ffmpeg"); err != nil {
		t.Skip("ffmpeg yo'q")
	}
	c, err := Detect("ffmpeg")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if c.Backend == BackendNone || !strings.Contains(c.Encoder, "h264") {
		t.Fatalf("backend aniqlanmadi: %+v", c)
	}
	t.Logf("detect: backend=%s enc=%s dec=%s gpu=%q", c.Backend, c.Encoder, c.HEVCDecoder, c.GPUName)
}

func TestDetect_MissingFFmpeg(t *testing.T) {
	c, err := Detect("yoq-binar")
	if err == nil || c.Backend != BackendNone {
		t.Fatalf("xato kutilardi: %+v %v", c, err)
	}
}
