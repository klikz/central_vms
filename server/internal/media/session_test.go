// S2.4 menejer testlari: holat mashinasi, dedup, taymautlar, limitlar —
// hammasi fake clock/fake agent/fake MTX bilan (deterministik).
package media

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"central-vms/server/internal/gpu"
	"central-vms/server/internal/proto"
	"central-vms/server/internal/registry"
	"central-vms/server/internal/store"
)

type fakeSender struct {
	mu     sync.Mutex
	sent   []proto.Envelope
	online map[string]bool
}

func (f *fakeSender) Send(agentID, typ string, data any) error {
	raw, _ := json.Marshal(data)
	f.mu.Lock()
	f.sent = append(f.sent, proto.Envelope{Type: typ, Data: raw})
	f.mu.Unlock()
	return nil
}
func (f *fakeSender) Online(agentID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.online[agentID]
}
func (f *fakeSender) count(typ string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.sent {
		if e.Type == typ {
			n++
		}
	}
	return n
}
func (f *fakeSender) lastStart() proto.MediaStart {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out proto.MediaStart
	for _, e := range f.sent {
		if e.Type == proto.MsgMediaStart {
			_ = json.Unmarshal(e.Data, &out)
		}
	}
	return out
}

type fakeMTX struct {
	mu      sync.Mutex
	paths   map[string]bool
	deleted []string
	readers int
	alive   bool
}

func newFakeMTX() *fakeMTX {
	return &fakeMTX{paths: map[string]bool{}, readers: 1, alive: true}
}
func (f *fakeMTX) EnsurePath(name string, _ PathConf) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths[name] = true
	return nil
}
func (f *fakeMTX) DeletePath(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, name)
	delete(f.paths, name)
	return nil
}
func (f *fakeMTX) Readers(string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readers, nil
}
func (f *fakeMTX) URLs(host string, whepPort, rtspPort int) (string, string) {
	return fmt.Sprintf("http://%s:%d", host, whepPort), fmt.Sprintf("rtsp://%s:%d", host, rtspPort)
}
func (f *fakeMTX) Alive() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive
}
func (f *fakeMTX) setReaders(n int) {
	f.mu.Lock()
	f.readers = n
	f.mu.Unlock()
}
func (f *fakeMTX) setAlive(a bool) {
	f.mu.Lock()
	f.alive = a
	f.mu.Unlock()
}

type mtest struct {
	m   *Manager
	fc  *fakeClock
	fs  *fakeSender
	mtx *fakeMTX
	reg *registry.Registry
	cam registry.Camera
	tk  *Tokens
}

func newMTest(t *testing.T, cfg MgrConfig, caps gpu.Caps) *mtest {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
	if _, err := store.MigrateUp(db, dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	reg := registry.New(db)
	if _, err := reg.CreateUser("u1", "$x", "viewer", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.CreateUser("u2", "$x", "viewer", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.CreateClient("c1", 0, "ag_1", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := reg.UpsertCamera(1, registry.SyncCamera{ID: "cam-uuid-1", Name: "kam",
		CodecMain: "h265", CodecSub: "h264"}); err != nil {
		t.Fatal(err)
	}
	// Server siyosati: transcode off (direct oqim uchun).
	mode := "off"
	if err := reg.PatchCameraPolicy("cam-uuid-1", nil, &mode, nil); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClock{t: time.Date(2026, 11, 20, 6, 0, 0, 0, time.UTC)}
	tk := NewTokens(db, fc)
	fs := &fakeSender{online: map[string]bool{"ag_1": true}}
	mtx := newFakeMTX()
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg.Tick = 10 * time.Millisecond
	m := NewManager(cfg, Deps{
		Reg: reg, Store: reg, Agent: fs, MTX: mtx, Tokens: tk, Clock: fc, Lg: lg,
		GPU: caps, PublicHost: "192.168.1.50", WHEPPort: 8889, RTSPPort: 8554,
	})
	t.Cleanup(m.Close)
	cam, err := reg.GetCamera("cam-uuid-1")
	if err != nil {
		t.Fatal(err)
	}
	return &mtest{m: m, fc: fc, fs: fs, mtx: mtx, reg: reg, cam: cam, tk: tk}
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("muddat o'tdi: %s", msg)
}

func TestHappyPathDirect(t *testing.T) {
	mt := newMTest(t, MgrConfig{IdleStop: 10 * time.Millisecond}, gpu.Caps{Backend: gpu.BackendNone})
	mt.mtx.setReaders(0) // idle o'lchov uchun o'quvchi qolmadi
	res, err := mt.m.Start(mt.cam, 1, "", "play")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if res.PlayURL != "http://192.168.1.50:8889/cam/ag_1/cam-uuid-1/whep" {
		t.Fatalf("play url: %s", res.PlayURL)
	}
	if mt.fs.count(proto.MsgMediaStart) != 1 {
		t.Fatalf("media_start soni: %d", mt.fs.count(proto.MsgMediaStart))
	}
	ms := mt.fs.lastStart()
	if ms.Ingest != "rtsp" || ms.Publish.Token == "" || ms.Transcode.By != "none" {
		t.Fatalf("media_start: %+v", ms)
	}
	if st, _ := mt.reg.ListActiveSessions(); len(st) != 1 || st[0].State != "publishing" {
		t.Fatalf("DB state: %+v", st)
	}

	// media_ready → playing.
	mt.m.OnMediaReady("ag_1", proto.MediaReady{SessionID: res.SessionID, PublishID: "p1", ReadyMs: 120})
	waitFor(t, func() bool {
		st, _ := mt.reg.ListActiveSessions()
		return len(st) == 1 && st[0].State == "playing"
	}, "playing bo'lmadi")

	// refresh → TTL uzayadi, token hali yashaydi.
	if err := mt.m.Refresh(res.SessionID, 120); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	// stop → media_stop + path o'chirilishi (readers=1 bo'lsa ham refcount=0
	// bo'lsa idle hisob boshlanadi; IdleStop=0 → darhol).
	if err := mt.m.Stop(res.SessionID, "user"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	mt.fc.advance(20 * time.Millisecond) // idle hisob tugashi (fake soat)
	waitFor(t, func() bool {
		mt.mtx.mu.Lock()
		defer mt.mtx.mu.Unlock()
		return len(mt.mtx.deleted) == 1
	}, "path o'chirilmadi")
	if mt.fs.count(proto.MsgMediaStop) != 1 {
		t.Fatalf("media_stop soni: %d", mt.fs.count(proto.MsgMediaStop))
	}
	st, _ := mt.reg.ListActiveSessions()
	if len(st) != 0 {
		t.Fatalf("faol sessiyalar qoldi: %+v", st)
	}
}

func TestDedupSharedPublish(t *testing.T) {
	mt := newMTest(t, MgrConfig{IdleStop: 10 * time.Millisecond}, gpu.Caps{Backend: gpu.BackendNone})
	r1, err := mt.m.Start(mt.cam, 1, "sub", "play")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := mt.m.Start(mt.cam, 2, "", "preview")
	if err != nil {
		t.Fatal(err)
	}
	// media_start har sessiyaga bitta (agent token/path bo'yicha qayta
	// ishlatadi); dedup PUBLISH darajasida: bitta path, bitta token.
	if mt.fs.count(proto.MsgMediaStart) != 2 {
		t.Fatalf("media_start soni: %d", mt.fs.count(proto.MsgMediaStart))
	}
	mt.mtx.mu.Lock()
	npaths := len(mt.mtx.paths)
	mt.mtx.mu.Unlock()
	if npaths != 1 {
		t.Fatalf("dedup buzildi: pathlar=%d", npaths)
	}
	s1, s2 := mt.fs.sent[0], mt.fs.sent[1]
	var ms1, ms2 proto.MediaStart
	_ = json.Unmarshal(s1.Data, &ms1)
	_ = json.Unmarshal(s2.Data, &ms2)
	if ms1.Publish.Token != ms2.Publish.Token || ms1.Publish.URLs.RTSP != ms2.Publish.URLs.RTSP {
		t.Fatalf("token/path farq qildi: %s vs %s", ms1.Publish.Token, ms2.Publish.Token)
	}
	mt.m.OnMediaReady("ag_1", proto.MediaReady{SessionID: r1.SessionID, PublishID: "p1"})
	waitFor(t, func() bool {
		st, _ := mt.reg.ListActiveSessions()
		return len(st) == 2 && st[0].State == "playing" && st[1].State == "playing"
	}, "ikkalasi ham playing bo'lmadi")

	if err := mt.m.Stop(r1.SessionID, "user"); err != nil {
		t.Fatal(err)
	}
	// Birinchi stopdan keyin ham publish turg'uni (refcount=1, readers=1).
	mt.fc.advance(20 * time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	if mt.fs.count(proto.MsgMediaStop) != 0 {
		t.Fatal("refcount>0 da media_stop yuborildi")
	}
	if err := mt.m.Stop(r2.SessionID, "user"); err != nil {
		t.Fatal(err)
	}
	// refcount=0, lekin readers=1 → publish hali turg'un (§16.2).
	mt.fc.advance(20 * time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	if mt.fs.count(proto.MsgMediaStop) != 0 {
		t.Fatal("readers>0 da media_stop yuborildi")
	}
	// readers=0 bo'lsa — idle stop ishlaydi.
	mt.mtx.setReaders(0)
	mt.fc.advance(20 * time.Millisecond)
	waitFor(t, func() bool { return mt.fs.count(proto.MsgMediaStop) == 1 }, "media_stop yuborilmadi")
}

func TestReadyTimeout(t *testing.T) {
	mt := newMTest(t, MgrConfig{MediaReadyTO: 5 * time.Second}, gpu.Caps{Backend: gpu.BackendNone})
	res, err := mt.m.Start(mt.cam, 1, "", "play")
	if err != nil {
		t.Fatal(err)
	}
	mt.fc.advance(6 * time.Second) // taymautdan o'tib ketadi
	waitFor(t, func() bool {
		return mt.fs.count(proto.MsgMediaStop) == 1
	}, "media_stop (timeout) yuborilmadi")
	// DB'da failed, xotirada sessiya yo'q.
	var state string
	_ = mt.reg // state tekshiruvi: active ro'yxati bo'sh bo'lishi kerak
	waitFor(t, func() bool {
		st, _ := mt.reg.ListActiveSessions()
		return len(st) == 0
	}, "sessiya yopilmadi")
	_ = res
	_ = state
}

func TestSessionTTLExpiry(t *testing.T) {
	mt := newMTest(t, MgrConfig{SessionTTL: 30 * time.Second}, gpu.Caps{Backend: gpu.BackendNone})
	res, err := mt.m.Start(mt.cam, 1, "", "play")
	if err != nil {
		t.Fatal(err)
	}
	mt.m.OnMediaReady("ag_1", proto.MediaReady{SessionID: res.SessionID, PublishID: "p1"})
	waitFor(t, func() bool {
		st, _ := mt.reg.ListActiveSessions()
		return len(st) == 1 && st[0].State == "playing"
	}, "playing")
	mt.fc.advance(31 * time.Second)
	waitFor(t, func() bool {
		st, _ := mt.reg.ListActiveSessions()
		return len(st) == 0
	}, "TTL yopmadi")
}

func TestLimits(t *testing.T) {
	mt := newMTest(t, MgrConfig{GlobalMax: 2, UserMax: 1}, gpu.Caps{Backend: gpu.BackendNone})
	if _, err := mt.m.Start(mt.cam, 1, "", "play"); err != nil {
		t.Fatal(err)
	}
	if _, err := mt.m.Start(mt.cam, 1, "main", "play"); err != ErrFull {
		t.Fatalf("user limit ishlamadi: %v", err)
	}
	if _, err := mt.m.Start(mt.cam, 2, "main", "play"); err != nil {
		t.Fatal(err)
	}
	if _, err := mt.m.Start(mt.cam, 3, "main", "play"); err != ErrFull {
		t.Fatalf("global limit ishlamadi: %v", err)
	}
}

func TestMediaOffline(t *testing.T) {
	mt := newMTest(t, MgrConfig{}, gpu.Caps{Backend: gpu.BackendNone})
	if _, err := mt.m.Start(mt.cam, 1, "", "play"); err != nil {
		t.Fatal(err)
	}
	mt.mtx.setAlive(false)
	waitFor(t, func() bool {
		st, _ := mt.reg.ListActiveSessions()
		return len(st) == 0
	}, "media_offline yopmadi")
}

func TestIdleStopReaders(t *testing.T) {
	mt := newMTest(t, MgrConfig{IdleStop: 50 * time.Millisecond}, gpu.Caps{Backend: gpu.BackendNone})
	res, err := mt.m.Start(mt.cam, 1, "", "play")
	if err != nil {
		t.Fatal(err)
	}
	mt.m.OnMediaReady("ag_1", proto.MediaReady{SessionID: res.SessionID, PublishID: "p1"})
	if err := mt.m.Stop(res.SessionID, "user"); err != nil {
		t.Fatal(err)
	}
	// readers=1 → publish turg'un; readers=0 → to'xtaydi.
	time.Sleep(150 * time.Millisecond)
	if mt.fs.count(proto.MsgMediaStop) != 0 {
		t.Fatal("readers>0 da publish to'xtadi")
	}
	mt.mtx.setReaders(0)
	mt.fc.advance(60 * time.Millisecond)
	waitFor(t, func() bool { return mt.fs.count(proto.MsgMediaStop) == 1 }, "idle stop ishlamadi")
}

func TestDecideTranscode(t *testing.T) {
	nv := gpu.Caps{Backend: gpu.BackendNVENC, Encoder: "h264_nvenc"}
	none := gpu.Caps{Backend: gpu.BackendNone}
	cases := []struct {
		mode         string
		caps         gpu.Caps
		pub, backend string
	}{
		{"off", nv, "direct", "none"},
		{"auto", nv, "server_tc", "nvenc"},
		{"auto", none, "direct", "none"}, // GPU yo'q → fallback (root §5)
		{"server", nv, "server_tc", "nvenc"},
		{"server", none, "direct", "none"},
		{"client", nv, "direct", "none"}, // client GPU'si agentda hisoblanadi
	}
	for _, c := range cases {
		pub, backend, _ := decideTranscode(c.mode, c.caps)
		if pub != c.pub || backend != c.backend {
			t.Errorf("mode=%s caps=%v: %s/%s != %s/%s", c.mode, c.caps.Backend, pub, backend, c.pub, c.backend)
		}
	}
}

func TestServerTCUsesMainSource(t *testing.T) {
	mt := newMTest(t, MgrConfig{}, gpu.Caps{Backend: gpu.BackendNVENC, Encoder: "h264_nvenc"})
	mode := "auto"
	if err := mt.reg.PatchCameraPolicy("cam-uuid-1", nil, &mode, nil); err != nil {
		t.Fatal(err)
	}
	mt.cam.TranscodeMode = "auto"
	res, err := mt.m.Start(mt.cam, 1, "sub", "play") // play_profile=sub, transcode auto
	if err != nil {
		t.Fatal(err)
	}
	ms := mt.fs.lastStart()
	if ms.Profile != "main" {
		t.Fatalf("server_tc main yuborishi kerak: %+v", ms)
	}
	// Agent originalni cam/ ga yuboradi (server FFmpeg'i shuni o'qiydi):
	if ms.Publish.URLs.RTSP != "rtsp://192.168.1.50:8554/cam/ag_1/cam-uuid-1" {
		t.Fatalf("agent publish URL: %s", ms.Publish.URLs.RTSP)
	}
	if ms.Transcode.By != "server" || ms.Transcode.Backend != "nvenc" {
		t.Fatalf("transcode spec: %+v", ms.Transcode)
	}
	if res.PlayURL != "http://192.168.1.50:8889/tc/ag_1/cam-uuid-1/whep" {
		t.Fatalf("play url tc bo'lishi kerak: %s", res.PlayURL)
	}
	if !res.Transcoded || res.Backend != "nvenc" {
		t.Fatalf("result: %+v", res)
	}
}
