// vmsd — Central VMS serveri: API + WS + statika serve (server REJA §3).
// S0 chegarasi: config, log, migratsiya, /healthz, statika, port tekshiruvi,
// graceful shutdown. Auth/registry/WS/media — S1/S2.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"central-vms/server/internal/audit"
	"central-vms/server/internal/auth"
	"central-vms/server/internal/config"
	"central-vms/server/internal/gpu"
	"central-vms/server/internal/httpapi"
	"central-vms/server/internal/logx"
	"central-vms/server/internal/media"
	"central-vms/server/internal/ports"
	"central-vms/server/internal/registry"
	"central-vms/server/internal/store"
	"central-vms/server/internal/version"
	"central-vms/server/internal/wsagent"
	"central-vms/server/internal/wsui"
)

func main() {
	if err := run(); err != nil {
		// Talab (2026-11-19): xatolar terminalga batafsil chiqadi.
		fmt.Fprintf(os.Stderr, "vmsd: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		cfgPath  = flag.String("config", "config/dev.yaml", "konfiguratsiya YAML fayli")
		webDir   = flag.String("web", "", "UI statikasi papkasi (bo'sh → config.web_dir)")
		probe    = flag.Bool("probe", false, "server quvvatini o'lchab chiqish (make probe)")
		probeQ   = flag.Bool("probe-quick", false, "probe: sig'im o'lchovisiz (tez)")
		migrateO = flag.Bool("migrate-only", false, "faqat migratsiyani bajarib chiqish")
		migrate  = flag.String("migrate", "", "migratsiya amali: up|down|status (make migrate-*)")
		seedAdm  = flag.String("seed-admin", "", "admin yaratish/yangilash: username:parol (make admin)")
		showVer  = flag.Bool("version", false, "versiyani chiqarish")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("vmsd %s (commit %s, built %s, protocol %d)\n", version.Version, version.Commit, version.BuiltAt, protoVersion)
		return nil
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if *webDir != "" {
		cfg.WebDir = *webDir
	}

	lg, logW, err := logx.NewLogger(cfg.LogLevel, cfg.LogFile, cfg.LogMaxMB, cfg.LogKeep)
	if err != nil {
		return err
	}
	defer logW.Close()
	lg = lg.With(slog.String("svc", "vmsd"), slog.String("version", version.Version))

	if *migrate != "" {
		return runMigrate(lg, cfg, *migrate)
	}

	// Port tekshiruvi (REJA §2.1/1): vmsd FAQAT o'zi bog'laydigan portlarni
	// talab qiladi — API va auth-hook (TCP 8189). MediaMTX portlari
	// (8554/8888/8889, UDP 8000/8001/8189)ni bizning MediaMTX'imiz band
	// qiladi; tashqi MediaMTX bilan to'qnashuvni `make port-check`
	// (`make start` chaqiradi) oldindan aniqlaydi — vmsd tashqi xizmatni
	// o'zi to'xtatmaydi. seed-admin/migrate-only rejimlarda portlar kerak emas.
	if apiPort := portOf(cfg.Bind); apiPort != 0 && *seedAdm == "" && !*migrateO {
		hookPort := portOf(cfg.MediaMTX.AuthHookBind)
		if err := ports.Check([]int{apiPort, hookPort}, nil); err != nil {
			return err
		}
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	done, err := store.MigrateUp(db, cfg.Migrations)
	if err != nil {
		return fmt.Errorf("migratsiya: %w", err)
	}
	if len(done) > 0 {
		lg.Info("migratsiyalar qo'llanildi", slog.Any("versions", done))
	}
	if *migrateO {
		lg.Info("migrate-only: tayyor")
		return nil
	}
	if *probe {
		return runProbe(lg, cfg, db, *probeQ)
	}
	if *seedAdm != "" {
		return runSeedAdmin(lg, db, *seedAdm)
	}

	// --- S1 bog'lamalari (root §3: bitta binary ichida) ---
	reg := registry.New(db)
	// Restartdan keyin eski "online" belgilari noto‘g'ri qoladi: hozir
	// birorta agent ulanmagan (§17 holatlar; 2026-11-20 kuzatildi).
	if n, err := reg.ResetOnlineStates(); err != nil {
		lg.Warn("online holatlar tozalanmadi", "xato", err)
	} else if n > 0 {
		lg.Info("eski online belgilari tozalandi", "mijozlar", n)
	}
	tokens, err := auth.NewTokens(cfg.Auth.JWTSecret,
		time.Duration(cfg.Auth.AccessTTLMin)*time.Minute,
		time.Duration(cfg.Auth.RefreshTTLD)*24*time.Hour, nil)
	if err != nil {
		return err
	}
	hub := wsagent.NewHub(wsagent.Config{
		ServerVersion: version.Version, MinProto: protoVersion, MaxProto: protoVersion,
	}, reg, lg)
	deps := &httpapi.Deps{
		Cfg:    cfg,
		Reg:    reg,
		Tokens: tokens,
		Login:  auth.NewRate(5, 5*time.Minute, nil),
		Audit:  audit.New(db),
		// §3: ulanish so'rovlari (kod bekor qilindi — 1.0-draft.10).
		Join:  wsagent.NewJoiner(db, reg, lg, cfg.Join.RatePer5m, nil),
		Agent: hub,
		Lg:    lg,
	}
	// --- S2 media bog'lamalari ---
	caps, gerr := gpu.Detect(cfg.Media.FFmpegPath)
	if gerr != nil {
		lg.Warn("GPU/ffmpeg aniqlanmadi — transcode o'chiriladi", "err", gerr)
	} else {
		lg.Info("media backend", "backend", caps.Backend, "encoder", caps.Encoder, "gpu", caps.GPUName)
	}
	if !caps.Override(cfg.Media.TranscodeBackend) {
		lg.Warn("transcode_backend so'ralmagan imkoniyat — avtomatik tanlovda qoladi",
			"requested", cfg.Media.TranscodeBackend, "detected", caps.Backend)
	}
	// Runtime probe: ro'yxatda bor encoder drayver/xato tufayli ishlamasligi
	// mumkin — zanjir bo'yicha tekshirib, ishlaydiganini tanlaymiz.
	caps, notes := gpu.Probe(cfg.Media.FFmpegPath, caps)
	for _, n := range notes {
		lg.Warn("probe: backend ishlamadi", "detail", n)
	}
	lg.Info("transcode backend (probe natijasi)", "backend", caps.Backend, "encoder", caps.Encoder)
	tr := media.NewTranscoder(cfg.Media.FFmpegPath, cfg.Media.MaxTranscodes, lg)
	defer tr.StopAll()
	mtokens := media.NewTokens(db, nil)
	mclient := media.NewClient(cfg.MediaMTX.ControlURL)
	sessRows, _ := reg.ListActiveSessions()
	for _, sr := range sessRows { // restart: jarayon yo'qoldi — DB'da failed
		_ = reg.SetSessionState(sr.ID, "failed")
	}
	mediaCfg := media.MgrConfig{
		MediaReadyTO: time.Duration(cfg.Media.ReadyTOs) * time.Second,
		SessionTTL:   time.Duration(cfg.Media.SessionTTLMin) * time.Minute,
		RefreshExt:   time.Duration(cfg.Media.SessionRenewMin) * time.Minute,
		PublishTTL:   time.Duration(cfg.Media.PublishTTLS) * time.Second,
		IdleStop:     time.Duration(cfg.Media.IdleStopS) * time.Second,
		UserMax:      cfg.Limits.UserMaxSessions,
		GlobalMax:    cfg.Limits.GlobalMaxSessions,
		PreviewMax:   cfg.Limits.PreviewMax,
	}
	uiHub := wsui.NewHub(tokens, lg, wsui.Config{})
	hub.SetUI(uiHub)
	publicHost := config.ResolvePublicHost(cfg.MediaMTX.PublicHost)
	lg.Info("media manzili (agent/brauzer ko‘radigan)", "host", publicHost,
		"sozlangan_public_host", cfg.MediaMTX.PublicHost)

	mgr := media.NewManager(mediaCfg, media.Deps{
		UI:  uiHub,
		Reg: reg, Store: reg, Agent: hub, MTX: mclient, Tokens: mtokens,
		Lg: lg, GPU: caps, Transcode: tr, PublicHost: publicHost,
		WHEPPort: cfg.MediaMTX.WebRTCPort, RTSPPort: cfg.MediaMTX.RTSPPort,
	})
	defer mgr.Close()
	tr.SetOnFailure(mgr.OnTranscodeFailure)
	hub.SetSink(mgr)
	deps.Media = mgr
	deps.MTXHook = media.NewHook(mtokens, lg)
	deps.UI = uiHub

	deps.Mux = httpapi.Routes(deps)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := newHTTPServer(cfg, lg, deps)
	return serveUntilSignal(ctx, lg, cfg, srv)
}

const protoVersion = 1 // protocol.md §1 (hozirgi kontrakt versiyasi)

func portOf(bind string) int {
	_, ps, err := net.SplitHostPort(bind)
	if err != nil {
		return 0
	}
	p, err := strconv.Atoi(ps)
	if err != nil {
		return 0
	}
	return p
}
