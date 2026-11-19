// Package logx — hajm bo'yicha rotatsiyalanadigan log fayli yozuvchisi.
// Talab (2026-11-19): barcha xatolar terminal bilan birga faylga ham
// yozilsin; client va server loglari ALLOHIIDA fayllarda.
package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// Writer — mutex'li, hajm chegarali fayl yozuvchi. io.Writer interfeysi
// slog handler'lariga to'g'ridan-to'g'ri ulanadi.
type Writer struct {
	mu      sync.Mutex
	path    string
	maxSize int64 // bayt
	keep    int   // .1 … .keep saqlanadi
	f       *os.File
	size    int64
	enabled bool
}

// New — faylni ochadi (kerakli kataloglar yaratiladi). path bo'sh →
// yozuv o'chirilgan (Write nil qaytaradi, faqat terminal ishlaydi).
// maxMB < 1 → 1 MB; keep < 1 → 3.
func New(path string, maxMB, keep int) (*Writer, error) {
	w := &Writer{path: path, keep: keep, enabled: path != ""}
	if w.keep < 1 {
		w.keep = 3
	}
	if maxMB < 1 {
		maxMB = 1
	}
	w.maxSize = int64(maxMB) * 1024 * 1024
	if !w.enabled {
		return w, nil
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

// Enabled — fayl yozuvi yoki/yo'q.
func (w *Writer) Enabled() bool { return w.enabled }

// Path — fayl yo'li (bo'sh → o'chirilgan).
func (w *Writer) Path() string { return w.path }

func (w *Writer) open() error {
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return fmt.Errorf("logx: katalog: %w", err)
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return fmt.Errorf("logx: ochib bo'lmadi: %w", err)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("logx: stat: %w", err)
	}
	w.f, w.size = f, fi.Size()
	return nil
}

// Write — bitta chaqiruv butun qator bo'lib yoziladi (mutex), hajm
// oshsa avval rotatsiya.
func (w *Writer) Write(p []byte) (int, error) {
	if !w.enabled {
		return len(p), nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size+int64(len(p)) > w.maxSize {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rotateLocked — .keep → .1 siljitish, .keep o'chiriladi, qayta ochish.
func (w *Writer) rotateLocked() error {
	if w.f != nil {
		w.f.Close()
		w.f = nil
	}
	// .(keep) o'chiriladi; .(i) → .(i+1) orqaga siljitiladi.
	_ = os.Remove(w.suffix(w.keep))
	for i := w.keep - 1; i >= 1; i-- {
		_ = os.Rename(w.suffix(i), w.suffix(i+1))
	}
	if err := os.Rename(w.path, w.suffix(1)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("logx: rotatsiya: %w", err)
	}
	return w.open()
}

func (w *Writer) suffix(i int) string { return w.path + "." + strconv.Itoa(i) }

// Close — faylni yopadi (idempotent).
func (w *Writer) Close() error {
	if !w.enabled {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}
