package main

import (
	"net"
	"os"
	"strings"

	"central-vms/client/internal/clock"
	"github.com/google/uuid"
)

func clockReal() clock.Clock { return clock.Real{} }

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "noma'lum"
	}
	return h
}

// osVersion — /etc/os-release PRETTY_NAME (§4.1 hello.os).
func osVersion() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "linux"
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return "linux"
}

// lanIP — birinchi ulanish uchun mos manzil (§4.1 hello.lan_ip).
func lanIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipn.IP
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.To4() == nil {
			continue
		}
		return ip.String()
	}
	return ""
}

// bootID — har yuklashda yangi (§4.1 hello.boot_id). Mavjud bo'lsa /proc dan olamiz.
var bootIDCache = uuid.NewString()

func bootID() string {
	if b, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			return v
		}
	}
	return bootIDCache
}
