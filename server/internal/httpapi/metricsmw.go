// metricsmw.go — HTTP qatlam hisoblagichlari (2026-11-22 barqarorlik
// ishleri). Avval /metrics faqat gauge'lar berardi (agent/UI soni,
// kamerolar); HTTP so'rovlarining soni/xatosi/davomiyligi ko'rinmas edi —
// "server sekinlashdi" shikoyatini raqam bilan tasdiqlab bo'lmas edi.
//
// Eslatma: WS endpoint'lari (/api/agent/ws, /api/ui/ws) o'lchovdan
// TASHQARI — ular soatlab yashovchi ulanishlar, histogramni buzadi;
// ularning gauge'lari allaqachon hub'larda bor (Count/ConnStats).
package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// bucket'lar sekundda (Prometheus le etiketi uchun, kumulyativ).
// [5]array: len(...) konstanta bo'lishi uchun (massiv emas).
var httpBuckets = [5]float64{0.01, 0.05, 0.2, 1.0, 5.0}

// HTTPMetrics — atomik hisoblagichlar (lock'siz; har so'rovda bir marta).
type HTTPMetrics struct {
	total    atomic.Int64
	c2xx     atomic.Int64
	c3xx     atomic.Int64
	c4xx     atomic.Int64
	c5xx     atomic.Int64
	inflight atomic.Int64
	durSumMs atomic.Int64
	buckets  [len(httpBuckets)]atomic.Int64
}

// Wrap — status va davomiylikni yig'adi. Hijack (WS upgrade) talab
// qilinadigan yo'llar o'ldan tashqarida — shuning uchun statusWriter
// Hijacker'ni emulyatsiya qilmaydi (kerak bo'lsa WS yo'li skip qilinadi).
func (m *HTTPMetrics) Wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agent/ws", "/api/ui/ws":
			h.ServeHTTP(w, r)
			return
		}
		m.total.Add(1)
		m.inflight.Add(1)
		defer m.inflight.Add(-1)
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		h.ServeHTTP(sw, r)
		d := time.Since(start).Seconds()
		m.durSumMs.Add(int64(d * 1000))
		for i, b := range httpBuckets {
			if d <= b {
				m.buckets[i].Add(1)
				break
			}
		}
		switch sw.status / 100 {
		case 2:
			m.c2xx.Add(1)
		case 3:
			m.c3xx.Add(1)
		case 4:
			m.c4xx.Add(1)
		case 5:
			m.c5xx.Add(1)
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Write — Prometheus matni (/metrics ichida chaqiriladi).
func (m *HTTPMetrics) Write(w io.Writer) {
	fmt.Fprintf(w, "vmsd_http_requests_total %d\n", m.total.Load())
	fmt.Fprintf(w, "vmsd_http_inflight %d\n", m.inflight.Load())
	fmt.Fprintf(w, "vmsd_http_responses_total{class=\"2xx\"} %d\n", m.c2xx.Load())
	fmt.Fprintf(w, "vmsd_http_responses_total{class=\"3xx\"} %d\n", m.c3xx.Load())
	fmt.Fprintf(w, "vmsd_http_responses_total{class=\"4xx\"} %d\n", m.c4xx.Load())
	fmt.Fprintf(w, "vmsd_http_responses_total{class=\"5xx\"} %d\n", m.c5xx.Load())
	fmt.Fprintf(w, "vmsd_http_duration_ms_sum %d\n", m.durSumMs.Load())
	cum := int64(0)
	for i, b := range httpBuckets {
		cum += m.buckets[i].Load()
		fmt.Fprintf(w, "vmsd_http_duration_seconds_bucket{le=\"%g\"} %d\n", b, cum)
	}
	fmt.Fprintf(w, "vmsd_http_duration_seconds_bucket{le=\"+Inf\"} %d\n", m.total.Load())
}
