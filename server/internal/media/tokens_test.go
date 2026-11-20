package media

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"central-vms/server/internal/store"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// advance — testdan vaqtni oldinga surish (menejer tick'lari shuni ko'radi).
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTokens(t *testing.T) (*Tokens, *fakeClock) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "tok.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
	if _, err := store.MigrateUp(db, dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// FK zanjiri: user → client → camera → sessions (media_tokens.session_id).
	mustExec(t, db, `INSERT INTO users (username, pass_hash, role) VALUES ('u1','$x','viewer')`)
	mustExec(t, db, `INSERT INTO clients (name, agent_id, agent_secret_hash) VALUES ('c1','ag_1','h')`)
	mustExec(t, db, `INSERT INTO cameras (id, client_id, name) VALUES ('c1',1,'kam')`) // id='c1'
	mustExec(t, db, `INSERT INTO sessions (id, camera_id, user_id, profile, kind) VALUES ('sess-1','c1',1,'sub','preview')`)
	mustExec(t, db, `INSERT INTO sessions (id, camera_id, user_id, profile, kind) VALUES ('sess-9','c1',1,'main','play')`)
	fc := &fakeClock{t: time.Date(2026, 11, 20, 6, 0, 0, 0, time.UTC)}
	return NewTokens(db, fc), fc
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	res, err := db.Exec(q)
	if err != nil {
		t.Fatalf("seed %q: %v", q, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("seed %q: %d qator", q, n)
	}
}

func TestMintLookupSweep(t *testing.T) {
	tk, fc := newTokens(t)
	raw, exp, err := tk.Mint("sess-1", "cam/ag_1/cam-1", "read", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 40 {
		t.Fatalf("token qisqa: %q", raw)
	}
	if !exp.Equal(fc.t.Add(30 * time.Second)) {
		t.Fatalf("exp: %v", exp)
	}
	rec, err := tk.Lookup(raw)
	if err != nil || rec == nil {
		t.Fatalf("lookup: %v %v", rec, err)
	}
	if rec.SessionID != "sess-1" || rec.Path != "cam/ag_1/cam-1" || rec.Action != "read" {
		t.Fatalf("rec: %+v", rec)
	}
	// Noto'g'ri action mint rad etiladi.
	if _, _, err := tk.Mint("s", "p", "write", time.Minute); err == nil {
		t.Fatal("action=write qabul qilindi")
	}
	// Muddat: 31 s keyin token o'lgan hisoblanadi.
	fc.t = fc.t.Add(31 * time.Second)
	if rec, _ := tk.Lookup(raw); rec != nil {
		t.Fatal("muddati o'tgan token topildi")
	}
	// Sweep tozalaydi.
	n, err := tk.Sweep()
	if err != nil || n != 1 {
		t.Fatalf("sweep: %d %v", n, err)
	}
}

func TestExtractToken(t *testing.T) {
	if got := ExtractToken(HookReq{Token: "A"}, "Bearer B"); got != "A" {
		t.Fatalf("token maydoni: %q", got)
	}
	if got := ExtractToken(HookReq{}, "Bearer B"); got != "B" {
		t.Fatalf("bearer: %q", got)
	}
	if got := ExtractToken(HookReq{Query: "x=1&token=T&y=2"}, ""); got != "T" {
		t.Fatalf("query: %q", got)
	}
	if got := ExtractToken(HookReq{}, "Basic dXNlcjpwYXNz"); got != "" {
		t.Fatalf("basic token emas: %q", got)
	}
}

func postHook(h http.Handler, body any, authHeader string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/internal/auth", bytes.NewReader(b))
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHookAllowDeny(t *testing.T) {
	tk, fc := newTokens(t)
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHook(tk, lg)

	pub, _, _ := tk.Mint("sess-9", "cam/ag_1/c1", "publish", 60*time.Second)
	read, _, _ := tk.Mint("sess-9", "cam/ag_1/c1", "read", 30*time.Second)
	tcPub, _, _ := tk.Mint("", "tc/ag_1/c1", "publish", 60*time.Second)

	// WHIP publish (Bearer) → allow + expireAt.
	rec := postHook(h, HookReq{Action: "publish", Path: "cam/ag_1/c1", Protocol: "webrtc"}, "Bearer "+pub)
	if rec.Code != 200 {
		t.Fatalf("publish: %d %s", rec.Code, rec.Body)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["allow"] != true || resp["expireAt"] == nil {
		t.Fatalf("javob: %s", rec.Body)
	}

	// RTSP read (query token) → allow.
	rec = postHook(h, HookReq{Action: "read", Path: "cam/ag_1/c1", Protocol: "rtsp", Query: "token=" + read}, "")
	if rec.Code != 200 {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}

	// publish token'i read action'ida rad (action mos emas).
	rec = postHook(h, HookReq{Action: "read", Path: "cam/ag_1/c1", Query: "token=" + pub}, "")
	if rec.Code != 403 {
		t.Fatalf("action-mismatch o'tdi: %d", rec.Code)
	}
	// boshqa path'da rad.
	rec = postHook(h, HookReq{Action: "read", Path: "cam/ag_1/c2", Query: "token=" + read}, "")
	if rec.Code != 403 {
		t.Fatalf("path-mismatch o'tdi: %d", rec.Code)
	}
	// tc path — faqat o'z ichki tokeni bilan.
	rec = postHook(h, HookReq{Action: "publish", Path: "tc/ag_1/c1", Protocol: "rtsp", Token: tcPub}, "")
	if rec.Code != 200 {
		t.Fatalf("tc publish: %d %s", rec.Code, rec.Body)
	}
	rec = postHook(h, HookReq{Action: "publish", Path: "tc/ag_1/c1", Token: pub}, "")
	if rec.Code != 403 {
		t.Fatalf("tc'da tashqi token o'tdi: %d", rec.Code)
	}
	// no_token / unknown / expired / bad_action / api.
	if rec = postHook(h, HookReq{Action: "read", Path: "x"}, ""); rec.Code != 403 {
		t.Fatalf("no_token: %d", rec.Code)
	}
	if rec = postHook(h, HookReq{Action: "read", Path: "x", Token: "soxta"}, ""); rec.Code != 403 {
		t.Fatalf("unknown: %d", rec.Code)
	}
	fc.t = fc.t.Add(2 * time.Minute)
	if rec = postHook(h, HookReq{Action: "publish", Path: "cam/ag_1/c1", Token: pub}, ""); rec.Code != 403 {
		t.Fatalf("expired: %d", rec.Code)
	}
	if rec = postHook(h, HookReq{Action: "update", Path: "x", Token: pub}, ""); rec.Code != 403 {
		t.Fatalf("bad_action: %d", rec.Code)
	}
	if rec = postHook(h, HookReq{Action: "api", Path: "", Token: pub}, ""); rec.Code != 403 {
		t.Fatalf("api action: %d", rec.Code)
	}
}
