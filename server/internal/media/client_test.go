package media

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

type captured struct {
	mu     sync.Mutex
	method string
	path   string
	body   map[string]any
	user   string
}

func mockMTX(t *testing.T, status int, respBody string) (*httptest.Server, *captured) {
	t.Helper()
	cp := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cp.mu.Lock()
		cp.method, cp.path = r.Method, r.URL.Path
		u, _, _ := r.BasicAuth()
		cp.user = u
		b, _ := io.ReadAll(r.Body)
		if len(b) > 0 {
			_ = json.Unmarshal(b, &cp.body)
		}
		cp.mu.Unlock()
		if status == 404 {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, cp
}

func TestEnsurePath(t *testing.T) {
	srv, cp := mockMTX(t, 200, `{}`)
	c := NewClient(srv.URL)
	if err := c.EnsurePath("cam/ag_1/uuid-x", PathConf{}); err != nil {
		t.Fatalf("EnsurePath: %v", err)
	}
	cp.mu.Lock()
	defer cp.mu.Unlock()
	if cp.method != http.MethodPut || cp.path != "/v3/config/paths/put/cam/ag_1/uuid-x" {
		t.Fatalf("req: %s %s", cp.method, cp.path)
	}
	if cp.body["source"] != "publisher" {
		t.Fatalf("body: %v", cp.body)
	}
	if cp.user != "vmsapi" {
		t.Fatalf("basic auth yuborilmadi: %q", cp.user)
	}
}

func TestDeletePath(t *testing.T) {
	srv, _ := mockMTX(t, 404, "")
	c := NewClient(srv.URL)
	if err := c.DeletePath("cam/x/y"); err != nil {
		t.Fatalf("404 idempotent bo'lishi kerak: %v", err)
	}
}

func TestReaders(t *testing.T) {
	srv, _ := mockMTX(t, 200, `{"itemCount":1,"name":"cam/a/b","readers":3}`)
	c := NewClient(srv.URL)
	n, err := c.Readers("cam/a/b")
	if err != nil || n != 3 {
		t.Fatalf("readers: %d %v", n, err)
	}
	// Path yo'q (404) → xato (poller loglaydi, sessiyani buzmaydi).
	srv404, _ := mockMTX(t, 404, "")
	if _, err := NewClient(srv404.URL).Readers("cam/a/b"); err == nil {
		t.Fatal("404 readersda xato berishi kerak")
	}
}

func TestAlive(t *testing.T) {
	srv, _ := mockMTX(t, 200, `{"conf":{}}`)
	if !NewClient(srv.URL).Alive() {
		t.Fatal("Alive false bo'ldi")
	}
	if NewClient("http://127.0.0.1:1").Alive() {
		t.Fatal("birikmagan manzilda Alive true bo'ldi")
	}
}
