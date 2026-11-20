package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRate_Window(t *testing.T) {
	fc := &fakeClock{t: time.Date(2026, 11, 20, 5, 0, 0, 0, time.UTC)}
	r := NewRate(5, 5*time.Minute, fc)
	for i := range 5 {
		if ok, _ := r.Allow("10.0.0.1"); !ok {
			t.Fatalf("urinish %d: ruxsat berildi deb kutilmagan edi", i+1)
		}
	}
	ok, retry := r.Allow("10.0.0.1")
	if ok || retry <= 0 || retry > 300 {
		t.Fatalf("6-urinish: ok=%v retry=%d", ok, retry)
	}
	// Boshqa IP ga ta'sir yo'q.
	if ok, _ := r.Allow("10.0.0.2"); !ok {
		t.Fatal("boshqa IP bloklandi")
	}
	fc.Add(5 * time.Minute)
	if ok, _ := r.Allow("10.0.0.1"); !ok {
		t.Fatal("oyna tugagach ruxsat berilmadi")
	}
}

func TestRate_Reset(t *testing.T) {
	fc := &fakeClock{t: time.Date(2026, 11, 20, 5, 0, 0, 0, time.UTC)}
	r := NewRate(2, 5*time.Minute, fc)
	r.Allow("ip")
	r.Allow("ip")
	if ok, _ := r.Allow("ip"); ok {
		t.Fatal("limitdan oshdi")
	}
	r.Reset("ip")
	if ok, _ := r.Allow("ip"); !ok {
		t.Fatal("Reset keyin ruxsat berilmadi")
	}
}

func TestMiddlewareAndRole(t *testing.T) {
	tk, _ := newTestTokens(t)
	acc, _, _ := tk.IssueAccess(1, "admin1", "admin")
	view, _, _ := tk.IssueAccess(2, "viewer1", "viewer")
	ref, _, _ := tk.IssueRefresh(1, "admin1", "admin")

	hello := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := ClaimsFrom(r)
		if !ok {
			t.Error("claims context'da yo'q")
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(c.Role))
	})
	mw := tk.Middleware(RequireRole("admin", hello))

	get := func(auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		return rec
	}
	if rec := get(""); rec.Code != 401 {
		t.Fatalf("token yo'q: %d", rec.Code)
	}
	if rec := get("Bearer " + ref); rec.Code != 401 { // refresh access emas
		t.Fatalf("refresh token: %d", rec.Code)
	}
	if rec := get("Bearer " + view); rec.Code != 403 {
		t.Fatalf("viewer admin endpoint'da: %d", rec.Code)
	}
	if rec := get("Bearer soxta.token.here"); rec.Code != 401 {
		t.Fatalf("soxta token: %d", rec.Code)
	}
	rec := get("Bearer " + acc)
	if rec.Code != 200 || rec.Body.String() != "admin" {
		t.Fatalf("admin: %d %q", rec.Code, rec.Body.String())
	}
}
