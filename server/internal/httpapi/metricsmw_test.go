// metricsmw_test.go — HTTP hisoblagichlari: status klasslari, bucketlar,
// WS yo'llarining o'lchovdan tashqarida qolishi (2026-11-22).
package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPMetrics_CountsAndClasses(t *testing.T) {
	m := &HTTPMetrics{}
	h := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusNoContent) // 2xx
		case "/bad":
			http.Error(w, "x", http.StatusBadRequest) // 4xx
		case "/boom":
			http.Error(w, "x", http.StatusInternalServerError) // 5xx
		case "/moved":
			w.WriteHeader(http.StatusFound) // 3xx sinfi
		}
	}))
	for _, p := range []string{"/ok", "/ok", "/bad", "/boom", "/moved"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
	}
	var buf bytes.Buffer
	m.Write(&buf)
	out := buf.String()
	for _, want := range []string{
		"vmsd_http_requests_total 5",
		"vmsd_http_responses_total{class=\"2xx\"} 2",
		"vmsd_http_responses_total{class=\"3xx\"} 1",
		"vmsd_http_responses_total{class=\"4xx\"} 1",
		"vmsd_http_responses_total{class=\"5xx\"} 1",
		"vmsd_http_inflight 0",
		"vmsd_http_duration_seconds_bucket{le=\"+Inf\"} 5",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics'da yo'q: %q\n---\n%s", want, out)
		}
	}
}

func TestHTTPMetrics_SkipsWSPaths(t *testing.T) {
	m := &HTTPMetrics{}
	h := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for _, p := range []string{"/api/agent/ws", "/api/ui/ws"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, p, nil))
	}
	var buf bytes.Buffer
	m.Write(&buf)
	if !strings.Contains(buf.String(), "vmsd_http_requests_total 0") {
		t.Errorf("WS yo'llari o'lchovga tushdi:\n%s", buf.String())
	}
}
