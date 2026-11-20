package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"central-vms/server/internal/config"
	"central-vms/server/internal/gpu"
)

// runProbe — `make probe` / `vmsd --probe` (root §4.2, §16.3).
//  1. Imkoniyatlarni aniqlaydi (ffmpeg -encoders/-decoders) va RUNTIME
//     probe bilan HAQIQATAN ishlaydigan backend'ni tanlaydi (drayver eski
//     bo'lsa nvenc ro'yxatda bo'lib ishda yiqiladi — amalda kuzatilgan).
//  2. Parallel transcode sig'imi O'LCHANADI (real-time factor 1.0 dan
//     yuqori bo'lgan maksimal parallel son) va settings'ga yoziladi.
//
// GPU modeli nomiga qarab tarmoq QILINMAYDI — faqat imkoniyat/o'lchov.
func runProbe(lg *slog.Logger, cfg config.Config, db *sql.DB, quick bool) error {
	lg.Info("probe boshlandi", slog.String("ffmpeg", cfg.Media.FFmpegPath))

	caps, err := gpu.Detect(cfg.Media.FFmpegPath)
	if err != nil {
		lg.Warn("ffmpeg topilmadi — transcode serverda o'chiriladi", slog.Any("err", err))
		fmt.Println("probe: ffmpeg IMKONIYATLARI: topilmadi")
		return nil
	}
	fmt.Printf("probe: aniqlangan backend (ro'yxat bo'yicha): %s (%s)\n", caps.Backend, caps.Encoder)
	if out, err := exec.Command("nvidia-smi", "--query-gpu=name,memory.total,driver_version",
		"--format=csv,noheader").Output(); err == nil {
		fmt.Printf("probe: GPU: %s\n", strings.TrimSpace(string(out)))
	} else {
		fmt.Println("probe: GPU: nvidia-smi yo'q (NVIDIA emas yoki drayver yo'q)")
	}

	// Runtime probe — ishlaydigan encodergacha zanjir bo'yicha tushadi.
	caps, notes := gpu.Probe(cfg.Media.FFmpegPath, caps)
	for _, n := range notes {
		lg.Warn("probe: backend ishlamadi", slog.String("detail", n))
		fmt.Printf("probe: ISHLAMAYDI: %s\n", strings.SplitN(n, ": ", 2)[0])
	}
	fmt.Printf("probe: TANLANGAN backend (runtime): %s encoder=%s hevc_decode=%s\n",
		caps.Backend, caps.Encoder, caps.HEVCDecoder)
	if caps.Backend == gpu.BackendNone {
		fmt.Println("probe: XATO: hech qanday encoder ishlamadi — server transcode o'chiriladi")
		return nil
	}

	// Sig'im o'lchovi: real-time factor >= 1.0 bo'lgan maksimal parallel son.
	maxN := cfg.Media.MaxTranscodes
	if !quick {
		fmt.Println("probe: parallel transcode sig'imi o'lchanmoqda (720p15, ~2 s har biri)…")
		maxN = 0
		for _, n := range []int{1, 2, 4, 6, 8} {
			rt, took := measureParallel(cfg.Media.FFmpegPath, caps.Encoder, n)
			fmt.Printf("probe:   parallel=%d  o'rtacha real-time=%.2f  (%.1f s)\n", n, rt, took.Seconds())
			if rt >= 0.95 {
				maxN = n
			} else {
				break
			}
		}
		if maxN == 0 {
			maxN = 1
		}
	} else {
		fmt.Println("probe: quick rejim — sig'im o'lchovi o'tkazib yuborildi")
	}
	fmt.Printf("probe: TAVSIYA: server_max_transcodes = %d\n", maxN)

	if db != nil {
		_, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('server_transcode_backend', ?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(caps.Backend))
		if err != nil {
			return fmt.Errorf("probe: settings yozish: %w", err)
		}
		if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('server_max_transcodes', ?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, strconv.Itoa(maxN)); err != nil {
			return fmt.Errorf("probe: settings yozish: %w", err)
		}
		fmt.Println("probe: settings'ga yozildi: server_transcode_backend, server_max_transcodes")
	}
	return nil
}

// measureParallel — N ta mustaqil qisqa encode'ni parallel yuborib,
// o'rtacha real-time factor qaytaradi (1.0 = real vaqtda ulguradi).
func measureParallel(ffmpegPath, encoder string, n int) (float64, time.Duration) {
	const durS = 2
	var wg sync.WaitGroup
	var mu sync.Mutex
	var sum, done float64
	t0 := time.Now()
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(ffmpegPath, "-hide_banner", "-loglevel", "error",
				"-f", "lavfi", "-i", fmt.Sprintf("testsrc=size=1280x720:rate=15:duration=%d", durS),
				"-an", "-c:v", encoder, "-preset", "veryfast", "-f", "null", "-")
			st := time.Now()
			if err := cmd.Run(); err != nil {
				return
			}
			el := time.Since(st).Seconds()
			mu.Lock()
			sum += float64(durS) / el
			done++
			mu.Unlock()
		}()
	}
	wg.Wait()
	took := time.Since(t0)
	if done == 0 {
		return 0, took
	}
	return sum / done, took
}
