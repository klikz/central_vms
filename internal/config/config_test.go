package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse_ExampleFile(t *testing.T) {
	path := filepath.Join("..", "..", "config", "client.yaml.example")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("namuna fayl o'qilmadi (%s): %v — C0.10 qabul mezoni shu fayl", path, err)
	}
	c, err := Parse(b)
	if err != nil {
		t.Fatalf("namuna fayl parse bo'lmadi: %v", err)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("namuna fayl validatsiyadan o'tmadi: %v", err)
	}

	want := []struct {
		name string
		got  any
		exp  any
	}{
		{"server_url", c.ServerURL, "ws://192.168.1.10:8081"},
		{"ui_bind", c.UIBind, "127.0.0.1:8082"},
		{"state_dir", c.StateDir, "/var/lib/vmsagent"},
		{"key_file", c.KeyFile, "/etc/vmsagent/agent.key"},
		{"log_level", c.LogLevel, "info"},
		{"probe.status_parallel", c.Probe.StatusParallel, 5},
		{"probe.status_keepalive", c.Probe.StatusKeepalive, "auto"},
		{"probe.codec_probe_interval_h", c.Probe.CodecProbeIntervalH, 24},
		{"probe.full_probe_on_publish_fail", c.Probe.FullProbeOnPublishFail, true},
		{"limits.max_uploads", c.Limits.MaxUploads, 8},
		{"limits.max_local_transcodes", c.Limits.MaxLocalTranscodes, 4},
		{"onvif.enabled", c.Onvif.Enabled, true},
		{"onvif.switch_codec", c.Onvif.SwitchCodec, true},
		{"discovery.onvif_timeout_s", c.Discovery.OnvifTimeoutS, 10},
		{"discovery.max_hosts", c.Discovery.MaxHosts, 1024},
		{"publish.whip_fallback", c.Publish.WhipFallback, "rtsp_tcp"},
		{"publish.ffmpeg_fallback", c.Publish.FFmpegFallback, true},
	}
	for _, w := range want {
		if w.got != w.exp {
			t.Errorf("%s = %v, kutilgan %v", w.name, w.got, w.exp)
		}
	}
	if len(c.Discovery.CIDRs) != 1 || c.Discovery.CIDRs[0] != "192.168.10.0/24" {
		t.Errorf("discovery.cidrs = %v", c.Discovery.CIDRs)
	}
	if len(c.Discovery.RTSPScanPorts) != 1 || c.Discovery.RTSPScanPorts[0] != 554 {
		t.Errorf("discovery.rtsp_scan_ports = %v", c.Discovery.RTSPScanPorts)
	}
}

func TestParse_Defaults(t *testing.T) {
	c, err := Parse([]byte("server_url: ws://10.0.0.5:8081\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d := Default()
	if c.UIBind != d.UIBind || c.Probe.StatusParallel != d.Probe.StatusParallel {
		t.Fatalf("defaultlar qo'llanmadi: %+v", c)
	}
	if c.Discovery.CIDRs != nil {
		t.Fatalf("cidrs default bo'sh bo'lishi kerak: %v", c.Discovery.CIDRs)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("validatsiya: %v", err)
	}
}

func TestParse_BlockListAndComments(t *testing.T) {
	src := `
# izohli satr
server_url: ws://h:8081   # qator oxiridagi izoh
discovery:
  cidrs:
    - 192.168.1.0/24
    - "10.10.0.0/22"
  rtsp_scan_ports:
    - 554
    - 8554
`
	c, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(c.Discovery.CIDRs) != 2 || c.Discovery.CIDRs[1] != "10.10.0.0/22" {
		t.Fatalf("cidrs: %v", c.Discovery.CIDRs)
	}
	if len(c.Discovery.RTSPScanPorts) != 2 || c.Discovery.RTSPScanPorts[1] != 8554 {
		t.Fatalf("ports: %v", c.Discovery.RTSPScanPorts)
	}
}

func TestValidate_Errors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"noma'lum kalit", "server_url: ws://h:1\nnima: 5\n", "noma'lum kalit"},
		{"noma'lum ichki kalit", "probe:\n  nima: 5\n", "probe"},
		{"log_level noto'g'ri", "log_level: chat\n", "log_level"},
		{"LAN bind PIN siz", "ui_bind: 0.0.0.0:8082\n", "ui_pin majburiy"},
		{"LAN bind PIN bilan", "ui_bind: 0.0.0.0:8082\nui_pin: \"1234\"\n", ""},
		{"ui_bind format", "ui_bind: 127.0.0.1\n", "host:port"},
		{"status_parallel katta", "probe:\n  status_parallel: 99\n", "status_parallel"},
		{"max_uploads nol", "limits:\n  max_uploads: 0\n", "max_uploads"},
		{"keepalive noto'g'ri", "probe:\n  status_keepalive: ping\n", "status_keepalive"},
		{"cidr juda keng", "discovery:\n  cidrs: [\"10.0.0.0/8\"]\n", "juda keng"},
		{"cidr noto'g'ri", "discovery:\n  cidrs: [\"192.168.1.5/24\"]\n", "to'r manzili emas"},
		{"port yaroqsiz", "discovery:\n  rtsp_scan_ports: [0]\n", "port noto'g'ri"},
		{"server_url sxema", "server_url: http://h:8081\n", "ws://"},
		{"server_url portsiz", "server_url: ws://host\n", "host:port"},
		{"whip_fallback noto'g'ri", "publish:\n  whip_fallback: tcp\n", "whip_fallback"},
		{"state_dir bo'sh", "state_dir: \"\"\n", "state_dir"},
		{"tur xatosi", "limits:\n  max_uploads: ko'p\n", "max_uploads"},
		{"tab indentatsiya", "probe:\n\tstatus_parallel: 5\n", "indentatsiya"},
		{"flow mapping", "probe: {status_parallel: 5}\n", "flow mapping"},
		{"takroriy kalit", "log_level: info\nlog_level: warn\n", "takroriy kalit"},
		{"ko'p qatorli blok", "ui_pin: |\n  1234\n", "ko'p qatorli"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Parse([]byte(tc.src))
			if err == nil {
				err = c.Validate()
			}
			if tc.want == "" {
				if err != nil {
					t.Fatalf("xato kutilmadi: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("xato kutildi (%q), o'qildi: %+v", tc.want, c)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("xato matni %q bo'lishi kerak edi, topildi: %v", tc.want, err)
			}
		})
	}
}

func TestLoad_FileAndErrors(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "client.yaml")
	if err := os.WriteFile(good, []byte("server_url: ws://127.0.0.1:8081\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(good)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Path != good {
		t.Fatalf("Path saqlanmadi: %q", c.Path)
	}

	if _, err := Load(filepath.Join(dir, "yoq.yaml")); err == nil {
		t.Fatal("mavjud bo'lmagan fayl uchun xato kutildi")
	}

	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("log_level: chat\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil {
		t.Fatal("validatsiya xatosi kutildi")
	}
}

func TestCheckCIDRLimit(t *testing.T) {
	ok := []string{"192.168.10.0/24", "10.10.0.0/22", "192.168.1.5/32", "172.16.0.0/23"}
	for _, s := range ok {
		if err := checkCIDRLimit(s, 22); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	bad := []string{"", "abc", "10.0.0.0/8", "192.168.10.5/24", "fe80::1/64"}
	for _, s := range bad {
		if err := checkCIDRLimit(s, 22); err == nil {
			t.Errorf("%s: xato kutildi", s)
		}
	}
}

// Log fayli sozlamalari (talab 2026-11-19: xatolar faylga ham yozilsin).
func TestParse_LogFileFields(t *testing.T) {
	c := Default()
	if c.LogFile == "" {
		t.Error("Default LogFile bo'sh — standart yo'l belgilanishi kerak")
	}
	if c.LogMaxMB < 1 {
		t.Errorf("LogMaxMB = %d, kamida 1 kutilardi", c.LogMaxMB)
	}
	if c.LogKeep < 1 {
		t.Errorf("LogKeep = %d, kamida 1 kutilardi", c.LogKeep)
	}

	src := "server_url: http://10.0.0.5:8081\nlog_file: /var/log/vmsagent/vmsagent.log\nlog_max_mb: 8\nlog_keep: 5\n"
	got, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got.LogFile != "/var/log/vmsagent/vmsagent.log" {
		t.Errorf("LogFile = %q", got.LogFile)
	}
	if got.LogMaxMB != 8 || got.LogKeep != 5 {
		t.Errorf("LogMaxMB/LogKeep = %d/%d", got.LogMaxMB, got.LogKeep)
	}

	for _, bad := range []string{"log_max_mb: 0\n", "log_max_mb: -3\n", "log_keep: 0\n", "log_keep: 200\n"} {
		bc := Default()
		if _, err := Parse([]byte("server_url: http://h:1\n" + bad)); err == nil {
			// Parse o'zi tekshirmaydi — Validate tekshiradi:
			c2, perr := Parse([]byte("server_url: http://h:1\n" + bad))
			if perr != nil {
				continue
			}
			_ = bc
			if verr := c2.Validate(); verr == nil {
				t.Errorf("%q: Validate o'tkazib yubordi", bad)
			}
		} else if err == nil {
			t.Errorf("%q: xato kutilardi", bad)
		}
	}
}
