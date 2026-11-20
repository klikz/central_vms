package cams

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ONVIF mijoz (ixtiyoriy; docs/protocol.md §16.1, §12, C2.12).
// GetProfiles / GetStreamUri / SetVideoEncoderConfiguration — sub profilni
// 720p/10fps/700kbps CBR/GOP 2s/B-frame'siz qilish uchun (C2.13 codec_fix).
// Dahua ONVIF ko'pincha HTTP Digest yoki WS-Security UsernameToken ishlatadi —
// bu yerda WS-Security PasswordDigest (SHA1) qo'llanadi.

// OnvifClient — bitta kameraning ONVIF device_service manzili.
type OnvifClient struct {
	XAddr string
	User  string
	Pass  string
	HC    *http.Client
}

// NewOnvifClient — HC nil → 10 s taymautli standart client.
func NewOnvifClient(xaddr, user, pass string) *OnvifClient {
	return &OnvifClient{XAddr: xaddr, User: user, Pass: pass,
		HC: &http.Client{Timeout: 10 * time.Second}}
}

// Profile — ONVIF media profili (token + encoder konfiguratsiya tokeni).
type Profile struct {
	Token             string
	Name              string
	VideoEncoderToken string
}

// EncoderParams — SetVideoEncoderConfiguration parametrlari (§16.1 maqsad).
type EncoderParams struct {
	Encoding     string // H264
	Width        int
	Height       int
	FPS          int
	BitrateKbps  int    // CBR limit
	RateControl  string // CBR
	GovLengthSec int    // GOP uzunligi (sekund)
}

const (
	nsSOAP = "http://www.w3.org/2003/05/soap-envelope"
	nsONV  = "http://www.onvif.org/ver10/network/wsdl"
	nsWSU  = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"
	nsWSSE = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
	nsTRT  = "http://www.onvif.org/ver10/media/wsdl"
	nsTT   = "http://www.onvif.org/ver10/schema"
)

// GetProfiles — barcha media profillari.
func (c *OnvifClient) GetProfiles(ctx context.Context) ([]Profile, error) {
	body := `<trt:GetProfiles xmlns:trt="` + nsTRT + `"/>`
	raw, err := c.do(ctx, body)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Body struct {
			GetProfilesResponse struct {
				Profiles []struct {
					Token string `xml:"token,attr"`
					Name  struct {
						Value string `xml:",chardata"`
					} `xml:"Name"`
					VEC struct {
						Token string `xml:"token,attr"`
					} `xml:"VideoEncoderConfiguration"`
				} `xml:"Profiles"`
			} `xml:"GetProfilesResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("onvif: GetProfiles parse: %w", err)
	}
	out := make([]Profile, 0, len(resp.Body.GetProfilesResponse.Profiles))
	for _, p := range resp.Body.GetProfilesResponse.Profiles {
		out = append(out, Profile{Token: p.Token, Name: strings.TrimSpace(p.Name.Value),
			VideoEncoderToken: p.VEC.Token})
	}
	return out, nil
}

// GetStreamUri — profil uchun RTSP URI.
func (c *OnvifClient) GetStreamUri(ctx context.Context, profileToken string) (string, error) {
	body := `<trt:GetStreamUri xmlns:trt="` + nsTRT + `" xmlns:tt="` + nsTT + `">` +
		`<trt:StreamSetup><tt:Stream>RTP-RTSP</tt:Stream><tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport></trt:StreamSetup>` +
		`<trt:ProfileToken>` + xmlEscape(profileToken) + `</trt:ProfileToken></trt:GetStreamUri>`
	raw, err := c.do(ctx, body)
	if err != nil {
		return "", err
	}
	var resp struct {
		Body struct {
			GetStreamUriResponse struct {
				MediaUri struct {
					URI string `xml:"Uri"`
				} `xml:"MediaUri"`
			} `xml:"GetStreamUriResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("onvif: GetStreamUri parse: %w", err)
	}
	return strings.TrimSpace(resp.Body.GetStreamUriResponse.MediaUri.URI), nil
}

// SetVideoEncoderConfiguration — sub profilni sozlash (C2.13).
func (c *OnvifClient) SetVideoEncoderConfiguration(ctx context.Context, configToken string, p EncoderParams) error {
	gov := p.GovLengthSec * p.FPS
	if gov < 1 {
		gov = 20
	}
	body := `<trt:SetVideoEncoderConfiguration xmlns:trt="` + nsTRT + `" xmlns:tt="` + nsTT + `">` +
		`<trt:Configuration token="` + xmlEscape(configToken) + `">` +
		`<tt:Encoding>` + xmlEscape(p.Encoding) + `</tt:Encoding>` +
		`<tt:Resolution><tt:Width>` + itoaI(p.Width) + `</tt:Width><tt:Height>` + itoaI(p.Height) + `</tt:Height></tt:Resolution>` +
		`<tt:RateControl><tt:BitrateLimit>` + itoaI(p.BitrateKbps) + `</tt:BitrateLimit>` +
		`<tt:FrameRate>` + itoaI(p.FPS) + `</tt:FrameRate><tt:ConstantBitrate>true</tt:ConstantBitrate></tt:RateControl>` +
		`<tt:H264><tt:GovLength>` + itoaI(gov) + `</tt:GovLength>` +
		`<tt:MultiThreaded>true</tt:MultiThreaded></tt:H264>` +
		`</trt:Configuration></trt:SetVideoEncoderConfiguration>`
	_, err := c.do(ctx, body)
	return err
}

// do — SOAP konvertini POST qiladi; Fault bo'lsa xato, bo'lmasa xom Body.
func (c *OnvifClient) do(ctx context.Context, bodyXML string) ([]byte, error) {
	reqXML := c.envelope(bodyXML)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.XAddr, strings.NewReader(reqXML))
	if err != nil {
		return nil, fmt.Errorf("onvif: so'rov: %w", err)
	}
	req.Header.Set("Content-Type", `application/soap+xml; charset=utf-8`)
	resp, err := c.HC.Do(req)
	if err != nil {
		return nil, fmt.Errorf("onvif: transport: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("onvif: javob o'qilmadi: %w", err)
	}
	if msg := faultText(raw); msg != "" {
		return nil, fmt.Errorf("onvif: SOAP Fault: %s", msg)
	}
	return raw, nil
}

// envelope — WS-Security UsernameToken (PasswordDigest) bilan SOAP konverti.
func (c *OnvifClient) envelope(bodyXML string) string {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil { // PRNG uzilishi fallback: nol nonce
		nonce = make([]byte, 16)
	}
	created := time.Now().UTC().Format("2001-01-02T15:04:05Z")
	digest := passwordDigest(nonce, created, c.Pass)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<s:Envelope xmlns:s="` + nsSOAP + `" xmlns:trt="` + nsTRT + `" xmlns:tt="` + nsTT + `">`)
	b.WriteString(`<s:Header><wsse:Security xmlns:wsse="` + nsWSSE + `" xmlns:wsu="` + nsWSU + `" s:mustUnderstand="1">`)
	b.WriteString(`<wsse:UsernameToken><wsse:Username>` + xmlEscape(c.User) + `</wsse:Username>`)
	b.WriteString(`<wsse:Password Type="` + nsWSSE + `#PasswordDigest">` + digest + `</wsse:Password>`)
	b.WriteString(`<wsse:Nonce EncodingType="` + nsWSSE + `#Base64Binary">` +
		base64.StdEncoding.EncodeToString(nonce) + `</wsse:Nonce>`)
	b.WriteString(`<wsu:Created>` + created + `</wsu:Created></wsse:UsernameToken></wsse:Security></s:Header>`)
	b.WriteString(`<s:Body>` + bodyXML + `</s:Body></s:Envelope>`)
	return b.String()
}

// passwordDigest = base64(SHA1(nonce_raw + created + password)).
func passwordDigest(nonce []byte, created, pass string) string {
	h := sha1.New()
	h.Write(nonce)
	h.Write([]byte(created))
	h.Write([]byte(pass))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// faultText — SOAP Fault Reason/Text ni topadi (bo'lmasa "").
func faultText(raw []byte) string {
	if !strings.Contains(string(raw), "Fault") {
		return ""
	}
	var f struct {
		Body struct {
			Fault struct {
				Code struct {
					Value   string `xml:"Value"`
					Subcode struct {
						Value string `xml:"Value"`
					} `xml:"Subcode"`
				} `xml:"Code"`
				Reason struct {
					Texts []struct {
						Value string `xml:",chardata"`
					} `xml:"Text"`
				} `xml:"Reason"`
			} `xml:"Fault"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(raw, &f); err != nil {
		return "unparsable fault"
	}
	ft := f.Body.Fault
	if ft.Code.Value == "" && ft.Code.Subcode.Value == "" && len(ft.Reason.Texts) == 0 {
		return "" // Fault so'zi bor lekin tuzilma yo'q — xato emas deb o'tamiz
	}
	parts := []string{}
	if ft.Code.Subcode.Value != "" {
		parts = append(parts, ft.Code.Subcode.Value)
	}
	if ft.Code.Value != "" {
		parts = append(parts, ft.Code.Value)
	}
	for _, t := range ft.Reason.Texts {
		if s := strings.TrimSpace(t.Value); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " — ")
}

// xmlEscape — foydalanuvchi/qurilmadan kelgan qiymatlarni XML'ga xavfsiz joylash.
func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func itoaI(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	if neg {
		return "-" + string(d)
	}
	return string(d)
}
