// Package media — MediaMTX boshqaruvi: control API klienti (S2.2),
// token mint + auth-hook (S2.3), sessiya holat mashinasi (S2.4),
// transcode manager (S2.7). Kontrakt: docs/protocol.md §7.1/§15.
package media

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Client — MediaMTX v1.11.3 HTTP API (apiAddress 127.0.0.1:9997).
// Auth: authHTTPExclude api → localhost uchun ichki ruxsat (parol talab emas);
// baribir `vmsapi` basic auth yuboriladi (prod'da majburiy bo'lib qolsa ham
// ishlashi uchun).
type Client struct {
	base string
	hc   *http.Client
	user string
	pass string

	// Pattern'lar ro'yxati keshi (har 30 sda yangilanadi).
	pmu      sync.Mutex
	patNames []string
	patAt    time.Time
}

func NewClient(baseURL string) *Client {
	return &Client{
		base: baseURL,
		// polling ~2 s oraliqda ishlatiladi — timeout qisqa (S2 idle stop).
		hc:   &http.Client{Timeout: 3 * time.Second},
		user: "vmsapi",
		pass: "vmsapi-dev-only",
	}
}

// PathConf — bizning path rejimimiz (source: publisher — faqat agent publish qiladi).
type PathConf struct {
	Source   string `json:"source"`
	Record   bool   `json:"record"`
	ReadOnly bool   `json:"readOnly"`
}

// EnsurePath — path konfiguratsiyasini qo'yadi (PUT — idempotent).
func (c *Client) EnsurePath(name string, conf PathConf) error {
	if conf.Source == "" {
		conf.Source = "publisher"
	}
	body, _ := json.Marshal(conf)
	return c.do(http.MethodPut, "/v3/config/paths/put/"+name, body, nil)
}

// DeletePath — path'ni o'chiradi (404 → xato emas: idempotent).
// PathConfigured — MediaMTX sozlamasida bu yo'l allaqachon mavjudmi
// (maxsus nom yoki `~^...` regex pattern orqali).
//
// v1.11.3'da `/` saqlagan nomlar bilan `PUT /v3/config/paths/put/{name}`
// ROUTE'ga mos kelmaydi ("404 page not found") — ya'ni bizning
// `cam/<agent>/<uuid>` va `tc/...` yo'llarini dinamik qo'shib bo'lmaydi.
// Ularni `mediamtx.yml`'dagi `~^cam/.+` / `~^tc/.+` patternlari qoplaydi,
// shuning uchun EnsurePath ularga umuman kerak emas (2026-11-21: har
// preview'da log'da 404 ko'rinardi).
func (c *Client) PathConfigured(name string) (bool, error) {
	c.pmu.Lock()
	defer c.pmu.Unlock()
	if len(c.patNames) == 0 || time.Since(c.patAt) > 30*time.Second {
		var out struct {
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
		}
		if err := c.do(http.MethodGet, "/v3/config/paths/list", nil, &out); err != nil {
			return false, err
		}
		c.patNames = make([]string, 0, len(out.Items))
		for _, it := range out.Items {
			c.patNames = append(c.patNames, it.Name)
		}
		c.patAt = time.Now()
	}
	for _, n := range c.patNames {
		if n == name {
			return true, nil
		}
		if strings.HasPrefix(n, "~") {
			re, err := regexp.Compile(strings.TrimPrefix(n, "~"))
			if err == nil && re.MatchString(name) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (c *Client) DeletePath(name string) error {
	err := c.do(http.MethodDelete, "/v3/config/paths/delete/"+name, nil, nil)
	if err != nil && isMTXNotFound(err) {
		return nil
	}
	return err
}

// Readers — path'dagi faol o'quvchilar soni (idle stop shu orqali: readers=0
// → 10 s keyin upload to'xtatish; root §16.2). Xato bo'lsa -1 (poller loglaydi).
func (c *Client) Readers(path string) (int, error) {
	// v1.11.3 tekshirildi: /v3/paths/get/{name} — YAKKA obyekt va `readers`
	// — OBYEKTLAR RO'YXATI (id/remoteAddr). Son ko'rinishi ham qabul
	// qilinadi (moslashuvchanlik).
	var out struct {
		ReadersAny json.RawMessage `json:"readers"`
		Items      []struct {
			Name       string          `json:"name"`
			ReadersAny json.RawMessage `json:"readers"`
		} `json:"items"`
	}
	if err := c.do(http.MethodGet, "/v3/paths/get/"+path, nil, &out); err != nil {
		// Path yo'q (404) — o'quvchi ham yo'q: idle stop uchun 0 deb olamiz.
		if isMTXNotFound(err) {
			return 0, nil
		}
		return -1, err
	}
	raw := out.ReadersAny
	if len(out.Items) > 0 {
		raw = out.Items[0].ReadersAny
	}
	return countReaders(raw)
}

// URLs — WHEP/RTSP asos manzillari (portlar config'dan; §7.1 URL namunasi).
func (c *Client) URLs(host string, whepPort, rtspPort int) (whepBase, rtspBase string) {
	return fmt.Sprintf("http://%s:%d", host, whepPort), fmt.Sprintf("rtsp://%s:%d", host, rtspPort)
}

// Alive — MediaMTX jonlimi (healthz/ready; S2.4 watchdog shu orqali
// media_offline holatini aniqlaydi).
func (c *Client) Alive() bool {
	err := c.do(http.MethodGet, "/v3/config/global/get", nil, nil)
	return err == nil
}

func (c *Client) do(method, path string, body []byte, out any) error {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.user, c.pass)
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("mediamtx api %s: %w", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return &mtxError{code: 404, body: string(b)}
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("mediamtx api %s: %d: %s", path, resp.StatusCode, string(b))
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("mediamtx api %s: parse: %w", path, err)
		}
	}
	return nil
}

// countReaders — readers maydonini son yoki ro'yxat ko'rinishida o'qiydi.
func countReaders(raw json.RawMessage) (int, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		return len(arr), nil
	}
	return -1, fmt.Errorf("readers maydoni kutilmagan ko'rinishda: %s", raw)
}

type mtxError struct {
	code int
	body string
}

func (e *mtxError) Error() string { return fmt.Sprintf("mediamtx 404: %s", e.body) }

func isMTXNotFound(err error) bool {
	var me *mtxError
	return errors.As(err, &me)
}
