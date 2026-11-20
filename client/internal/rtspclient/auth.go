package rtspclient

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// 401 javobda Basic va Digest (RFC 2617, MD5, qop=auth) avtorizatsiyasi.
// Dahua firmwarelarida odatda Digest; eski modellarda Basic uchraydi.

type challenge struct {
	scheme string            // "basic" | "digest"
	params map[string]string // lowercase kalitlar
}

// parseChallenge — WWW-Authenticate / Authorization qiymatini ajratadi.
// Qiymatlar tirnoqli bo'lishi va ichida vergul bo'lishi mumkin.
func parseChallenge(v string) (challenge, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return challenge{}, fmt.Errorf("rtsp: auth challenge bo'sh")
	}
	scheme, rest, ok := strings.Cut(v, " ")
	if !ok {
		rest = ""
	}
	ch := challenge{scheme: strings.ToLower(scheme), params: map[string]string{}}
	switch ch.scheme {
	case "basic", "digest":
	default:
		return ch, fmt.Errorf("rtsp: qo'llab-quvvatlanmaydigan auth sxemasi: %q", scheme)
	}
	rest = strings.TrimSpace(rest)
	for rest != "" {
		eq := strings.IndexByte(rest, '=')
		if eq < 0 {
			return ch, fmt.Errorf("rtsp: auth parametri yomon: %q", rest)
		}
		key := strings.ToLower(strings.TrimSpace(rest[:eq]))
		rest = strings.TrimSpace(rest[eq+1:])
		var val string
		if strings.HasPrefix(rest, `"`) {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				return ch, fmt.Errorf("rtsp: tirnoq yopilmagan: %q", rest)
			}
			val = rest[1 : 1+end]
			rest = rest[end+2:] // yopish tirnoqdan keyin
			if comma := strings.IndexByte(rest, ','); comma == 0 {
				rest = strings.TrimSpace(rest[1:])
			} else if rest != "" {
				return ch, fmt.Errorf("rtsp: auth parametri yomon: %q", rest)
			}
		} else {
			comma := strings.IndexByte(rest, ',')
			if comma < 0 {
				val, rest = rest, ""
			} else {
				val, rest = rest[:comma], strings.TrimSpace(rest[comma+1:])
			}
		}
		ch.params[key] = val
	}
	return ch, nil
}

// authHeader — challenge asosida Authorization qiymati.
func (c *Conn) authHeader(ch challenge, method, uri string) (string, error) {
	if c.opts.User == "" {
		return "", fmt.Errorf("rtsp: foydalanuvchi yo'q, auth talab qilindi")
	}
	if ch.scheme == "basic" {
		return "Basic " + base64.StdEncoding.EncodeToString(
			[]byte(c.opts.User+":"+c.opts.Pass)), nil
	}
	realm, nonce := ch.params["realm"], ch.params["nonce"]
	if realm == "" || nonce == "" {
		return "", fmt.Errorf("rtsp: digest challenge'da realm/nonce yo'q")
	}
	if alg := ch.params["algorithm"]; alg != "" && !strings.EqualFold(alg, "MD5") {
		return "", fmt.Errorf("rtsp: digest algoritmi qo'llab-quvvatlanmaydi: %q", alg)
	}
	qop := ""
	if v := ch.params["qop"]; v != "" {
		found := false
		for _, q := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(q), "auth") {
				found = true
			}
		}
		if !found {
			return "", fmt.Errorf("rtsp: digest qop qo'llab-quvvatlanmaydi: %q", v)
		}
		qop = "auth"
	}
	sum := md5.Sum([]byte(c.opts.User + ":" + realm + ":" + c.opts.Pass))
	ha1 := hex.EncodeToString(sum[:])
	sum = md5.Sum([]byte(method + ":" + uri))
	ha2 := hex.EncodeToString(sum[:])
	nc := "00000001"
	cnonce := randomHex()
	respIn := ha1 + ":" + nonce + ":" + ha2
	if qop != "" {
		respIn = ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2
	}
	sum = md5.Sum([]byte(respIn))
	fields := []string{
		`username="` + quoteAuth(c.opts.User) + `"`,
		`realm="` + quoteAuth(realm) + `"`,
		`nonce="` + quoteAuth(nonce) + `"`,
		`uri="` + quoteAuth(uri) + `"`,
		`response="` + hex.EncodeToString(sum[:]) + `"`,
		"algorithm=MD5",
	}
	if qop != "" {
		fields = append(fields, `qop=auth`, "nc="+nc, `cnonce="`+cnonce+`"`)
	}
	return "Digest " + strings.Join(fields, ", "), nil
}

// normalizeDigestURI — Hikvision kabi kameralar digest HA2'ni RTSP
// standart portini tushirib qoldirib hisoblaydi (2026-11-20:
// rtsp://192.168.0.24:554/... bilan 401, rtsp://192.168.0.24/... bilan
// 200 qaytardi). Port standart bo'lmasa bo'sh qaytadi (urinish shart emas).
func normalizeDigestURI(u string) string {
	p, err := url.Parse(u)
	if err != nil || p.Port() == "" {
		return ""
	}
	def := map[string]string{"rtsp": "554", "rtsps": "322"}[strings.ToLower(p.Scheme)]
	if p.Port() != def {
		return ""
	}
	p.Host = p.Hostname()
	return p.String()
}

func quoteAuth(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

func randomHex() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil { // PRNG uzilishi fallback: nol — xavfsizlik xatosi emas, nonce sifatida
		return "0000000000000000"
	}
	return hex.EncodeToString(b[:])
}
