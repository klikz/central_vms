// doctor.go — `vmsd -doctor`: bir buyruqli o'zini-tekshirish (2026-11-22
// barqarorlik ishlari). Maqsad: "nima xato?" savoliga support talabsiz
// javob berish — config, DB, migratsiyalar, MediaMTX, portlar, ffmpeg,
// zaxira yangiligi va disk joyi. Chiqish: OK/WARN/FAIL qatorlari;
// FAIL bo'lsa exit 1 (WARN — 0, chunki ular ko'pincha ixtiyoriy).
//
// Eslatma: systemd ostida jwt_secret /etc/vmsd/secret.env'da bo'ladi —
// qo'lda tekshirishda avval `set -a; . /etc/vmsd/secret.env; set +a`.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"central-vms/server/internal/config"
	"central-vms/server/internal/store"
)

type doctorReport struct{ fails, warns int }

func (r *doctorReport) ok(f string, a ...any)   { fmt.Printf("OK   "+f+"\n", a...) }
func (r *doctorReport) warn(f string, a ...any) { r.warns++; fmt.Printf("WARN "+f+"\n", a...) }
func (r *doctorReport) fail(f string, a ...any) { r.fails++; fmt.Printf("FAIL "+f+"\n", a...) }

func runDoctor(cfg config.Config) error {
	r := &doctorReport{}
	fmt.Printf("vmsd doctor (%s)\n", cfg.Path)

	// 1. JWT sir — server umuman boshlanmasligining 1-sababi.
	if len(cfg.Auth.JWTSecret) >= 32 {
		r.ok("jwt_secret: yaroqli (%d belgi)", len(cfg.Auth.JWTSecret))
	} else {
		r.fail("jwt_secret: bo'sh/32 belgidan qisqa — config auth.jwt_secret yoki VMSD_JWT_SECRET (systemd: /etc/vmsd/secret.env) kerak")
	}

	// 2. DB: ochilishi, butunligi, migratsiyalar.
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		r.fail("DB ochilmadi (%s): %v", cfg.DBPath, err)
	} else {
		defer db.Close()
		var ic string
		if err := db.QueryRow(`PRAGMA integrity_check;`).Scan(&ic); err != nil || ic != "ok" {
			r.fail("DB integrity_check: %v (%s) — tiklash: zaxiradan qaytarish (deploy/install-notes)")
		} else {
			r.ok("DB: %s (integrity ok, WAL)", cfg.DBPath)
		}
		applied, available, err := store.Status(db, cfg.Migrations)
		switch {
		case err != nil:
			r.fail("migratsiya holati o'qilmadi: %v (migrations_dir: %s)", err, cfg.Migrations)
		case len(applied) < len(available):
			r.fail("migratsiyalar to'liq qo'llanilmagan: %d/%d — serverni bir marta ishga tushiring yoki `vmsd -migrate up`", len(applied), len(available))
		default:
			r.ok("migratsiyalar: %d ta qo'llanilgan", len(applied))
		}
	}

	// 3. Ishlab turgan nusxa (doctor server yonida ham ishlaydi).
	running := healthzOK(cfg.Bind)
	if running != "" {
		r.ok("server ishlayapti: /healthz javob berdi (%s)", running)
	} else {
		r.warn("server hozir ishlamayapti (%s band emas yoki javob yo'q)", cfg.Bind)
		// Portlar bandmi (tashqi xizmat egallab olganmi)?
		for _, p := range []string{cfg.Bind, cfg.MediaMTX.AuthHookBind} {
			ln, err := net.Listen("tcp", p)
			if err != nil {
				r.fail("bind %s band: %v (make port-check yoki tashqi xizmatni to'xtating)", p, err)
			} else {
				_ = ln.Close()
			}
		}
	}

	// 4. UI statikasi.
	if st, err := os.Stat(filepath.Join(cfg.WebDir, "index.html")); err != nil {
		r.warn("UI topilmadi (%s/index.html) — `make ui-build` bajarilganmi? API ishlaydi, brauzer bo'sh qoladi", cfg.WebDir)
	} else {
		r.ok("UI statikasi: %s (%d bayt)", cfg.WebDir, st.Size())
	}

	// 5. MediaMTX: API (control) va RTSP porti. MediaMTX o'lsa preview'lar
	// javobsiz qoladi — eng ko'p qidiriladigan belgi.
	if host, port := urlHostPort(cfg.MediaMTX.ControlURL, "9997"); true {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 2*time.Second)
		if err != nil {
			r.fail("MediaMTX API ulanalmadi (%s:%s): %v — vms-media.service ishlayaptimi?", host, port, err)
		} else {
			_ = c.Close()
			r.ok("MediaMTX API: %s:%s javob berdi", host, port)
		}
	}
	rtspAddr := net.JoinHostPort("127.0.0.1", fmt.Sprint(cfg.MediaMTX.RTSPPort))
	if c, err := net.DialTimeout("tcp", rtspAddr, 2*time.Second); err != nil {
		r.fail("MediaMTX RTSP portga ulanalmadi (%s): %v", rtspAddr, err)
	} else {
		_ = c.Close()
		r.ok("MediaMTX RTSP: %s tinglayapti", rtspAddr)
	}

	// 6. FFmpeg (transcode uchun).
	ff := cfg.Media.FFmpegPath
	if ff == "" {
		ff = "ffmpeg"
	}
	if p, err := exec.LookPath(ff); err != nil {
		r.warn("ffmpeg topilmadi (%s) — H.265 transcode ishlamaydi, HEVC kameralar original oqimda qoladi", ff)
	} else {
		out, _ := exec.Command(p, "-version").Output()
		line := firstLine(string(out))
		r.ok("ffmpeg: %s (%s)", p, line)
	}

	// 7. Zaxira yangiligi (vmsd-backup.timer sozlanmagan mashinalarni
	// aniqlash uchun).
	bdir := os.Getenv("VMS_BACKUP_DIR")
	if bdir == "" {
		bdir = "/var/backups/vmsd"
	}
	files, _ := filepath.Glob(filepath.Join(bdir, "vmsd-*.db"))
	if len(files) == 0 {
		r.warn("zaxira papkasida nusxa yo'q (%s) — `make install` timer o'rnatganmi? Qo'lda: deploy/backup.sh", bdir)
	} else {
		var newest time.Time
		for _, f := range files {
			if st, err := os.Stat(f); err == nil && st.ModTime().After(newest) {
				newest = st.ModTime()
			}
		}
		if age := time.Since(newest); age > 26*time.Hour {
			r.warn("oxirgi zaxira %s oldin (%s) — timer ishlamayapti yoki noto'g'ri papkaga yozmoqda", age.Round(time.Hour), newest.Format(time.RFC3339))
		} else {
			r.ok("zaxira: %d nusxa, oxirgisi %s oldin", len(files), age.Round(time.Minute))
		}
	}

	// 8. Disk joyi (DB turgan disk; 500 MB'dan kam bo'lsa SQLite yozuv
	// qotadi — eng yashirin muammo). Platform-spesifik qismi:
	// doctor_disk_unix.go (Statfs) / doctor_disk_windows.go
	// (GetDiskFreeSpaceEx) — `make buildWin` shu tufayli uzilmasligi uchun.
	if dir := filepath.Dir(cfg.DBPath); true {
		if free, ok := diskFreeBytes(dir); ok {
			switch {
			case free < 200<<20:
				r.fail("disk joyi: %s'da faqat %d MB qoldi — DB yozuvlari qotadi", dir, free>>20)
			case free < 1<<30:
				r.warn("disk joyi: %s'da %d MB qoldi (1 GB tavsiya etiladi)", dir, free>>20)
			default:
				r.ok("disk joyi: %s'da %d MB bo'sh", dir, free>>20)
			}
		} else {
			r.warn("disk joyi aniqlanmadi (%s)", dir)
		}
	}

	fmt.Printf("\nxulosa: %d FAIL, %d WARN\n", r.fails, r.warns)
	if r.fails > 0 {
		return fmt.Errorf("doctor: %d ta xato topildi", r.fails)
	}
	return nil
}

// healthzOK — ishlayotgan server versiyasini qaytaradi (bo'sh — ishlamayapti).
func healthzOK(bind string) string {
	host, port := splitHostPort(bind)
	u := fmt.Sprintf("http://%s:%s/healthz", host, port)
	c, err := http.Get(u)
	if err != nil {
		return ""
	}
	defer c.Body.Close()
	if c.StatusCode != http.StatusOK {
		return ""
	}
	var v struct {
		Version string `json:"version"`
	}
	_ = json.NewDecoder(c.Body).Decode(&v)
	if v.Version != "" {
		return v.Version
	}
	return "javob bor"
}

func urlHostPort(raw, defPort string) (string, string) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "127.0.0.1", defPort
	}
	p := u.Port()
	if p == "" {
		p = defPort
	}
	return u.Hostname(), p
}

func splitHostPort(bind string) (string, string) {
	host, port, err := net.SplitHostPort(bind)
	if err != nil { // ":8081" ko'rinishi
		return "127.0.0.1", strings.TrimPrefix(bind, ":")
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return host, port
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
