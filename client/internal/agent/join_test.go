package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"central-vms/client/internal/devserver"
)

func TestJoinURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ws://192.168.1.10:8081", "http://192.168.1.10:8081/api/agent/join"},
		{"wss://vms.example.com", "https://vms.example.com/api/agent/join"},
		{"http://127.0.0.1:8081/", "http://127.0.0.1:8081/api/agent/join"},
		{"ws://host:8081/?x=1#frag", "http://host:8081/api/agent/join"},
	}
	for _, c := range cases {
		got, err := joinURL(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("%s → %s, kutilgan %s", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"ftp://x", "://x", ""} {
		if _, err := joinURL(bad); err == nil {
			t.Fatalf("%q uchun xato kutilmadi", bad)
		}
	}
}

func stubJoin(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(ts.Close)
	return ts
}

func joinInfo() JoinInfo {
	return JoinInfo{
		JoinKey: strings.Repeat("ab", 32), AgentSecret: strings.Repeat("9f", 24),
		Hostname: "joy-01", Name: "Oxangaron",
	}
}

// So'rov pending → admin tasdiqlaydi → polling approved: agent_id o'sha
// joyda qaytadi, sir esa serverdan qaytmaydi.
func TestJoin_PendingThenApproved(t *testing.T) {
	var n atomic.Int32
	ts := stubJoin(t, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "pending", "request_id": 7})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "approved", "agent_id": "ag_9f2c1a7e"})
	})
	res, err := Join(context.Background(), ts.URL, joinInfo())
	if err != nil {
		t.Fatalf("birinchi so'rov: %v", err)
	}
	if res.Status != "pending" || res.RequestID != 7 {
		t.Fatalf("natija: %+v", res)
	}
	res, err = Join(context.Background(), ts.URL, joinInfo())
	if err != nil {
		t.Fatalf("polling: %v", err)
	}
	if res.Status != "approved" || res.AgentID != "ag_9f2c1a7e" {
		t.Fatalf("polling natijasi: %+v", res)
	}
}

func TestJoin_Errors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   any
		want   error
	}{
		{"403 rejected", http.StatusForbidden, map[string]string{"error": "rejected", "detail": "bu PC eski"}, ErrRejected},
		{"429 rate_limited", http.StatusTooManyRequests, map[string]string{"error": "rate_limited"}, ErrRateLimited},
		{"400 bad_key", http.StatusBadRequest, map[string]string{"error": "bad_key"}, ErrBadKey},
		{"500 kutilmagan", http.StatusInternalServerError, map[string]string{"error": "boom"}, ErrBadResponse},
		{"200 holat noto'g'ri", http.StatusOK, map[string]string{"status": "nima"}, ErrBadResponse},
		{"200 approved lekin agent_id yo'q", http.StatusOK, map[string]string{"status": "approved"}, ErrBadResponse},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts := stubJoin(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				_ = json.NewEncoder(w).Encode(c.body)
			})
			_, err := Join(context.Background(), ts.URL, joinInfo())
			if err == nil {
				t.Fatal("xato kutilmadi")
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("xato: %v, kutilgan %v", err, c.want)
			}
			var je *JoinError
			if !errors.As(err, &je) {
				t.Fatalf("JoinError kutilmadi: %T", err)
			}
			if je.Status != c.status {
				t.Fatalf("Status=%d, kutilgan %d", je.Status, c.status)
			}
		})
	}
}

func TestJoin_UlanibBoLMadi(t *testing.T) {
	_, err := Join(context.Background(), "ws://127.0.0.1:1", joinInfo())
	if err == nil {
		t.Fatal("xato kutilmadi")
	}
	var je *JoinError
	if !errors.As(err, &je) || je.Status != 0 {
		t.Fatalf("kutilgan: JoinError{Status:0}, oldik: %v", err)
	}
}

func TestJoin_ContextBekor(t *testing.T) {
	ts := stubJoin(t, func(w http.ResponseWriter, _ *http.Request) { time.Sleep(2 * time.Second) })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := Join(ctx, ts.URL, joinInfo()); err == nil {
		t.Fatal("taymaut xatosi kutilmadi")
	}
}

// Integratsiya: devserver bilan to'liq §3 oqimi (so'rov → tasdiq → WS
// uchun sirning o'sha-oyda qolishi).
func TestJoin_DevserverBilan(t *testing.T) {
	srv := devserver.New(devserver.Options{})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	in := joinInfo()
	res, err := Join(context.Background(), ts.URL, in)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if res.Status != "approved" || !strings.HasPrefix(res.AgentID, "ag_") {
		t.Fatalf("natija: %+v", res)
	}
	// Shu kalit bilan qayta so'rov — bir xil agent_id (restart holati).
	res2, err := Join(context.Background(), ts.URL, in)
	if err != nil {
		t.Fatalf("qayta join: %v", err)
	}
	if res2.AgentID != res.AgentID {
		t.Fatalf("agent_id o'zgardi: %q vs %q", res2.AgentID, res.AgentID)
	}
}
