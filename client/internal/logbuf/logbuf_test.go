package logbuf

import (
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"central-vms/client/internal/redact"
)

func TestRing_KeepLastN(t *testing.T) {
	r := New(3)
	for _, l := range []string{"a", "b", "c", "d", "e"} {
		r.Add(l)
	}
	if got, want := r.Len(), 3; got != want {
		t.Fatalf("Len=%d, kutilgan %d", got, want)
	}
	got := r.Recent(0) // 0 = butun bufer
	want := []string{"c", "d", "e"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Recent(0)=%v, kutilgan %v", got, want)
	}
	if got2 := r.Recent(2); strings.Join(got2, ",") != "d,e" {
		t.Fatalf("Recent(2)=%v", got2)
	}
	// limit buferdan katta bo'lsa — hammasi
	if got3 := r.Recent(99); len(got3) != 3 {
		t.Fatalf("Recent(99)=%v", got3)
	}
}

func TestRing_KichikSigim(t *testing.T) {
	r := New(0) // himoya: kamida 1
	r.Add("x")
	r.Add("y")
	if got := r.Recent(0); len(got) != 1 || got[0] != "y" {
		t.Fatalf("New(0) xatti ishlayapti: %v", got)
	}
}

func TestHandler_RedaktasiyadanKeyinBufer(t *testing.T) {
	r := New(10)
	// Tartib: redact → logbuf → stderr (bu yerda Discard)
	text := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo})
	log := slog.New(redact.NewHandlerWithNext(NewHandler(text, r)))

	log.Info("kamera qo'shildi", "rtsp_url", "rtsp://admin:JIZNIQ_PAROL@10.0.0.5/cam")

	lines := r.Recent(0)
	if len(lines) != 1 {
		t.Fatalf("buferda %d qator: %v", len(lines), lines)
	}
	line := lines[0]
	if strings.Contains(line, "JIZNIQ_PAROL") {
		t.Fatalf("buferda parol qoldi: %s", line)
	}
	if !strings.Contains(line, "****") {
		t.Fatalf("yashirish belgisi yo'q: %s", line)
	}
	if !strings.Contains(line, "INFO") || !strings.Contains(line, "kamera qo'shildi") {
		t.Fatalf("format noto'g'ri: %s", line)
	}
}

func TestHandler_DarajaFiltri(t *testing.T) {
	r := New(10)
	text := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn})
	log := slog.New(redact.NewHandlerWithNext(NewHandler(text, r)))

	log.Debug("shunday bo'lmasin")
	log.Info("ham bo'lmasin")
	log.Warn("ogohlantirish keldi")

	lines := r.Recent(0)
	if len(lines) != 1 || !strings.Contains(lines[0], "ogohlantirish keldi") {
		t.Fatalf("daraja filtri ishlamadi: %v", lines)
	}
}

func TestHandler_WithAttrsWithGroup(t *testing.T) {
	r := New(10)
	text := slog.NewTextHandler(io.Discard, nil)
	base := NewHandler(text, r)
	log := slog.New(redact.NewHandlerWithNext(base.WithAttrs([]slog.Attr{slog.String("cam_id", "c1")})))
	log2 := slog.New(redact.NewHandlerWithNext(base.WithGroup("g")))

	log.Info("holat")
	log2.Info("guruh")
	if r.Len() != 2 {
		t.Fatalf("WithAttrs/WithGroup dan keyin bufer: %d", r.Len())
	}
}

func TestHandler_NilNext(t *testing.T) {
	r := New(5)
	log := slog.New(NewHandler(nil, r)) // faqat bufer
	log.Info("faqat buferga")
	if r.Len() != 1 {
		t.Fatalf("nil next bilan ishlamadi: %d", r.Len())
	}
	// panic bo'lmasligi kerak
	_ = slog.New(NewHandler(nil, r).WithAttrs([]slog.Attr{slog.Int("n", 1)})).WithGroup("x")
}

func TestRing_Musroboziy(t *testing.T) {
	r := New(50)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				r.Add("x")
			}
		}(i)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = r.Recent(10)
			}
		}()
	}
	wg.Wait()
	if r.Len() != 50 {
		t.Fatalf("Len=%d", r.Len())
	}
}
