package rtspclient

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// authParams — Authorization sarlavhasidagi k=v juftliklari (test orakoli).
func authParams(h string) (map[string]string, error) {
	i := strings.IndexByte(h, ' ')
	if i < 0 {
		return nil, fmt.Errorf("bo'sh Authorization: %q", h)
	}
	ch, err := parseChallenge(h[:i] + " " + h[i+1:])
	if err != nil {
		return nil, err
	}
	return ch.params, nil
}

func TestDigestAuthFlow(t *testing.T) {
	const user, pass = "admin", "Dahua123!"
	addr := fakeServer(t, func(s *errSink, br *bufio.Reader, w net.Conn) {
		req, err := ParseRequest(br)
		if err != nil {
			return
		}
		if req.Headers["authorization"] != "" {
			s.Errorf("birinchi so'rovda Authorization bo'lmasligi kerak")
		}
		writeRaw(s, w, "resp-401-digest.txt")

		req2, err := ParseRequest(br)
		if err != nil {
			return
		}
		auth := req2.Headers["authorization"]
		if !strings.HasPrefix(auth, "Digest ") {
			s.Fatalf("Authorization = %q, Digest kutilardi", auth)
		}
		p, err := authParams(auth)
		if err != nil {
			s.Fatalf("Authorization parse: %v", err)
		}
		// Server tomonidan kutilgan javobni mustaqil hisoblaymiz.
		nonce := "5f4dcc3b5aa765d61d832efdeb8f7b3a"
		ha1 := md5hex(user + ":Dahua:" + pass)
		ha2 := md5hex("GET_PARAMETER:" + req2.URL)
		want := md5hex(fmt.Sprintf("%s:%s:%s:%s:auth:%s", ha1, nonce, p["nc"], p["cnonce"], ha2))
		if p["response"] != want {
			s.Errorf("digest response = %q, kutilgan %q", p["response"], want)
		}
		if p["username"] != user {
			s.Errorf("username = %q", p["username"])
		}
		writeRaw(s, w, "resp-getparameter.txt")
	})
	c, err := Dial(context.Background(), rtspURL(addr, "", "/cam/realmonitor?channel=1&subtype=0"),
		Options{User: user, Pass: pass})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if err := c.GetParameter(context.Background()); err != nil {
		t.Fatalf("get_parameter auth'dan keyin: %v", err)
	}
}

func TestBasicAuthFlow(t *testing.T) {
	const user, pass = "viewer", "p@ss:word"
	addr := fakeServer(t, func(s *errSink, br *bufio.Reader, w net.Conn) {
		if _, err := ParseRequest(br); err != nil {
			return
		}
		writeRaw(s, w, "resp-401-basic.txt")
		req2, err := ParseRequest(br)
		if err != nil {
			return
		}
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
		if req2.Headers["authorization"] != want {
			s.Errorf("Authorization = %q, kutilgan %q", req2.Headers["authorization"], want)
		}
		writeRaw(s, w, "resp-getparameter.txt")
	})
	c, err := Dial(context.Background(), rtspURL(addr, "", "/x"), Options{User: user, Pass: pass})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if err := c.GetParameter(context.Background()); err != nil {
		t.Fatalf("get_parameter: %v", err)
	}
}

func TestKredsSiz401QiymatSizQaytadi(t *testing.T) {
	addr := fakeServer(t, func(s *errSink, br *bufio.Reader, w net.Conn) {
		if _, err := ParseRequest(br); err != nil {
			return
		}
		writeRaw(s, w, "resp-401-basic.txt")
	})
	c, err := Dial(context.Background(), rtspURL(addr, "", "/x"), Options{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	err = c.GetParameter(context.Background())
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 401 {
		t.Fatalf("StatusError 401 kutilardi, oldi: %v", err)
	}
}

func TestParseChallenge(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		scheme  string
		params  map[string]string
		wantErr bool
	}{
		{"digest_toliq", `Digest realm="Dahua", nonce="abc", stale=false, qop="auth", algorithm=MD5`,
			"digest", map[string]string{"realm": "Dahua", "nonce": "abc", "stale": "false", "qop": "auth", "algorithm": "MD5"}, false},
		{"basic_sodda", `Basic realm="Dahua"`, "basic", map[string]string{"realm": "Dahua"}, false},
		{"digest_qopsiz", `Digest realm="r", nonce="n"`, "digest", map[string]string{"realm": "r", "nonce": "n"}, false},
		{"qiymat_ichida_vergul", `Digest realm="a,b", nonce="n"`, "digest", map[string]string{"realm": "a,b", "nonce": "n"}, false},
		{"sxema_nomaalum", `Bearer realm="x"`, "", nil, true},
		{"bo_sh", ``, "", nil, true},
		{"param_yomon", `Digest realm`, "", nil, true},
		{"tirnoq_yopilmagan", `Digest realm="r, nonce="n"`, "", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch, err := parseChallenge(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatal("xato kutilardi")
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if ch.scheme != tc.scheme {
				t.Errorf("scheme = %q, xohlangan %q", ch.scheme, tc.scheme)
			}
			for k, v := range tc.params {
				if ch.params[k] != v {
					t.Errorf("params[%q] = %q, xohlangan %q", k, ch.params[k], v)
				}
			}
		})
	}
}

func TestDigestHisoblash(t *testing.T) {
	// RFC 2617 §3.2.2.1 namunasi (qop=auth, cnonce/nc bizning formatimiz).
	ch := challenge{scheme: "digest", params: map[string]string{
		"realm": "realm", "nonce": "dcd98b7102dd2f0e8b11d0f600bfb0c093", "qop": "auth",
	}}
	c := Conn{opts: Options{User: "Mufasa", Pass: "testdocs"}}
	hdr, err := c.authHeader(ch, "GET_PARAMETER", "/testdir/index.html")
	if err != nil {
		t.Fatalf("authHeader: %v", err)
	}
	p, err := authParams(hdr)
	if err != nil {
		t.Fatalf("authParams: %v", err)
	}
	ha1 := md5hex("Mufasa:realm:testdocs")
	ha2 := md5hex("GET_PARAMETER:/testdir/index.html")
	want := md5hex(ha1 + ":" + ch.params["nonce"] + ":" + p["nc"] + ":" + p["cnonce"] + ":auth:" + ha2)
	if p["response"] != want {
		t.Errorf("response = %q, kutilgan %q", p["response"], want)
	}
	if p["nc"] != "00000001" {
		t.Errorf("nc = %q", p["nc"])
	}
}

func TestDigestSHA256Rad(t *testing.T) {
	ch := challenge{scheme: "digest", params: map[string]string{
		"realm": "r", "nonce": "n", "algorithm": "SHA-256",
	}}
	c := Conn{opts: Options{User: "u", Pass: "p"}}
	if _, err := c.authHeader(ch, "OPTIONS", "/x"); err == nil {
		t.Fatal("SHA-256 rad etilishi kerak")
	}
}

// Hikvision digest URI'si: standart port tushiriladi (2026-11-20).
func TestNormalizeDigestURI(t *testing.T) {
	cases := map[string]string{
		"rtsp://192.168.0.24:554/Streaming/Channels/102": "rtsp://192.168.0.24/Streaming/Channels/102",
		"rtsps://cam.example:322/live":                   "rtsps://cam.example/live",
		"rtsp://192.168.0.24:8554/live":                  "", // port standart emas
		"rtsp://192.168.0.24/live":                       "", // port yo'q
	}
	for in, want := range cases {
		if got := normalizeDigestURI(in); got != want {
			t.Errorf("normalizeDigestURI(%q) = %q, kutilardi %q", in, got, want)
		}
	}
}
