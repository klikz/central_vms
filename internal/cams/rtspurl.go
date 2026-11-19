package cams

import (
	"fmt"
	"net/url"
	"strings"
)

// RTSP URL qurish va yorliqlash (docs/protocol.md §4.2, §10.2).
// Log/UI'da har doim Masked() versiyasi ko'rsatiladi — parol ko'rinmaydi.

// RTSPURL — kamera manzili yadrosi (host/port/path + profil yo'llari).
type RTSPURL struct {
	Host string
	Port int
	Path string // main profil yo'li
	User string
	Pass string
}

// NewRTSPURL — §16 Dahua shabloni bo'yicha quradi.
// path bo'sh bo'lsa default realmonitor yo'li qo'yiladi.
func NewRTSPURL(host string, port int, path string) *RTSPURL {
	if port == 0 {
		port = 554
	}
	if path == "" {
		path = "/cam/realmonitor?channel=1&subtype=0"
	}
	return &RTSPURL{Host: host, Port: port, Path: path}
}

// SubURL — ikkinchi (sub) profil: subtype=0 → subtype=1 (Dahua),
// bo'lmasa yo'l o'zgarishsiz qoladi.
func (u *RTSPURL) SubURL() *RTSPURL {
	c := *u
	c.Path = strings.Replace(u.Path, "subtype=0", "subtype=1", 1)
	return &c
}

// WithAuth — usernama/parolni qo'shadi (keystore'dan olingan).
func (u *RTSPURL) WithAuth(user, pass string) *RTSPURL {
	c := *u
	c.User, c.Pass = user, pass
	return &c
}

// String — to'liq URL (faqich tarmoq so'rovi uchun; log'ga EMAS).
func (u *RTSPURL) String() string {
	cred := ""
	if u.User != "" {
		cred = url.QueryEscape(u.User) + ":" + url.QueryEscape(u.Pass) + "@"
	}
	return fmt.Sprintf("rtsp://%s%s:%d%s", cred, u.Host, u.Port, u.Path)
}

// Masked — parol yashirilgan ko'rinish (§10.2): rtsp://user:****@host:554/path
func (u *RTSPURL) Masked() string {
	cred := ""
	if u.User != "" {
		cred = url.QueryEscape(u.User) + ":****@"
	}
	return fmt.Sprintf("rtsp://%s%s:%d%s", cred, u.Host, u.Port, u.Path)
}

// ParseRTSPURL — UI/CSV orqali kiritilgan URL'ni o'qiydi.
func ParseRTSPURL(raw string) (*RTSPURL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("RTSP URL: %w", err)
	}
	if u.Scheme != "rtsp" {
		return nil, fmt.Errorf("RTSP URL: sxema %q (rtsp kutilmoqda)", u.Scheme)
	}
	port := 554
	if u.Port() != "" {
		if _, err := fmt.Sscanf(u.Port(), "%d", &port); err != nil {
			return nil, fmt.Errorf("RTSP URL: port: %w", err)
		}
	}
	out := &RTSPURL{Host: u.Hostname(), Port: port, Path: pathAndQuery(u)}
	if out.Path == "" {
		out.Path = "/"
	}
	if u.User != nil {
		out.User = u.User.Username()
		if p, ok := u.User.Password(); ok {
			out.Pass = p
		}
	}
	return out, nil
}

// MaskRawURL — istalgan URL satridagi parolni yashirish (log xavfsizligi).
// url.URL.String() "****" ni escape qilib yuborishi mumkin, shuning uchun
// satr qurish qo'lda bajariladi.
func MaskRawURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	auth := u.User.Username()
	if _, ok := u.User.Password(); ok {
		auth += ":****"
	}
	host := u.Host
	if host == "" {
		return raw
	}
	return u.Scheme + "://" + auth + "@" + host + pathAndQuery(u)
}

// pathAndQuery — yo'l + so'roq qatori (escape'langan holda).
func pathAndQuery(u *url.URL) string {
	p := u.EscapedPath()
	if p == "" {
		p = "/"
	}
	if u.RawQuery != "" {
		p += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		p += "#" + u.Fragment
	}
	return p
}
