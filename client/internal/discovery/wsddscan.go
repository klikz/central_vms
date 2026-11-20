package discovery

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// wsddDevices — WS-Discovery multicast Probe (§16.1): 239.255.255.250:3702
// ga so'rov yuborib, ProbeMatch javoblarini yig'adi. Multicast ishlamasa
// (tarmoq taqiqlagan bo'lsa) xato emas — bo'sh ro'yxat qaytadi, port-scan
// davom etadi.

const (
	wsddGroup = "239.255.255.250"
	wsddPort  = 3702
)

// probeTemplate — ONVIF kameralariga mo'ljallangan Probe xabari.
const probeTemplate = `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
 xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing"
 xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery">
 <s:Header>
  <a:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</a:Action>
  <a:MessageID>urn:uuid:%s</a:MessageID>
  <a:To>http://schemas.xmlsoap.org/ws/2005/04/discovery/multicast</a:To>
 </s:Header>
 <s:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></s:Body>
</s:Envelope>`

func wsddDevices(timeout time.Duration) []Device {
	pc, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil
	}
	defer pc.Close()
	uc := pc.(*net.UDPConn)
	msg := []byte(fmt.Sprintf(probeTemplate, "c0ffee00-0000-4000-8000-000000000001"))
	dst := &net.UDPAddr{IP: net.ParseIP(wsddGroup), Port: wsddPort}
	if _, err := uc.WriteTo(msg, dst); err != nil {
		return nil // multicast mavjud emas — port-scan baribir ishlaydi
	}
	var (
		mu   sync.Mutex
		out  []Device
		seen = map[string]bool{}
	)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = uc.SetReadDeadline(time.Now().Add(1200 * time.Millisecond))
		buf := make([]byte, 65536)
		n, _, err := uc.ReadFrom(buf)
		if err != nil {
			continue
		}
		devs, perr := ParseProbeMatches(buf[:n])
		if perr != nil {
			continue
		}
		mu.Lock()
		for _, d := range devs {
			k := d.EndpointRef
			if k == "" {
				k = d.IP + "|" + d.MAC
			}
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, d)
		}
		mu.Unlock()
	}
	return out
}
