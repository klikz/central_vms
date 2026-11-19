package discovery

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"

	"central-vms/client/internal/rtspclient"
)

// RTSP port-scan (docs/protocol.md §16.1.2, §16.4) — ONVIF bo'lmagan
// Dahua uchun zaxira: discovery.cidrs bo'yicha TCP 554 + anonim DESCRIBE.
// Cheklovlar: /22 dan keng to'r rad etiladi, jami host ≤ max_hosts (1024),
// 25 parallel, host uchun timeout (default 500 ms).

// ScanOptions — skan sozlamalari (konfiguratsiyadan uzatiladi).
type ScanOptions struct {
	CIDRs    []string
	Port     int           // RTSP porti (0 → 554)
	Parallel int           // bir vaqtda (0 → 25, §16.1)
	Timeout  time.Duration // bitta host uchun (0 → 500 ms, §16.1)
	MaxHosts int           // jami host chegarasi (0 → 1024, §16.4)
}

func (o ScanOptions) withDefaults() ScanOptions {
	if o.Port == 0 {
		o.Port = rtspDefaultPort
	}
	if o.Parallel <= 0 {
		o.Parallel = 25
	}
	if o.Timeout <= 0 {
		o.Timeout = 500 * time.Millisecond
	}
	if o.MaxHosts <= 0 {
		o.MaxHosts = 1024
	}
	return o
}

// HostProbe — bitta host:portni tekshiradi; true → kamera javob berdi.
// Testlar uchun inject qilinadi; prod'da RTSPHostProbe.
type HostProbe func(ctx context.Context, host string, port int) bool

// ScanHosts — CIDR ro'yxatini tekshiradi va hostlar ro'yxatini qaytaradi.
// /22 dan keng yoki jami MaxHosts'dan katta → xato (§16.4).
func ScanHosts(cidrs []string, maxHosts int) ([]string, error) {
	if len(cidrs) == 0 {
		return nil, fmt.Errorf("scan: discovery.cidrs bo'sh")
	}
	if maxHosts <= 0 {
		maxHosts = 1024
	}
	var out []string
	for _, c := range cidrs {
		ips, err := hostsOfCIDR(c)
		if err != nil {
			return nil, err
		}
		if len(out)+len(ips) > maxHosts {
			return nil, fmt.Errorf("scan: jami host %d > %d (§16.4)", len(out)+len(ips), maxHosts)
		}
		out = append(out, ips...)
	}
	return out, nil
}

// hostsOfCIDR — bitta CIDR'dagi barcha IPv4 manzillari.
// Prefiks /22 dan keng bo'lsa rad (§16.4). Prefiksiz host → /32.
func hostsOfCIDR(c string) ([]string, error) {
	if _, _, err := net.ParseCIDR(c); err != nil {
		// ehtimol oddiy host — /32 deb olamiz
		ip := net.ParseIP(c)
		if ip == nil {
			return nil, fmt.Errorf("scan: CIDR/host yaroqsiz: %q", c)
		}
		return []string{ip.String()}, nil
	}
	ip, network, err := net.ParseCIDR(c)
	_ = ip
	if err != nil {
		return nil, fmt.Errorf("scan: CIDR yaroqsiz %q: %w", c, err)
	}
	ones, bits := network.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("scan: faqat IPv4 qo'llab-quvvatlanadi: %q", c)
	}
	if ones < 22 { // /22 dan keng → rad (1024 dan ko'p host)
		return nil, fmt.Errorf("scan: %q juda keng to'r (≤/22 ruxsat etiladi, §16.4)", c)
	}
	base := binary.BigEndian.Uint32(network.IP)
	count := uint32(1) << (32 - ones)
	out := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], base+i)
		out = append(out, net.IP(b[:]).String())
	}
	return out, nil
}

// Scan — hostlarni Parallel chegarasi bilan probe qiladi; javob
// beraganlarni Device qilib qaytaradi (tartib: CIDR tartibi).
// ctx bekor qilinsa — qolgan hostlar tashlanadi, to'plangan natija qaytadi.
func Scan(ctx context.Context, opts ScanOptions, probe HostProbe) ([]Device, error) {
	opts = opts.withDefaults()
	hosts, err := ScanHosts(opts.CIDRs, opts.MaxHosts)
	if err != nil {
		return nil, err
	}
	if probe == nil {
		return nil, fmt.Errorf("scan: probe berilmagan")
	}
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		got []Device
	)
	sem := make(chan struct{}, opts.Parallel)
	for _, h := range hosts {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return got, nil // bekor qilindi — qismiy natija (xato emas)
		}
		wg.Add(1)
		go func(h string) {
			defer wg.Done()
			defer func() { <-sem }()
			hctx, cancel := context.WithTimeout(ctx, opts.Timeout)
			defer cancel()
			if probe(hctx, h, opts.Port) {
				mu.Lock()
				got = append(got, Device{IP: h, Port: opts.Port})
				mu.Unlock()
			}
		}(h)
	}
	wg.Wait()
	return got, nil
}

// RTSPHostProbe — haqiqiy probe: anonim DESCRIBE (rtspclient orqali).
// Javob kelsa (200) → true. Ulanish/xato → false (host topilmagan).
func RTSPHostProbe(timeout time.Duration) HostProbe {
	return func(ctx context.Context, host string, port int) bool {
		u := &url.URL{Scheme: "rtsp", Host: net.JoinHostPort(host, strconv.Itoa(port)),
			Path: "/cam/realmonitor", RawQuery: "channel=1&subtype=0"}
		c, err := rtspclient.Dial(ctx, u.String(), rtspclient.Options{Timeout: timeout})
		if err != nil {
			return false
		}
		defer c.Close()
		sdp, err := c.Describe(ctx)
		if err != nil {
			return false
		}
		return len(sdp) > 0
	}
}
