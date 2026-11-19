package localui

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"central-vms/client/internal/config"
)

func newTestServer(t *testing.T, pin string) *httptest.Server {
	t.Helper()
	cfg := config.Default()
	cfg.UIPin = pin
	s, err := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), "test-ver", Deps{
		Status: func() Status {
			return Status{Version: "test-ver", CamsTotal: 100, CamsOnline: 97, Publishes: 3, CamsEpoch: 7,
				LastError: "kamera javob bermayapti (cam-7)", LastErrorAt: "2026-11-19T10:00:00Z", LastHeartbeat: "2026-11-19T10:00:05Z"}
		},
		LogLines: func(limit int) []string {
			all := []string{"qator-1", "qator-2", "qator-3"}
			if limit < len(all) {
				return all[limit-1:]
			}
			return all
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(s.mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestIndex_ServedAndHasFiveScreens(t *testing.T) {
	ts := newTestServer(t, "")
	res, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if n := strings.Count(string(body), `data-screen="`); n < 5 {
		t.Fatalf("ekranlar soni %d (>=5 kerak): index.html", n)
	}
	if !strings.Contains(string(body), `<html lang="uz">`) {
		t.Fatal("til o'zbek bo'lishi kerak")
	}
}

func TestStaticAssets(t *testing.T) {
	ts := newTestServer(t, "")
	for _, path := range []string{"/app.css", "/app.js"} {
		res, err := ts.Client().Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d", path, res.StatusCode)
		}
	}
}

func TestHealthz(t *testing.T) {
	ts := newTestServer(t, "")
	res, err := ts.Client().Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]string
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if out["status"] != "ok" || out["version"] != "test-ver" {
		t.Fatalf("healthz: %+v", out)
	}
}

func TestStatusEndpoint(t *testing.T) {
	ts := newTestServer(t, "")
	res, err := ts.Client().Get(ts.URL + "/api/ui/status")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var st Status
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if st.CamsTotal != 100 || st.CamsOnline != 97 || st.Publishes != 3 || st.CamsEpoch != 7 {
		t.Fatalf("status noto'g'ri: %+v", st)
	}
	// §17: holat chizig'i oxirgi xato va heartbeat'ni ko'rsatadi.
	if st.LastError == "" || st.LastErrorAt == "" || st.LastHeartbeat == "" {
		t.Errorf("last_error/last_heartbeat yo'q: %+v", st)
	}

	// GET dan boshqa metod rad etiladi
	res2, err := ts.Client().Post(ts.URL+"/api/ui/status", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if res2.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("metod tekshiruvi ishlamadi: %d", res2.StatusCode)
	}
}

func TestSettings_GetAndPut(t *testing.T) {
	ts := newTestServer(t, "")

	res, err := ts.Client().Get(ts.URL + "/api/ui/settings")
	if err != nil {
		t.Fatal(err)
	}
	var cur Settings
	if err := json.NewDecoder(res.Body).Decode(&cur); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	res.Body.Close()

	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/ui/settings",
		strings.NewReader(`{"server_url":"ws://10.0.0.9:8081","log_level":"debug"}`))
	req.Header.Set("Content-Type", "application/json")
	res2, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("PUT status: %d", res2.StatusCode)
	}
	var got Settings
	if err := json.NewDecoder(res2.Body).Decode(&got); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if got.ServerURL != "ws://10.0.0.9:8081" || got.LogLevel != "debug" {
		t.Fatalf("sozlama yangilanmadi: %+v", got)
	}
}

func TestSettings_RejectsBadValues(t *testing.T) {
	ts := newTestServer(t, "")
	cases := []struct {
		body string
		want int
	}{
		{`{"server_url":"http://x:1"}`, http.StatusBadRequest},
		{`{"log_level":"chat"}`, http.StatusBadRequest},
		{`{`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/ui/settings", strings.NewReader(tc.body))
		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.want {
			t.Fatalf("body=%s status=%d, kutilgan %d", tc.body, res.StatusCode, tc.want)
		}
	}
}

func TestPin_RequiredForWrites(t *testing.T) {
	ts := newTestServer(t, "1234")

	// PIN siz yozish → 401
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/ui/settings", strings.NewReader(`{"log_level":"warn"}`))
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("PIN siz yozish: %d, kutilgan 401", res.StatusCode)
	}

	// PIN bilan → 200
	req2, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/ui/settings", strings.NewReader(`{"log_level":"warn"}`))
	req2.Header.Set("X-Agent-Pin", "1234")
	res2, err := ts.Client().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("PIN bilan yozish: %d", res2.StatusCode)
	}

	// O'qish PIN talab qilmaydi (§10.3: yozuv endpoint'lari)
	res3, err := ts.Client().Get(ts.URL + "/api/ui/status")
	if err != nil {
		t.Fatal(err)
	}
	res3.Body.Close()
	if res3.StatusCode != http.StatusOK {
		t.Fatalf("o'qish: %d", res3.StatusCode)
	}
}

func TestRun_GracefulShutdown(t *testing.T) {
	cfg := config.Default()
	s, err := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), "test", Deps{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	lnErr := make(chan error, 1)
	go func() { lnErr <- s.Run(ctx, "127.0.0.1:18090") }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get("http://127.0.0.1:18090/healthz")
		if err == nil {
			res.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-lnErr:
		if err != nil {
			t.Fatalf("Run xato qaytardi: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run mayin to'xtamadi")
	}
}

func TestRun_BandPort(t *testing.T) {
	cfg := config.Default()
	s, err := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), "test", Deps{})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:18091")
	if err != nil {
		t.Skipf("port band qilib bo'lmadi: %v", err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Run(ctx, "127.0.0.1:18091"); err == nil {
		t.Fatal("band port uchun xato kutildi")
	}
}

// --- C0.14: jurnal, favicon, noma'lum endpoint (browser konsolidagi 404lar) ---

func TestLogEndpoint(t *testing.T) {
	ts := newTestServer(t, "")

	for _, tc := range []struct {
		name, path string
		code       int
		wantCount  int
	}{
		{"limit=2", "/api/ui/log?limit=2", http.StatusOK, 2},
		{"limit yo'q (default 200)", "/api/ui/log", http.StatusOK, 3},
		{"limit=1000 clamp", "/api/ui/log?limit=1000", http.StatusOK, 3},
		{"limit=0 xato", "/api/ui/log?limit=0", http.StatusBadRequest, 0},
		{"limit=abc xato", "/api/ui/log?limit=abc", http.StatusBadRequest, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ts.Client().Get(ts.URL + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.code {
				t.Fatalf("kod=%d, kutilgan %d", res.StatusCode, tc.code)
			}
			if tc.code != http.StatusOK {
				return
			}
			var out struct {
				Lines []string `json:"lines"`
				Count int      `json:"count"`
			}
			if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
				t.Fatal(err)
			}
			if out.Count != tc.wantCount || len(out.Lines) != tc.wantCount {
				t.Fatalf("count=%d lines=%d, kutilgan %d", out.Count, len(out.Lines), tc.wantCount)
			}
		})
	}

	// GET bo'lmagan metod
	res, err := ts.Client().Post(ts.URL+"/api/ui/log", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST kod=%d, kutilgan 405", res.StatusCode)
	}
}

func TestFavicon(t *testing.T) {
	ts := newTestServer(t, "")
	res, err := ts.Client().Get(ts.URL + "/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("favicon kod=%d, kutilgan 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "image/svg+xml" {
		t.Fatalf("content-type=%q", ct)
	}
	b, _ := io.ReadAll(res.Body)
	if !strings.HasPrefix(string(b), "<svg") {
		t.Fatalf("favicon SVG emas: %q", string(b)[:min(40, len(b))])
	}
}

func TestUnknownAPIEndpoint_JSON404(t *testing.T) {
	ts := newTestServer(t, "")
	res, err := ts.Client().Get(ts.URL + "/api/ui/kelajak_endpoint")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("kod=%d, kutilgan 404", res.StatusCode)
	}
	var out map[string]string
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("JSON 404 kutilardi: %v", err)
	}
	if out["error"] != "unknown_endpoint" || out["path"] != "/api/ui/kelajak_endpoint" {
		t.Fatalf("javob: %v", out)
	}
}

func TestUnknownAPIEndpoint_QueryYoqiladi(t *testing.T) {
	ts := newTestServer(t, "")
	// so'roq qatoridagi token javobda ko'rinmasligi kerak (§10.2)
	res, err := ts.Client().Get(ts.URL + "/api/ui/x?token=MAXFIY")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if strings.Contains(string(b), "MAXFIY") {
		t.Fatalf("javobda token qoldi: %s", b)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
