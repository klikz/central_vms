package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// Config — client.yaml to'liq sxemasi (docs/protocol.md §11).
// Server `config_push` orqali Limits/Probe qismlarini dinamik yangilaydi (§5.7):
// server qiymatlari fayldagidan ustun bo'ladi.
type Config struct {
	ServerURL string `yaml:"server_url"`
	UIBind    string `yaml:"ui_bind"`
	UIPin     string `yaml:"ui_pin"`
	StateDir  string `yaml:"state_dir"`
	KeyFile   string `yaml:"key_file"`
	LogLevel  string `yaml:"log_level"`

	// Log fayli (talab 2026-11-19): xatolar terminal+BILAN faylga ham.
	// Client fayli ALLOHIIDA (serverniki vmsd o'z faylida).
	LogFile  string `yaml:"log_file"`
	LogMaxMB int    `yaml:"log_max_mb"`
	LogKeep  int    `yaml:"log_keep"`

	Probe struct {
		StatusParallel         int    `yaml:"status_parallel"`
		StatusKeepalive        string `yaml:"status_keepalive"` // auto|get_parameter|options|describe
		CodecProbeIntervalH    int    `yaml:"codec_probe_interval_h"`
		FullProbeOnPublishFail bool   `yaml:"full_probe_on_publish_fail"`
	} `yaml:"probe"`

	Limits struct {
		MaxUploads         int `yaml:"max_uploads"`
		MaxLocalTranscodes int `yaml:"max_local_transcodes"`
	} `yaml:"limits"`

	Onvif struct {
		Enabled     bool `yaml:"enabled"`
		SwitchCodec bool `yaml:"switch_codec"`
	} `yaml:"onvif"`

	Discovery struct {
		CIDRs         []string `yaml:"cidrs"`
		OnvifTimeoutS int      `yaml:"onvif_timeout_s"`
		RTSPScanPorts []int    `yaml:"rtsp_scan_ports"`
		MaxHosts      int      `yaml:"max_hosts"`
	} `yaml:"discovery"`

	Publish struct {
		WhipFallback   string `yaml:"whip_fallback"`   // rtsp_tcp|off
		FFmpegFallback bool   `yaml:"ffmpeg_fallback"` // Go yo'li ishlamasa
	} `yaml:"publish"`

	// Path — yuklangan faylning yo'li (xatolarni ko'rsatish uchun).
	Path string `yaml:"-"`
}

// Default — barcha maydonlarning boshlang'ich qiymatlari (§10, §11).
func Default() Config {
	var c Config
	c.ServerURL = ""
	c.UIBind = "127.0.0.1:8082"
	c.UIPin = ""
	c.StateDir = "/var/lib/vmsagent"
	c.KeyFile = "/etc/vmsagent/agent.key"
	c.LogLevel = "info"
	c.LogFile = "/var/log/vmsagent/vmsagent.log"
	c.LogMaxMB = 4
	c.LogKeep = 5

	c.Probe.StatusParallel = 5
	c.Probe.StatusKeepalive = "auto"
	c.Probe.CodecProbeIntervalH = 24
	c.Probe.FullProbeOnPublishFail = true

	c.Limits.MaxUploads = 8
	c.Limits.MaxLocalTranscodes = 4

	c.Onvif.Enabled = true
	c.Onvif.SwitchCodec = true

	c.Discovery.CIDRs = nil
	c.Discovery.OnvifTimeoutS = 10
	c.Discovery.RTSPScanPorts = []int{554}
	c.Discovery.MaxHosts = 1024

	c.Publish.WhipFallback = "rtsp_tcp"
	c.Publish.FFmpegFallback = true
	return c
}

// Load — faylni o'qib, defaultlar bilan birlashtiradi va tekshiradi.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("konfiguratsiyani o'qib bo'lmadi (%s): %w", path, err)
	}
	c, err := Parse(b)
	if err != nil {
		return Config{}, err
	}
	c.Path = path
	if err := c.Validate(); err != nil {
		return c, fmt.Errorf("konfiguratsiya noto'g'ri (%s): %w", path, err)
	}
	return c, nil
}

// Parse — baytlardan Config (testlar uchun fayl o'qimasdan).
func Parse(src []byte) (Config, error) {
	m, err := parseYAML(string(src))
	if err != nil {
		return Config{}, err
	}
	c := Default()

	setStr := func(key string, dst *string) error {
		if v, ok := getPath(m, key); ok {
			s, err := asString(v)
			if err != nil {
				return fieldErr(key, err)
			}
			*dst = s
		}
		return nil
	}
	setInt := func(key string, dst *int) error {
		if v, ok := getPath(m, key); ok {
			n, err := asInt(v)
			if err != nil {
				return fieldErr(key, err)
			}
			*dst = n
		}
		return nil
	}
	setBool := func(key string, dst *bool) error {
		if v, ok := getPath(m, key); ok {
			b, err := asBool(v)
			if err != nil {
				return fieldErr(key, err)
			}
			*dst = b
		}
		return nil
	}

	for _, f := range []struct {
		key string
		set func() error
	}{
		{"server_url", func() error { return setStr("server_url", &c.ServerURL) }},
		{"ui_bind", func() error { return setStr("ui_bind", &c.UIBind) }},
		{"ui_pin", func() error { return setStr("ui_pin", &c.UIPin) }},
		{"state_dir", func() error { return setStr("state_dir", &c.StateDir) }},
		{"key_file", func() error { return setStr("key_file", &c.KeyFile) }},
		{"log_level", func() error { return setStr("log_level", &c.LogLevel) }},
		{"log_file", func() error { return setStr("log_file", &c.LogFile) }},
		{"log_max_mb", func() error { return setInt("log_max_mb", &c.LogMaxMB) }},
		{"log_keep", func() error { return setInt("log_keep", &c.LogKeep) }},

		{"probe.status_parallel", func() error { return setInt("probe.status_parallel", &c.Probe.StatusParallel) }},
		{"probe.status_keepalive", func() error { return setStr("probe.status_keepalive", &c.Probe.StatusKeepalive) }},
		{"probe.codec_probe_interval_h", func() error {
			return setInt("probe.codec_probe_interval_h", &c.Probe.CodecProbeIntervalH)
		}},
		{"probe.full_probe_on_publish_fail", func() error {
			return setBool("probe.full_probe_on_publish_fail", &c.Probe.FullProbeOnPublishFail)
		}},

		{"limits.max_uploads", func() error { return setInt("limits.max_uploads", &c.Limits.MaxUploads) }},
		{"limits.max_local_transcodes", func() error {
			return setInt("limits.max_local_transcodes", &c.Limits.MaxLocalTranscodes)
		}},

		{"onvif.enabled", func() error { return setBool("onvif.enabled", &c.Onvif.Enabled) }},
		{"onvif.switch_codec", func() error { return setBool("onvif.switch_codec", &c.Onvif.SwitchCodec) }},

		{"discovery.onvif_timeout_s", func() error { return setInt("discovery.onvif_timeout_s", &c.Discovery.OnvifTimeoutS) }},
		{"discovery.max_hosts", func() error { return setInt("discovery.max_hosts", &c.Discovery.MaxHosts) }},
		{"publish.whip_fallback", func() error { return setStr("publish.whip_fallback", &c.Publish.WhipFallback) }},
		{"publish.ffmpeg_fallback", func() error { return setBool("publish.ffmpeg_fallback", &c.Publish.FFmpegFallback) }},
	} {
		if err := f.set(); err != nil {
			return Config{}, err
		}
	}

	if v, ok := getPath(m, "discovery.cidrs"); ok {
		list, err := asStringSlice(v)
		if err != nil {
			return Config{}, fieldErr("discovery.cidrs", err)
		}
		c.Discovery.CIDRs = list
	}
	if v, ok := getPath(m, "discovery.rtsp_scan_ports"); ok {
		list, err := asIntSlice(v)
		if err != nil {
			return Config{}, fieldErr("discovery.rtsp_scan_ports", err)
		}
		c.Discovery.RTSPScanPorts = list
	}

	if err := checkUnknownKeys(m); err != nil {
		return Config{}, err
	}
	return c, nil
}

// knownKeys — §11 sxemasidagi ruxsat etilgan kalitlar. Noma'lum kalit xato:
// printy xatosi yoki eski konfiguratsiya jimgina ishlatilmasin.
var knownKeys = map[string]bool{
	"server_url": true, "ui_bind": true, "ui_pin": true, "state_dir": true,
	"key_file": true, "log_level": true, "log_file": true, "log_max_mb": true,
	"log_keep": true, "probe": true, "limits": true,
	"onvif": true, "discovery": true, "publish": true,
}

var knownSub = map[string]map[string]bool{
	"probe": {"status_parallel": true, "status_keepalive": true,
		"codec_probe_interval_h": true, "full_probe_on_publish_fail": true},
	"limits": {"max_uploads": true, "max_local_transcodes": true},
	"onvif":  {"enabled": true, "switch_codec": true},
	"discovery": {"cidrs": true, "onvif_timeout_s": true, "rtsp_scan_ports": true,
		"max_hosts": true},
	"publish": {"whip_fallback": true, "ffmpeg_fallback": true},
}

func checkUnknownKeys(m map[string]any) error {
	for k, v := range m {
		if !knownKeys[k] {
			return fmt.Errorf("noma'lum kalit: %q", k)
		}
		sub, ok := knownSub[k]
		if !ok {
			continue
		}
		mm, ok := v.(map[string]any)
		if !ok {
			continue
		}
		for kk := range mm {
			if !sub[kk] {
				return fmt.Errorf("noma'lum kalit: %q.%q", k, kk)
			}
		}
	}
	return nil
}

// Validate — qiymatlarning to'g'riligi va xavfsizlik talablari (§10.3, §16.4).
func (c Config) Validate() error {
	if c.ServerURL != "" {
		u, err := parseWSURL(c.ServerURL)
		if err != nil {
			return fmt.Errorf("server_url: %w", err)
		}
		_ = u
	}

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log_level: debug|info|warn|error kerak, topildi %q", c.LogLevel)
	}

	if c.LogMaxMB < 1 || c.LogMaxMB > 512 {
		return fmt.Errorf("log_max_mb: 1..512 kerak, topildi %d", c.LogMaxMB)
	}
	if c.LogKeep < 1 || c.LogKeep > 50 {
		return fmt.Errorf("log_keep: 1..50 kerak, topildi %d", c.LogKeep)
	}

	host, port, err := net.SplitHostPort(c.UIBind)
	if err != nil {
		return fmt.Errorf("ui_bind: host:port formati kerak (%v)", err)
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("ui_bind: port noto'g'ri: %q", port)
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		// §10.3: LAN'ga chiqarish faqat PIN bilan ruxsat etiladi.
		if c.UIPin == "" {
			return fmt.Errorf("ui_bind: LAN manzili (%s) uchun ui_pin majburiy", host)
		}
	}

	switch c.Probe.StatusKeepalive {
	case "auto", "get_parameter", "options", "describe":
	default:
		return fmt.Errorf("probe.status_keepalive: noto'g'ri %q", c.Probe.StatusKeepalive)
	}
	if c.Probe.StatusParallel < 1 || c.Probe.StatusParallel > 32 {
		return fmt.Errorf("probe.status_parallel: 1..32 oralig'ida bo'lishi kerak (hozir %d)", c.Probe.StatusParallel)
	}
	if c.Probe.CodecProbeIntervalH < 1 {
		return fmt.Errorf("probe.codec_probe_interval_h: >= 1 bo'lishi kerak")
	}

	if c.Limits.MaxUploads < 1 || c.Limits.MaxUploads > 64 {
		return fmt.Errorf("limits.max_uploads: 1..64 oralig'ida bo'lishi kerak (hozir %d)", c.Limits.MaxUploads)
	}
	if c.Limits.MaxLocalTranscodes < 0 || c.Limits.MaxLocalTranscodes > 16 {
		return fmt.Errorf("limits.max_local_transcodes: 0..16 oralig'ida bo'lishi kerak")
	}

	if c.Discovery.OnvifTimeoutS < 1 || c.Discovery.OnvifTimeoutS > 60 {
		return fmt.Errorf("discovery.onvif_timeout_s: 1..60 oralig'ida bo'lishi kerak")
	}
	if c.Discovery.MaxHosts < 1 || c.Discovery.MaxHosts > 4096 {
		return fmt.Errorf("discovery.max_hosts: 1..4096 oralig'ida bo'lishi kerak")
	}
	for _, cidr := range c.Discovery.CIDRs {
		if err := checkCIDRLimit(cidr, 22); err != nil {
			return fmt.Errorf("discovery.cidrs: %w", err)
		}
	}
	for _, p := range c.Discovery.RTSPScanPorts {
		if p < 1 || p > 65535 {
			return fmt.Errorf("discovery.rtsp_scan_ports: port noto'g'ri: %d", p)
		}
	}

	switch c.Publish.WhipFallback {
	case "rtsp_tcp", "off":
	default:
		return fmt.Errorf("publish.whip_fallback: rtsp_tcp|off kerak, topildi %q", c.Publish.WhipFallback)
	}

	if c.StateDir == "" || c.KeyFile == "" {
		return fmt.Errorf("state_dir va key_file bo'sh bo'lmasligi kerak")
	}
	return nil
}

// checkCIDRLimit — skan uchun ruxsat etilgan eng keng to'r (§16.4: /22 dan keng rad).
func checkCIDRLimit(cidr string, maxPrefix int) error {
	if cidr == "" {
		return fmt.Errorf("bo'sh CIDR")
	}
	if !strings.Contains(cidr, "/") {
		cidr += "/32"
	}
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("CIDR noto'g'ri: %q", cidr)
	}
	ones, bits := network.Mask.Size()
	if bits == 128 {
		return fmt.Errorf("IPv6 CIDR qo'llab-quvvatlanmaydi: %q", cidr)
	}
	if ones < maxPrefix {
		return fmt.Errorf("%q juda keng (/%d), eng katta /%d ruxsat etilgan", cidr, ones, maxPrefix)
	}
	if ip.String() != network.IP.String() {
		return fmt.Errorf("%q to'r manzili emas (hosts qismi nol bo'lishi kerak)", cidr)
	}
	return nil
}

func parseWSURL(s string) (string, error) {
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "ws://") && !strings.HasPrefix(lower, "wss://") {
		return "", fmt.Errorf("ws:// yoki wss:// bilan boshlanishi kerak: %q", s)
	}
	rest := s[len("ws://"):]
	if strings.HasPrefix(lower, "wss://") {
		rest = s[len("wss://"):]
	}
	if _, _, err := net.SplitHostPort(strings.TrimPrefix(rest, "//")); err != nil {
		// port ko'rsatilmagan — standart 8081 emas, xato qaytaramiz (aniqlik uchun)
		return "", fmt.Errorf("host:port formati kerak: %q", s)
	}
	return rest, nil
}

// --- yordamchilar ---

func getPath(m map[string]any, dotted string) (any, bool) {
	parts := strings.Split(dotted, ".")
	cur := m
	for i, p := range parts {
		v, ok := cur[p]
		if !ok {
			return nil, false
		}
		if i == len(parts)-1 {
			return v, true
		}
		next, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		cur = next
	}
	return nil, false
}

func fieldErr(key string, err error) error {
	return fmt.Errorf("%s: %w", key, err)
}

func asString(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case bool:
		return strconv.FormatBool(x), nil
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf("satr kutildi, topildi %T", v)
	}
}

func asInt(v any) (int, error) {
	switch x := v.(type) {
	case int64:
		return int(x), nil
	case string:
		n, err := strconv.Atoi(x)
		if err != nil {
			return 0, fmt.Errorf("son kutildi: %q", x)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("son kutildi, topildi %T", v)
	}
}

func asBool(v any) (bool, error) {
	switch x := v.(type) {
	case bool:
		return x, nil
	case string:
		b, err := strconv.ParseBool(x)
		if err != nil {
			return false, fmt.Errorf("boolean kutildi: %q", x)
		}
		return b, nil
	default:
		return false, fmt.Errorf("boolean kutildi, topildi %T", v)
	}
}

func asStringSlice(v any) ([]string, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("ro'yxat kutildi, topildi %T", v)
	}
	out := make([]string, 0, len(list))
	for _, it := range list {
		s, err := asString(it)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func asIntSlice(v any) ([]int, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("ro'yxat kutildi, topildi %T", v)
	}
	out := make([]int, 0, len(list))
	for _, it := range list {
		n, err := asInt(it)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}
