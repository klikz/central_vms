package discovery

import (
	"context"
	"net"
	"testing"
	"time"

	"central-vms/client/internal/cams"
)

type fakeCreator struct{ created []cams.Camera }

func (f *fakeCreator) Create(c cams.Camera, secret string) (cams.Camera, error) {
	c.ID = "cam" + string(rune('a'+len(f.created)))
	f.created = append(f.created, c)
	return c, nil
}

func waitScan(t *testing.T, sc *Scanner) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && sc.Progress().Running {
		time.Sleep(10 * time.Millisecond)
	}
	if sc.Progress().Running {
		t.Fatal("skan tugamadi")
	}
}

func testCfg(probe HostProbe) ScannerConfig {
	return ScannerConfig{
		CIDRs: []string{"127.0.0.1/32"}, Ports: []int{554},
		MaxHosts: 4, Parallel: 2, HostTimeout: 100 * time.Millisecond,
		WsddTimeout: 30 * time.Millisecond, Probe: probe,
		Wsdd: func(time.Duration) []Device { return nil },
	}
}

func TestScanner_ScanAndAdd(t *testing.T) {
	probe := func(ctx context.Context, host string, port int) bool { return port == 554 }
	fc := &fakeCreator{}
	sc := NewScanner(fc, func() []Known { return nil }, testCfg(probe))
	if err := sc.StartScan(); err != nil {
		t.Fatal(err)
	}
	waitScan(t, sc)
	if p := sc.Progress(); p.Scanned != 1 || p.Total != 1 || p.Found != 1 {
		t.Errorf("progress: %+v", p)
	}
	res := sc.Results()
	if len(res) != 1 || res[0].IP != "127.0.0.1" || res[0].AlreadyKnown {
		t.Fatalf("natija: %+v", res)
	}
	rep, err := sc.AddSelected([]string{DeviceKey(res[0].Device)}, BulkAddInput{Enabled: true, Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Added) != 1 {
		t.Fatalf("added=%d, 1 kutilardi (failed=%v)", len(rep.Added), rep.Failed)
	}
}

func TestScanner_Incremental(t *testing.T) {
	// Probe sekin: skan ketayotganda Results() allaqachon topilganlarni
	// ko'rsatishi kerak (UI jonli ro'yxat shunga tayanadi).
	gate := make(chan struct{})
	probe := func(ctx context.Context, host string, port int) bool {
		<-gate
		return true
	}
	sc := NewScanner(&fakeCreator{}, nil, testCfg(probe))
	if err := sc.StartScan(); err != nil {
		t.Fatal(err)
	}
	if !sc.Progress().Running {
		t.Fatal("skan ishlashi kerak")
	}
	if len(sc.Results()) != 0 {
		t.Fatal("hozircha bo'sh bo'lishi kerak edi")
	}
	close(gate)
	waitScan(t, sc)
	if len(sc.Results()) != 1 {
		t.Fatalf("tugagach 1 ta kutilardi: %+v", sc.Results())
	}
}

func TestScanner_KnownFiltered(t *testing.T) {
	probe := func(ctx context.Context, host string, port int) bool { return true }
	fc := &fakeCreator{}
	sc := NewScanner(fc, func() []Known {
		return []Known{{ID: "old1", IP: "127.0.0.1", Port: 554}}
	}, testCfg(probe))
	if err := sc.StartScan(); err != nil {
		t.Fatal(err)
	}
	waitScan(t, sc)
	res := sc.Results()
	if len(res) != 1 || !res[0].AlreadyKnown || res[0].ExistingID != "old1" {
		t.Fatalf("already_known kutilardi: %+v", res)
	}
	rep, err := sc.AddSelected([]string{DeviceKey(res[0].Device)}, BulkAddInput{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Added) != 0 {
		t.Errorf("known qayta qo'shildi: %+v", rep.Added)
	}
}

func TestScanner_ClosedPortNoResult(t *testing.T) {
	sc := NewScanner(&fakeCreator{}, nil, ScannerConfig{
		CIDRs: []string{"127.0.0.1/32"}, Ports: []int{1},
		MaxHosts: 2, HostTimeout: 50 * time.Millisecond, WsddTimeout: 20 * time.Millisecond,
		Wsdd: func(time.Duration) []Device { return nil },
	})
	if err := sc.StartScan(); err != nil {
		t.Fatal(err)
	}
	waitScan(t, sc)
	if len(sc.Results()) != 0 {
		t.Errorf("yopiq port natija bermasligi kerak")
	}
}

func TestScanner_BannerEnrich(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for { // probe + banner: bir nechta ulanish qabul qilinadi
			c, err := ln.Accept()
			if err != nil {
				return
			}
			b := make([]byte, 512)
			_, _ = c.Read(b)
			_, _ = c.Write([]byte("RTSP/1.0 200 OK\r\nCSeq: 1\r\nServer: TestCam-9000\r\n\r\n"))
			_ = c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	sc := NewScanner(&fakeCreator{}, nil, ScannerConfig{
		CIDRs: []string{"127.0.0.1/32"}, Ports: []int{port}, MaxHosts: 2,
		HostTimeout: 300 * time.Millisecond, WsddTimeout: 20 * time.Millisecond,
		Wsdd: func(time.Duration) []Device { return nil },
	})
	if err := sc.StartScan(); err != nil {
		t.Fatal(err)
	}
	waitScan(t, sc)
	res := sc.Results()
	if len(res) != 1 || res[0].Model != "TestCam-9000" {
		t.Fatalf("banner orqali nom kutilardi: %+v", res)
	}
}

func TestDedupeByIP(t *testing.T) {
	in := []Device{
		{IP: "10.0.0.5", Model: "Hikvision", MAC: "aa:bb", Port: 0}, // wsdd
		{IP: "10.0.0.5", Port: 554},                                 // port-scan
		{IP: "10.0.0.9", Port: 8554},                                // scan-only
	}
	out := dedupeByIP(in)
	if len(out) != 2 {
		t.Fatalf("dedup: %+v", out)
	}
	if out[0].Port != 554 || out[0].Model != "Hikvision" {
		t.Errorf("wsdd ustuvor + port default bo'lishi kerak: %+v", out[0])
	}
	if out[1].IP != "10.0.0.9" || out[1].Port != 8554 {
		t.Errorf("scan-only saqlanishi kerak: %+v", out[1])
	}
}

func TestMarkKnownByIP(t *testing.T) {
	res := []MergeResult{{Device: Device{IP: "1.2.3.4", Port: 554, MAC: "aa:bb"}}}
	markKnownByIP(res, []Known{{ID: "old", IP: "1.2.3.4", Port: 0}})
	if !res[0].AlreadyKnown || res[0].ExistingID != "old" {
		t.Fatalf("IP bo'yicha known bo'lishi kerak edi: %+v", res[0])
	}
}

func TestIpLess(t *testing.T) {
	if ipLess("10.0.0.10", "10.0.0.9") {
		t.Error("son tartibi buzildi")
	}
	if !ipLess("10.0.0.9", "10.0.0.10") || !ipLess("10.0.0.0", "192.168.0.1") {
		t.Error("tartib noto'g'ri")
	}
}

func TestLocalNetsSane(t *testing.T) {
	nets := localNets()
	for _, n := range nets {
		if n.IP.IsLoopback() || n.IP.IsLinkLocalUnicast() {
			t.Errorf("tashlanishi kerak edi: %v", n)
		}
	}
}
