package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
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
	cfg     config.Config
	cfgPath string // konfig fayl yo'li ("Save" shu yerga yozadi)
	log     *slog.Logger
	ring    *logbuf.Ring
	logw    *logx.Writer // alohida CLIENT log fayli (nil → faqat terminal)

	// reloadCh — UI "Save"/"Enroll" bosilganda main sikli sessiyani qayta
	// boshlaydi (SIGHUP bilan bir xil yo'l). Qiymat: majburiy reconnect.
	reloadCh chan bool

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
// setSettings — lokal UI "Save": qiymatlarni FAYLGA yozadi, state.json'ni
// yangilaydi va sessiyani qayta boshlashni so'raydi.
//
// Bu bo'lmaganda UI'dagi Server URL faqat UI'ning o'z nusxasida o'zgalar edi:
// agent esa state.json'dagi eski manzilga ulanishda davom etardi
// (2026-11-20 operator xatosi — "log hali 192.168.1.10 da").
func (r *runtime) setSettings(in localui.Settings) error {
	serverURL, logLevel, name := in.ServerURL, in.LogLevel, in.Name
	upd := map[string]string{}
	if serverURL != "" {
		upd["server_url"] = serverURL
	}
	if logLevel != "" {
		upd["log_level"] = logLevel
	}
	// Nom: bo'sh ham yuborilishi mumkin (o'chirish), lekin biz faqat
	// to'lganini yozamiz — aks holda UI'da maydon bo'sh qolsa nom yo'qoladi.
	if name != "" {
		upd["client_name"] = name
	}
	if len(upd) == 0 {
		return nil
	}
	if r.cfgPath == "" {
		return errors.New("konfig fayl yo'li noma'lum")
	}
	if err := config.SetValues(r.cfgPath, upd); err != nil {
		return err
	}
	r.mu.Lock()
	urlChanged := false
	if serverURL != "" && serverURL != r.cfg.ServerURL {
		r.cfg.ServerURL = serverURL
		urlChanged = true
	}
	if logLevel != "" {
		r.cfg.LogLevel = logLevel
	}
	nameChanged := false
	if name != "" && name != r.cfg.ClientName {
		r.cfg.ClientName = name
		nameChanged = true
	}
	if r.st != nil {
		if serverURL != "" && r.st.ServerURL != serverURL {
			r.st.ServerURL = serverURL
			urlChanged = true
		}
	}
	r.mu.Unlock()
	if logLevel != "" {
		setLogLevel(logLevel)
	}
	if urlChanged && r.st != nil {
		if err := r.st.Save(); err != nil {
			return fmt.Errorf("state saqlanmadi: %w", err)
		}
	}
	if urlChanged || nameChanged {
		r.requestReload(true)
		r.log.Info("sozlama saqlandi — sessiya qayta boshlanadi",
			"server", redact.String(r.cfg.ServerURL), "name", r.cfg.ClientName)
	}
	return nil
}

// joinFromUI — UI tugmasi: so'rovni darhol yuborish (yoki holatini
// so'rash). §3.1: kod yo'q — so'rov admin tasdig'ini kutadi.
func (r *runtime) joinFromUI(ctx context.Context) error {
	r.mu.Lock()
	enrolled, agentID := r.st != nil && r.st.AgentID != "", ""
	if r.st != nil {
		agentID = r.st.AgentID
	}
	r.mu.Unlock()
	if enrolled {
		return fmt.Errorf("%w: %s", localui.ErrAlreadyEnrolled, agentID)
	}
	if err := r.joinOnce(ctx); err != nil {
		return mapJoinError(err)
	}
	return nil
}

// joinOnce — §3.1/§3.2: bitta so'rov yuborish yoki tasdiq holatini
// so'rash (polling). Tasdiqlansa agent_id saqlanadi va sessiya
// boshlanadi; pending bo'lsa keyingi urinish avtomatik davom etadi.
func (r *runtime) joinOnce(ctx context.Context) error {
	r.mu.Lock()
	cname, st := r.cfg.ClientName, r.st
	r.mu.Unlock()
	if st == nil || st.ServerURL == "" {
		return errors.New("join: server_url sozlanmagan")
	}
	if err := r.ensureJoinKeys(st); err != nil {
		return err
	}
	secret, _ := r.ks.Get(keystore.RefAgentSecret)
	res, err := agent.Join(ctx, st.ServerURL, agent.JoinInfo{
		JoinKey: st.JoinKey, AgentSecret: secret, Hostname: hostname(),
		Name: cname, OS: osVersion(), LanIP: lanIP(), AgentVersion: version,
	})
	if err != nil {
		var je *agent.JoinError
		if errors.As(err, &je) && je.Code == "rejected" {
			st.JoinStatus = "rejected"
		}
		_ = st.Save()
		r.setError(err)
		return err
	}
	st.JoinStatus = res.Status
	switch res.Status {
	case "approved":
		if st.AgentID != res.AgentID {
			st.AgentID = res.AgentID
			st.SecretRef = keystore.RefAgentSecret
			r.requestReload(true)
		}
		r.log.Info("ulanish tasdiqlandi — sessiya boshlanadi",
			"agent_id", res.AgentID, "request_id", res.RequestID)
	case "pending":
		r.log.Info("ulanish so'rovi yuborildi — admin tasdig'i kutilmoqda",
			"request_id", res.RequestID, "server", redact.String(st.ServerURL))
	case "rejected":
		r.log.Warn("ulanish so'rovi rad etilgan — admin bilan bog'laning",
			"request_id", res.RequestID)
	}
	if err := st.Save(); err != nil {
		return fmt.Errorf("join: state saqlanmadi: %w", err)
	}
	return nil
}

// ensureJoinKeys — doimiy kalit va sir BIR MARTA yaratiladi (§3.1:
// sirni agent o'zi yaratadi, server uni faqat hash ko'rinishida saqlaydi).
func (r *runtime) ensureJoinKeys(st *statestore.State) error {
	if st.JoinKey == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("join: kalit entropy: %w", err)
		}
		st.JoinKey = hex.EncodeToString(b)
	}
	if sec, ok := r.ks.Get(keystore.RefAgentSecret); !ok || sec == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("join: sir entropy: %w", err)
		}
		r.ks.Put(keystore.RefAgentSecret, base64.RawURLEncoding.EncodeToString(b))
		if err := r.ks.Save(); err != nil {
			return fmt.Errorf("join: sir saqlanmadi: %w", err)
		}
	}
	return nil
}

// joinLoop — tasdiq kutilayotgan payt so'rovni muntazam takrorlaydi
// (polling). Tasdiqlangach yoki agent to'xtatilgach tugaydi.
func (r *runtime) joinLoop(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		r.mu.Lock()
		enrolled := r.st != nil && r.st.AgentID != ""
		r.mu.Unlock()
		if enrolled {
			return
		}
		if err := r.joinOnce(ctx); err != nil {
			r.log.Debug("join urinishi", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// currentSettings — lokal UI'ga joriy qiymatlarni qaytaradi (Save'dan
// keyin UI o'zgartirilgan nomni darhol ko'radi).
func (r *runtime) currentSettings() localui.Settings {
	r.mu.Lock()
	defer r.mu.Unlock()
	return localui.Settings{
		ServerURL: r.cfg.ServerURL, LogLevel: r.cfg.LogLevel, Name: r.cfg.ClientName,
	}
}

// mapJoinError — serverning §3.2 xato kodlarini lokal UI tushunadigan
// sentinel xatolarga aylantiradi (UI har biri uchun aniq matn ko‘rsatadi).
func mapJoinError(err error) error {
	var je *agent.JoinError
	if !errors.As(err, &je) {
		return err
	}
	switch je.Code {
	case "rejected":
		return fmt.Errorf("%w: %v", localui.ErrJoinRejected, err)
	case "rate_limited":
		return fmt.Errorf("%w: %v", localui.ErrRateLimited, err)
	case "bad_key":
		return fmt.Errorf("%w: %v", localui.ErrBadKey, err)
	}
	return err
}

// requestReload — main sikliga signal (to'lgan bo'lsa takrorlamaymiz).
func (r *runtime) requestReload(reconnect bool) {
	if r.reloadCh == nil {
		return
	}
	select {
	case r.reloadCh <- reconnect:
	default:
	}
}

func (r *runtime) bootstrap(ctx context.Context) error {
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

	// §3 (1.0-draft.10): kod yo'q — agent ulanish so'rovi yuboradi va
	// admin tasdig'ini kutadi. Tasdiqlangach agent_id saqlanadi.
	if st.AgentID == "" {
		if err := r.joinOnce(ctx); err != nil {
			r.log.Info("ulanish so'rovi yuborilmadi — qayta uriladi",
				"server", redact.String(st.ServerURL), "err", err)
		}
	}

	r.caps = r.probeCaps(ctx)
	r.sess = r.newSession()
	return nil
}

func (r *runtime) newSession() *agent.Session {
	secret, _ := r.ks.Get(keystore.RefAgentSecret)
	r.mu.Lock()
	cname := r.cfg.ClientName
	r.mu.Unlock()
	cfg := agent.SessionConfig{
		ServerURL: r.st.ServerURL, AgentID: r.st.AgentID, Secret: secret,
		AgentVersion: version, OS: osVersion(), Hostname: hostname(), LanIP: lanIP(),
		Name:   cname,
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
		JoinStatus: r.st.JoinStatus,
		CamsTotal:  len(r.st.Cameras), Publishes: 0, CamsEpoch: r.st.CamsEpoch,
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
