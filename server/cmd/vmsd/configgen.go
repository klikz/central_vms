// configgen.go — BIRINCHI ISHGA TUSHIRISH: `-config` bilan ko'rsatilgan
// fayl BO'LMA-SA avtomatik default yaratiladi va dastur shu bilan ishga
// tushadi (2026-11-23 foydalanuvchi talabi).
//
// Qoidalar:
//   - Mavjud faylga HECQACHON tegilmaydi — buzilgan/ noto'g'ri config
//     bo'lsa ham oddiy xato bilan chiqamiz (ustiga yozish ma'lumot
//     yo'qotardi).
//   - Yo'llar binar turgan papkadan ABSOLYUT hisoblanadi — ish papkasiga
//     (cwd) bog'liq bo'lmaslik uchun; Windows/Linux bir xil mantik
//     (Windows'da C:/... ko'rinishida, to'g'ri slash'lar qabul qilinadi).
//   - jwt_secret tasodifiy generatsiya qilinadi (server bo'sh secret
//     bilan boshlanmas edi) — fayl 0600.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// exeBaseDir — binar joylashgan papka (symlink'lar ochilgan holda).
func exeBaseDir() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		exe = wd
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.ToSlash(filepath.Dir(exe))
}

const serverConfigTemplate = `# vmsd konfiguratsiyasi — BIRINCHI ISHGA TUSHIRISDA AVTOMATIK YARATILDI (%[1]s).
# DIQQAT: qiymatlarni muhitga qarab tekshirib chiqing (bind, public_host,
# web_dir). jwt_secret o'zi generatsiya qilindi — fayl 0600; u o'zgarsa
# barcha tokenlar bekor bo'ladi.

bind: ":8081"
web_dir: "%[2]s/web"
db_path: "%[2]s/var/vmsd.db"
migrations_dir: "%[2]s/migrations"

mediamtx:
  control_url: "http://127.0.0.1:9997"
  auth_hook_bind: "127.0.0.1:8189"
  webrtc_port: 8889
  rtsp_port: 8554
  hls_port: 8888
  public_host: ""

auth:
  jwt_secret: "%[3]s"
  access_ttl_min: 15
  refresh_ttl_days: 7

join:
  rate_per_5m: 5

limits:
  user_max_sessions: 16
  global_max_sessions: 48
  preview_max: 8
  per_client_uploads: 8

media:
  idle_stop_s: 10
  session_ttl_min: 5
  session_renew_min: 2
  publish_ttl_s: 60
  read_token_ttl_s: 30
  server_max_transcodes: 5
  ffmpeg_path: "ffmpeg"

retention_days: 90
log_level: "info"
log_file: "%[2]s/var/log/vmsd.log"
log_max_mb: 16
log_keep: 10
`

// ensureServerConfig — fayl bo'lmasa yaratadi; (created, err).
func ensureServerConfig(path string) (bool, error) {
	if path == "" {
		return false, fmt.Errorf("config yo'li bo'sh")
	}
	if _, err := os.Stat(path); err == nil {
		return false, nil // mavjud — TEGILMAYDI
	} else if !os.IsNotExist(err) {
		return false, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return false, fmt.Errorf("jwt_secret generatsiya: %w", err)
	}
	body := fmt.Sprintf(serverConfigTemplate,
		time.Now().UTC().Format(time.RFC3339), exeBaseDir(), hex.EncodeToString(secret))
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return false, fmt.Errorf("config papkasi: %w", err)
		}
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return false, fmt.Errorf("config yozish: %w", err)
	}
	return true, nil
}
