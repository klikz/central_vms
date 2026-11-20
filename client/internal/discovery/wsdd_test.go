package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readWSDD(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "wsdd", name))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return b
}

// §16.1: ProbeMatch XML'ni parse qilish — tarmoq talab qilmaydi.
func TestWSDD_ParseDahua(t *testing.T) {
	devs, err := ParseProbeMatches(readWSDD(t, "probe-match-dahua.xml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(devs) != 1 {
		t.Fatalf("device'lar = %d, 1 kutilardi", len(devs))
	}
	d := devs[0]
	if d.EndpointRef != "urn:uuid:0000aaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Errorf("EndpointRef = %q", d.EndpointRef)
	}
	if d.Name != "Dahua" {
		t.Errorf("Name = %q, Dahua kutilardi (scope .../name/Dahua)", d.Name)
	}
	if d.Model != "IPC-HFW1230S" {
		t.Errorf("Model = %q, IPC-HFW1230S kutilardi (scope .../hardware/...)", d.Model)
	}
	if len(d.XAddrs) != 1 || d.XAddrs[0] != "http://192.168.10.31:80/onvif/device_service" {
		t.Errorf("XAddrs = %v", d.XAddrs)
	}
	if d.IP != "192.168.10.31" {
		t.Errorf("IP = %q, 192.168.10.31 kutilardi (XAddrs host)", d.IP)
	}
	if !strings.Contains(strings.Join(d.Types, " "), "NetworkVideoTransmitter") {
		t.Errorf("Types = %v, NetworkVideoTransmitter kutilardi", d.Types)
	}
}

func TestWSDD_ParseEmpty(t *testing.T) {
	devs, err := ParseProbeMatches(readWSDD(t, "probe-match-empty.xml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(devs) != 0 {
		t.Fatalf("device'lar = %d, 0 kutilardi", len(devs))
	}
}

// XML yaroqsiz bo'lsa — aniq xato (jimgina bo'sh ro'yxat qaytarmasin).
func TestWSDD_BadXML(t *testing.T) {
	if _, err := ParseProbeMatches([]byte("<s:Envelope><oops")); err == nil {
		t.Fatal("yaroqsiz XML'da xato kutilardi")
	}
}

// Scope'lar: mac_address va serial bo'lsa ajratiladi (§16.2 unikal kalit).
func TestWSDD_ScopeMacSerial(t *testing.T) {
	xml := `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
            xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing">
 <s:Body><d:ProbeMatches><d:ProbeMatch>
  <a:EndpointReference><a:Address>urn:uuid:x</a:Address></a:EndpointReference>
  <d:Types>dn:NetworkVideoTransmitter</d:Types>
  <d:Scopes>onvif://www.onvif.org/name/Acme onvif://www.onvif.org/hardware/IP-123
onvif://www.onvif.org/mac_address/3c:a5:09:11:22:33 onvif://www.onvif.org/serial/SN9</d:Scopes>
  <d:XAddrs>http://10.0.0.9:80/onvif/device_service</d:XAddrs>
 </d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`
	devs, err := ParseProbeMatches([]byte(xml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(devs) != 1 {
		t.Fatalf("device'lar = %d", len(devs))
	}
	d := devs[0]
	if d.MAC != "3c:a5:09:11:22:33" {
		t.Errorf("MAC = %q", d.MAC)
	}
	if d.Serial != "SN9" {
		t.Errorf("Serial = %q", d.Serial)
	}
}
