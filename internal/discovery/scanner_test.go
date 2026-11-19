package discovery

import (
	"context"
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
	n, err := sc.StartScan()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("scanned=%d, 1 kutilardi", n)
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

func TestScanner_KnownFiltered(t *testing.T) {
	probe := func(ctx context.Context, host string, port int) bool { return true }
	fc := &fakeCreator{}
	sc := NewScanner(fc, func() []Known {
		return []Known{{ID: "old1", IP: "127.0.0.1", Port: 554}}
	}, testCfg(probe))
	if _, err := sc.StartScan(); err != nil {
		t.Fatal(err)
	}
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

func TestScanner_ProbeNilUsesTCP(t *testing.T) {
	// 554 band bo'lmasa TCP probe false → natija bo'sh, xato emas.
	sc := NewScanner(&fakeCreator{}, nil, ScannerConfig{
		CIDRs: []string{"127.0.0.1/32"}, Ports: []int{1},
		MaxHosts: 2, HostTimeout: 50 * time.Millisecond, WsddTimeout: 20 * time.Millisecond,
		Wsdd: func(time.Duration) []Device { return nil },
	})
	if _, err := sc.StartScan(); err != nil {
		t.Fatal(err)
	}
	if len(sc.Results()) != 0 {
		t.Errorf("yopiq port natija bermasligi kerak")
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
