package logx

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestWriterRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "vmsd.log")
	w, err := New(path, 1, 2) // 1 MB, 2 ta zaxira
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	line := bytes.Repeat([]byte("x"), 1000)
	line = append(line, '\n')
	for i := 0; i < 2000; i++ { // ~2 MB → kamida 1 marta rotatsiya
		if _, err := w.Write(line); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("faol fayl yo'q: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotatsiya bo'lmadi (.1 yo'q): %v", err)
	}
	if _, err := os.Stat(path + ".3"); err == nil {
		t.Fatal("keep=2 — .3 mavjud bo'lmasligi kerak edi")
	}
}

func TestWriterDisabled(t *testing.T) {
	w, err := New("", 1, 1)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if w.Enabled() {
		t.Fatal("bo'sh path — yozuv o'chirilgan bo'lishi kerak")
	}
	if n, err := w.Write([]byte("salom")); err != nil || n != 5 {
		t.Fatalf("Write: n=%d err=%v", n, err)
	}
}

func TestNewLoggerLevels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmsd.log")
	lg, w, err := NewLogger("warn", path, 1, 1)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer w.Close()
	lg.Info("bu yozilmaydi", slog.String("k", "info"))
	lg.Error("bu yoziladi", slog.String("err", "sabab"))
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if bytes.Contains(b, []byte("yozilmaydi")) {
		t.Fatalf("warn darajasida info yozilgan: %s", b)
	}
	if !bytes.Contains(b, []byte("bu yoziladi")) {
		t.Fatalf("error yozilmadi: %s", b)
	}
}
