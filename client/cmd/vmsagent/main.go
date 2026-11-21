// vmsagent — joydagi (site) agent: kameralarni boshqaradi va serverga bitta
// chiqish WebSocket orqali ulanadi (docs/protocol.md §2).
package main

import (
	"central-vms/client/internal/cams"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"central-vms/client/internal/camsvc"
	"central-vms/client/internal/config"
	"central-vms/client/internal/discovery"
	"central-vms/client/internal/localui"
	"central-vms/client/internal/logbuf"
	"central-vms/client/internal/logx"
	"central-vms/client/internal/media"
	"central-vms/client/internal/publish"
	"central-vms/client/internal/redact"
	"central-vms/client/internal/selftest"
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

	// run() xatoni o'zi loglaydi — FAYL ham ulangan logger orqali
	// (main'dagi log hali faylsiz: yo'l konfigdan bilinadi).
	if err := run(ctx, log, *cfgPath, sighup, ring); err != nil {
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
	return newLoggerAt(level, ring, file, os.Stderr)
}

// newLoggerAt — terminal yozuvchi inject qilinadi (test uchun).
// agentLevel — barcha loggerlar shu o'zgaruvchini ishlatadi: UI orqali
// log_level o'zgarsa qayta qurish talab qilinmaydi, darhol kuchga kiradi.
var agentLevel = new(slog.LevelVar)

func newLoggerAt(level string, ring *logbuf.Ring, file *logx.Writer, term io.Writer) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	agentLevel.Set(lvl)
	out := io.Writer(term)
	if file != nil && file.Enabled() {
		out = io.MultiWriter(term, file)
	}
	text := slog.NewTextHandler(out, &slog.HandlerOptions{Level: agentLevel})
	return slog.New(redact.NewHandlerWithNext(logbuf.NewHandler(text, ring)))
}

// setLogLevel — UI "Save" orqali: noto'g'ri nom bo'lsa info da qoladi.
func setLogLevel(name string) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(name)); err != nil {
		lvl = slog.LevelInfo
	}
	agentLevel.Set(lvl)
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

func run(ctx context.Context, log *slog.Logger, cfgPath string,
	sighup <-chan os.Signal, ring *logbuf.Ring) (runErr error) {

	// BARCHA xatolar terminal + faylga: chiqish nuqtasida bir marta,
	// fayl ulangan logger bilan yoziladi (detailed: err zanjiri).
	defer func() {
		if runErr != nil {
			log.Error("agent ishi tugadi", "err", redact.Err(runErr))
		}
	}()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		// BARCHA xatolar faylga ham tushishi kerak: config buzuq bo'lsa
		// ham — standart CLIENT log yo'liga yozamiz (sozlab bo'lmaydi).
		if fw, ferr := logx.New(defaultLogFile, 4, 5); ferr == nil {
			defer fw.Close()
			log = newLogger("info", ring, fw) // defer ham faylli loggerni ishlatadi
			log.Error("KONFIGURATSIYA O'QILMADI", "path", cfgPath, "err", redact.Err(err))
		}
		return fmt.Errorf("konfiguratsiya: %w", err)
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

	rt := &runtime{cfg: cfg, cfgPath: cfgPath, log: log, ring: ring, reloadCh: make(chan bool, 1)}
	rt.selftest = rt.newSelftest() // C4.6 paneli (deps'lar lazy o'qiydi)
	// C3.20: registry + media boshqaruvchisi (hodisalar o'zaro bog'langan).
	// gate — kamera qurilmasiga RTSP murojaatlari: pull oqimi faol
	// paytida probe/preview rasm o'tkazib yuboriladi (§12.1/§8.3b).
	rt.gate = cams.NewGate()
	rt.media = media.New(media.Config{
		Log:          rt.log,
		Gate:         rt.gate,
		LookupCamera: rt.lookupCamera,
		LookupSecret: func(ref string) (string, bool) { return rt.ks.Get(ref) },
		Send: func(typ string, data any) error {
			rt.mu.Lock()
			sess := rt.sess
			rt.mu.Unlock()
			if sess == nil {
				return errors.New("sessiya yo'q")
			}
			return sess.Send(typ, data)
		},
	})
	rt.publishes = publish.New(clockReal(), rt.log, rt.media.OnReady, rt.media.OnStopped)
	rt.media.SetRegistry(rt.publishes)
	if err := rt.bootstrap(ctx); err != nil {
		return err
	}
	// §3 (1.0-draft.10): agent ulanish so'rovi yuboradi va admin tasdig'ini
	// kutadi — tasdiqlangach sessiya avtomatik boshlanadi.
	// §3.2: so'rov/tasdiq kuzatuvi doim yuradi — identity bo'lmasa
	// (birinchi ishga tushish yoki "Clear connection" dan keyin) so'rov
	// yuboradi, bo'lsa jim turadi.
	go rt.joinLoop(ctx, 15*time.Second)
	if cfg.UIPin != "" {
		log.Warn("ui_pin kaliti endi ishlatilmaydi (§10.3 bekor qilindi) — fayldan olib tashlang")
	}
	if h := strings.SplitN(cfg.UIBind, ":", 2)[0]; h != "127.0.0.1" && h != "localhost" && h != "::1" {
		log.Warn("lokal UI tarmoqda ochiq — PIN olib tashlangan, shuning uchun bu manzilga "+
			"faqat ishonchli tarmoqda ruxsat bering", "ui_bind", cfg.UIBind)
	}

	// UI (§17): holat chizig'i sessiya holatini ko'rsatadi (C1.13)
	cs := camsvc.New(rt.st, rt.ks)
	cs.OnChanged = rt.onCamsChanged
	rt.cs = cs // §6.5: qo'shish/o'chirish darhol sync
	pv := newPreviewSvc(cs, rt.ks, rt.previews, rt.snap, rt.log, rt.reportCamStatus)
	pv.SetGate(rt.gate)
	rt.pv = pv
	go pv.Run(ctx)
	ui, err := localui.NewServer(cfg, log, version, localui.Deps{
		Status:       rt.statusFn,
		LogLines:     func(limit int) []string { return ring.Recent(limit) },
		Cams:         cs,
		Discovery:    newDiscoverySvc(cs, cfg),
		Snapshots:    pv,
		SelftestRun:  func(ctx context.Context) []selftest.Result { return rt.selftest.Run(ctx) },
		SelftestLast: func() []selftest.Result { return rt.selftest.Last() },
		// UI "Save" → faylga yozish + sessiyani qayta boshlash;
		// UI "Join" → §3.1 ulanish so'rovini yuborish/holatini so'rash.
		SetSettings:   rt.setSettings,
		GetSettings:   rt.currentSettings,
		JoinNow:       rt.joinFromUI,
		ResetIdentity: rt.resetIdentity,
	})
	if err != nil {
		return fmt.Errorf("lokal UI: %w", err)
	}
	uiErr := make(chan error, 1)
	// ui_bind localhost bo'lmasa — ogohlantirish (1.0-draft.10: LAN bind
	// taqiqlanmaydi, lekin xavf ko'rinib turishi kerak). Parol eksporti
	// endi bir martalik nonce talab qiladi (§10.2 audit, 2026-11-21).
	if h, _, err := net.SplitHostPort(cfg.UIBind); err == nil &&
		h != "127.0.0.1" && h != "localhost" && h != "::1" && h != "" {
		log.Warn("lokal UI tarmoqda ochiq", "ui_bind", cfg.UIBind,
			"izoh", "bu manzildagi har qanday PC kamera qo'sha/tahrirlay oladi; parol eksporti nonce talab qiladi")
	}
	go func() { uiErr <- ui.Run(ctx, cfg.UIBind) }()
	log.Info("lokal UI ishga tushdi", "addr", "http://"+cfg.UIBind)

	// Sessiya: uzilishda backoff bilan qayta ulanadi (§2, §3.3)
	sessStop := rt.startSession(ctx)
	defer sessStop()
	// §4.6: media_stats — 10 s'da bir faol publish'larni yig'adi (C3.15).
	stats := publish.NewStats(rt.publishes, clockReal(), rt.media.StatsSource, rt.media.StatsEmit)
	go stats.Run(ctx)

	// reload — §9.4: config qayta o'qiladi, publish'lar uzilmaydi.
	// SIGHUP va UI "Save"/"Enroll" bitta yo'lni ishlatadi (xulq bir xil).
	reload := func(force bool) {
		nc, err := config.Load(cfgPath)
		if err != nil {
			log.Warn("config qayta o'qilmadi", "err", redact.Err(err))
			return
		}
		rt.mu.Lock()
		serverChanged := nc.ServerURL != rt.cfg.ServerURL
		rt.cfg = nc
		rt.mu.Unlock()
		if nc.LogFile != cfg.LogFile || nc.LogMaxMB != cfg.LogMaxMB || nc.LogKeep != cfg.LogKeep {
			if nw, werr := openLogFile(&nc); werr != nil {
				log.Error("log fayli qayta ochilmadi", "path", nc.LogFile, "err", redact.Err(werr))
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
		log = rt.log
		log.Info("config qayta o'qildi", "log_level", nc.LogLevel, "server_url", redact.String(nc.ServerURL))
		if rt.st != nil && (serverChanged || force) {
			rt.st.ServerURL = nc.ServerURL
			if err := rt.st.Save(); err != nil {
				log.Warn("state saqlanmadi", "err", redact.Err(err))
			}
			sessStop()
			sessStop = rt.startSession(ctx)
			log.Info("sessiya qayta boshlandi", "server", redact.String(nc.ServerURL))
		}
	}

	for {
		select {
		case <-ctx.Done():
			// §9.4 graceful shutdown tartibi: publish'lar (media_stopped
			// {agent_restart}) → WS 1001 → state flush → chiqish.
			log.Info("to'xtatish belgisi olindi, mayin chiqamiz")
			rt.mu.Lock()
			pub := rt.publishes
			rt.mu.Unlock()
			if pub != nil {
				pub.CloseAll(publish.ReasonAgentRestart)
			}
			sessStop() // WS'ni 1001 (Going Away) bilan yopadi
			if rt.st != nil {
				if err := rt.st.Save(); err != nil {
					log.Error("state flush qilinishi muvaffaqiyatsiz", "err", redact.Err(err))
				}
			}
			return <-uiErr
		case err := <-uiErr:
			if err != nil {
				return fmt.Errorf("lokal UI: %w", err)
			}
			return nil
		case <-sighup:
			reload(false)
		case force := <-rt.reloadCh:
			// UI "Save"/"Enroll": fayl allaqachon yangilangan; force=true
			// bo'lsa manzil o'zgarmasa ham sessiya qayta boshlanadi
			// (ro'yxatdan o'tgandan keyin darhol ulanish uchun).
			reload(force)
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

// newDiscoverySvc — §16.2: WS-Discovery + port-scan skaneri.
// CIDRs sozlanmagan bo'lsa skaner faol interfeys tarmoqlarini oladi.
func newDiscoverySvc(cs *camsvc.Service, cfg config.Config) *discovery.Scanner {
	return discovery.NewScanner(cs, func() []discovery.Known {
		var out []discovery.Known
		for _, c := range cs.List() {
			out = append(out, discovery.Known{ID: c.ID, IP: c.Host, Port: c.Port})
		}
		return out
	}, discovery.ScannerConfig{
		CIDRs:    cfg.Discovery.CIDRs,
		Ports:    cfg.Discovery.RTSPScanPorts,
		MaxHosts: cfg.Discovery.MaxHosts,
	})
}
