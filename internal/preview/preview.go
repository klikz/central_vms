// Package preview — kamera uchun bitta JPEG kadr (preview) saqlash va
// olish. Doimiy oqim emas: kamera qo'shilganda / ulanganda / holati
// o'zgarganda yoki foydalanuvchi rasmni bosganda bir marta olinadi.
package preview

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Store — <dir>/<camID>.jpg fayllari (atomik: .tmp → rename).
type Store struct{ dir string }

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("preview dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Path(id string) string { return filepath.Join(s.dir, id+".jpg") }

// Stat — snapshot mavjud bo'lsa fayl yangilanish vaqti.
func (s *Store) Stat(id string) (time.Time, bool) {
	st, err := os.Stat(s.Path(id))
	if err != nil {
		return time.Time{}, false
	}
	return st.ModTime(), true
}

func (s *Store) Remove(id string) { _ = os.Remove(s.Path(id)) }

// Capturer — ffmpeg orqali bitta kadr. Bin/Timeout inject qilinadi (testlar).
type Capturer struct {
	Store   *Store
	Bin     string        // default "ffmpeg"
	Timeout time.Duration // default 20 s (birinchi kalit-kadrgacha kutish)
	Log     *slog.Logger
}

// Capture — kameradan 1 JPEG kadr oladi (RTSP/TCP). URL parolli bo'lishi
// mumkin — xato matnida parol yashiriladi (§10.2).
func (c *Capturer) Capture(ctx context.Context, camID, url string) error {
	if c.Store == nil {
		return fmt.Errorf("preview: store sozlanmagan")
	}
	bin := c.Bin
	if bin == "" {
		bin = "ffmpeg"
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	log := c.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out := c.Store.Path(camID)
	// .jpg.tmp emas — ffmpeg muxerni cho'qqidagi kengaytmadan tanlaydi.
	tmp := out + ".tmp.jpg"
	// -rtsp_transport tcp — INPUT opsiyasi (rtsp demuxer tanlaydi; MediaMTX
	// faqat TCP qabul qiladi: UDP bilan 461 qaytadi).
	args := []string{"-hide_banner", "-loglevel", "error", "-rtsp_transport", "tcp",
		"-y", "-i", url, "-frames:v", "1", "-q:v", "4", tmp}
	cmd := exec.CommandContext(ctx, bin, args...)
	stderr, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(tmp)
		msg := strings.TrimSpace(string(stderr))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("preview ffmpeg: %s", redactURL(msg, url))
	}
	if err := os.Rename(tmp, out); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("preview rename: %w", err)
	}
	log.Info("preview olindi", "camera_id", camID)
	return nil
}

// redactURL — xato matnida rtsp://user:pass@ ko'rinsa parolni yashiradi.
func redactURL(msg, url string) string {
	if i := strings.Index(url, "://"); i > 0 {
		if j := strings.Index(url[i+3:], "@"); j >= 0 {
			msg = strings.ReplaceAll(msg, url, url[:i+3]+"***@"+url[i+3+j+1:])
		}
	}
	return msg
}
