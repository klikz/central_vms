package auth

import (
	"testing"
	"time"
)

// TestMain (goleak) pass_test.go da — paket bo'ylab bitta.

type fakeClock struct{ t time.Time }

func (f *fakeClock) Now() time.Time      { return f.t }
func (f *fakeClock) Add(d time.Duration) { f.t = f.t.Add(d) }

func newTestTokens(t *testing.T) (*Tokens, *fakeClock) {
	t.Helper()
	fc := &fakeClock{t: time.Date(2026, 11, 20, 5, 0, 0, 0, time.UTC)}
	tk, err := NewTokens("0123456789abcdef0123456789abcdef", 15*time.Minute, 7*24*time.Hour, fc)
	if err != nil {
		t.Fatalf("NewTokens: %v", err)
	}
	return tk, fc
}

func TestAccessRoundTrip(t *testing.T) {
	tk, _ := newTestTokens(t)
	tok, exp, err := tk.IssueAccess(7, "admin1", "admin")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !exp.Equal(time.Date(2026, 11, 20, 5, 15, 0, 0, time.UTC)) {
		t.Fatalf("exp=%s (15 min bo'lishi kerak)", exp)
	}
	c, err := tk.Verify(tok, "access")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.Sub != 7 || c.Role != "admin" || c.Typ != "access" {
		t.Fatalf("claims: %+v", c)
	}
}

func TestAccessExpiry_FakeClock(t *testing.T) {
	tk, fc := newTestTokens(t)
	tok, _, _ := tk.IssueAccess(1, "u", "viewer")
	fc.Add(14*time.Minute + 59*time.Second)
	if _, err := tk.Verify(tok, "access"); err != nil {
		t.Fatalf("14:59 da token yaroqli bo'lishi kerak: %v", err)
	}
	fc.Add(2 * time.Second)
	if _, err := tk.Verify(tok, "access"); err == nil {
		t.Fatal("15 min+ keyin muddat tugashi kerak")
	}
}

func TestWrongTypeAndTamper(t *testing.T) {
	tk, _ := newTestTokens(t)
	acc, _, _ := tk.IssueAccess(1, "u", "viewer")
	ref, _, _ := tk.IssueRefresh(1, "u", "viewer")

	if _, err := tk.Verify(acc, "refresh"); err == nil {
		t.Fatal("access'ni refresh deb qabul qilmadi")
	}
	if _, err := tk.Verify(ref, "access"); err == nil {
		t.Fatal("refresh'ni access deb qabul qilmadi")
	}
	// Boshqa kaliz bilan imzolangan token qabul qilmasligi kerak.
	tk2, _ := NewTokens("boshqa-secret-0123456789abcdef0123", 15*time.Minute, time.Hour, nil)
	if _, err := tk2.Verify(acc, "access"); err == nil {
		t.Fatal("begona imzo qabul qilindi")
	}
	// Soxta payload (role almashtirilgan).
	if _, err := tk.Verify(acc+"x", "access"); err == nil {
		t.Fatal("buzilgan token qabul qilindi")
	}
}

func TestRefreshHasJTI(t *testing.T) {
	tk, _ := newTestTokens(t)
	tok, _, _ := tk.IssueRefresh(3, "viewer1", "viewer")
	c, err := tk.Verify(tok, "refresh")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.JTI == "" {
		t.Fatal("refresh jti'siz — rotatsiya kuzatib bo'lmaydi")
	}
}

func TestSecretEqual(t *testing.T) {
	if !SecretEqual("abc", "abc") || SecretEqual("abc", "abd") {
		t.Fatal("SecretEqual xato")
	}
}
