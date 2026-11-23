// configgen.go — BIRINCHI ISHGA TUSHIRISH (2026-11-23 foydalanuvchi
// talabi): `-config` bilan ko'rsatilgan fayl BO'LMA-SA default yaratiladi
// va agent shu bilan ishga tushadi.
//
//   - Mavjud faylga HECQACHON tegilmaydi (buzilgan bo'lsa ham — xato bilan
//     chiqamiz).
//   - server_url BO'SH yaratiladi: agent baribir ishga tushadi, lokal UI
//     (http://127.0.0.1:8082) orqali server manzili kiritiladi — UI "Save"
//     uni shu faylga yozadi va sessiyani qayta boshlaydi (agent.go
//     setSettings → config.SetValues → reload).
//   - Yo'llar binar turgan papkadan ABSOLYUT (Windows: C:/..., Linux:
//     /opt/... yoki build/) — cwd'ga bog'liq emas; Windows/Linux bir xil
//     mantik.
//
// DIQQAT: shablon mijozning o'z YAML o'quvchisiga (yamlsub) mos bo'lishi
// kerak — 2 bo'shliq indent, oddiy kalit:qiymat, izoh `#`.
package main

import (
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

const agentConfigTemplate = `# vmsagent konfiguratsiyasi - BIRINCHI ISHGA TUSHIRISDA AVTOMATIK YARATILDI (%[1]s).
# server_url BO'SH: dastur baribir ishga tushadi. Brauzerda lokal UI'ni
# oching (ui_bind manzili) va Server URL maydoniga server manzilini
# yozing - u shu faylga saqlanadi, sessiya avtomatik qayta boshlanadi.

server_url: ""
ui_bind: "127.0.0.1:8082"
client_name: ""
state_dir: "%[2]s/data"
key_file: "%[2]s/data/agent.key"
log_level: info
log_file: "%[2]s/logs/vmsagent.log"
log_max_mb: 4
log_keep: 5

probe:
  status_parallel: 5
  status_keepalive: auto
  codec_probe_interval_h: 24
  full_probe_on_publish_fail: true

limits:
  max_uploads: 4
  max_local_transcodes: 4

onvif:
  enabled: true
  switch_codec: true

publish:
  whip_fallback: rtsp_tcp
  ffmpeg_fallback: true
`

// ensureAgentConfig — fayl bo'lmasa yaratadi; (created, err).
func ensureAgentConfig(path string) (bool, error) {
	if path == "" {
		return false, fmt.Errorf("config yo'li bo'sh")
	}
	if _, err := os.Stat(path); err == nil {
		return false, nil // mavjud — TEGILMAYDI
	} else if !os.IsNotExist(err) {
		return false, err
	}
	body := fmt.Sprintf(agentConfigTemplate,
		time.Now().UTC().Format(time.RFC3339), exeBaseDir())
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
