package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"central-vms/client/internal/agent"
	"central-vms/client/internal/cams"
	"central-vms/client/internal/caps"
	"central-vms/client/internal/config"
	"central-vms/client/internal/keystore"
	"central-vms/client/internal/localui"
	"central-vms/client/internal/logbuf"
	"central-vms/client/internal/logx"
	"central-vms/client/internal/media"
	"central-vms/client/internal/preview"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/publish"
	"central-vms/client/internal/redact"
	"central-vms/client/internal/selftest"
	"central-vms/client/internal/statestore"
)

// runtime — agentning ish vaqtidagi bog'lamalari (kontrakt §3.2, §10.1).
type runtime struct {
	cfg  config.Config
	log  *slog.Logger
	ring *logbuf.Ring
	logw *logx.Writer // alohida CLIENT log fayli (nil → faqat terminal)

	ks *keystore.Store
	st *statestore.State

	sess      *agent.Session
	publishes *publish.Registry // §4.10 hb publishes + §9.1 refcount
	media     *media.Manager    // C3.20 media sessiyalari
	selftest  *selftest.Runner  // C4.6 paneli

	previews *preview.Store    // <state_dir>/previews/<camID>.jpg
	snap     *preview.Capturer // ffmpeg orqali 1 kadr

	mu              sync.Mutex
	online          bool
	versionMismatch bool
	serverVer       string
	protoMin        int
	protoMax        int
	lastHB          time.Time
	lastErr         string
	lastError       time.Time
	caps            proto.Caps
	welcome         proto.Welcome
	syncer          *agent.Syncer
}

// hintPerm — ruxsat xatosiga amaliy maslahat qo'shadi: `/etc/vmsagent` va
// `/var/lib/vmsagent` root huquqini talab qiladi, yangi operator buni tushunmaydi.
func hintPerm(err error, path string) error {
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}
	hints := []string{
		"Yo'l: " + path + " — root huquqi kerak bo'lishi mumkin",
		"lokal uchun: `make run` (config/client.local.yaml, yo'llar ./var/ ichida)",
		"o'rnatish:   `sudo make install`",
		"yoki config'da state_dir/key_file ni yozib bo'ladigan papkaga o'zgartiring",
	}
	return fmt.Errorf("%w (%s)", err, strings.Join(hints, "; "))
}

// bootstrap — keystore/statestore ochish, kerak bo'lsa enroll, sessiyani tayyorlash.
func (r *runtime) bootstrap(ctx context.Context, enrollCode string) error {
	if err := os.MkdirAll(r.cfg.StateDir, 0o750); err != nil {
		return hintPerm(fmt.Errorf("state_dir: %w", err), r.cfg.StateDir)
	}
	keyPath := r.cfg.KeyFile
	if keyPath == "" {
		keyPath = filepath.Join(r.cfg.StateDir, "agent.key")
	}
	if _, err := keystore.NewKeyFile(keyPath); err != nil {
		return hintPerm(err, keyPath)
	}
	ks, err := keystore.Open(keyPath, filepath.Join(r.cfg.StateDir, "secrets.bin"))
	if err != nil {
		return hintPerm(err, keyPath)
	}
	r.ks = ks

	st, err := statestore.Load(filepath.Join(r.cfg.StateDir, "state.json"))
	if err != nil {
		return err
	}
	r.st = st

	pstore, err := preview.NewStore(filepath.Join(r.cfg.StateDir, "previews"))
	if err != nil {
		return err
	}
	r.previews = pstore
	r.snap = &preview.Capturer{Store: pstore, Log: r.log}

	if st.ServerURL == "" {
		st.ServerURL = r.cfg.ServerURL
	}

	// Birinchi ishga tushish: -enroll KOD berilgan bo'lsa yoki state bo'sh bo'lsa (§3.1)
	if st.AgentID != "" && enrollCode != "" {
		r.log.Warn("agent allaqachon ro'yxatdan o'tgan — `-enroll` kodi e'tiborga olinmadi",
			"agent_id", st.AgentID)
	}
	if st.AgentID == "" {
		if enrollCode == "" {
			r.log.Info("agent ro'yxatdan o'tmagan — UI yoki `-enroll KOD` orqali ulaning",
				"server", redact.String(st.ServerURL))
		} else if err := r.enroll(ctx, enrollCode); err != nil {
			return err
		}
	}

	r.caps = r.probeCaps(ctx)
	r.sess = r.newSession()
	return nil
}

// enroll — §3.1: agent_id/agent_secret olib, keystore+state'ga saqlash.
func (r *runtime) enroll(ctx context.Context, code string) error {
	host, _ := os.Hostname()
	res, err := agent.Enroll(ctx, r.st.ServerURL, agent.EnrollInfo{
		Code: code, Hostname: host, OS: osVersion(), LanIP: lanIP(), AgentVersion: version,
	})
	if err != nil {
		r.setError(err)
		return fmt.Errorf("enroll: %w", err)
	}
	r.ks.Put(keystore.RefAgentSecret, res.AgentSecret)
	if err := r.ks.Save(); err != nil {
		return fmt.Errorf("enroll: secret saqlanmadi: %w", err)
	}
	st := r.st
	st.AgentID = res.AgentID
	st.SecretRef = keystore.RefAgentSecret
	if err := st.Save(); err != nil {
		return fmt.Errorf("enroll: state saqlanmadi: %w", err)
	}
	r.log.Info("agent ro'yxatdan o'tdi", "agent_id", res.AgentID)
	return nil
}

func (r *runtime) newSession() *agent.Session {
	secret, _ := r.ks.Get(keystore.RefAgentSecret)
	cfg := agent.SessionConfig{
		ServerURL: r.st.ServerURL, AgentID: r.st.AgentID, Secret: secret,
		AgentVersion: version, OS: osVersion(), Hostname: hostname(), LanIP: lanIP(),
		BootID: bootID(), CamsEpoch: r.st.CamsEpoch, Caps: r.caps,
		Clock: clockReal(), Log: r.log,
		OnDisconnect: func(error) {
			r.mu.Lock()
			r.online = false
			r.mu.Unlock()
		},
		OnCompat: func(w proto.Welcome) { // §1: versiya mos emas — C4.7 banneri
			r.mu.Lock()
			r.versionMismatch = true
			r.online = false
			if w.Compat.ServerVersion != nil {
				r.serverVer = *w.Compat.ServerVersion
			}
			if w.Compat.MinSupported != nil {
				r.protoMin = *w.Compat.MinSupported
			}
			if w.Compat.MaxSupported != nil {
				r.protoMax = *w.Compat.MaxSupported
			}
			r.mu.Unlock()
			r.log.Error("PROTOKOL VERSIYASI MOS EMAS", "server", redact.String(fmt.Sprint(w.Compat.ServerVersion)),
				"oralik", fmt.Sprintf("%d..%d", ptrVal(w.Compat.MinSupported), ptrVal(w.Compat.MaxSupported)))
		},
		OnWelcome: func(w proto.Welcome) {
			r.mu.Lock()
			r.online, r.welcome = true, w
			r.versionMismatch = false // muvaffaqiyatli qo'l berish — banner o'chadi
			r.mu.Unlock()
			r.log.Info("welcome qabul qilindi", "server_version", w.ServerVersion,
				"needs_full_sync", w.NeedsFullSync, "cams_epoch", w.CamsEpoch)
			// §6.5: epoch farq yoki needs_full_sync → to'liq cams_sync
			go r.runSync()
		},
		OnHeartbeat: func(now time.Time) {
			r.mu.Lock()
			r.lastHB = now
			r.online = true
			r.mu.Unlock()
		},
		Stats: func() map[string]any { // §4.10: hb'da mem_mb, publishes
			var ms goruntime.MemStats
			goruntime.ReadMemStats(&ms)
			pub := 0
			r.mu.Lock()
			if r.publishes != nil {
				pub = len(r.publishes.List())
			}
			r.mu.Unlock()
			return map[string]any{"mem_mb": int64(ms.Alloc / (1024 * 1024)), "publishes": int64(pub)}
		},
	}
	sess := agent.NewSession(cfg)
	r.syncer = agent.NewSyncer(sess, r.st, clockReal(), r.log)
	h := &agent.Handlers{KS: r.ks, St: r.st, Log: r.log, Sess: sess,
		SetLogLevel:   func(l string) error { return r.setLogLevel(l) },
		CamsAck:       r.syncer.OnCamsAck,
		MediaStart:    r.media.OnStart, // C3.20: media_start → publish sessiyasi
		MediaStop:     r.media.OnStop,
		MediaCodecFix: func(cf proto.CodecFix) { r.applyCodecFix(cf) },
		RefreshMedia: func(mr proto.MediaRefresh) error {
			r.media.OnRefresh(mr)
			return nil
		},
		CamsRequest: func(req proto.CamsRequest) {
			// §5.1: server so'roqi — to'liq yoki delta sync yuboramiz
			if req.Full {
				go r.runSync()
				return
			}
			go func() {
				if err := r.syncer.PushDelta(r.st.CamsEpoch, req.CameraIDs); err != nil {
					r.log.Warn("cams_sync (delta) yetmadi", "err", redact.String(err.Error()))
				}
			}()
		}}
	sess.SetOnMessage(h.Handle)
	return sess
}

// runSync — sync'ni goroutine'da bajaradi; xato UI statusida ko'rinadi (§4.3).
func (r *runtime) runSync() {
	r.mu.Lock()
	w, sy := r.welcome, r.syncer
	r.mu.Unlock()
	if sy == nil {
		return
	}
	if err := sy.SyncIfRequired(w); err != nil && !errors.Is(err, context.Canceled) {
		r.log.Warn("cams_sync yetmadi", "err", redact.String(err.Error()))
	}
}

// statusFn — UI status chizig'i uchun (C1.13).
func (r *runtime) statusFn() localui.Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := localui.Status{
		Version: version, ServerOnline: r.online, AgentID: r.st.AgentID,
		CamsTotal: len(r.st.Cameras), Publishes: 0, CamsEpoch: r.st.CamsEpoch,
		FFmpegOK: r.caps.FFmpegVersion != "",
	}
	// §17 holat chizig'i: oxirgi heartbeat + oxirgi xato (redaktor orqali).
	if r.versionMismatch {
		st.VersionMismatch = true
		st.ServerVersion = r.serverVer
		st.ProtoMin, st.ProtoMax = r.protoMin, r.protoMax
	}
	if !r.lastHB.IsZero() {
		st.LastHeartbeat = r.lastHB.UTC().Format(time.RFC3339)
	}
	if r.lastErr != "" {
		st.LastError = r.lastErr
		if !r.lastError.IsZero() {
			st.LastErrorAt = r.lastError.UTC().Format(time.RFC3339)
		}
	}
	for _, c := range r.st.Cameras {
		if c.Status == "online" {
			st.CamsOnline++
		}
	}
	return st
}

// ptrVal — nil xavfsiz o'qish (compat maydonlari uchun).
func ptrVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// applyCodecFix — §12.3: server codec_fix → state'dagi kodek maydonlarini
// yangilash + qayta sync (cam_status). ONVIF orqali qo'llash C2.12'da
// package darajasida testlangan; bu yerda holat yozuvi va xabarlashamiz.
func (r *runtime) applyCodecFix(cf proto.CodecFix) {
	changed := 0
	r.mu.Lock()
	for i := range r.st.Cameras {
		c := &r.st.Cameras[i]
		if !cf.ApplyAll && c.ID != cf.CameraID {
			continue
		}
		if cf.Target.Profile == "sub" || cf.Target.Profile == "" {
			c.CodecSub, c.BframeSub = cf.Target.Codec, cf.Target.NoBFrames
		}
		if cf.Target.Profile != "sub" {
			c.CodecMain, c.BframeMain = cf.Target.Codec, cf.Target.NoBFrames
		}
		changed++
	}
	r.mu.Unlock()
	if changed == 0 {
		r.log.Warn("codec_fix: kamera topilmadi", "camera_id", redact.String(cf.CameraID))
		return
	}
	r.log.Info("codec_fix qo'llandi", "camera_id", redact.String(cf.CameraID),
		"kodek", redact.String(cf.Target.Codec), "kameralar", changed)
	if err := r.st.Save(); err != nil {
		r.log.Error("state saqlanmadi", "err", redact.Err(err))
	}
	go r.runSync() // yangi kodek holatini serverga yuboramiz (§4.2)
}

// lookupCamera — state kamera ro'yxatidan ID bo'yicha (media.Manager uchun).
func (r *runtime) lookupCamera(id string) (cams.Camera, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.st.Cameras {
		if c.ID == id {
			return c, true
		}
	}
	return cams.Camera{}, false
}

func (r *runtime) setError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastErr, r.lastError = redact.String(err.Error()), time.Now()
}

func (r *runtime) setLogLevel(l string) error {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(l)); err != nil {
		return err
	}
	r.mu.Lock()
	r.cfg.LogLevel = l
	r.mu.Unlock()
	r.log = newLogger(l, r.ring, r.logw)
	return nil
}

func (r *runtime) probeCaps(ctx context.Context) proto.Caps {
	c, err := caps.Detect(ctx, "")
	if err != nil {
		r.log.Warn("FFmpeg topilmadi — publish ishlamaydi", "err", redact.String(err.Error()))
		return proto.Caps{}
	}
	r.log.Info("joy imkoniyatlari", "gpu", c.GPU, "cores", c.CPUCores,
		"ram_mb", c.RAMMB, "ffmpeg", c.FFmpegVersion, "hevc_decode", c.HEVCDecode)
	return c
}

// startSession — sessiyani alohida goroutine'da ishga tushiradi;
// qaytarilgan funksiya mayin to'xtatadi (SIGHUP'da server_url o'zgarsa kerak).
func (r *runtime) startSession(parent context.Context) func() {
	ctx, cancel := context.WithCancel(parent)
	sess := r.newSession()
	r.mu.Lock()
	r.sess = sess
	r.mu.Unlock()

	go func() {
		if err := sess.Run(ctx); err != nil {
			r.setError(err)
			r.log.Warn("sessiya sikli tugadi", "err", redact.String(err.Error()))
		}
	}()
	return func() {
		cancel()
		r.mu.Lock()
		r.online = false
		r.mu.Unlock()
	}
}
