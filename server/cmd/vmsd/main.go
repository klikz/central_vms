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

	"central-vms/server/internal/config"
	"central-vms/server/internal/logx"
	"central-vms/server/internal/ports"
	"central-vms/server/internal/store"
	"central-vms/server/internal/version"
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
		migrateO = flag.Bool("migrate-only", false, "faqat migratsiyani bajarib chiqish")
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

	if *probe {
		return runProbe(lg, cfg)
	}

	// Port tekshiruvi (REJA §2.1/1): band bo'lsa aniq xato, tashqi xizmatni
	// o'zi to'xtatmaydi. API bind porti 0 bo'lsa (testlar) tekshirilmaydi.
	if apiPort := portOf(cfg.Bind); apiPort != 0 {
		if err := ports.Check(
			[]int{apiPort, cfg.MediaMTX.RTSPPort, cfg.MediaMTX.HLSPort, cfg.MediaMTX.WebRTCPort},
			[]int{9997, cfg.MediaMTX.WebRTCPort},
		); err != nil {
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := newHTTPServer(cfg, lg)
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
