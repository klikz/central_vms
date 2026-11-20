package discovery

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"
)

// WS-Discovery ProbeMatch parse (docs/protocol.md §16.1).
// Multicast so'rov (239.255.255.250:3702) C2.16/C3 tarmoq qismida; bu yerda
// faqat kelgan XML javobni parse qilamiz — tarmoq test talab qilmaydi.

// Device — bitta topilgan kamera haqidagi xulosa (§16.2 natija ro'yxati).
type Device struct {
	EndpointRef string   // urn:uuid:... (ONVIF constant device id)
	Types       []string // dn:NetworkVideoTransmitter ...
	Scopes      []string // onvif://.../name/..., /hardware/..., /mac_address/...
	XAddrs      []string // ONVIF device_service manzillari
	Name        string   // scope .../name/
	Model       string   // scope .../hardware/
	MAC         string   // scope .../mac_address/ (bo'lsa)
	Serial      string   // scope .../serial/ (bo'lsa)
	IP          string   // XAddrs[0] host (bo'lsa)
	Port        int      // RTSP porti (0 → 554; port-scan C2.16 to'ldiradi)
}

// soapEnvelope — WS-Discovery javob konverti (local-name bo'yicha moslash).
type soapEnvelope struct {
	Body struct {
		ProbeMatches struct {
			Matches []struct {
				EndpointReference struct {
					Address string `xml:"Address"`
				} `xml:"EndpointReference"`
				Types  string `xml:"Types"`
				Scopes string `xml:"Scopes"`
				XAddrs string `xml:"XAddrs"`
			} `xml:"ProbeMatch"`
		} `xml:"ProbeMatches"`
	} `xml:"Body"`
}

// ParseProbeMatches — bitta ProbeMatch SOAP javobini qayta ishlaydi.
// Bo'sh `<ProbeMatches/>` → bo'sh ro'yxat (xato emas). Yaroqsiz XML → xato.
func ParseProbeMatches(raw []byte) ([]Device, error) {
	var env soapEnvelope
	if err := xml.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("wsdd: SOAP parse: %w", err)
	}
	out := make([]Device, 0, len(env.Body.ProbeMatches.Matches))
	for _, m := range env.Body.ProbeMatches.Matches {
		d := Device{
			EndpointRef: strings.TrimSpace(m.EndpointReference.Address),
			Types:       splitList(m.Types),
			Scopes:      splitList(m.Scopes),
			XAddrs:      splitList(m.XAddrs),
		}
		applyScopes(&d)
		d.IP = hostFromXAddr(d.XAddrs)
		out = append(out, d)
	}
	return out, nil
}

// splitList — WS-Discovery maydonlari bo'sh joy/yangi qator bilan ajraladi.
func splitList(s string) []string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// applyScopes — onvif://www.onvif.org/<kalit>/<qiymat> scope'laridan
// name/hardware/mac_address/serial ni ajratadi. Qiymat foiz-escape bo'lishi
// mumkin (%20 va h.k.).
func applyScopes(d *Device) {
	for _, sc := range d.Scopes {
		i := strings.Index(sc, "onvif.org/")
		if i < 0 {
			continue
		}
		rest := sc[i+len("onvif.org/"):] // name/Dahua yoki hardware/IPC...
		k, v, ok := strings.Cut(rest, "/")
		if !ok {
			continue
		}
		v = unescapeScope(v)
		switch k {
		case "name":
			d.Name = v
		case "hardware":
			d.Model = v
		case "mac_address", "MAC", "mac": // Hikvision "MAC/xx:.." yozadi
			d.MAC = v
		case "serial":
			d.Serial = v
		}
	}
}

func unescapeScope(v string) string {
	if !strings.Contains(v, "%") {
		return v
	}
	if u, err := url.PathUnescape(v); err == nil {
		return u
	}
	return v
}

// hostFromXAddr — XAddrs[0] URL'idan host (port'siz) oladi.
func hostFromXAddr(xaddrs []string) string {
	if len(xaddrs) == 0 {
		return ""
	}
	u, err := url.Parse(xaddrs[0])
	if err != nil {
		return ""
	}
	host := u.Hostname() // portni oladi
	return host
}
