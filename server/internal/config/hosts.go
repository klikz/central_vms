// Media URL'larida (agent publish, brauzer WHEP) ko'rinadigan manzil.
package config

import (
	"net"
	"strings"
)

// ResolvePublicHost — `media.public_host` atlangan bo'lsa o'sha ishlatiladi;
// bo'sh bo'lsa LAN interfeysining global IPv4 manzili qaytariladi.
//
// Nima uchun kerak: avval bo'sh qiymat 127.0.0.1 ga tushib qolardi va
// boshqa mashinadagi agent publish'ni O'Z localhost'iga ochmoqchi bo'lib,
// hech narsa chiqarmasdi (2026-11-20, ikkinchi PC'dagi client bilan
// ulanishda topildi).
func ResolvePublicHost(explicit string) string {
	if h := strings.TrimSpace(explicit); h != "" {
		return h
	}
	return pickHost(snapshotIfaces())
}

// iface — testlanadigan ko'rinish (net.Interface Go 1.26'da Addrs() metodi
// bilan beriladi, maydon emas).
type iface struct {
	name     string
	up       bool
	loopback bool
	addrs    []net.Addr
}

func snapshotIfaces() []iface {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]iface, 0, len(ifs))
	for _, ifc := range ifs {
		it := iface{name: ifc.Name, up: ifc.Flags&net.FlagUp != 0, loopback: ifc.Flags&net.FlagLoopback != 0}
		if addrs, err := ifc.Addrs(); err == nil {
			it.addrs = addrs
		}
		out = append(out, it)
	}
	return out
}

// pickHost — tanlov qoidalari: faol, loopback emas, konteyner/VPN soxta
// interfeyslari emas, global IPv4. Birinchi mos keladigan olinadi.
func pickHost(ifs []iface) string {
	for _, ifc := range ifs {
		if !ifc.up || ifc.loopback || isVirtual(ifc.name) {
			continue
		}
		for _, a := range ifc.addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			return ip4.String()
		}
	}
	return "127.0.0.1"
}

// isVirtual — Docker/ko'prik/VPN interfeyslari LAN manzili sifatida xato
// tanlanadi (bizning mashinada docker0 172.17.0.1 birinchi chiqadi).
func isVirtual(name string) bool {
	prefixes := []string{"docker", "br-", "veth", "virbr", "tun", "tap", "wg", "zt", "lo", "cni", "flannel", "cali", "kube"}
	for _, p := range prefixes {
		if strings.HasPrefix(strings.ToLower(name), p) {
			return true
		}
	}
	return false
}
