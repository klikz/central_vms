package logx

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// NewLogger — terminal (stderr) + fayl (agar path bo'sh emas) yozuvchi slog
// loggeri. Daraja: debug|info|warn|error (noma'lum → info).
// Talab (2026-11-19): xatolar terminalga batafsil VA alohida server fayliga.
func NewLogger(level, file string, maxMB, keep int) (*slog.Logger, *Writer, error) {
	w, err := New(file, maxMB, keep)
	if err != nil {
		return nil, nil, err
	}
	out := io.Writer(os.Stderr)
	if w.Enabled() {
		out = io.MultiWriter(os.Stderr, w)
	}
	h := slog.NewTextHandler(out, &slog.HandlerOptions{Level: parseLevel(level)})
	return slog.New(h), w, nil
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
