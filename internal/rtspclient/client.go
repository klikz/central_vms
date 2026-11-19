package rtspclient

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Transport qismi: ulanish, so'rov/yavob almashinuvi, taymaut (ctx orqali).
// Har bir so'rovda CSeq o'sadi; ulanish qayta ishlatiladi.

const (
	defaultUserAgent = "vmsagent/1.0"
	defaultTimeout   = 5 * time.Second
	rtspDefaultPort  = "554"
)

// Options — mijoz sozlamalari (konfiguratsiyadan uzatiladi, global yo'q).
type Options struct {
	User      string // bo'sh bo'lsa URL userinfo'sidan olinadi
	Pass      string
	Timeout   time.Duration // har so'rov uchun; 0 bo'lsa defaultTimeout
	UserAgent string        // bo'sh bo'lsa defaultUserAgent
}

func (o Options) timeout() time.Duration {
	if o.Timeout > 0 {
		return o.Timeout
	}
	return defaultTimeout
}

func (o Options) userAgent() string {
	if o.UserAgent != "" {
		return o.UserAgent
	}
	return defaultUserAgent
}

// StatusError — server 200 bo'lmagan javob qaytarsa (C2.7 fallback mantiqi
// status kodlari bo'yicha qaror qiladi).
type StatusError struct {
	Method string
	Code   int
	Reason string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("rtsp: %s javob %d %s", e.Method, e.Code, e.Reason)
}

// Conn — bitta RTSP/TCP ulanishi. Bir vaqtda bitta goroutine ishlatadi
// (single-writer); parallel probe uchun har kamera o'z Conn'ini ochadi.
type Conn struct {
	conn    net.Conn
	br      *bufio.Reader
	uri     string // so'rov URI'si (kredsiz, to'liq rtsp://)
	opts    Options
	cseq    int
	session string // SETUP javobidagi Session (publish yo'li)
}

// Dial — TCP ulanish ochadi. URL'dagi kreds opts'ga ko'chiriladi,
// so'rov URI'siga tushmaydi (RFC 2326 §3.2, §10.2: parol hech qayerda
// ko'rinmaydi).
func Dial(ctx context.Context, rawURL string, opts Options) (*Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("rtsp: URL yaroqsiz: %w", err)
	}
	if u.Scheme != "rtsp" {
		return nil, fmt.Errorf("rtsp: sxema rtsp bo'lishi kerak, %q keldi", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("rtsp: URL'da host yo'q")
	}
	if opts.User == "" && u.User != nil {
		opts.User = u.User.Username()
		if p, ok := u.User.Password(); ok {
			opts.Pass = p
		}
	}
	u.User = nil // URI'dan kredsni olamiz
	if !hasPort(u.Host) {
		u.Host = net.JoinHostPort(u.Host, rtspDefaultPort)
	}
	d := net.Dialer{Timeout: opts.timeout()}
	host := u.Host
	c, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, fmt.Errorf("rtsp: ulanib bo'lmadi %s: %w", host, err)
	}
	return &Conn{conn: c, br: bufio.NewReader(c), uri: u.String(), opts: opts}, nil
}

func hasPort(hostPort string) bool {
	h, p, err := net.SplitHostPort(hostPort)
	_ = h
	return err == nil && p != ""
}

// Close — ulanishni yopadi.
func (c *Conn) Close() error { return c.conn.Close() }

// Do — so'rov yuboradi va javobni o'qiydi. ctx taymauti connection
// deadline'iga qo'llanadi. 401 kelsa va kreds bo'lsa bir marta
// (Basic/Digest) qayta urinish (auth.go).
func (c *Conn) Do(ctx context.Context, method string, headers Headers, body []byte) (*Response, error) {
	resp, err := c.doOnce(ctx, method, headers, body)
	if err != nil {
		return nil, err
	}
	if resp.Status == 401 && c.opts.User != "" && c.opts.Pass != "" {
		ch, perr := parseChallenge(resp.Headers["www-authenticate"])
		if perr != nil {
			return nil, perr
		}
		auth, aerr := c.authHeader(ch, method, c.uri)
		if aerr != nil {
			return nil, fmt.Errorf("rtsp: %s: auth: %w", method, aerr)
		}
		h2 := Headers{"authorization": auth}
		for k, v := range headers {
			if k != "authorization" {
				h2[k] = v
			}
		}
		return c.doOnce(ctx, method, h2, body)
	}
	return resp, nil
}

// doAt — doOnce, lekin boshqa URI bilan (SETUP trackID=... uchun).
func (c *Conn) doAt(ctx context.Context, method, uri string, headers Headers, body []byte) (*Response, error) {
	saved := c.uri
	c.uri = uri
	defer func() { c.uri = saved }()
	return c.doOnce(ctx, method, headers, body)
}

func (c *Conn) doOnce(ctx context.Context, method string, headers Headers, body []byte) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("rtsp: %s: %w", method, err)
	}
	c.cseq++
	h := Headers{}
	for k, v := range headers {
		if v == "" {
			continue // bo'sh sarlavha yuborilmaydi (gortsplib qat'iy: "Session:" xato)
		}
		h[k] = v
	}
	h["user-agent"] = c.opts.userAgent()
	if len(body) > 0 {
		h["content-length"] = strconv.Itoa(len(body))
	}
	req := &Request{Method: method, URL: c.uri, CSeq: c.cseq, Headers: h, Body: body}

	dl, ok := ctx.Deadline()
	if !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout())
		defer cancel()
		dl, _ = ctx.Deadline()
	}
	if err := c.conn.SetDeadline(dl); err != nil {
		return nil, fmt.Errorf("rtsp: %s: deadline: %w", method, err)
	}
	if _, err := c.conn.Write(req.Serialize()); err != nil {
		return nil, fmt.Errorf("rtsp: %s: yozib bo'lmadi: %w", method, err)
	}
	resp, err := ParseResponse(c.br)
	if err != nil {
		return nil, fmt.Errorf("rtsp: %s: javob o'qilmadi: %w", method, err)
	}
	return resp, nil
}

// ok yoki StatusError qaytaradi.
func check(method string, resp *Response) error {
	if resp.Status != 200 {
		return &StatusError{Method: method, Code: resp.Status, Reason: resp.Reason}
	}
	return nil
}

// Options — `Public` ro'yxatini qaytaradi (bo'sh bo'lsa nil).
func (c *Conn) Options(ctx context.Context) ([]string, error) {
	resp, err := c.Do(ctx, "OPTIONS", Headers{}, nil)
	if err != nil {
		return nil, err
	}
	if err := check("OPTIONS", resp); err != nil {
		return nil, err
	}
	var pub []string
	if v := resp.Headers["public"]; v != "" {
		for _, m := range strings.Split(v, ",") {
			if m = strings.TrimSpace(m); m != "" {
				pub = append(pub, m)
			}
		}
	}
	return pub, nil
}

// GetParameter — keepalive probe (arzon holat tekshiruvi, §12.1).
func (c *Conn) GetParameter(ctx context.Context) error {
	resp, err := c.Do(ctx, "GET_PARAMETER", Headers{}, nil)
	if err != nil {
		return err
	}
	return check("GET_PARAMETER", resp)
}

// Describe — SDP body qaytaradi (tahlil: internal/cams).
func (c *Conn) Describe(ctx context.Context) ([]byte, error) {
	resp, err := c.Do(ctx, "DESCRIBE", Headers{"accept": "application/sdp"}, nil)
	if err != nil {
		return nil, err
	}
	if err := check("DESCRIBE", resp); err != nil {
		return nil, err
	}
	return resp.Body, nil
}
