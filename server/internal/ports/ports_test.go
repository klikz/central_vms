package ports

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestCheck_Free(t *testing.T) {
	if err := Check([]int{freePort(t)}, nil); err != nil {
		t.Fatalf("bo'sh portda xato berdi: %v", err)
	}
}

func TestCheck_TCPOccupied(t *testing.T) {
	p := freePort(t)
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	err = Check([]int{p}, nil)
	if err == nil {
		t.Fatal("band TCP port uchun xato kutilardi")
	}
	if !strings.Contains(err.Error(), "tcp/"+strconv.Itoa(p)) {
		t.Fatalf("xatoda port ko'rsatilmadi: %v", err)
	}
	if !strings.Contains(err.Error(), "to'xtating") {
		t.Fatalf("xatoda maslahat yo'q: %v", err)
	}
}

func TestCheck_UDPOccupied(t *testing.T) {
	uc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("udp listen: %v", err)
	}
	defer uc.Close()
	p := uc.LocalAddr().(*net.UDPAddr).Port
	if err := Check(nil, []int{p}); err == nil {
		t.Fatal("band UDP port uchun xato kutilardi")
	}
}

func TestCheck_AllConflictsListed(t *testing.T) {
	l1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l1.Close()
	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	p1 := l1.Addr().(*net.TCPAddr).Port
	p2 := l2.Addr().(*net.TCPAddr).Port
	err = Check([]int{p1, p2}, nil)
	if err == nil {
		t.Fatal("xato kutilardi")
	}
	if !strings.Contains(err.Error(), strconv.Itoa(p1)) || !strings.Contains(err.Error(), strconv.Itoa(p2)) {
		t.Fatalf("barcha konfliktlar sanalmadi: %v", err)
	}
}
