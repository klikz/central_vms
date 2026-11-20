package localui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"central-vms/client/internal/config"
)

func newDepsServer(t *testing.T, pin string, d Deps) *httptest.Server {
	t.Helper()
	cfg := config.Default()
	cfg.UIPin = pin
	s, err := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), "test-ver", d)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.mux)
	t.Cleanup(ts.Close)
	return ts
}

func putJSON(t *testing.T, url string, body any, pin string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if pin != "" {
		req.Header.Set("X-Agent-Pin", pin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func postJSON(t *testing.T, url string, body any, pin string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if pin != "" {
		req.Header.Set("X-Agent-Pin", pin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

// "Save" sozlamani qaytarib berish bilan cheklanmasligi kerak: u runtime'ga
// yetib borishi shart (faylga yozish + qayta ulanish shu yerda boshlanadi).
func TestSettings_SaveReachesRuntime(t *testing.T) {
	var got Settings
	ts := newDepsServer(t, "", Deps{
		SetSettings: func(v Settings) error { got = v; return nil },
	})
	res := putJSON(t, ts.URL+"/api/ui/settings", map[string]string{
		"server_url": "ws://192.168.0.27:8081", "log_level": "debug",
		"name": "  Oxangaron  UI   ",
	}, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("holat: %d", res.StatusCode)
	}
	if got.ServerURL != "ws://192.168.0.27:8081" || got.LogLevel != "debug" {
		t.Fatalf("runtime'ga yetmadi: %+v", got)
	}
}

func TestSettings_SaveRejectedURL(t *testing.T) {
	called := false
	ts := newDepsServer(t, "", Deps{SetSettings: func(Settings) error { called = true; return nil }})
	res := putJSON(t, ts.URL+"/api/ui/settings", map[string]string{"server_url": "http://192.168.0.27:8081"}, "")
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("ws:// bo'lmagan manzil qabul qilindi: %d", res.StatusCode)
	}
	if called {
		t.Fatal("yaroqsiz manzil runtime'ga yuborildi")
	}
}

func TestSettings_SaveFailureReported(t *testing.T) {
	ts := newDepsServer(t, "", Deps{SetSettings: func(Settings) error {
		return fmt.Errorf("diskda yozib bo'lmadi")
	}})
	res := putJSON(t, ts.URL+"/api/ui/settings", map[string]string{"server_url": "ws://x:1"}, "")
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("saqlash xatosi yashirildi: %d", res.StatusCode)
	}
	var body map[string]string
	_ = json.NewDecoder(res.Body).Decode(&body)
	if body["error"] != "save_failed" {
		t.Fatalf("javob: %v", body)
	}
}

// UI orqali ro'yxatdan o'tish: kod 8 xonali son, bo'sh joy/tire bilan
// yozilsa ham qabul bo'ladi.
func TestEnrollEndpoint(t *testing.T) {
	var got string
	ts := newDepsServer(t, "", Deps{Enroll: func(_ context.Context, code string) error {
		got = code
		return nil
	}})
	res := postJSON(t, ts.URL+"/api/ui/enroll", map[string]string{"code": "4173 9265"}, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("holat: %d", res.StatusCode)
	}
	if got != "41739265" {
		t.Fatalf("kod normallashtirilmadi: %q", got)
	}
}

func TestEnrollEndpointErrors(t *testing.T) {
	cases := []struct {
		name   string
		enroll func(context.Context, string) error
		body   map[string]string
		want   int
		wantEr string
	}{
		{"bo'sh kod", func(context.Context, string) error { return nil }, map[string]string{"code": "  "}, http.StatusBadRequest, "code_required"},
		{"allaqachon", func(context.Context, string) error {
			return fmt.Errorf("%w: ag_1", ErrAlreadyEnrolled)
		}, map[string]string{"code": "11112222"}, http.StatusConflict, "already_enrolled"},
		{"server rad etdi", func(context.Context, string) error {
			return fmt.Errorf("enroll: HTTP 401 (bad_code)")
		}, map[string]string{"code": "11112222"}, http.StatusBadGateway, "enroll_failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts := newDepsServer(t, "", Deps{Enroll: c.enroll})
			res := postJSON(t, ts.URL+"/api/ui/enroll", c.body, "")
			if res.StatusCode != c.want {
				t.Fatalf("holat %d, kutilgan %d", res.StatusCode, c.want)
			}
			var body map[string]string
			_ = json.NewDecoder(res.Body).Decode(&body)
			if body["error"] != c.wantEr {
				t.Fatalf("error=%v, kutilgan %q", body, c.wantEr)
			}
		})
	}
}

// PIN qo'yilgan bo'lsa yozuv amallari PIN'siz bajarilmasligi kerak (§10.3).
func TestEnrollEndpoint_RequiresPin(t *testing.T) {
	called := false
	ts := newDepsServer(t, "1234", Deps{Enroll: func(context.Context, string) error { called = true; return nil }})
	res := postJSON(t, ts.URL+"/api/ui/enroll", map[string]string{"code": "11112222"}, "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("PIN'siz qabul qilindi: %d", res.StatusCode)
	}
	if called {
		t.Fatal("PIN'siz enroll bajarildi")
	}
	res = postJSON(t, ts.URL+"/api/ui/enroll", map[string]string{"code": "11112222"}, "1234")
	if res.StatusCode != http.StatusOK || !called {
		t.Fatalf("PIN bilan ishlamadi: %d called=%v", res.StatusCode, called)
	}
}

// Nom — serverda ko'rinadigan nom: UI'da ortiqcha bo'sh joylar tozalanadi
// va so'rov runtime'ga tozalangan holda yetadi.
func TestSettings_NameSanitized(t *testing.T) {
	var got Settings
	ts := newDepsServer(t, "", Deps{SetSettings: func(v Settings) error { got = v; return nil }})
	res := putJSON(t, ts.URL+"/api/ui/settings", map[string]string{"name": "  Oxangaron\tuy   "}, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("holat: %d", res.StatusCode)
	}
	if got.Name != "Oxangaron uy" {
		t.Fatalf("nom tozalanmadi: %q", got.Name)
	}
}
