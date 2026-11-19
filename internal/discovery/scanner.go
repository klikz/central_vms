package discovery

import (
	"context"
	"net"
	"sort"
	"sync"
	"time"
)

// Scanner — DiscoveryService'ning real implementatsiyasi (§16.2):
// WS-Discovery multicast Probe + CIDR bo'yicha RTSP port-scan + merge.
// Skan FONIDA yuradi: StartScan darhol qaytadi, Results() topilganlarni
// jarayon davomida ko'rsatib boradi (UI jonli ro'yxat uchun poll qiladi).

// ScannerConfig — skan parametrlari (config.Discovery'dan).
type ScannerConfig struct {
	CIDRs         []string                     // bo'sh → faol interfeys tarmoqlari
	Ports         []int                        // default [554]
	MaxHosts      int                          // default 1024 (§16.4)
	Parallel      int                          // default 64 (§16.1)
	HostTimeout   time.Duration                // bitta host (default 500 ms)
	WsddTimeout   time.Duration                // multicast kutish (default 3 s)
	Probe         HostProbe                    // nil → TCPHostProbe (port ochiqligi)
	Wsdd          func(time.Duration) []Device // nil → wsddDevices (test uchun inject)
	BannerTimeout time.Duration                // RTSP OPTIONS banner (0 → 500 ms)
}

// Progress — skan holati (UI progress qatori uchun).
type Progress struct {
	Running    bool      `json:"running"`
	Scanned    int       `json:"scanned"`
	Total      int       `json:"total"`
	Found      int       `json:"found"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
}

type Scanner struct {
	creator CameraCreator
	known   func() []Known
	cfg     ScannerConfig

	mu         sync.Mutex
	raw        []Device
	scanned    int
	total      int
	running    bool
	startedAt  time.Time
	finishedAt time.Time
}

func NewScanner(creator CameraCreator, known func() []Known, cfg ScannerConfig) *Scanner {
	if len(cfg.Ports) == 0 {
		cfg.Ports = []int{rtspDefaultPort}
	}
	if cfg.MaxHosts <= 0 {
		cfg.MaxHosts = 1024
	}
	if cfg.Parallel <= 0 {
		cfg.Parallel = 64
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
	if cfg.BannerTimeout <= 0 {
		cfg.BannerTimeout = 500 * time.Millisecond
	}
	return &Scanner{creator: creator, known: known, cfg: cfg}
}

// StartScan — skanni fonda boshlaydi va darhol qaytadi. Allaqachon
// ishlayotgan bo'lsa yangi skan boshlanmaydi (xato emas).
func (s *Scanner) StartScan() error {
	hosts, err := ScanHosts(s.cidrs(), s.cfg.MaxHosts)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.raw = nil
	s.scanned = 0
	s.total = len(hosts) * len(s.cfg.Ports)
	s.startedAt = time.Now()
	s.finishedAt = time.Time{}
	s.mu.Unlock()

	go s.scan(hosts)
	return nil
}

// scan — wsdd avval (tez natija), so'ng port-scan: har ochiq port topilishi
// bilan ro'yxatga qo'shiladi (jonli ko'rinish). Banner ma'lumoti oxirida
// joyida to'ldiriladi.
func (s *Scanner) scan(hosts []string) {
	defer func() {
		s.mu.Lock()
		s.running = false
		s.finishedAt = time.Now()
		s.mu.Unlock()
	}()

	wsdd := s.cfg.Wsdd
	if wsdd == nil {
		wsdd = wsddDevices
	}
	for _, d := range wsdd(s.cfg.WsddTimeout) {
		s.add(d)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	sem := make(chan struct{}, s.cfg.Parallel)
	var wg sync.WaitGroup
	for _, port := range s.cfg.Ports {
		for _, h := range hosts {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				wg.Wait()
				return
			}
			wg.Add(1)
			go func(h string, port int) {
				defer wg.Done()
				defer func() { <-sem }()
				pctx, pcancel := context.WithTimeout(ctx, s.cfg.HostTimeout)
				defer pcancel()
				ok := s.cfg.Probe(pctx, h, port)
				s.tick()
				if ok {
					s.add(Device{IP: h, Port: port})
				}
			}(h, port)
		}
	}
	wg.Wait()
	s.enrich(ctx)
}

// add — topilgan qurilmani jonli ro'yxatga qo'shadi (IP bo'yicha takror
// birlashtiriladi: wsdd ma'lumoti ustuvor).
func (s *Scanner) add(d Device) {
	if d.Port == 0 {
		d.Port = rtspDefaultPort
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.raw {
		if s.raw[i].IP == d.IP {
			if s.raw[i].Model == "" && d.Model != "" {
				s.raw[i] = d
			}
			return
		}
	}
	s.raw = append(s.raw, d)
}

func (s *Scanner) tick() {
	s.mu.Lock()
	s.scanned++
	s.mu.Unlock()
}

// enrich — nomi yo'q qurilmalarga RTSP banner so'raydi (paralel).
func (s *Scanner) enrich(ctx context.Context) {
	s.mu.Lock()
	idx := make([]int, 0)
	for i := range s.raw {
		if s.raw[i].Model == "" {
			idx = append(idx, i)
		}
	}
	s.mu.Unlock()
	if len(idx) == 0 {
		return
	}
	sem := make(chan struct{}, s.cfg.Parallel)
	var wg sync.WaitGroup
	for _, i := range idx {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			s.mu.Lock()
			ip, port := s.raw[i].IP, s.raw[i].Port
			s.mu.Unlock()
			srv, auth := RTSPBanner(ctx, ip, port, s.cfg.BannerTimeout)
			if srv == "" && auth {
				srv = "RTSP (auth)"
			}
			if srv != "" {
				s.mu.Lock()
				s.raw[i].Model = srv
				s.mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
}

// Progress — skan holati.
func (s *Scanner) Progress() Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Progress{Running: s.running, Scanned: s.scanned, Total: s.total,
		Found: len(s.raw), StartedAt: s.startedAt, FinishedAt: s.finishedAt}
}

// Results — hozirga qadar topilganlar (mavjud kameralar bilan
// birlashtirilgan, IP tartibida).
func (s *Scanner) Results() []MergeResult {
	s.mu.Lock()
	devs := append([]Device(nil), s.raw...)
	s.mu.Unlock()
	devs = dedupeByIP(devs)
	var known []Known
	if s.known != nil {
		known = s.known()
	}
	res := Merge(devs, known)
	markKnownByIP(res, known)
	sort.Slice(res, func(a, b int) bool { return ipLess(res[a].IP, res[b].IP) })
	return res
}

// AddSelected — tanlangan kalitlarni kamera qilib qo'shish (§16.3).
func (s *Scanner) AddSelected(keys []string, in BulkAddInput) (BulkAddReport, error) {
	want := make(map[string]bool, len(keys))
	for _, k := range keys {
		want[k] = true
	}
	var picked []Device
	for _, m := range s.Results() {
		if want[DeviceKey(m.Device)] && !m.AlreadyKnown {
			picked = append(picked, m.Device)
		}
	}
	in.Devices = append(in.Devices, picked...)
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

// ipLess — IP manzillarini son tartibida solishtirish (matn emas).
func ipLess(a, b string) bool {
	ai, bi := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	if ai == nil || bi == nil {
		return a < b
	}
	for i := range ai {
		if ai[i] != bi[i] {
			return ai[i] < bi[i]
		}
	}
	return false
}

// dedupeByIP — bitta IP bo'yicha bir yozuv (wsdd ustuvor); port 0 → 554.
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

// markKnownByIP — Merge MAC/serial bo'yicha mos kelmagan holatda IP+port
// bo'yicha ikkilamchi moslash (kamera modelida MAC saqlanmaydi).
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
