// doctor.go — `vmsagent -doctor`: bir buyruqli o'zini-tekshirish
// (2026-11-22 barqarorlik ishlari; server tomondagi `vmsd -doctor` bilan
// bir uslub). Hech narsani o'zgartirmaydi: o'qish + TCP dial + qisqa
// fayl tekshiruvi. FAIL bo'lsa exit 1.
package main

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/config"
	"central-vms/client/internal/keystore"
	"central-vms/client/internal/statestore"
)

type agentReport struct{ fails, warns int }

func (r *agentReport) ok(f string, a ...any)   { fmt.Printf("OK   "+f+"\n", a...) }
func (r *agentReport) warn(f string, a ...any) { r.warns++; fmt.Printf("WARN "+f+"\n", a...) }
func (r *agentReport) fail(f string, a ...any) { r.fails++; fmt.Printf("FAIL "+f+"\n", a...) }

func runAgentDoctor(cfg config.Config) error {
	r := &agentReport{}
	fmt.Printf("vmsagent doctor (%s)\n", cfg.Path)

	// 1. Server manzili va yetib borish.
	if cfg.ServerURL == "" {
		r.fail("server_url bo'sh — config/client.yaml'ga server manzilini yozing")
	} else if base, err := httpBase(cfg.ServerURL); err != nil {
		r.fail("server_url yaroqsiz: %v", err)
	} else {
		hc := &http.Client{Timeout: 4 * time.Second}
		resp, err := hc.Get(base + "/healthz")
		if err != nil {
			r.fail("serverga yetib borilmadi (%s/healthz): %v — server o'chiqmi, tarmoq/TLS to'g'rimi?", base, err)
		} else {
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				r.warn("server /healthz %d qaytardi (proksi orqali bo'lishi mumkin)", resp.StatusCode)
			} else {
				r.ok("server: %s javob berdi", cfg.ServerURL)
			}
		}
	}

	// 2. Kalit fayli (agent_id kaliti; 0600 bo'lishi shart).
	if st, err := os.Stat(cfg.KeyFile); err != nil {
		r.warn("kalit fayli yo'q (%s) — agent hali ro'yxatdan o'tmagan (lokal UI'dan join qiling)", cfg.KeyFile)
	} else {
		if !keystore.KeyPermsOK(st) {
			r.warn("kalit fayli izinlari %o — chmod 600 tavsiya etiladi", st.Mode().Perm())
		} else {
			r.ok("kalit fayli: %s (%o)", cfg.KeyFile, st.Mode().Perm())
		}
	}

	// 3. StateDir yozuv uchun yaroqli (state.json shu yerda).
	tmpPath := filepath.Join(cfg.StateDir, ".doctor-write-test")
	if err := os.MkdirAll(cfg.StateDir, 0o750); err != nil {
		r.fail("state_dir yaratib bo'lmadi (%s): %v", cfg.StateDir, err)
	} else if err := os.WriteFile(tmpPath, []byte("x"), 0o600); err != nil {
		r.fail("state_dir yozuv uchun yopiq (%s): %v (systemd ostida User=vmsagent — ReadWritePaths tekshiring)", cfg.StateDir, err)
	} else {
		_ = os.Remove(tmpPath)
		r.ok("state_dir: %s (yozuv OK)", cfg.StateDir)
	}

	// 4. state.json: agent_id + kameralar + TCP yetib borish.
	st, err := statestore.Load(filepath.Join(cfg.StateDir, "state.json"))
	if err != nil && !os.IsNotExist(err) {
		r.fail("state.json o'qilmadi: %v", err)
	} else if st == nil || st.AgentID == "" {
		r.warn("agent_id hali yo'q — serverga birinchi ulanish/join bajarilmagan")
	} else {
		r.ok("agent_id: %s", st.AgentID)
		camList := st.SnapshotCameras()
		var enabled []cams.Camera
		for _, c := range camList {
			if c.Enabled {
				enabled = append(enabled, c)
			}
		}
		if len(enabled) == 0 {
			r.warn("yoqilgan kamera yo'q (jami %d) — lokal UI'dan qo'shing", len(camList))
		} else {
			var unreachable []string
			for _, c := range enabled {
				addr := net.JoinHostPort(c.Host, fmt.Sprint(c.Port))
				conn, err := net.DialTimeout("tcp", addr, 1500*time.Millisecond)
				if err != nil {
					unreachable = append(unreachable, fmt.Sprintf("%s (%s)", c.Name, addr))
					continue
				}
				_ = conn.Close()
			}
			if len(unreachable) == 0 {
				r.ok("kameralar: %d ta, barchasining RTSP portiga yetib borildi", len(enabled))
			} else {
				r.fail("kameralarga yetib borilmadi: %s", strings.Join(unreachable, "; "))
			}
		}
	}

	// 5. FFmpeg (fallback publish/local transcode uchun).
	if cfg.Limits.MaxLocalTranscodes > 0 || cfg.Publish.FFmpegFallback {
		if p, err := exec.LookPath("ffmpeg"); err != nil {
			r.warn("ffmpeg topilmadi — local transcode/WHIP fallback ishlamaydi")
		} else {
			r.ok("ffmpeg: %s", p)
		}
	}

	// 6. Lokal UI bindi (localhost bo'lmasa parol eksporti xavfsiz emas —
	// main.go'dagi ogohlantirish bilan bir xil; doctor oldindan ko'rsatadi).
	if !strings.HasPrefix(cfg.UIBind, "127.0.0.1") && !strings.HasPrefix(cfg.UIBind, "localhost") {
		r.warn("ui_bind=%s localhost EMAS — lokal UI tarmoqqa ochiq, parol eksportini ehtiyot qiling", cfg.UIBind)
	} else {
		r.ok("ui_bind: %s (faqat lokal)", cfg.UIBind)
	}

	// 7. Log papkasi yozuv uchun yaroqli.
	if lg := filepath.Dir(cfg.LogFile); lg != "" && lg != "." {
		if f, err := os.OpenFile(filepath.Join(lg, ".doctor.logtest"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err != nil {
			r.warn("log papkasiga yozib bo'lmadi (%s): %v", lg, err)
		} else {
			_ = f.Close()
			_ = os.Remove(filepath.Join(lg, ".doctor.logtest"))
			r.ok("log papkasi: %s", lg)
		}
	}

	fmt.Printf("\nxulosa: %d FAIL, %d WARN\n", r.fails, r.warns)
	if r.fails > 0 {
		return fmt.Errorf("doctor: %d ta xato topildi", r.fails)
	}
	return nil
}

// httpBase — server_url (ws:// yoki wss://) dan HTTP asos: /healthz
// tekshiruvi uchun.
func httpBase(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("noto'g'ri sxema: %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(u.Path, "/api/agent/ws") {
		u.Path = strings.TrimSuffix(u.Path, "/api/agent/ws")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
