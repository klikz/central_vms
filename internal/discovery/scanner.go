package discovery

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// Scanner — localui.DiscoveryService'ning real implementatsiyasi (§16.2):
// WS-Discovery multicast Probe + CIDR bo'yicha RTSP port-scan + merge.
// Natijalar xotirada saqlanadi; qo'shish BulkAdd orqali (creator=camsvc).

// ScannerConfig — skan parametrlari (config.Discovery'dan).
type ScannerConfig struct {
	CIDRs       []string                     // bo'sh → faol interfeys tarmoqlari
	Ports       []int                        // default [554]
	MaxHosts    int                          // default 1024 (§16.4)
	Parallel    int                          // default 25 (§16.1)
	HostTimeout time.Duration                // bitta host (default 500 ms)
	WsddTimeout time.Duration                // multicast kutish (default 3 s)
	Probe       HostProbe                    // nil → TCPHostProbe (port ochiqligi)
	Wsdd        func(time.Duration) []Device // nil → wsddDevices (test uchun inject)
}

type Scanner struct {
	creator CameraCreator
	known   func() []Known
	cfg     ScannerConfig

	mu      sync.Mutex
	last    []MergeResult
	running bool
}

func NewScanner(creator CameraCreator, known func() []Known, cfg ScannerConfig) *Scanner {
	if len(cfg.Ports) == 0 {
		cfg.Ports = []int{rtspDefaultPort}
	}
	if cfg.MaxHosts <= 0 {
		cfg.MaxHosts = 1024
	}
	if cfg.Parallel <= 0 {
		cfg.Parallel = 25
	}
	if cfg.HostTimeout <= 0 {
		cfg.HostTimeout = 500 * time.Millisecond
	}
	if cfg.WsddTimeout <= 0 {
		cfg.WsddTimeout = 3 * time.Second
	}
	if cfg.Probe == nil {
		cfg.Probe = TCPHostProbe(cfg.HostTimeout)
	}
	return &Scanner{creator: creator, known: known, cfg: cfg}
}

// StartScan — WS-Discovery + port-scan; qaytarish: probe qilingan host soni.
func (s *Scanner) StartScan() (int, error) {
	s.mu.Lock()
	if s.running {
		n := len(s.last)
		s.mu.Unlock()
		return n, nil
	}
	s.mu.Unlock()

	wsdd := s.cfg.Wsdd
	if wsdd == nil {
		wsdd = wsddDevices
	}
	devs := wsdd(s.cfg.WsddTimeout)
	ctx, cancel := context.WithTimeout(context.Background(),
		s.cfg.WsddTimeout+60*time.Second)
	defer cancel()
	hosts, err := ScanHosts(s.cidrs(), s.cfg.MaxHosts)
	if err != nil {
		return 0, err
	}
	scanned := len(hosts)
	for _, port := range s.cfg.Ports {
		got, err := Scan(ctx, ScanOptions{
			CIDRs: s.cidrs(), Port: port, Parallel: s.cfg.Parallel,
			Timeout: s.cfg.HostTimeout, MaxHosts: s.cfg.MaxHosts,
		}, s.cfg.Probe)
		if err != nil {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			return scanned, fmt.Errorf("port-scan %d: %w", port, err)
		}
		devs = append(devs, got...)
	}
	devs = dedupeByIP(devs)
	var known []Known
	if s.known != nil {
		known = s.known()
	}
	res := Merge(devs, known)
	markKnownByIP(res, known) // MAC saqlanmagan kameralar: IP+port bo'yicha ham "known"
	s.mu.Lock()
	s.last = res
	s.running = false
	s.mu.Unlock()
	return scanned, nil
}

// markKnownByIP — Merge MAC/serial bo'yicha mos kelmagan holatda IP+port
// bo'yicha ikkilamchi moslash qo'yadi (kamera modelida MAC saqlanmaydi).
func markKnownByIP(res []MergeResult, known []Known) {
	byIP := map[string]string{}
	for _, k := range known {
		p := k.Port
		if p == 0 {
			p = rtspDefaultPort
		}
		byIP[k.IP+":"+itoa(p)] = k.ID
	}
	for i := range res {
		if res[i].AlreadyKnown {
			continue
		}
		if id, ok := byIP[res[i].IP+":"+itoa(res[i].Port)]; ok {
			res[i].AlreadyKnown, res[i].ExistingID = true, id
		}
	}
}

// dedupeByIP — bitta IP bo'yicha bir yozuv: WS-Discovery ma'lumoti
// (model/ONVIF/MAC to'liq) ustuvor; port-scan yozuvi faqat yangi IP uchun
// qoladi. Port 0 → 554 (UI jadvalida ham shu ko'rinadi).
func dedupeByIP(devs []Device) []Device {
	byIP := map[string]int{}
	out := make([]Device, 0, len(devs))
	for _, d := range devs {
		if d.Port == 0 {
			d.Port = rtspDefaultPort
		}
		if i, ok := byIP[d.IP]; ok {
			if out[i].Model == "" && d.Model != "" {
				out[i].Model, out[i].Name, out[i].MAC = d.Model, d.Name, d.MAC
			}
			continue
		}
		byIP[d.IP] = len(out)
		out = append(out, d)
	}
	return out
}

// Results — oxirgi skan natijalari.
func (s *Scanner) Results() []MergeResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]MergeResult(nil), s.last...)
}

// AddSelected — tanlangan kalitlarni kamera qilib qo'shish (§16.3).
// AlreadyKnown kalitlar chiqarib tashlanadi.
func (s *Scanner) AddSelected(keys []string, in BulkAddInput) (BulkAddReport, error) {
	want := make(map[string]bool, len(keys))
	for _, k := range keys {
		want[k] = true
	}
	s.mu.Lock()
	for _, m := range s.last {
		if want[DeviceKey(m.Device)] && !m.AlreadyKnown {
			in.Devices = append(in.Devices, m.Device)
		}
	}
	s.mu.Unlock()
	return BulkAdd(s.creator, in), nil
}

// cidrs — sozlangan yoki interfeyslardan olingan to'r ro'yxati.
func (s *Scanner) cidrs() []string {
	if len(s.cfg.CIDRs) > 0 {
		return s.cfg.CIDRs
	}
	var out []string
	for _, n := range localNets() {
		ones, bits := n.Mask.Size()
		if bits != 32 || ones < 22 {
			continue // §16.4: /22 dan keng to'r skanlanmaydi (docker0 va h.k.)
		}
		out = append(out, n.String())
	}
	return out
}

// localNets — faol interfeyslarning IPv4 tarmoqlari (loopback/link-local yo'q).
func localNets() []*net.IPNet {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []*net.IPNet
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := ipn.IP.To4()
			if v4 == nil || v4.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, &net.IPNet{IP: v4, Mask: net.IPMask(ipn.Mask)})
		}
	}
	return out
}
