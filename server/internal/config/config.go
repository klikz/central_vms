// Package config — vmsd konfiguratsiyasi (YAML).
// Sxema: server REJA §3/§5, root REJA §5/§10; limitlar `settings` jadvalidagi
// qiymatlardan oldin keladi (S1'da DB `settings` ustun yozadi).
package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config — config.yaml to'liq sxemasi.
type Config struct {
	Bind       string `yaml:"bind"`    // vmsd API/WS porti (default :8081)
	WebDir     string `yaml:"web_dir"` // UI statikasi (build/web), diskdan serve
	DBPath     string `yaml:"db_path"` // SQLite fayli (WAL)
	Migrations string `yaml:"migrations_dir"`

	MediaMTX struct {
		ControlURL   string `yaml:"control_url"`    // MediaMTX HTTP API (default http://127.0.0.1:9997)
		AuthHookBind string `yaml:"auth_hook_bind"` // vmsd ichidagi /internal/auth bindi (127.0.0.1:8189)
		WebRTCPort   int    `yaml:"webrtc_port"`    // WHEP/WHIP porti (8889) — URL qurish uchun
		RTSPPort     int    `yaml:"rtsp_port"`      // 8554 — rtsp_tc URL uchun
		HLSPort      int    `yaml:"hls_port"`       // 8888 — fallback
		PublicHost   string `yaml:"public_host"`    // brauzer/agent ko'radigan manzil (bo'sh → bind host'i)
	} `yaml:"mediamtx"`

	Auth struct {
		JWTSecret    string `yaml:"jwt_secret"`       // kamida 32 belgi; bo'sh bo'lsa VMSD_JWT_SECRET
		AccessTTLMin int    `yaml:"access_ttl_min"`   // default 15
		RefreshTTLD  int    `yaml:"refresh_ttl_days"` // default 7
	} `yaml:"auth"`

	Enrollment struct {
		TTLH      int `yaml:"ttl_h"`       // bir martalik kod amaldagi muddati (24)
		RatePer5m int `yaml:"rate_per_5m"` // /api/agent/enroll rate-limit (5)
	} `yaml:"enrollment"`

	Limits struct {
		UserMaxSessions   int `yaml:"user_max_sessions"`   // 16
		GlobalMaxSessions int `yaml:"global_max_sessions"` // 48
		PreviewMax        int `yaml:"preview_max"`         // 8 (global), user uchun 1
		PerClientUploads  int `yaml:"per_client_uploads"`  // 8 (default; client'da sozlanadi)
	} `yaml:"limits"`

	Media struct {
		IdleStopS        int    `yaml:"idle_stop_s"`           // 10 (preview ~1)
		SessionTTLMin    int    `yaml:"session_ttl_min"`       // 5
		SessionRenewMin  int    `yaml:"session_renew_min"`     // 2
		PublishTTLS      int    `yaml:"publish_ttl_s"`         // 60 (media_start.ttl_s)
		ReadTokenTTLS    int    `yaml:"read_token_ttl_s"`      // 30
		MaxTranscodes    int    `yaml:"server_max_transcodes"` // 5 boshlang'ich; probe yangilaydi
		ReadyTOs         int    `yaml:"ready_timeout_s"`       // 10 — media_ready kutiladi (REJA §7.1)
		FFmpegPath       string `yaml:"ffmpeg_path"`           // default "ffmpeg"
		TranscodeBackend string `yaml:"transcode_backend"`     // auto|nvenc|vaapi|amf|cpu (bo'sh/auto = avtomatik aniqlash)
	} `yaml:"media"`

	RetentionDays int    `yaml:"retention_days"` // sessions/audit_log 90 kun
	LogLevel      string `yaml:"log_level"`      // debug|info|warn|error
	LogFile       string `yaml:"log_file"`       // bo'sh → faqat terminal
	LogMaxMB      int    `yaml:"log_max_mb"`
	LogKeep       int    `yaml:"log_keep"`

	// Path — yuklangan fayl yo'li (xatolarni ko'rsatish uchun).
	Path string `yaml:"-"`
}

// Default — barcha maydonlarning boshlang'ich qiymatlari (root §10, REJA §4).
func Default() Config {
	var c Config
	c.Bind = ":8081"
	c.WebDir = "web"
	c.DBPath = "var/vmsd.db"
	c.Migrations = "migrations"

	c.MediaMTX.ControlURL = "http://127.0.0.1:9997"
	c.MediaMTX.AuthHookBind = "127.0.0.1:8189"
	c.MediaMTX.WebRTCPort = 8889
	c.MediaMTX.RTSPPort = 8554
	c.MediaMTX.HLSPort = 8888

	c.Auth.AccessTTLMin = 15
	c.Auth.RefreshTTLD = 7
	c.Enrollment.TTLH = 24
	c.Enrollment.RatePer5m = 5

	c.Limits.UserMaxSessions = 16
	c.Limits.GlobalMaxSessions = 48
	c.Limits.PreviewMax = 8
	c.Limits.PerClientUploads = 8

	c.Media.IdleStopS = 10
	c.Media.SessionTTLMin = 5
	c.Media.SessionRenewMin = 2
	c.Media.PublishTTLS = 60
	c.Media.ReadTokenTTLS = 30
	c.Media.MaxTranscodes = 5
	c.Media.FFmpegPath = "ffmpeg"

	c.RetentionDays = 90
	c.LogLevel = "info"
	c.LogFile = "/var/log/vmsd/vmsd.log"
	c.LogMaxMB = 8
	c.LogKeep = 5
	return c
}

// Load — faylni o'qiydi, defaultlar bilan to'ldiradi, validatsiya qiladi.
// jwt_secret bo'sh bo'lsa VMSD_JWT_SECRET muhit o'zgaruvchisidan oladi.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: o'qib bo'lmadi %s: %w", path, err)
	}
	c := Default()
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true) // noma'lum maydon → xato (optograf yozuvlarni darhol ko'rsin)
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	c.Path = path
	if c.Auth.JWTSecret == "" {
		c.Auth.JWTSecret = os.Getenv("VMSD_JWT_SECRET")
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}
