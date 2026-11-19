// vmsagent — joydagi (site) agent: kameralarni boshqaradi va serverga bitta
// chiqish WebSocket orqali ulanadi (docs/protocol.md §2).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"central-vms/client/internal/camsvc"
	"central-vms/client/internal/config"
	"central-vms/client/internal/localui"
	"central-vms/client/internal/logbuf"
	"central-vms/client/internal/redact"
)

// Versiya ldflags orqali qo'yiladi (root REJA §4.3):
//
//	-X main.version=$(git describe --tags) -X main.commit=$(git rev-parse --short HEAD)
var (
	version = "dev"
	commit  = "none"
	builtAt = "unknown"
)

func printVersion() {
	fmt.Printf("vmsagent %s (commit %s, built %s)\n", version, commit, builtAt)
}

func main() {
	var (
		cfgPath     = flag.String("config", "config/client.yaml", "konfiguratsiya fayli (YAML)")
		showVersion = flag.Bool("version", false, "versiyani chiqarib chiqish")
		enrollCode  = flag.String("enroll", "", "bir martalik enrollment kodi (§3.1, bir marta ishlatiladi)")
	)
	flag.Parse()

	if *showVersion {
		printVersion()
		return
	}

	// Jurnal ekrani uchun oxirgi 500 qator xotirada (§17).
	ring := logbuf.New(500)
	log := newLogger("info", ring)
	log.Info("vmsagent ishga tushmoqda", "version", version, "commit", commit, "config", *cfgPath)

	// SIGINT/SIGTERM — graceful shutdown; SIGHUP — config reload (§9.4).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)

	if err := run(ctx, log, *cfgPath, *enrollCode, sighup, ring); err != nil {
		log.Error("agent ishi tugadi", "err", redact.Err(err))
		os.Exit(1)
	}
	log.Info("vmsagent to'xtatildi")
}

// newLogger — yozuvlar: avval redaktor (§10.2), keyin bufer (Jurnal ekrani),
// keyin stderr. Tartib muhim: buferga YASHIRILGAN qator tushadi.
func newLogger(level string, ring *logbuf.Ring) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	text := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	return slog.New(redact.NewHandlerWithNext(logbuf.NewHandler(text, ring)))
}

func run(ctx context.Context, log *slog.Logger, cfgPath, enrollCode string,
	sighup <-chan os.Signal, ring *logbuf.Ring) error {

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	log = newLogger(cfg.LogLevel, ring)

	rt := &runtime{cfg: cfg, log: log, ring: ring}
	if err := rt.bootstrap(ctx, enrollCode); err != nil {
		return err
	}

	// UI (§17): holat chizig'i sessiya holatini ko'rsatadi (C1.13)
	ui, err := localui.NewServer(cfg, log, version, localui.Deps{
		Status:   rt.statusFn,
		LogLines: func(limit int) []string { return ring.Recent(limit) },
		Cams:     camsvc.New(rt.st, rt.ks),
	})
	if err != nil {
		return fmt.Errorf("lokal UI: %w", err)
	}
	uiErr := make(chan error, 1)
	go func() { uiErr <- ui.Run(ctx, cfg.UIBind) }()
	log.Info("lokal UI ishga tushdi", "addr", "http://"+cfg.UIBind)

	// Sessiya: uzilishda backoff bilan qayta ulanadi (§2, §3.3)
	sessStop := rt.startSession(ctx)
	defer sessStop()

	for {
		select {
		case <-ctx.Done():
			log.Info("to'xtatish belgisi olindi, mayin chiqamiz")
			sessStop()
			return <-uiErr
		case err := <-uiErr:
			if err != nil {
				return fmt.Errorf("lokal UI: %w", err)
			}
			return nil
		case <-sighup:
			// §9.4: config qayta o'qiladi, publish'lar uzilmaydi.
			nc, err := config.Load(cfgPath)
			if err != nil {
				log.Warn("SIGHUP: config qayta o'qilmadi", "err", redact.Err(err))
				continue
			}
			rt.mu.Lock()
			serverChanged := nc.ServerURL != rt.cfg.ServerURL
			rt.cfg = nc
			rt.mu.Unlock()
			rt.log = newLogger(nc.LogLevel, ring)
			log.Info("config qayta o'qildi", "log_level", nc.LogLevel, "server_url", redact.String(nc.ServerURL))
			if serverChanged && rt.st != nil {
				rt.st.ServerURL = nc.ServerURL
				if err := rt.st.Save(); err != nil {
					log.Warn("state saqlanmadi", "err", redact.Err(err))
				}
				sessStop()
				sessStop = rt.startSession(ctx)
				log.Info("server_url o'zgardi — sessiya qayta boshlandi")
			}
		}
	}
}

func ffmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

func checkFFmpeg(log *slog.Logger) {
	if !ffmpegAvailable() {
		log.Warn("ffmpeg topilmadi — H.265 transcode/remux ishlamaydi (tashqi bog'liqlik)")
		return
	}
	out, err := exec.Command("ffmpeg", "-hide_banner", "-muxers").Output()
	if err == nil && !containsWhip(string(out)) {
		log.Warn("ffmpeg versiyasi eski: '-f whip' yo'q (>= 7.1 kerak)")
	}
}

// containsWhip — `ffmpeg -muxers` chiqishida whip muxeri bormi (FFmpeg >= 7.1).
func containsWhip(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "E") && strings.Contains(line, "whip") {
			return true
		}
	}
	return false
}
