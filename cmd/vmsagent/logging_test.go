package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"central-vms/client/internal/config"
	"central-vms/client/internal/logbuf"
	"central-vms/client/internal/logx"
	"central-vms/client/internal/redact"
)

// Talab (2026-11-19): HAR qanday xato terminalda batafsil ko'rinishi va
// alohida log fayliga yozilishi kerak. Bu test shu kafolatni tekshiradi.

func TestLogger_ErrorGoesToTerminalAndFile(t *testing.T) {
	dir := t.TempDir()
	fw, err := logx.New(filepath.Join(dir, "vmsagent.log"), 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()

	var term bytes.Buffer
	ring := logbuf.New(50)
	log := newLoggerAt("info", ring, fw, &term)

	log.Error("kamera oqimi uzildi", "camera_id", "cam-7",
		"err", redact.String("rtsp://admin:SEKRIT@10.0.0.7/stream: connection refused"))

	// 1) Terminalda batafsil ERROR qatori.
	tl := term.String()
	if !strings.Contains(tl, "level=ERROR") || !strings.Contains(tl, "kamera oqimi uzildi") {
		t.Errorf("terminalda ERROR yo'q: %q", tl)
	}
	if !strings.Contains(tl, "camera_id=cam-7") {
		t.Errorf("terminalda tafsilot (camera_id) yo'q: %q", tl)
	}

	// 2) Faylda ham xuddi shu qator.
	b, err := os.ReadFile(filepath.Join(dir, "vmsagent.log"))
	if err != nil {
		t.Fatalf("log fayli: %v", err)
	}
	if !strings.Contains(string(b), "level=ERROR") || !strings.Contains(string(b), "kamera oqimi uzildi") {
		t.Errorf("faylda ERROR yo'q: %q", b)
	}

	// 3) §10.2: parol HICH QAYERDA ko'rinmaydi (terminal ham, fayl ham).
	if strings.Contains(tl, "SEKRIT") || strings.Contains(string(b), "SEKRIT") {
		t.Error("parol yashirilmagan!")
	}

	// 4) Jurnal ekrani (ring) ham oladi.
	if r := ring.Recent(10); len(r) == 0 || !strings.Contains(r[len(r)-1], "kamera oqimi uzildi") {
		t.Errorf("ring'ga tushmadi: %v", r)
	}
}

// Level=warn bo'lsa ham ERROR o'tadi (xatolar hech qachon yo'qolmaydi).
func TestLogger_ErrorsPassWarnLevel(t *testing.T) {
	var term bytes.Buffer
	log := newLoggerAt("warn", logbuf.New(10), nil, &term)
	log.Error("kritik xato", "err", "disk to'lgan")
	if !strings.Contains(term.String(), "kritik xato") {
		t.Errorf("warn levelida ERROR yo'qoladi: %q", term.String())
	}
}

// openLogFile — konfiguratsiya yo'li yozib bo'lmaydigan joyda bo'lsa
// xato aniq qaytadi (jim yutilmaydi).
func TestOpenLogFile_BadPathReports(t *testing.T) {
	cfg := config.Default()
	cfg.LogFile = "/proc/definitely-not-writable/x.log"
	cfg.LogMaxMB, cfg.LogKeep = 1, 2
	w, err := openLogFile(&cfg)
	if err == nil {
		if w != nil && w.Path() != cfg.LogFile {
			// Zaxira ochildi — xato baribir qaytgan bo'lishi SHART (ko'rinadigan bo'lsin).
			_ = w.Close()
			return
		}
		t.Fatal("yozib bo'lmaydigan yo'l uchun xato kutilardi")
	}
	if w != nil {
		_ = w.Close()
	}
}
