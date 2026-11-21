package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"central-vms/client/internal/clock"
	"central-vms/client/internal/devserver"
	"central-vms/client/internal/proto"
)

// devEnv — devserver + ws manzil + enroll qilinggan agent ma'lumotlari
// test uchun doimiy kalit/sir (§3.1: sirni agent o'zi yaratadi).
var secretOf = strings.Repeat("9f", 24)

func testJoinInfo() JoinInfo {
	return JoinInfo{
		JoinKey: strings.Repeat("ab", 32), AgentSecret: secretOf,
		Hostname: "test-joy", Name: "Test-joy",
	}
}

type devEnv struct {
	srv     *devserver.Server
	wsURL   string
	agentID string
	secret  string
	dials   *atomic.Int64
}

func startDev(t *testing.T, o devserver.Options) *devEnv {
	t.Helper()
	srv := devserver.New(o)
	base := srv.Handler()
	var dials atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/agent/ws") {
			dials.Add(1)
		}
		base.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)

	id, secret, code := joinViaHTTP(t, ts.URL)
	if code != http.StatusOK {
		t.Fatalf("enroll: %d", code)
	}
	return &devEnv{
		srv:     srv,
		wsURL:   "ws" + strings.TrimPrefix(ts.URL, "http"),
		agentID: id,
		secret:  secret,
		dials:   &dials,
	}
}

// joinViaHTTP — §3: so‘rov yuborib, tasdiqlangan agent_id ni oladi
// (devserver tasdiqni avtomatik bajaradi).
func joinViaHTTP(t *testing.T, url string) (agentID, secret string, status int) {
	t.Helper()
	res, err := Join(context.Background(), url, testJoinInfo())
	if err != nil {
		var je *JoinError
		if errors.As(err, &je) {
			return "", "", je.Status
		}
		t.Fatal(err)
	}
	return res.AgentID, secretOf, http.StatusOK
}

func baseCfg(env *devEnv, c clock.Clock) SessionConfig {
	return SessionConfig{
		ServerURL: env.wsURL, AgentID: env.agentID, Secret: env.secret,
		AgentVersion: "0.3.1", OS: "Ubuntu 24.04", Hostname: "joy-01", LanIP: "10.0.0.5",
		BootID: "boot-1", CamsEpoch: 7,
		Caps:  proto.Caps{GPU: "nvenc", CPUCores: 8, RAMMB: 8192, FFmpegVersion: "n9.0.2", HEVCDecode: true},
		Clock: c,
	}
}

func TestRunOnce_OK(t *testing.T) {
	var gotHello proto.Hello
	var helloOK atomic.Bool
	env := startDev(t, devserver.Options{OnHello: func(h proto.Hello) {
		gotHello = h
		helloOK.Store(true)
	}})

	var gotWelcome proto.Welcome
	cfg := baseCfg(env, clock.NewFake(time.Now()))
	cfg.OnWelcome = func(w proto.Welcome) { gotWelcome = w }

	s := NewSession(cfg)
	wel, err := s.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !helloOK.Load() {
		t.Fatal("hello yetib bormadi")
	}
	if gotHello.ProtocolVersion != proto.ProtocolVersion {
		t.Fatalf("protocol_version=%d", gotHello.ProtocolVersion)
	}
	if gotHello.CamsEpoch != 7 || gotHello.BootID != "boot-1" || gotHello.Hostname != "joy-01" {
		t.Fatalf("hello maydonlari: %+v", gotHello)
	}
	if gotHello.Caps.GPU != "nvenc" || gotHello.Caps.CPUCores != 8 || !gotHello.Caps.HEVCDecode {
		t.Fatalf("caps: %+v", gotHello.Caps)
	}
	if !wel.Compat.OK || gotWelcome.NeedsFullSync != wel.NeedsFullSync {
		t.Fatalf("welcome: %+v", wel)
	}
	if wel.Limits.MaxUploads == nil || *wel.Limits.MaxUploads != 8 {
		t.Fatalf("limits: %+v", wel.Limits)
	}
}

func TestRunOnce_BadSecret(t *testing.T) {
	env := startDev(t, devserver.Options{})
	cfg := baseCfg(env, clock.NewFake(time.Now()))
	cfg.Secret = "NOTO'G'RI"

	_, err := NewSession(cfg).RunOnce(context.Background())
	if err == nil {
		t.Fatal("xato kutilmadi")
	}
	var ce *ConnError
	if !errors.As(err, &ce) || ce.Code != 4003 {
		t.Fatalf("xato: %v (ConnError 4003 kutilmadi)", err)
	}
	if p := profileForError(err); p != ProfileRevoked {
		t.Fatalf("profil=%v, kutilgan ProfileRevoked", p)
	}
}

func TestRunOnce_VersionMismatch(t *testing.T) {
	// Server faqat 2..3 versiyalarni qo'llab-quvvatlaydi → agent (v1) mos emas
	env := startDev(t, devserver.Options{MinVersion: 2, MaxVersion: 3, ServerVer: "1.9.0"})
	cfg := baseCfg(env, clock.NewFake(time.Now()))
	var compat *proto.Welcome
	cfg.OnCompat = func(w proto.Welcome) { compat = &w } // C4.7: UI banneri ma'lumoti

	_, err := NewSession(cfg).RunOnce(context.Background())
	if !errors.Is(err, ErrCompat) {
		t.Fatalf("xato: %v, kutilgan ErrCompat", err)
	}
	if compat == nil || compat.Compat.OK {
		t.Fatal("OnCompat chaqirilmadi — banner ma'lumoti yo'q")
	}
	if compat.Compat.MinSupported == nil || *compat.Compat.MinSupported != 2 ||
		compat.Compat.MaxSupported == nil || *compat.Compat.MaxSupported != 3 {
		t.Errorf("compat oralig'i noto'g'ri: %+v", compat.Compat)
	}
	if p := profileForError(err); p != ProfileVersion {
		t.Fatalf("profil=%v, kutilgan ProfileVersion", p)
	}
}

func TestRunOnce_BadURL(t *testing.T) {
	cfg := baseCfg(&devEnv{wsURL: "http://not-ws", agentID: "ag_x"}, clock.NewFake(time.Now()))
	if _, err := NewSession(cfg).RunOnce(context.Background()); err == nil {
		t.Fatal("sxema xatosi kutilmadi")
	}
}

// C1.4/C1.5: uzilishdan keyin qayta ulanish — fake clock bilan (haqiqiy uyqu yo'q)
func TestRun_Reconnect_FakeClock(t *testing.T) {
	env := startDev(t, devserver.Options{})
	cfg := baseCfg(env, clock.NewFake(time.Now()))
	cfg.Secret = "NOTO'G'RI" // har urinishda 4003 → ProfileRevoked (60 s)

	fc := cfg.Clock.(*clock.Fake)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := NewSession(cfg)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	waitDials := func(n int64, timeout time.Duration) {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for env.dials.Load() < n && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if env.dials.Load() < n {
			t.Fatalf("ulanishlar soni=%d, kutilgan %d", env.dials.Load(), n)
		}
	}

	waitDials(1, 3*time.Second)
	// Backoff 60 s — fake clock'ni oldinga suramiz (testda uyqu yo'q)
	fc.Advance(60 * time.Second)
	waitDials(2, 3*time.Second)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run xato qaytardi: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run ctx bekor qilinishini sezmadi")
	}
}

// C1.4: muvaffaqiyatli ulanishdan keyin idle heartbeat (§2) yuboriladi
func TestServe_HeartbeatAndGracefulClose(t *testing.T) {
	var mu sync.Mutex
	hbCount := 0
	closedCode := -1
	env := startDev(t, devserver.Options{
		OnMessage: func(_ string, e proto.Envelope) {
			if e.Type == proto.MsgHB {
				mu.Lock()
				hbCount++
				mu.Unlock()
			}
		},
		OnClose: func(_ string, code int) {
			mu.Lock()
			closedCode = code
			mu.Unlock()
		},
	})

	cfg := baseCfg(env, clock.Real{})
	cfg.HbInterval = 30 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())

	s := NewSession(cfg)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := hbCount
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	got := hbCount
	mu.Unlock()
	if got < 3 {
		t.Fatalf("heartbeatlar soni=%d, kutilgan ≥3", got)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run mayin to'xtamadi")
	}
	// §9.4: agent mayin yopilganda WS kodi 1001 (Going Away) bo'ladi.
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	code := closedCode
	mu.Unlock()
	if code != 1001 {
		t.Fatalf("yopinish kodi=%d, kutilgan 1001 (§9.4)", code)
	}
}

func TestProfileForCloseCode(t *testing.T) {
	cases := []struct {
		code int
		want Profile
	}{
		{4001, ProfileVersion},
		{4003, ProfileRevoked},
		{4004, ProfileRevoked},
		{4002, ProfileNormal},
		{1006, ProfileNormal},
		{0, ProfileNormal},
	}
	for _, c := range cases {
		if got := profileForCloseCode(c.code); got != c.want {
			t.Fatalf("kod %d → %v, kutilgan %v", c.code, got, c.want)
		}
	}
}

// Agentning o'zi yuboradigan WS ping o'qish muddatini uzaytiradi: server
// umuman ping yubormasa ham (eski versiya) ulanish tirik qoladi
// (2026-11-21 audit topilmasi).
func TestServe_AgentPingKeepsConnection(t *testing.T) {
	env := startDev(t, devserver.Options{})
	cfg := baseCfg(env, clock.Real{})
	cfg.ReadTO = 400 * time.Millisecond
	cfg.PingEvery = 150 * time.Millisecond
	cfg.HbInterval = time.Hour // heartbeat ishtirmaymiz — faqat ping
	s := NewSession(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res := make(chan error, 1)
	go func() { res <- s.connectAndServe(ctx) }()
	select {
	case err := <-res:
		t.Fatalf("ulanish uzildi (ping saqlab qolishi kerak edi): %v", err)
	case <-time.After(1500 * time.Millisecond):
	}
	cancel()
	select {
	case <-res:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel'dan keyin serve qaytmadi")
	}
}

// Nazorat: ping o'chiq, server ham ping yubormaydi → o'qish muddati tugaydi.
func TestServe_ReadTimeoutWithoutPing(t *testing.T) {
	env := startDev(t, devserver.Options{})
	cfg := baseCfg(env, clock.Real{})
	cfg.ReadTO = 300 * time.Millisecond
	cfg.PingEvery = time.Hour
	cfg.HbInterval = time.Hour
	s := NewSession(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res := make(chan error, 1)
	go func() { res <- s.connectAndServe(ctx) }()
	select {
	case err := <-res:
		if err == nil {
			t.Fatal("xato kutilmadi")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("o'qish muddati tugamadi (ping islayaptimi?)")
	}
}
