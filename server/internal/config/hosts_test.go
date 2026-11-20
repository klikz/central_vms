package config

import (
	"net"
	"testing"
)

func ipnet(s string) *net.IPNet {
	return &net.IPNet{IP: net.ParseIP(s), Mask: net.CIDRMask(22, 32)}
}

func TestResolvePublicHost_Explicit(t *testing.T) {
	if got := ResolvePublicHost("10.0.0.5"); got != "10.0.0.5" {
		t.Fatalf("explicit: %q", got)
	}
	if got := ResolvePublicHost("  10.0.0.6 "); got != "10.0.0.6" {
		t.Fatalf("trim: %q", got)
	}
}

func TestPickHost_SkipsLoopbackAndDocker(t *testing.T) {
	ifs := []iface{
		{name: "lo", up: true, loopback: true, addrs: []net.Addr{ipnet("127.0.0.1")}},
		{name: "docker0", up: true, addrs: []net.Addr{ipnet("172.17.0.1")}},
		{name: "enp4s0", up: true, addrs: []net.Addr{ipnet("169.254.9.9"), ipnet("192.168.0.27")}},
	}
	if got := pickHost(ifs); got != "192.168.0.27" {
		t.Fatalf("docker0/link-local tanlandi: %q", got)
	}
	if got := pickHost(nil); got != "127.0.0.1" {
		t.Fatalf("bo'sh ro'yxat: %q", got)
	}
	// Faqat link-local bo'lsa — uni tanlamaymiz (LAN'da ishlatib bo'lmaydi).
	only := []iface{{name: "eth0", up: true, addrs: []net.Addr{ipnet("169.254.1.1")}}}
	if got := pickHost(only); got != "127.0.0.1" {
		t.Fatalf("link-local tanlandi: %q", got)
	}
}
