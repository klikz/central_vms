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

func newDepsServer(t *testing.T, d Deps) *httptest.Server {
	t.Helper()
	cfg := config.Default()
	s, err := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), "test-ver", d)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.mux)
	t.Cleanup(ts.Close)
	return ts
}

func putJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
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
	ts := newDepsServer(t, Deps{
		SetSettings: func(v Settings) error { got = v; return nil },
	})
	res := putJSON(t, ts.URL+"/api/ui/settings", map[string]string{
		"server_url": "ws://192.168.0.27:8081", "log_level": "debug",
		"name": "  Oxangaron  UI   ",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("holat: %d", res.StatusCode)
	}
	if got.ServerURL != "ws://192.168.0.27:8081" || got.LogLevel != "debug" {
		t.Fatalf("runtime'ga yetmadi: %+v", got)
	}
}

func TestSettings_SaveRejectedURL(t *testing.T) {
	called := false
	ts := newDepsServer(t, Deps{SetSettings: func(Settings) error { called = true; return nil }})
	res := putJSON(t, ts.URL+"/api/ui/settings", map[string]string{"server_url": "http://192.168.0.27:8081"})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("ws:// bo'lmagan manzil qabul qilindi: %d", res.StatusCode)
	}
	if called {
		t.Fatal("yaroqsiz manzil runtime'ga yuborildi")
	}
}

func TestSettings_SaveFailureReported(t *testing.T) {
	ts := newDepsServer(t, Deps{SetSettings: func(Settings) error {
		return fmt.Errorf("diskda yozib bo'lmadi")
	}})
	res := putJSON(t, ts.URL+"/api/ui/settings", map[string]string{"server_url": "ws://x:1"})
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("saqlash xatosi yashirildi: %d", res.StatusCode)
	}
	var body map[string]string
	_ = json.NewDecoder(res.Body).Decode(&body)
	if body["error"] != "save_failed" {
		t.Fatalf("javob: %v", body)
	}
}

// UI tugmasi: §3.1 ulanish so'rovi (kod so'ralmaydi).
func TestJoinEndpoint(t *testing.T) {
	called := false
	ts := newDepsServer(t, Deps{JoinNow: func(context.Context) error {
		called = true
		return nil
	}})
	res := postJSON(t, ts.URL+"/api/ui/join", map[string]string{})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("holat: %d", res.StatusCode)
	}
	if !called {
		t.Fatal("join chaqirilmadi")
	}
}

func TestJoinEndpointErrors(t *testing.T) {
	cases := []struct {
		name   string
		join   func(context.Context) error
		want   int
		wantEr string
	}{
		{"allaqachon ulangan", func(context.Context) error {
			return fmt.Errorf("%w: ag_1", ErrAlreadyEnrolled)
		}, http.StatusConflict, "already_enrolled"},
		{"server rad etdi", func(context.Context) error {
			return fmt.Errorf("%w: HTTP 403 (rejected)", ErrJoinRejected)
		}, http.StatusForbidden, "rejected"},
		{"urinishlar chegarasi", func(context.Context) error {
			return fmt.Errorf("%w: HTTP 429", ErrRateLimited)
		}, http.StatusTooManyRequests, "rate_limited"},
		{"boshqa xato", func(context.Context) error {
			return fmt.Errorf("join: HTTP 0 (ulanib bo'lmadi)")
		}, http.StatusBadGateway, "join_failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts := newDepsServer(t, Deps{JoinNow: c.join})
			res := postJSON(t, ts.URL+"/api/ui/join", map[string]string{})
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

// 1.0-draft.10: PIN olib tashlandi — yozuv amallari PIN'siz bajariladi
// (operator talabi; eski §10.3 qoidasi bekor qilindi).
// Eski konfiguratsiyada `ui_pin` to'lgan bo'lsa ham (1.0-draft.10: kalit
// o'qiladi, lekin ishlatilmaydi) join so'rovi PIN'siz o'tadi.
func TestJoinEndpoint_LegacyPinIgnored(t *testing.T) {
	called := false
	cfg := config.Default()
	cfg.UIPin = "1234"
	s, err := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), "test-ver",
		Deps{JoinNow: func(context.Context) error { called = true; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.mux)
	t.Cleanup(ts.Close)
	res := postJSON(t, ts.URL+"/api/ui/join", map[string]string{})
	if res.StatusCode != http.StatusOK || !called {
		t.Fatalf("eski ui_pin yozuvi join'ni to'sib qo'ydi: %d called=%v", res.StatusCode, called)
	}
}
