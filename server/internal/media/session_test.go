// S2.4 menejer testlari: holat mashinasi, dedup, taymautlar, limitlar —
// hammasi fake clock/fake agent/fake MTX bilan (deterministik).
package media

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	mu        sync.Mutex
	paths     map[string]bool
	deleted   []string
	readers   int
	alive     bool
	patterned bool // mediamtx.yml'da ~^cam/.+ patternlari bor deb hisoblash
	ensure    int  // EnsurePath chaqiruvlari soni
}

func newFakeMTX() *fakeMTX {
	return &fakeMTX{paths: map[string]bool{}, readers: 1, alive: true}
}

// PathConfigured — dev/prod mediamtx.yml'dagi kabi patternlar bilan
// qoplangan yo'llar uchun EnsurePath chaqirilmasligi kerak.
func (f *fakeMTX) PathConfigured(name string) (bool, error) {
	if f.patterned {
		return strings.HasPrefix(name, "cam/") || strings.HasPrefix(name, "tc/"), nil
	}
	return false, nil
}

func (f *fakeMTX) EnsurePath(name string, _ PathConf) error {
	f.ensure++
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
	if os.Getenv("VMS_TEST_LOG") != "" {
		lg = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	cfg.Tick = 10 * time.Millisecond
	tr := NewTranscoder(fixturePath("fakeffmpeg.sh"), 5, lg)
	t.Cleanup(tr.StopAll)
	m := NewManager(cfg, Deps{
		Reg: reg, Store: reg, Agent: fs, MTX: mtx, Tokens: tk, Clock: fc, Lg: lg,
		GPU: caps, PublicHost: "192.168.1.50", WHEPPort: 8889, RTSPPort: 8554,
		Transcode: tr,
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
	res, err := mt.m.Start(mt.cam, 1, "", "play", BrowserCaps{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if res.WHEPURL != "http://192.168.1.50:8889/cam/ag_1/cam-uuid-1/whep" {
		t.Fatalf("play url: %s", res.WHEPURL)
	}
	if mt.fs.count(proto.MsgMediaStart) != 1 {
		t.Fatalf("media_start soni: %d", mt.fs.count(proto.MsgMediaStart))
	}
	ms := mt.fs.lastStart()
	// §5.2: ingest qiymatlari "whip" | "rtsp_tcp" ("rtsp" — eski xato
	// qiymat; haqiqiy vmsagent uni rad etadi).
	if ms.Ingest != "rtsp_tcp" || ms.Publish.Token == "" || ms.Transcode.By != "none" {
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
	if err := mt.m.Refresh(res.SessionID, 1, 120); err != nil {
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
	r1, err := mt.m.Start(mt.cam, 1, "sub", "play", BrowserCaps{})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := mt.m.Start(mt.cam, 2, "", "preview", BrowserCaps{})
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
	// Har sessiya uchun bitta media_stop (agent publish'ni session_id
	// bo'yicha kuzatadi — protocol.md §9).
	waitFor(t, func() bool { return mt.fs.count(proto.MsgMediaStop) == 2 }, "media_stop yuborilmadi")
}

func TestReadyTimeout(t *testing.T) {
	mt := newMTest(t, MgrConfig{MediaReadyTO: 5 * time.Second}, gpu.Caps{Backend: gpu.BackendNone})
	res, err := mt.m.Start(mt.cam, 1, "", "play", BrowserCaps{})
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
	res, err := mt.m.Start(mt.cam, 1, "", "play", BrowserCaps{})
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
	if _, err := mt.m.Start(mt.cam, 1, "", "play", BrowserCaps{}); err != nil {
		t.Fatal(err)
	}
	if _, err := mt.m.Start(mt.cam, 1, "main", "play", BrowserCaps{}); !isLimit(err, "user_limit") {
		t.Fatalf("user limit ishlamadi: %v", err)
	}
	if _, err := mt.m.Start(mt.cam, 2, "main", "play", BrowserCaps{}); err != nil {
		t.Fatal(err)
	}
	if _, err := mt.m.Start(mt.cam, 3, "main", "play", BrowserCaps{}); !isLimit(err, "global_limit") {
		t.Fatalf("global limit ishlamadi: %v", err)
	}
}

func TestMediaOffline(t *testing.T) {
	mt := newMTest(t, MgrConfig{}, gpu.Caps{Backend: gpu.BackendNone})
	if _, err := mt.m.Start(mt.cam, 1, "", "play", BrowserCaps{}); err != nil {
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
	res, err := mt.m.Start(mt.cam, 1, "", "play", BrowserCaps{})
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

func isLimit(err error, kind string) bool {
	var le *LimitError
	return errors.As(err, &le) && le.Kind == kind
}

func TestPreviewLimitKind(t *testing.T) {
	mt := newMTest(t, MgrConfig{PreviewMax: 1}, gpu.Caps{Backend: gpu.BackendNone})
	if _, err := mt.m.Start(mt.cam, 1, "sub", "preview", BrowserCaps{}); err != nil {
		t.Fatal(err)
	}
	if _, err := mt.m.Start(mt.cam, 2, "sub", "preview", BrowserCaps{}); !isLimit(err, "preview_limit") {
		t.Fatalf("preview_limit kutilardi: %v", err)
	}
	// play (preview emas) cheklanmaydi.
	if _, err := mt.m.Start(mt.cam, 2, "sub", "play", BrowserCaps{}); err != nil {
		t.Fatalf("play cheklanmasligi kerak: %v", err)
	}
}

func TestSessionsForAndPreviewOff(t *testing.T) {
	mt := newMTest(t, MgrConfig{}, gpu.Caps{Backend: gpu.BackendNone})
	r1, err := mt.m.Start(mt.cam, 1, "sub", "preview", BrowserCaps{})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := mt.m.Start(mt.cam, 2, "sub", "preview", BrowserCaps{})
	if err != nil {
		t.Fatal(err)
	}
	ids := mt.m.SessionsFor(1, "", "preview")
	if len(ids) != 1 || ids[0] != r1.SessionID {
		t.Fatalf("SessionsFor(user1): %v", ids)
	}
	all := mt.m.SessionsFor(0, mt.cam.ID, "")
	if len(all) != 2 {
		t.Fatalf("SessionsFor(all): %v", all)
	}
	// preview "on:false" semanticsi: chaqiruvchi topib to'xtatadi.
	for _, id := range mt.m.SessionsFor(1, mt.cam.ID, "preview") {
		if err := mt.m.Stop(id, "user"); err != nil {
			t.Fatal(err)
		}
	}
	if got := mt.m.SessionsFor(1, "", "preview"); len(got) != 0 {
		t.Fatalf("yopilgandan keyin: %v", got)
	}
	_ = r2
}

func TestDecideTranscode(t *testing.T) {
	cGPU := gpu.Caps{Backend: gpu.BackendNVENC, Encoder: "h264_nvenc"}
	cNone := gpu.Caps{Backend: gpu.BackendNone}
	cases := []struct {
		mode    string
		caps    gpu.Caps
		codec   string
		hevcOK  bool
		wantPub string
		wantBy  string
	}{
		// auto: H.264 — transcode KERAK EMAS (CPU/limit tejanadi).
		{"auto", cGPU, "h264", false, "direct", "none"},
		{"auto", cGPU, "H264", false, "direct", "none"},
		// auto: H.265 + brauzer HEVC'ni o'ynaydi → transcodesiz uzatiladi.
		{"auto", cGPU, "h265", true, "direct", "none"},
		// auto: H.265 + brauzer HEVC yo'q → server transcode (H.264 ga).
		{"auto", cGPU, "h265", false, "server_tc", "server"},
		// auto: H.265, serverda GPU yo'q → agent zaxirasi (by=client).
		{"auto", cNone, "h265", false, "direct", "client"},
		// Aniq rejimlar.
		{"off", cGPU, "h265", false, "direct", "none"},
		{"server", cGPU, "h264", true, "server_tc", "server"},
		{"server", cNone, "h265", false, "direct", "none"},
		{"client", cGPU, "h265", false, "direct", "client"},
	}
	for _, c := range cases {
		pub, backend, by := decideTranscode(c.mode, c.caps, c.codec, c.hevcOK)
		if pub != c.wantPub || by != c.wantBy {
			t.Errorf("mode=%s codec=%s hevcOK=%v: %s/%s/%s olindi, %s/%s kutilardi",
				c.mode, c.codec, c.hevcOK, pub, backend, by, c.wantPub, c.wantBy)
		}
	}
}

func TestServerTCUsesMainSource(t *testing.T) {
	mt := newMTest(t, MgrConfig{IdleStop: 10 * time.Millisecond}, gpu.Caps{Backend: gpu.BackendNVENC, Encoder: "h264_nvenc"})
	mode := "auto"
	if err := mt.reg.PatchCameraPolicy("cam-uuid-1", nil, &mode, nil); err != nil {
		t.Fatal(err)
	}
	mt.cam.TranscodeMode = "auto"
	// Manba: main=H.265, sub=H.264 (newMTest). Brauzer HEVC'ni o'ynamaydi →
	// auto H.265'ni H.264 ga o'giradi va MAIN manbadan oladi (sifat).
	res, err := mt.m.Start(mt.cam, 1, "main", "play", BrowserCaps{})
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
	// media_ready'dan keyin FFmpeg (fake) ishga tushishi kerak.
	mt.m.OnMediaReady("ag_1", proto.MediaReady{SessionID: res.SessionID, PublishID: "p1", Path: "cam/ag_1/cam-uuid-1"})
	waitFor(t, func() bool { return mt.m.deps.Transcode.Count() == 1 }, "transcode boshlanmadi")
	waitFor(t, func() bool {
		st, _ := mt.reg.ListActiveSessions()
		return len(st) == 1 && st[0].State == "playing"
	}, "playing")
	// Sessiya yopilsa (o'quvchi qolmasa) transcode ham to'xtaydi.
	mt.mtx.setReaders(0)
	if err := mt.m.Stop(res.SessionID, "user"); err != nil {
		t.Fatal(err)
	}
	mt.fc.advance(20 * time.Millisecond)
	waitFor(t, func() bool { return mt.m.deps.Transcode.Count() == 0 }, "transcode to'xtamadi")
	if res.WHEPURL != "http://192.168.1.50:8889/tc/ag_1/cam-uuid-1/whep" {
		t.Fatalf("play url tc bo'lishi kerak: %s", res.WHEPURL)
	}
	if !res.Transcoded || res.Backend != "nvenc" {
		t.Fatalf("result: %+v", res)
	}
}

// auto + H.264 manba → transcode BO'LMAYDI (CPU/limit tejanadi) va
// o'quvchi cam/... path'ini oladi.
func TestAutoH264SkipsTranscode(t *testing.T) {
	mt := newMTest(t, MgrConfig{IdleStop: 10 * time.Millisecond},
		gpu.Caps{Backend: gpu.BackendNVENC, Encoder: "h264_nvenc"})
	mode := "auto"
	if err := mt.reg.PatchCameraPolicy("cam-uuid-1", nil, &mode, nil); err != nil {
		t.Fatal(err)
	}
	// Kodexlarni H.264 ga o'zgartiramiz (newMTest'da main=h265).
	if _, err := mt.reg.DB().Exec(`UPDATE cameras SET codec_main='h264', codec_sub='h264' WHERE id='cam-uuid-1'`); err != nil {
		t.Fatal(err)
	}
	mt.cam.CodecMain = "h264"
	mt.cam.TranscodeMode = "auto"
	res, err := mt.m.Start(mt.cam, 1, "main", "play", BrowserCaps{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Transcoded || res.Backend != "none" {
		t.Fatalf("transcode kutilmadi: %+v", res)
	}
	if !strings.Contains(res.WHEPURL, "/cam/") {
		t.Fatalf("o'qish path'i cam/ bo'lishi kerak: %s", res.WHEPURL)
	}
	mt.m.OnMediaReady("ag_1", proto.MediaReady{SessionID: res.SessionID, PublishID: "p1", Path: "cam/ag_1/cam-uuid-1"})
	mt.fc.advance(2 * time.Second)
	if n := mt.m.deps.Transcode.Count(); n != 0 {
		t.Fatalf("FFmpeg boshlanmasligi kerak edi, bor: %d", n)
	}
}

// Brauzer yopilib qo'ysa (o'quvchilar 0 ga tushsa) sessiya TTL'gacha
// limitni band qilmasligi kerak: readers watchdog uni yopadi.
func TestGhostSessionClosedByReadersWatchdog(t *testing.T) {
	mt := newMTest(t, MgrConfig{IdleStop: 100 * time.Millisecond}, gpu.Caps{Backend: gpu.BackendNone})
	res, err := mt.m.Start(mt.cam, 1, "", "play", BrowserCaps{})
	if err != nil {
		t.Fatal(err)
	}
	mt.m.OnMediaReady("ag_1", proto.MediaReady{SessionID: res.SessionID, PublishID: "p1", Path: "cam/ag_1/cam-uuid-1"})
	mt.fc.advance(20 * time.Millisecond)

	// O'quvchi paydo bo'ldi (watchdog sawReaders=true deb yozadi —
	// tick haqiqiy taymer bilan aylanadi, shuning uchun kutamiz).
	mt.mtx.setReaders(2)
	time.Sleep(60 * time.Millisecond) // tick'lar readers>0 ni ko'rsin
	if len(mt.m.SessionsFor(1, mt.cam.ID, "play")) != 1 {
		t.Fatal("o'quvchi bor paytda sessiya yopilmasligi kerak")
	}

	// ...keyin yo'qoldi (brauzer o'ldi) — watchdog yopishi kerak.
	// Soatni ikki bosqichda suramiz: birinchi tick zeroSince belgilaydi,
	// ikkinchisi IdleStop o'tganini ko'radi.
	mt.mtx.setReaders(0)
	mt.fc.advance(60 * time.Millisecond)
	time.Sleep(60 * time.Millisecond) // tick readers=0 ni ko'radi → zeroSince
	if len(mt.m.SessionsFor(1, mt.cam.ID, "play")) != 1 {
		t.Fatal("sessiya erta yopildi")
	}
	mt.fc.advance(400 * time.Millisecond)
	waitFor(t, func() bool { return len(mt.m.SessionsFor(1, mt.cam.ID, "play")) == 0 }, "arvoh sessiya yopilmadi")
	rows, err := mt.reg.ListActiveSessions()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == res.SessionID {
			t.Fatalf("DB'da ham faol qoldi: %+v", r)
		}
	}
}

// 1.0-draft.12: agent media_ready'da noto‘g‘ri path yuborsa (eski mijoz
// profil yuborardi), o‘quvchi URL‘lari buzilmasligi kerak.
func TestMediaReadyWrongPathKeepsURLs(t *testing.T) {
	mt := newMTest(t, MgrConfig{IdleStop: time.Second}, gpu.Caps{Backend: gpu.BackendNone})
	res, err := mt.m.Start(mt.cam, 1, "", "preview", BrowserCaps{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	mt.m.OnMediaReady("ag_1", proto.MediaReady{SessionID: res.SessionID, PublishID: "p1",
		Path: "main"}) // eski mijoz xatosi: profil → path deb yuborilgan
	waitFor(t, func() bool {
		st, _ := mt.reg.ListActiveSessions()
		return len(st) == 1 && st[0].State == "playing"
	}, "playing bo‘lmadi")
	res2, err := mt.m.Start(mt.cam, 2, "", "preview", BrowserCaps{})
	if err != nil {
		t.Fatalf("ikkinchi start: %v", err)
	}
	want := "rtsp://192.168.1.50:8554/cam/ag_1/cam-uuid-1"
	if res2.RTSPURL != want {
		t.Fatalf("path buzildi: %s (kutilardi %s)", res2.RTSPURL, want)
	}
}

// 1.0-draft.12: oqim muvaffaqiyatsizligi sababi kameraga yoziladi —
// operator UI‘da "nima uchun ko‘rinmayapti" javobini ko‘radi.
func TestMediaStoppedRecordsCameraError(t *testing.T) {
	mt := newMTest(t, MgrConfig{IdleStop: time.Second}, gpu.Caps{Backend: gpu.BackendNone})
	res, err := mt.m.Start(mt.cam, 1, "", "preview", BrowserCaps{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	mt.m.OnMediaStopped("ag_1", proto.MediaStopped{SessionID: res.SessionID,
		CameraID: mt.cam.ID, PublishID: "p1", Reason: "publish_failed"})
	waitFor(t, func() bool {
		c, err := mt.reg.GetCamera(mt.cam.ID)
		return err == nil && c.LastError != ""
	}, "last_error yozilmadi")
	c, _ := mt.reg.GetCamera(mt.cam.ID)
	if !strings.Contains(c.LastError, "publish_failed") {
		t.Fatalf("sabab noto‘g‘ri: %q", c.LastError)
	}
	if c.LastErrorAt == "" {
		t.Fatal("last_error_at yozilmadi")
	}
}

// Normal to‘xtash (tomaqibilar ketdi) xato hisoblanmaydi.
func TestMediaStoppedNormalReasonNoError(t *testing.T) {
	mt := newMTest(t, MgrConfig{IdleStop: time.Second}, gpu.Caps{Backend: gpu.BackendNone})
	res, err := mt.m.Start(mt.cam, 1, "", "preview", BrowserCaps{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	mt.m.OnMediaStopped("ag_1", proto.MediaStopped{SessionID: res.SessionID,
		CameraID: mt.cam.ID, PublishID: "p1", Reason: "no_viewers"})
	time.Sleep(150 * time.Millisecond)
	c, err := mt.reg.GetCamera(mt.cam.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.LastError != "" {
		t.Fatalf("normal sabab xato deb yozildi: %q", c.LastError)
	}
}

// 1.0-draft.12: MediaMTX konfiguratsiyasi yo'lni pattern bilan qoplaydi —
// EnsurePath chaqirilmaydi (v1.11.3'da `/` li nomlar bilan PUT ishlamaydi;
// har preview'da log'da 404 ko'rinardi).
func TestEnsurePathSkippedForPatternedPaths(t *testing.T) {
	mt := newMTest(t, MgrConfig{IdleStop: time.Second}, gpu.Caps{Backend: gpu.BackendNone})
	mt.mtx.patterned = true
	if _, err := mt.m.Start(mt.cam, 1, "", "preview", BrowserCaps{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if mt.mtx.ensure != 0 {
		t.Fatalf("pattern bilan qoplangan yo'l uchun EnsurePath chaqirildi: %d", mt.mtx.ensure)
	}

	mt2 := newMTest(t, MgrConfig{IdleStop: time.Second}, gpu.Caps{Backend: gpu.BackendNone})
	mt2.mtx.patterned = false
	if _, err := mt2.m.Start(mt2.cam, 1, "", "preview", BrowserCaps{}); err != nil {
		t.Fatalf("start2: %v", err)
	}
	if mt2.mtx.ensure == 0 {
		t.Fatal("qoplanmagan yo'l uchun EnsurePath chaqirilishi kerak edi")
	}
}

// FFmpeg jarayoni uzilsa (kamera oqimi uzilishi, encoder xatosi) transcode
// qayta ko'tarilishi kerak: aks holda publish tirik qilsa ham tc/... path'
// yo'qolib, o'quvchilar abadiy 404 olardi (2026-11-21 server auditi).
func TestSession_TranscodeDiesIsRestarted(t *testing.T) {
	mt := newMTest(t, MgrConfig{IdleStop: 10 * time.Millisecond},
		gpu.Caps{Backend: gpu.BackendSoft, Encoder: "libx264"})
	mode := "auto"
	if err := mt.reg.PatchCameraPolicy("cam-uuid-1", nil, &mode, nil); err != nil {
		t.Fatal(err)
	}
	mt.cam.TranscodeMode = "auto"
	counts := filepath.Join(t.TempDir(), "starts.txt")
	t.Setenv("FAKEFFMPEG_COUNT", counts)
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	tr := NewTranscoder(fixturePath("fakeffmpeg_exit.sh"), 5, lg)
	tr.SetOnEnd(mt.m.OnTranscodeEnd)
	tr.SetOnFailure(mt.m.OnTranscodeFailure)
	t.Cleanup(tr.StopAll)
	mt.m.deps.Transcode = tr

	res, err := mt.m.Start(mt.cam, 1, "main", "play", BrowserCaps{})
	if err != nil {
		t.Fatal(err)
	}
	mt.m.OnMediaReady("ag_1", proto.MediaReady{SessionID: res.SessionID, PublishID: "p1",
		Path: "cam/ag_1/cam-uuid-1"})
	starts := func() int {
		b, err := os.ReadFile(counts)
		if err != nil {
			return 0
		}
		return bytes.Count(b, []byte("start"))
	}
	waitFor(t, func() bool { return starts() >= 1 }, "transcode boshlanmadi")
	// Sun'iy soat: qayta urinish oralig'i (1 s) shu soat bilan o'lchanadi.
	waitFor(t, func() bool {
		mt.fc.advance(2 * time.Second)
		return starts() >= 2
	}, "transcode qayta ko'tarilmadi")
	st, _ := mt.reg.ListActiveSessions()
	if len(st) != 1 || st[0].State == "ended" {
		t.Fatalf("sessiya yashab turishi kerak edi: %+v", st)
	}
}

// Sof qaror funksiyasi: uzoq yashagan transcode uzilsa urish byudjeti
// qayta to'ldiriladi, tez-tez o'lsa — yo'q.
func TestTCRestartDecision(t *testing.T) {
	if ok, _, reset := tcRestartDecision(2*time.Minute, true); !ok || !reset {
		t.Fatal("uzoq yashagan uzilishda byudjet to'ldirilishi kerak")
	}
	if ok, _, reset := tcRestartDecision(time.Second, true); !ok || reset {
		t.Fatal("tez uzilishda byudjet to'ldirilmasligi kerak")
	}
	if ok, _, _ := tcRestartDecision(time.Minute, false); ok {
		t.Fatal("o'quvchi qolmagan publish'da qayta ko'tarilmasligi kerak")
	}
}

// Aralash brauzer imkoniyatlari: bir kameraga avval HEVC'ni o'ynaydigan
// (direct), keyin o'ynamaydigan (server_tc) brauzer kelsa — ikkinchi
// sessiya ham ishlashi kerak. Avval publish kalitida rejim ham bo'lgani
// uchun ikkinchi sessiya media_ready kutib qotib, 5 s'dan keyin o'lar,
// transcode umuman boshlanmas edi (2026-11-21 server auditi).
func TestSession_MixedHevcSharesPublish(t *testing.T) {
	mt := newMTest(t, MgrConfig{IdleStop: 10 * time.Millisecond},
		gpu.Caps{Backend: gpu.BackendSoft, Encoder: "libx264"})
	mode := "auto"
	if err := mt.reg.PatchCameraPolicy("cam-uuid-1", nil, &mode, nil); err != nil {
		t.Fatal(err)
	}
	mt.cam.TranscodeMode = "auto"

	r1, err := mt.m.Start(mt.cam, 1, "main", "play", BrowserCaps{HEVC: true})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Transcoded {
		t.Fatalf("HEVC'li brauzer direct olishi kerak edi: %+v", r1)
	}
	mt.m.OnMediaReady("ag_1", proto.MediaReady{SessionID: r1.SessionID, PublishID: "p1",
		Path: "cam/ag_1/cam-uuid-1"})
	waitFor(t, func() bool {
		st, _ := mt.reg.ListActiveSessions()
		return len(st) == 1 && st[0].State == "playing"
	}, "birinchi sessiya playing")

	r2, err := mt.m.Start(mt.cam, 2, "main", "play", BrowserCaps{})
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Transcoded {
		t.Fatalf("HEVC'ni o'ynamaydigan brauzer transcode olishi kerak: %+v", r2)
	}
	if !strings.Contains(r2.RTSPURL, "/tc/") {
		t.Fatalf("o'quvchi tc/ path olishi kerak: %s", r2.RTSPURL)
	}
	// media_ready IKKINCHI marta kelmaydi (agent publish uchun bir marta
	// yuboradi) — sessiya shunga qaramay playing bo'lishi kerak.
	waitFor(t, func() bool {
		st, _ := mt.reg.ListActiveSessions()
		if len(st) != 2 {
			return false
		}
		for _, x := range st {
			if x.State != "playing" {
				return false
			}
		}
		return true
	}, "ikkinchi sessiya playing (media_ready'siz)")
	// Transcode shu paygacha boshlanmagan bo'lishi kerak edi (tick),
	// endi esa yurishi kerak.
	waitFor(t, func() bool { return mt.m.deps.Transcode.Count() == 1 }, "transcode boshlanmadi")

	// tc o'quvchi ketgach FFmpeg to'xtaydi, direct sessiya esa yashaydi.
	if err := mt.m.Stop(r2.SessionID, "user"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return mt.m.deps.Transcode.Count() == 0 }, "transcode to'xtamadi")
	st, _ := mt.reg.ListActiveSessions()
	if len(st) != 1 || st[0].ID != r1.SessionID {
		t.Fatalf("direct sessiya yashab turishi kerak edi: %+v", st)
	}
}

// IDOR: sessiya ID'si wsui Broadcast'ida tarqaladi — uni bilgan boshqa
// foydalanuvchi begga sessiyasini to'xtata/yozib qo'ya olmasligi kerak
// (2026-11-21 server auditi).
func TestSession_OwnerEnforced(t *testing.T) {
	mt := newMTest(t, MgrConfig{IdleStop: 10 * time.Millisecond}, gpu.Caps{Backend: gpu.BackendNone})
	r, err := mt.m.Start(mt.cam, 1, "main", "play", BrowserCaps{})
	if err != nil {
		t.Fatal(err)
	}
	if err := mt.m.StopAs(r.SessionID, 2, "user"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("begga stop: %v", err)
	}
	if err := mt.m.Refresh(r.SessionID, 2, 60); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("begga refresh: %v", err)
	}
	st, _ := mt.reg.ListActiveSessions()
	if len(st) != 1 {
		t.Fatalf("sessiya yashab turishi kerak edi: %+v", st)
	}
	if err := mt.m.StopAs(r.SessionID, 1, "user"); err != nil {
		t.Fatalf("o'z sessiyasi: %v", err)
	}
}
