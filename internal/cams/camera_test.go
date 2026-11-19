package cams

import (
	"regexp"
	"strings"
	"testing"
)

func TestNewID_UnikalVaUUIDv4(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := NewID()
		if !re.MatchString(id) {
			t.Fatalf("UUIDv4 emas: %q", id)
		}
		if seen[id] {
			t.Fatalf("takrorlangan id: %s", id)
		}
		seen[id] = true
	}
}

func TestSecretRefFor(t *testing.T) {
	for _, p := range []string{"main", "sub"} {
		ref := SecretRefFor("11111111-2222-3333-4444-555555555555", p)
		want := "cam:11111111-2222-3333-4444-555555555555:" + p
		if ref != want {
			t.Fatalf("ref=%q, kutilgan %q", ref, want)
		}
	}
}

func TestCameraValid(t *testing.T) {
	c := Camera{Host: "192.168.10.20"}
	if err := c.Valid(); err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("id bo'lmasa: %v", err)
	}
	c.ID = NewID()
	if err := c.Valid(); err != nil {
		t.Fatalf("to'g'ri kamera rad etildi: %v", err)
	}
	if c.Port != 554 {
		t.Fatalf("port defaulti=%d", c.Port)
	}
	if !strings.Contains(c.MainPath, "realmonitor") {
		t.Fatalf("main_path defaulti: %q", c.MainPath)
	}
	c2 := Camera{ID: NewID()}
	if err := c2.Valid(); err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("host bo'lmasa: %v", err)
	}
	c3 := Camera{ID: NewID(), Host: "h", Port: 8554, MainPath: "/custom"}
	if err := c3.Valid(); err != nil {
		t.Fatal(err)
	}
	if c3.Port != 8554 || c3.MainPath != "/custom" {
		t.Fatalf("berilgan qiymatlar o'zgardi: %+v", c3)
	}
}
