package rtspclient

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// errSink — server goroutine'larining xatolarini to'playdi. Test tugagach
// (cleanup'da) bir marta t'ga o'tkaziladi — "Fail in goroutine after test
// completed" panic'idan himoya.
type errSink struct {
	mu   sync.Mutex
	errs []string
}

type fatalStop struct{ msg string }

func (s *errSink) Helper() {}

func (s *errSink) Errorf(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, fmt.Sprintf(format, args...))
}

func (s *errSink) Fatalf(format string, args ...any) {
	s.Errorf(format, args...)
	panic(fatalStop{})
}

func (s *errSink) list() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.errs...)
}

// fakeServer — lokal TCP sun'iy RTSP server. handler har ulanish uchun
// alohida goroutine'da ishlaydi; xatolar errSink orqali yig'iladi.
func fakeServer(t *testing.T, handler func(s *errSink, br *bufio.Reader, w net.Conn)) net.Addr {
	t.Helper()
	sink := &errSink{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	var wg sync.WaitGroup
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return // listener yopildi (cleanup)
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					r := recover()
					if r == nil {
						return
					}
					if _, ok := r.(fatalStop); !ok {
						panic(r) // fatalStop bo'lmasa — haqiqiy panic, yashirmaymiz
					}
				}()
				handler(sink, bufio.NewReader(c), c)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
		for _, e := range sink.list() {
			t.Errorf("server tomoni: %s", e)
		}
	})
	return ln.Addr()
}

func rtspURL(addr net.Addr, userinfo, path string) string {
	if userinfo != "" {
		userinfo += "@"
	}
	return "rtsp://" + userinfo + addr.String() + path
}

func writeRaw(s *errSink, w net.Conn, name string) {
	s.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "rtsp", name))
	if err != nil {
		s.Fatalf("fixture: %v", err)
	}
	if _, err := w.Write(b); err != nil {
		s.Errorf("yozib bo'lmadi: %v", err)
	}
}

func TestOptionsOK(t *testing.T) {
	addr := fakeServer(t, func(s *errSink, br *bufio.Reader, w net.Conn) {
		req, err := ParseRequest(br)
		if err != nil {
			return
		}
		if req.Method != "OPTIONS" {
			s.Errorf("method = %q, OPTIONS kutilardi", req.Method)
		}
		writeRaw(s, w, "resp-options.txt")
	})
	c, err := Dial(context.Background(), rtspURL(addr, "", "/cam/realmonitor"), Options{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	pub, err := c.Options(context.Background())
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	if !contains(pub, "GET_PARAMETER") {
		t.Errorf("public ro'yxatida GET_PARAMETER yo'q: %v", pub)
	}
}

func TestDescribeReturnsSDP(t *testing.T) {
	addr := fakeServer(t, func(s *errSink, br *bufio.Reader, w net.Conn) {
		if _, err := ParseRequest(br); err != nil {
			return
		}
		writeRaw(s, w, "resp-describe.txt")
	})
	c, err := Dial(context.Background(), rtspURL(addr, "", "/cam/realmonitor"), Options{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	sdp, err := c.Describe(context.Background())
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if !strings.HasPrefix(string(sdp), "v=0") {
		t.Errorf("SDP noto'g'ri: %q", firstN(sdp, 20))
	}
}

func TestCSeqIncrements(t *testing.T) {
	want := 0
	addr := fakeServer(t, func(s *errSink, br *bufio.Reader, w net.Conn) {
		for i := 0; i < 2; i++ {
			want++
			req, err := ParseRequest(br)
			if err != nil {
				return
			}
			if req.CSeq != want {
				s.Errorf("so'rov %d CSeq = %d, kutilgan %d", i, req.CSeq, want)
			}
			writeRaw(s, w, "resp-getparameter.txt")
		}
	})
	c, err := Dial(context.Background(), rtspURL(addr, "", "/x"), Options{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	for i := 0; i < 2; i++ {
		if err := c.GetParameter(context.Background()); err != nil {
			t.Fatalf("get_parameter %d: %v", i, err)
		}
	}
}

func TestRequestURITopliqVaKredsiz(t *testing.T) {
	addr := fakeServer(t, func(s *errSink, br *bufio.Reader, w net.Conn) {
		req, err := ParseRequest(br)
		if err != nil {
			return
		}
		if strings.Contains(req.URL, "@") || strings.Contains(req.URL, "sirli_parol") {
			s.Errorf("so'rov URI'sida kreds bor: %q", req.URL)
		}
		if !strings.HasPrefix(req.URL, "rtsp://127.0.0.1") {
			s.Errorf("so'rov URI noto'g'ri: %q", req.URL)
		}
		writeRaw(s, w, "resp-getparameter.txt")
	})
	u := rtspURL(addr, "admin:sirli_parol", "/cam/realmonitor?channel=1&subtype=0")
	c, err := Dial(context.Background(), u, Options{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if err := c.GetParameter(context.Background()); err != nil {
		t.Fatalf("get_parameter: %v", err)
	}
}

func TestTimeoutNoResponse(t *testing.T) {
	addr := fakeServer(t, func(_ *errSink, br *bufio.Reader, _ net.Conn) {
		// O'qib turamiz — javob bermaymiz; cleanup ulanishni yopgach chiqamiz.
		for {
			if _, err := br.ReadString('\n'); err != nil {
				return
			}
		}
	})
	c, err := Dial(context.Background(), rtspURL(addr, "", "/x"), Options{Timeout: 150 * time.Millisecond})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	start := time.Now()
	_, err = c.Describe(context.Background())
	if err == nil {
		t.Fatal("taymaut xatosi kutilardi")
	}
	if el := time.Since(start); el > 1*time.Second {
		t.Errorf("taymaut kechikti: %v", el)
	}
	var se *StatusError
	if errors.As(err, &se) {
		t.Errorf("taymaut StatusError bo'lib chiqdi: %v", se)
	}
}

func TestConnRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // port endi band emas
	_, err = Dial(context.Background(), "rtsp://"+addr+"/x", Options{Timeout: 500 * time.Millisecond})
	if err == nil {
		t.Fatal("rad etilgan ulanishda xato kutilardi")
	}
	if !strings.Contains(err.Error(), "ulanib bo'lmadi") {
		t.Errorf("xato matni kutilmagan: %v", err)
	}
}

func TestServerJavobsizYopadi(t *testing.T) {
	addr := fakeServer(t, func(_ *errSink, _ *bufio.Reader, w net.Conn) {
		_ = w.Close()
	})
	c, err := Dial(context.Background(), rtspURL(addr, "", "/x"), Options{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if _, err := c.Options(context.Background()); err == nil {
		t.Fatal("uzilgan ulanishda xato kutilardi")
	}
}

func TestDialURLTekshiruvlari(t *testing.T) {
	tests := []struct{ name, raw string }{
		{"sxema_rtsp_emas", "http://127.0.0.1:554/x"},
		{"host_yoq", "rtsp:///x"},
		{"yomon_url", "rtsp://[:::"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Dial(context.Background(), tc.raw, Options{Timeout: 200 * time.Millisecond}); err == nil {
				t.Fatal("xato kutilardi")
			}
		})
	}
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
