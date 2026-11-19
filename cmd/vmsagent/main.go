// vmsagent — joydagi (site) agent: kameralarni boshqaradi va serverga bitta
// chiqish WebSocket orqali ulanadi (docs/protocol.md §2).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
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
	"central-vms/client/internal/logx"
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
	log := newLogger("info", ring, nil)
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
// keyin stderr + (bor bo'lsa) ALLOHIDA log fayli. Tartib muhim: buferga va
// faylga YASHIRILGAN qator tushadi. file==nil → faqat terminal.
// Barcha xatolar (level=error ham) ikkala joyga ham yoziladi — level
// filtridan error har doim o'tadi.
func newLogger(level string, ring *logbuf.Ring, file *logx.Writer) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	out := io.Writer(os.Stderr)
	if file != nil && file.Enabled() {
		out = io.MultiWriter(os.Stderr, file)
	}
	text := slog.NewTextHandler(out, &slog.HandlerOptions{Level: lvl})
	return slog.New(redact.NewHandlerWithNext(logbuf.NewHandler(text, ring)))
}

// defaultLogFile — config o'qilmagan holatda ham xatolar tushadigan
// standart CLIENT log yo'li.
const defaultLogFile = "/var/log/vmsagent/vmsagent.log"

// openLogFile — konfiguratsiyadagi CLIENT log faylini ochadi.
// Xato bo'lsa: terminalga BATAFSIL xato + joriy katalogda zaxira urinish.
func openLogFile(cfg *config.Config) (*logx.Writer, error) {
	w, err := logx.New(cfg.LogFile, cfg.LogMaxMB, cfg.LogKeep)
	if err == nil {
		return w, nil
	}
	fallback := "logs/vmsagent.log"
	if cfg.LogFile == fallback {
		return nil, err
	}
	w2, err2 := logx.New(fallback, cfg.LogMaxMB, cfg.LogKeep)
	if err2 != nil {
		return nil, fmt.Errorf("%w (zaxira %s ham: %v)", err, fallback, err2)
	}
	return w2, err // zaxira ochildi; asosiy xato bilan qaytamiz
}

func run(ctx context.Context, log *slog.Logger, cfgPath, enrollCode string,
	sighup <-chan os.Signal, ring *logbuf.Ring) error {

	cfg, err := config.Load(cfgPath)
	if err != nil {
		// BARCHA xatolar faylga ham tushishi kerak: config buzuq bo'lsa
		// ham — standart CLIENT log yo'liga yozamiz (sozlab bo'lmaydi).
		if fw, ferr := logx.New(defaultLogFile, 4, 5); ferr == nil {
			defer fw.Close()
			dlog := newLogger("info", ring, fw)
			dlog.Error("KONFIGURATSIYA O'QILMADI", "path", cfgPath, "err", redact.Err(err))
		}
		return err
	}
	logw, lerr := openLogFile(&cfg)
	if lerr != nil {
		log.Error("LOG FAYLI OCHILMADI — xatolar faylga yozilmayapti",
			"path", cfg.LogFile, "err", redact.Err(lerr))
	}
	if logw != nil {
		defer logw.Close()
	}
	log = newLogger(cfg.LogLevel, ring, logw)
	if logw != nil && logw.Enabled() {
		log.Info("log fayli", "path", logw.Path(), "max_mb", cfg.LogMaxMB, "keep", cfg.LogKeep)
	}

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
			if nc.LogFile != cfg.LogFile || nc.LogMaxMB != cfg.LogMaxMB || nc.LogKeep != cfg.LogKeep {
				if nw, werr := openLogFile(&nc); werr != nil {
					log.Error("SIGHUP: log fayli qayta ochilmadi", "path", nc.LogFile, "err", redact.Err(werr))
				} else {
					old := rt.logw
					rt.logw = nw
					if old != nil {
						_ = old.Close()
					}
					logw = nw
				}
			}
			rt.log = newLogger(nc.LogLevel, ring, logw)
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
