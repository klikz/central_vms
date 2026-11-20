package config

import (
	"fmt"
	"net"
	"strings"
)

// Validate — konfiguratsiyani ishga tushirish uchun yaroqli holatga keltiradi:
// yetishmayotgan qiymatlarni default bilan to'ldiradi va noto'g'rilari uchun
// aniq xato qaytaradi (root §10 limitlari, REJA §4 portlar).
func (c *Config) Validate() error {
	if c.Bind == "" {
		c.Bind = Default().Bind
	}
	if _, _, err := net.SplitHostPort(c.Bind); err != nil {
		return fmt.Errorf("config: bind %q: %w", c.Bind, err)
	}
	if c.MediaMTX.AuthHookBind == "" {
		c.MediaMTX.AuthHookBind = Default().MediaMTX.AuthHookBind
	}
	if h, _, err := net.SplitHostPort(c.MediaMTX.AuthHookBind); err != nil {
		return fmt.Errorf("config: mediamtx.auth_hook_bind %q: %w", c.MediaMTX.AuthHookBind, err)
	} else if h != "127.0.0.1" && h != "localhost" {
		// Auth hook faqat localhost'da eshitishi shart (protocol.md §15).
		return fmt.Errorf("config: mediamtx.auth_hook_bind hosti %q — faqat 127.0.0.1 bo'lishi kerak", h)
	}
	if c.DBPath == "" {
		return fmt.Errorf("config: db_path bo'sh bo'la olmaydi")
	}
	if c.Migrations == "" {
		c.Migrations = Default().Migrations
	}
	if len(c.Auth.JWTSecret) < 32 {
		return fmt.Errorf("config: auth.jwt_secret kamida 32 belgi bo'lishi kerak (hozir %d) — fayl yoki VMSD_JWT_SECRET orqali bering", len(c.Auth.JWTSecret))
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	case "":
		c.LogLevel = "info"
	default:
		return fmt.Errorf("config: log_level %q noto'g'ri (debug|info|warn|error)", c.LogLevel)
	}
	if err := positive("limits.user_max_sessions", c.Limits.UserMaxSessions); err != nil {
		return err
	}
	if err := positive("limits.global_max_sessions", c.Limits.GlobalMaxSessions); err != nil {
		return err
	}
	if c.Limits.GlobalMaxSessions < c.Limits.UserMaxSessions {
		return fmt.Errorf("config: global_max_sessions (%d) user_max_sessions (%d) dan kichik", c.Limits.GlobalMaxSessions, c.Limits.UserMaxSessions)
	}
	if err := positive("limits.preview_max", c.Limits.PreviewMax); err != nil {
		return err
	}
	if err := positive("limits.per_client_uploads", c.Limits.PerClientUploads); err != nil {
		return err
	}
	if err := positive("media.idle_stop_s", c.Media.IdleStopS); err != nil {
		return err
	}
	if err := positive("media.session_ttl_min", c.Media.SessionTTLMin); err != nil {
		return err
	}
	if err := positive("media.publish_ttl_s", c.Media.PublishTTLS); err != nil {
		return err
	}
	if err := positive("media.read_token_ttl_s", c.Media.ReadTokenTTLS); err != nil {
		return err
	}
	if err := positive("media.server_max_transcodes", c.Media.MaxTranscodes); err != nil {
		return err
	}
	if c.Media.FFmpegPath == "" {
		c.Media.FFmpegPath = "ffmpeg"
	}
	if err := positive("retention_days", c.RetentionDays); err != nil {
		return err
	}
	if err := positive("enrollment.ttl_h", c.Enrollment.TTLH); err != nil {
		return err
	}
	if err := positive("enrollment.rate_per_5m", c.Enrollment.RatePer5m); err != nil {
		return err
	}
	for name, p := range map[string]int{
		"mediamtx.webrtc_port": c.MediaMTX.WebRTCPort,
		"mediamtx.rtsp_port":   c.MediaMTX.RTSPPort,
		"mediamtx.hls_port":    c.MediaMTX.HLSPort,
	} {
		if p < 1 || p > 65535 {
			return fmt.Errorf("config: %s %d — port oralig'i 1..65535", name, p)
		}
	}
	if strings.TrimSpace(c.MediaMTX.ControlURL) == "" {
		return fmt.Errorf("config: mediamtx.control_url bo'sh")
	}
	return nil
}

func positive(name string, v int) error {
	if v <= 0 {
		return fmt.Errorf("config: %s musbat son bo'lishi kerak (hozir %d)", name, v)
	}
	return nil
}
