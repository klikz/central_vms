package auth

import (
	"strings"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestHashVerify(t *testing.T) {
	h, err := HashPassword("Salom-VMS-2026!")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$") {
		t.Fatalf("PHC formati buzilgan: %s", h)
	}
	ok, err := VerifyPassword("Salom-VMS-2026!", h)
	if err != nil || !ok {
		t.Fatalf("verify: ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword("notogri-parol", h)
	if err != nil {
		t.Fatalf("verify noto'g'ri parol xato berdi: %v", err)
	}
	if ok {
		t.Fatal("noto'g'ri parol o'tib ketdi")
	}
	// Har hash turli salt bilan.
	h2, _ := HashPassword("Salom-VMS-2026!")
	if h == h2 {
		t.Fatal("salt takrorlandi")
	}
}

func TestVerify_BadFormat(t *testing.T) {
	for _, bad := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$AAA$BBB",
		"$argon2id$v=19$badparams$AAA$BBB", "$argon2id$v=19$m=1,t=1,p=1$not-base64!!$BBB"} {
		if ok, err := VerifyPassword("x", bad); err == nil || ok {
			t.Errorf("format %q: ok=%v err=%v — xato kutilardi", bad, ok, err)
		}
	}
}
