package discovery

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// §16.4: /22 dan keng to'r va 1024 dan ko'p host rad etiladi.
func TestScanCIDRCheklovari(t *testing.T) {
	tests := []struct {
		name    string
		cidrs   []string
		wantErr bool
	}{
		{"bitta_host", []string{"127.0.0.1/32"}, false},
		{"/24_ruxsat", []string{"192.168.10.0/24"}, false},
		{"/22 chegarada (1024)", []string{"192.168.0.0/22"}, false},
		{"/21 rad", []string{"192.168.0.0/21"}, true},
		{"/8 rad", []string{"10.0.0.0/8"}, true},
		{"host_prefixsiz", []string{"192.168.10.5"}, false}, // /32 hisoblanadi
		{"yomon_cidr", []string{"nima-noto-gri"}, true},
		{"bo'sh ro'yxat", nil, true},
		{"ikki_/24 jami 512", []string{"10.1.0.0/24", "10.2.0.0/24"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ScanHosts(tc.cidrs, 1024)
			if (err != nil) != tc.wantErr {
				t.Errorf("ScanHosts(%v) err=%v, xohlangan err=%v", tc.cidrs, err, tc.wantErr)
			}
		})
	}
}

// 127.0.0.1/32 + lokal soxta RTSP server: javob beradigan host topiladi,
// bermaydigi topilmaydi.
func TestScanLocalRTSP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveFakeRTSP(c)
		}
	}()
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatal(err)
	}

	res, err := Scan(context.Background(), ScanOptions{
		CIDRs: []string{"127.0.0.1/32"}, Port: port, Parallel: 25, Timeout: 2 * time.Second,
	}, RTSPHostProbe(2*time.Second))
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(res) != 1 || res[0].IP != "127.0.0.1" || res[0].Port != port {
		t.Fatalf("natija = %+v, 127.0.0.1:%d topilishi kerak edi", res, port)
	}
}

// Server javob bermasa — host topilmagan deb hisoblanadi (xato emas).
func TestScanSilentHostNotFound(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() { // ulanishni qabul qiladi, javob bermaydi
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c // ochiq qo'yamiz, o'qimaymiz
		}
	}()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	res, err := Scan(context.Background(), ScanOptions{
		CIDRs: []string{"127.0.0.1/32"}, Port: port, Parallel: 25, Timeout: 300 * time.Millisecond,
	}, RTSPHostProbe(300*time.Millisecond))
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("natija = %+v, javobsiz host topilmasligi kerak", res)
	}
}

// §16.1: 25 parallel — bir vaqtda ko'pi bilan 25 probe.
func TestScanParallelMax25(t *testing.T) {
	const hosts = 64 // 10.0.0.0/26
	const hostsMarker = "10.0.0.63"
	var cur, max atomic.Int64
	entered := make(chan struct{}, hosts)
	rel := make(chan struct{})

	probe := func(_ context.Context, host string, _ int) bool {
		c := cur.Add(1)
		for {
			m := max.Load()
			if c <= m || max.CompareAndSwap(m, c) {
				break
			}
		}
		entered <- struct{}{}
		<-rel
		cur.Add(-1)
		return host == hostsMarker // faqat bitta host "topiladi"
	}

	type scanRes struct {
		res []Device
		err error
	}
	done := make(chan scanRes, 1)
	go func() {
		r, err := Scan(context.Background(), ScanOptions{
			CIDRs: []string{"10.0.0.0/26"}, Port: 554, Parallel: 25, Timeout: time.Second,
		}, probe)
		done <- scanRes{r, err}
	}()

	// 25 tasi kiradi, 26-misi kirmasligi kerak.
	for i := 0; i < 25; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("faqat %d probe boshlandi, 25 kutilardi", i)
		}
	}
	select {
	case <-entered:
		t.Fatal("25 dan ortiq parallel probe boshlandi (§16.1)")
	default:
	}
	// Barchasini chiqaramiz.
	for i := 0; i < hosts; i++ {
		select {
		case rel <- struct{}{}:
		case <-time.After(5 * time.Second):
			t.Fatalf("token %d o'qilmadi", i)
		}
	}
	sr := <-done
	if sr.err != nil {
		t.Fatalf("scan: %v", sr.err)
	}
	if m := max.Load(); m > 25 {
		t.Errorf("maksimal parallel = %d, 25 dan ortiq (§16.1)", m)
	}
	if m := max.Load(); m != 25 {
		t.Errorf("maksimal parallel = %d, 25 kutilardi", m)
	}
	if len(sr.res) != 1 || sr.res[0].IP != hostsMarker {
		t.Errorf("natija = %+v, faqat %s topilishi kerak", sr.res, hostsMarker)
	}
}

// serveFakeRTSP — minimal: so'rovni o'qib, 200 + SDP yuboradi.
func serveFakeRTSP(c net.Conn) {
	defer c.Close()
	br := bufio.NewReader(c)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	const resp = "RTSP/1.0 200 OK\r\nCSeq: 1\r\nContent-Type: application/sdp\r\nContent-Length: 30\r\n\r\nv=0\r\no=- 1 1 IN IP4 127.0.0.1\r\n"
	_, _ = c.Write([]byte(resp))
}

// O'chirish/uzilish: ctx bekor qilinsa Scan darhol qaytadi.
func TestScanCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	probe := func(_ context.Context, _ string, _ int) bool {
		cancel()
		return false
	}
	_, err := Scan(ctx, ScanOptions{CIDRs: []string{"10.0.0.0/24"}, Parallel: 2, Timeout: time.Second}, probe)
	if err != nil && !strings.Contains(err.Error(), "cancel") && ctx.Err() == nil {
		t.Errorf("kutilmagan xato: %v", err)
	}
}
