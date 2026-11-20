package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"central-vms/client/internal/devserver"
)

func TestEnrollURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ws://192.168.1.10:8081", "http://192.168.1.10:8081/api/agent/enroll"},
		{"wss://vms.example.com", "https://vms.example.com/api/agent/enroll"},
		{"http://127.0.0.1:8081/", "http://127.0.0.1:8081/api/agent/enroll"},
		{"ws://host:8081/?x=1#frag", "http://host:8081/api/agent/enroll"},
	}
	for _, c := range cases {
		got, err := enrollURL(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("%s → %s, kutilgan %s", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"ftp://x", "://x", ""} {
		if _, err := enrollURL(bad); err == nil {
			t.Fatalf("%q uchun xato kutilmadi", bad)
		}
	}
}

// stubServer — berilgan javobni qaytaradigan server
func stubServer(t *testing.T, status int, body any) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("metod: %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type: %q", ct)
		}
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["code"] == "" {
			t.Error("code maydoni bo'sh yuborildi")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestEnroll_OK(t *testing.T) {
	ts := stubServer(t, http.StatusOK, map[string]any{
		"agent_id": "ag_9f2c1a7e", "agent_secret": "S3cret",
		"protocol_version": 1, "server_time": "2026-11-19T10:00:00Z",
	})
	res, err := Enroll(context.Background(), ts.URL, EnrollInfo{Code: "KOD", Hostname: "joy-01"})
	if err != nil {
		t.Fatal(err)
	}
	if res.AgentID != "ag_9f2c1a7e" || res.AgentSecret != "S3cret" || res.ProtocolVersion != 1 {
		t.Fatalf("natija: %+v", res)
	}
	if res.ServerTime.Year() != 2026 {
		t.Fatalf("server_time: %v", res.ServerTime)
	}
}

func TestEnroll_Xatolar(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   any
		want   error
	}{
		{"401 bad_code", http.StatusUnauthorized, map[string]string{"error": "bad_code"}, ErrBadCode},
		{"409 already_enrolled", http.StatusConflict, map[string]any{"error": "already_enrolled", "agent_id": "ag_x"}, ErrAlreadyEnrolled},
		{"429 rate_limited", http.StatusTooManyRequests, map[string]string{"error": "rate_limited"}, ErrRateLimited},
		{"500 kutilmagan", http.StatusInternalServerError, map[string]string{"error": "boom"}, ErrBadResponse},
		{"200 lekin bo'sh maydonlar", http.StatusOK, map[string]string{"agent_id": "", "agent_secret": ""}, ErrBadResponse},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts := stubServer(t, c.status, c.body)
			_, err := Enroll(context.Background(), ts.URL, EnrollInfo{Code: "KOD"})
			if err == nil {
				t.Fatal("xato kutilmadi")
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("xato: %v, kutilgan %v", err, c.want)
			}
			var ee *EnrollError
			if !errors.As(err, &ee) {
				t.Fatalf("EnrollError kutilmadi: %T", err)
			}
			if ee.Status != c.status {
				t.Fatalf("Status=%d, kutilgan %d", ee.Status, c.status)
			}
		})
	}
}

func TestEnroll_UlanibBoLMadi(t *testing.T) {
	// eshitib turgan server yo'q (band bo'lmagan port)
	_, err := Enroll(context.Background(), "ws://127.0.0.1:1", EnrollInfo{Code: "KOD"})
	if err == nil {
		t.Fatal("xato kutilmadi")
	}
	var ee *EnrollError
	if !errors.As(err, &ee) || ee.Status != 0 {
		t.Fatalf("kutilgan: EnrollError{Status:0}, oldik: %v", err)
	}
}

func TestEnroll_ContextBekor(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	t.Cleanup(ts.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := Enroll(ctx, ts.URL, EnrollInfo{Code: "KOD"}); err == nil {
		t.Fatal("taymaut xatosi kutilmadi")
	}
}

// Integratsiya: haqiqiy devserver bilan to'liq enroll oqimi (§3.1)
func TestEnroll_DevserverBilan(t *testing.T) {
	srv := devserver.New(devserver.Options{EnrollCode: "KOD-1"})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	first, err := Enroll(context.Background(), ts.URL, EnrollInfo{Code: "KOD-1", Hostname: "joy"})
	if err != nil {
		t.Fatal(err)
	}
	if first.AgentID == "" {
		t.Fatal("agent_id bo'sh")
	}
	// Ikkinchi marta: 409
	if _, err := Enroll(context.Background(), ts.URL, EnrollInfo{Code: "KOD-1"}); !errors.Is(err, ErrAlreadyEnrolled) {
		t.Fatalf("ikkinchi enroll: %v", err)
	}
	// Yolg'on kod: 401
	if _, err := Enroll(context.Background(), ts.URL, EnrollInfo{Code: "YOLGON"}); !errors.Is(err, ErrBadCode) {
		t.Fatalf("yolg'on kod: %v", err)
	}
}
