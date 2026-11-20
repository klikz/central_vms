package preview

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func fakeFFmpeg(t *testing.T, fail bool) string {
	t.Helper()
	dir := t.TempDir()
	sh := filepath.Join(dir, "ff")
	body := "#!/bin/sh\nfor a in \"$@\"; do last=\"$a\"; done\nprintf 'JPEGDATA' > \"$last\"\nexit 0\n"
	if fail {
		body = "#!/bin/sh\nprev=\"\"\nfor a in \"$@\"; do if [ \"$prev\" = \"-i\" ]; then url=\"$a\"; fi; prev=\"$a\"; done\necho \"$url: Connection refused\" >&2\nexit 1\n"
	}
	if err := os.WriteFile(sh, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return sh
}

func TestCaptureOK(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := &Capturer{Store: st, Bin: fakeFFmpeg(t, false)}
	if err := c.Capture(context.Background(), "cam1", "rtsp://1.2.3.4/live"); err != nil {
		t.Fatal(err)
	}
	mt, ok := st.Stat("cam1")
	if !ok || mt.IsZero() {
		t.Fatal("snapshot fayli yo'q")
	}
	b, _ := os.ReadFile(st.Path("cam1"))
	if string(b) != "JPEGDATA" {
		t.Fatalf("kontent: %s", b)
	}
	st.Remove("cam1")
	if _, ok := st.Stat("cam1"); ok {
		t.Error("Remove islamadi")
	}
}

func TestCaptureErrorRedactsPassword(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	c := &Capturer{Store: st, Bin: fakeFFmpeg(t, true)}
	err := c.Capture(context.Background(), "cam1", "rtsp://user:sekr@1.2.3.4/live")
	if err == nil {
		t.Fatal("xato kutilardi")
	}
	if contains(err.Error(), "sekr") {
		t.Errorf("parol ochiq qoldi: %v", err)
	}
	if _, ok := st.Stat("cam1"); ok {
		t.Error("xatoda qismiy fayl qolmasligi kerak")
	}
}

func contains(s, sub string) bool {
	return filepath.Base(s) != "" && len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
