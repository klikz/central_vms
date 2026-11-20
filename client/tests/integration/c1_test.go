// Package integration — C1 qabul mezoni (G2 gate): enroll → ulanish → xabar almashinuvi
// → uzilish → backoff → qayta ulanish (qayta enroll'siz).
//
// Ishga tushirish: make itest SCENARIO=c1
package integration

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"central-vms/client/internal/agent"
	"central-vms/client/internal/clock"
	"central-vms/client/internal/devserver"
	"central-vms/client/internal/keystore"
	"central-vms/client/internal/proto"
)

type harness struct {
	dev     *devserver.Server
	srv     *httptest.Server
	wsURL   string
	dials   *atomic.Int64
	tempDir string

	// agent → server kelgan xabarlar (hb, cams_sync, secret_rotated, …)
	rec *recorder
}

// seenCount — agent yuborgan xabar turi soni
func (h *harness) seenCount(typ string) int { return h.rec.count(typ) }

func (h *harness) waitSeen(typ string, min int, d time.Duration) int {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if n := h.seenCount(typ); n >= min {
			return n
		}
		time.Sleep(20 * time.Millisecond)
	}
	return h.seenCount(typ)
}

// waitFor — shart bajarilgunicha yoki muddat tugaguncha kutadi (test sinxronizatsiyasi)
func (h *harness) waitFor(cond func() bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// recorder — agent→server xabarlarini tur bo'yicha sanaydi
type recorder struct {
	mu sync.Mutex
	m  map[string]int
}

func (r *recorder) add(typ string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m == nil {
		r.m = map[string]int{}
	}
	r.m[typ]++
}

func (r *recorder) count(typ string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.m[typ]
}

func newHarness(t *testing.T, o devserver.Options) *harness {
	t.Helper()
	dir := t.TempDir()
	if o.EnrollCode == "" {
		o.EnrollCode = "KOD-1"
	}
	if o.StateFile == "" {
		o.StateFile = filepath.Join(dir, "devserver.state.json")
	}
	if o.PingInterval == 0 {
		o.PingInterval = 300 * time.Millisecond
	}
	rec := &recorder{}
	userOnMsg := o.OnMessage
	o.OnMessage = func(agentID string, e proto.Envelope) {
		rec.add(e.Type)
		if userOnMsg != nil {
			userOnMsg(agentID, e)
		}
	}
	dev := devserver.New(o)
	base := dev.Handler()
	var dials atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/agent/ws") {
			dials.Add(1)
		}
		base.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return &harness{
		dev: dev, srv: srv,
		wsURL:   "ws" + strings.TrimPrefix(srv.URL, "http"),
		dials:   &dials,
		tempDir: dir,
		rec:     rec,
	}
}

func TestC1_EnrollConnectRotateReconnect(t *testing.T) {
	h := newHarness(t, devserver.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- 1) enroll (§3.1) ---
	res, err := agent.Enroll(ctx, h.srv.URL, agent.EnrollInfo{
		Code: "KOD-1", Hostname: "joy-01", OS: "Ubuntu 24.04", LanIP: "192.168.10.20", AgentVersion: "0.4.0",
	})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if !strings.HasPrefix(res.AgentID, "ag_") || res.AgentSecret == "" {
		t.Fatalf("enroll natija: %+v", res)
	}

	// --- 2) secret keystore'ga saqlanadi (§10.1) ---
	keyPath := filepath.Join(h.tempDir, "agent.key")
	if _, err := keystore.NewKeyFile(keyPath); err != nil {
		t.Fatal(err)
	}
	ks, err := keystore.Open(keyPath, filepath.Join(h.tempDir, "secrets.bin"))
	if err != nil {
		t.Fatal(err)
	}
	ks.Put(keystore.RefAgentSecret, res.AgentSecret)
	if err := ks.Save(); err != nil {
		t.Fatal(err)
	}

	// --- 3) ulanish + welcome (§3.3, §4.1) ---
	var mu sync.Mutex
	gotTypes := []string{}
	// Ulanish fazasi: Real soat (hb/ping haqiqiy davrda). Backoff fazasi uchun
	// alohida fake clock ishlatiladi (testda uyqu yo'q).
	fc := clock.NewFake(time.Now())
	cfg := agent.SessionConfig{
		ServerURL: h.wsURL, AgentID: res.AgentID, Secret: res.AgentSecret,
		AgentVersion: "0.4.0", Hostname: "joy-01", BootID: "boot-1", CamsEpoch: 3,
		HbInterval: 60 * time.Millisecond, Clock: clock.Real{},
		OnMessage: func(e proto.Envelope) {
			mu.Lock()
			gotTypes = append(gotTypes, e.Type)
			mu.Unlock()
		},
	}
	sess := agent.NewSession(cfg)
	// Dispatch: serverdan kelgan buyruqlarni agent bajarsin (C1.9)
	hnd := &agent.Handlers{KS: ks, Log: discardLog(), Sess: sess}
	sess.SetOnMessage(hnd.Handle)

	done := make(chan error, 1)
	go func() { done <- sess.Run(ctx) }()

	if !h.dev.WaitConnected(res.AgentID, 5*time.Second) {
		t.Fatal("agent ulanmadi (welcome o'qilmadi)")
	}

	// --- 4) heartbeat kelayapti (§2) — agent→server ---
	if n := h.waitSeen(proto.MsgHB, 2, 3*time.Second); n < 2 {
		t.Fatalf("hb soni=%d, kutilgan ≥2", n)
	}

	// --- 5) server tomonidan secret_rotate (§5.6) → agent ok:true ---
	if err := h.dev.Send(res.AgentID, proto.MsgSecretRotate, proto.SecretRotate{NewSecret: "YANGI-SECRET", GraceS: 5}); err != nil {
		t.Fatalf("secret_rotate yuborib bo'lmadi: %v", err)
	}
	if n := h.waitSeen(proto.MsgSecretRotated, 1, 3*time.Second); n < 1 {
		t.Fatal("secret_rotated javobi kelmadi (agent→server)")
	}
	// Yangi secret diskda (qayta ochib tekshiramiz)
	ks2, err := keystore.Open(keyPath, filepath.Join(h.tempDir, "secrets.bin"))
	if err != nil {
		t.Fatalf("qayta ochib bo'lmadi: %v", err)
	}
	if v, ok := ks2.Get(keystore.RefAgentSecret); !ok || v != "YANGI-SECRET" {
		t.Fatalf("yangi secret saqlanmagan: %q ok=%v", v, ok)
	}

	// --- 6) uzilish: serverni yopamiz → OnDisconnect + backoff (§2/§14) ---
	disconnects := make(chan error, 4)
	cfg2 := cfg
	cfg2.Clock = fc
	cfg2.OnDisconnect = func(err error) {
		select {
		case disconnects <- err:
		default:
		}
	}
	sess2 := agent.NewSession(cfg2)
	done2 := make(chan error, 1)
	go func() { done2 <- sess2.Run(ctx) }()
	if !h.dev.WaitConnected(res.AgentID, 5*time.Second) {
		t.Fatal("ikkinchi sessiya ulanmadi")
	}
	h.srv.Close() // server o'ldi

	select {
	case err := <-disconnects:
		if err == nil {
			t.Fatal("OnDisconnect xato bilan kelishi kerak edi")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnDisconnect chaqirilmadi")
	}

	// Backoff: fake clock'ni suramiz — urinishlar 1s, 2s, 4s ketma-ketligida o'sadi.
	// Har bir muvaffaqiyatsiz urinish OnDisconnect'ni chaqiradi (server allaqachon
	// yopilgani uchun HTTP handler ishlamaydi — shu hodisa sanaladi).
	got := 0
	deadline := time.Now().Add(5 * time.Second)
	for got < 2 && time.Now().Before(deadline) {
		fc.Advance(4 * time.Second)
		select {
		case <-disconnects:
			got++
		case <-time.After(150 * time.Millisecond):
		}
	}
	if got < 2 {
		t.Fatalf("qayta urinishlar soni=%d, kutilgan ≥2 (server o'lik)", got)
	}

	// --- 7) qayta ulanish QAYTA ENROLL'SIZ (secret keystore'da) ---
	h2 := newHarnessWithState(t, h.tempDir) // yangi mock, eski state fayli bilan
	sess3 := agent.NewSession(agent.SessionConfig{
		ServerURL: h2.wsURL, AgentID: res.AgentID, Secret: "YANGI-SECRET",
		AgentVersion: "0.4.0", HbInterval: time.Second, Clock: clock.NewFake(time.Now()),
	})
	wel, err := sess3.RunOnce(ctx)
	if err != nil {
		t.Fatalf("qayta ulanish (enroll'siz) ishlamadi: %v", err)
	}
	if !wel.Compat.OK {
		t.Fatalf("compat: %+v", wel.Compat)
	}
	if !h2.dev.WaitConnected(res.AgentID, 3*time.Second) {
		t.Fatal("yangi mock agentni tanimadi")
	}
	_ = sess2
	_ = done
	_ = done2
}

// newHarnessWithState — xuddi shu state faylni ishlatgan holda yangi mock (server "qayta boshlangan")
func newHarnessWithState(t *testing.T, dir string) *harness {
	return newHarness(t, devserver.Options{StateFile: filepath.Join(dir, "devserver.state.json")})
}

func waitForType(mu *sync.Mutex, types []string, want string, min int, d time.Duration) int {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := 0
		for _, t := range types {
			if t == want {
				n++
			}
		}
		mu.Unlock()
		if n >= min {
			return n
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	n := 0
	for _, t := range types {
		if t == want {
			n++
		}
	}
	return n
}

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}
