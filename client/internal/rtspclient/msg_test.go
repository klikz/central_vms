package rtspclient

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "rtsp", name))
	if err != nil {
		t.Fatalf("fixture o'qib bo'lmadi %s: %v", name, err)
	}
	return b
}

func TestParseResponseGolden(t *testing.T) {
	tests := []struct {
		file   string
		status int
		cseq   int
		header string // tekshiriladigan sarlavha (lowercase kalit)
		value  string
		body   string // body boshlanishi (bo'sh bo'lmasa)
	}{
		{"resp-options.txt", 200, 1, "public", "GET_PARAMETER", ""},
		{"resp-401-digest.txt", 401, 1, "www-authenticate", "Digest realm=", ""},
		{"resp-401-basic.txt", 401, 2, "www-authenticate", "Basic realm=", ""},
		{"resp-describe.txt", 200, 3, "content-type", "application/sdp", "v=0"},
		{"resp-getparameter.txt", 200, 4, "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			resp, err := ParseResponse(bufio.NewReader(bytes.NewReader(fixture(t, tc.file))))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if resp.Status != tc.status {
				t.Errorf("status = %d, xohlangan %d", resp.Status, tc.status)
			}
			if resp.CSeq != tc.cseq {
				t.Errorf("cseq = %d, xohlangan %d", resp.CSeq, tc.cseq)
			}
			if tc.header != "" && !strings.Contains(resp.Headers[tc.header], tc.value) {
				t.Errorf("header %q = %q, tarkibida %q yo'q", tc.header, resp.Headers[tc.header], tc.value)
			}
			if tc.body != "" && !strings.HasPrefix(string(resp.Body), tc.body) {
				t.Errorf("body = %q, boshlanishi %q bo'lishi kerak", firstN(resp.Body, 40), tc.body)
			}
			if tc.body == "" && tc.file != "resp-describe.txt" && len(resp.Body) != 0 {
				t.Errorf("body bo'sh bo'lishi kerak edi, %d bayt keldi", len(resp.Body))
			}
		})
	}
}

func TestParseResponseDescribeBodyExact(t *testing.T) {
	resp, err := ParseResponse(bufio.NewReader(bytes.NewReader(fixture(t, "resp-describe.txt"))))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Content-Length body uzunligi bilan mos kelishi kerak (io.ReadFull to'ldirgan).
	if cl := resp.Headers["content-length"]; cl != "" {
		want, err := strconv.Atoi(cl)
		if err != nil {
			t.Fatalf("content-length %q: %v", cl, err)
		}
		if len(resp.Body) != want {
			t.Errorf("body %d bayt, Content-Length %d", len(resp.Body), want)
		}
	}
}

func TestParseRequestGolden(t *testing.T) {
	tests := []struct {
		file   string
		method string
		url    string
		cseq   int
	}{
		{"req-describe.txt", "DESCRIBE", "rtsp://192.168.1.108:554/cam/realmonitor?channel=1&subtype=0", 3},
		{"req-getparameter.txt", "GET_PARAMETER", "rtsp://192.168.1.108:554/cam/realmonitor?channel=1&subtype=0", 4},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			req, err := ParseRequest(bufio.NewReader(bytes.NewReader(fixture(t, tc.file))))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if req.Method != tc.method {
				t.Errorf("method = %q, xohlangan %q", req.Method, tc.method)
			}
			if req.URL != tc.url {
				t.Errorf("url = %q, xohlangan %q", req.URL, tc.url)
			}
			if req.CSeq != tc.cseq {
				t.Errorf("cseq = %d, xohlangan %d", req.CSeq, tc.cseq)
			}
			if req.Headers["session"] == "" {
				t.Error("session sarlavhasi yo'q")
			}
		})
	}
}

func TestSerializeRequestGolden(t *testing.T) {
	req := &Request{
		Method: "DESCRIBE",
		URL:    "rtsp://192.168.1.108:554/cam/realmonitor?channel=1&subtype=0",
		CSeq:   3,
		Headers: Headers{
			"accept":     "application/sdp",
			"session":    "12345678",
			"user-agent": "vmsagent/1.0",
		},
	}
	got := req.Serialize()
	want := fixture(t, "req-describe.txt")
	if !bytes.Equal(got, want) {
		t.Errorf("serialize mos emas:\n--- oldi ---\n%s\n--- kutilgan ---\n%s", got, want)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"bom_yomon_birinchi_satr", "GARBAGE\r\n\r\n"},
		{"version_yomon", "HTTP/1.1 200 OK\r\n\r\n"},
		{"status_son_emas", "RTSP/1.0 abc OK\r\n\r\n"},
		{"sarlavha_ikki_nuqta_yoq", "RTSP/1.0 200 OK\r\nBrokenHeader\r\n\r\n"},
		{"body_trunkatsiya", "RTSP/1.0 200 OK\r\nContent-Length: 100\r\n\r\nshort"},
		{"body_chegara_ortdiq", "RTSP/1.0 200 OK\r\nContent-Length: 99999999\r\n\r\nx"},
		{"cseq_son_emas", "RTSP/1.0 200 OK\r\nCSeq: xx\r\n\r\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseResponse(bufio.NewReader(strings.NewReader(tc.input)))
			if err == nil {
				t.Fatal("xato kutilardi, nil qaytdi")
			}
		})
	}
}

func TestParseCSeqYoqBolsaNol(t *testing.T) {
	resp, err := ParseResponse(bufio.NewReader(strings.NewReader("RTSP/1.0 200 OK\r\nServer: Dahua\r\n\r\n")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.CSeq != 0 {
		t.Errorf("cseq = %d, xohlangan 0", resp.CSeq)
	}
	if resp.Reason != "OK" {
		t.Errorf("reason = %q, xohlangan OK", resp.Reason)
	}
}

func TestEOFAniq(t *testing.T) {
	_, err := ParseResponse(bufio.NewReader(strings.NewReader("")))
	if err == nil {
		t.Fatal("xato kutilardi")
	}
}

func firstN(b []byte, n int) string {
	if len(b) < n {
		n = len(b)
	}
	return string(b[:n])
}
