package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Xatolar terminal bilan birga faylga ham yozilishi kerak (foydalanuvchi
// talabi, 2026-11-19): hajm bo'yicha rotatsiya, client/server fayllari
// ALLOHIIDA.

func TestNew_CreatesFileAndDirs(t *testing.T) {
	dir := t.TempDir()
	w, err := New(filepath.Join(dir, "logs", "vmsagent.log"), 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("birinchi qator\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "logs", "vmsagent.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "birinchi qator\n" {
		t.Errorf("fayl mazmuni: %q", b)
	}
}

// Hajm chegarasidan oshsa — rotatsiya: asosiy fayl .1 ga ko'chadi.
func TestWrite_RotatesOnSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	w, err := New(path, 1, 3) // 1 MB
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	line := strings.Repeat("x", 500) + "\n"
	n := (1024*1024)/len(line) + 5 // chegaradan ko'p yozamiz
	for i := 0; i < n; i++ {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("rotatsiya bo'lmadi: %v (fayl .1 kutilardi)", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() > 2*1024*1024 {
		t.Errorf("asosiy fayl juda katta: %d bayt", fi.Size())
	}
}

// keep=2 bo'lsa .3 gacha fayllar o'chiriladi (disk to'lmaydi).
func TestRotate_KeepsLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	w, err := New(path, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	line := strings.Repeat("y", 1000) + "\n"
	// 4 MB yozamiz → 3+ marta rotatsiya.
	for i := 0; i < 4*(1024*1024/len(line)); i++ {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 3; i <= 5; i++ {
		if _, err := os.Stat(fmt.Sprintf("%s.%d", path, i)); err == nil {
			t.Errorf("%s.%d qolgan — keep=2 buzildi", path, i)
		}
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf(".1 bo'lishi kerak edi: %v", err)
	}
	if _, err := os.Stat(path + ".2"); err != nil {
		t.Errorf(".2 bo'lishi kerak edi: %v", err)
	}
}

// Parallel yozuvlar — bitta qator ham yo'qolmaydi, freymlar aralashmaydi.
func TestWrite_Concurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	w, err := New(path, 64, 3) // katta limit — rotatsiyasiz
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				fmt.Fprintf(w, "g%d-%d\n", g, i)
			}
		}(g)
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 1600 {
		t.Errorf("qatorlar = %d, 1600 kutilardi", len(lines))
	}
	for _, ln := range lines {
		if !strings.HasPrefix(ln, "g") || !strings.Contains(ln, "-") {
			t.Fatalf("freym aralashgan: %q", ln)
		}
	}
}

// Bo'sh yo'l → fayl yozuvi o'chirilgan (faqat terminal).
func TestNew_EmptyPathDisabled(t *testing.T) {
	w, err := New("", 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if w.Enabled() {
		t.Error("bo'sh yo'l bilan yozuv yoqilmasligi kerak")
	}
	if _, err := w.Write([]byte("x")); err != nil {
		t.Errorf("o'chirilgan yozuvchi xato qaytarmadi: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// Path bir xil bo'lsa — client/server fayllari adashib bitta bo'lmasin
// uchun nom tekshiruvi emas, lekin Close'dan keyin qayta ochish ishlaydi.
func TestReopen_AfterClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	w1, _ := New(path, 1, 3)
	fmt.Fprintln(w1, "bir")
	w1.Close()
	w2, err := New(path, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(w2, "ikki")
	w2.Close()
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "bir") || !strings.Contains(string(b), "ikki") {
		t.Errorf("qayta ochishda eski mazmun yo'qoldi: %q", b)
	}
}
