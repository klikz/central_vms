package rtspclient

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// RTSP/1.0 xabar modellari (RFC 2326). Probe uchun yetarli qamrov:
// OPTIONS / GET_PARAMETER / DESCRIBE (docs/protocol.md §12).
// Interleaved RTP ma'lumot kanallari bu paketda YO'Q — pull C3da alohida.

const (
	maxLineLen = 8 * 1024   // bitta sarlavha satri uchun chegara
	maxBodyLen = 512 * 1024 // SDP body uchun yetarli, xotira himoyasi
)

// Headers — lowercase kalitli sarlavhalar to'plami.
type Headers map[string]string

// Request — RTSP so'rovi (probe yo'li uchun minimal to'plam).
type Request struct {
	Method  string // OPTIONS | GET_PARAMETER | DESCRIBE | ...
	URL     string // to'liq rtsp:// URL (so'rov satridagi URI)
	CSeq    int
	Headers Headers
	Body    []byte
}

// Response — RTSP javobi.
type Response struct {
	Status  int
	Reason  string
	CSeq    int
	Headers Headers
	Body    []byte
}

// Serialize — deterministik baytlar: birinchi satr, so'ng barcha sarlavhalar
// (CSeq ham) alifbo tartibida (golden testlar shu tartibga bog'langan).
func (r *Request) Serialize() []byte {
	var b strings.Builder
	b.WriteString(r.Method)
	b.WriteString(" ")
	b.WriteString(r.URL)
	b.WriteString(" RTSP/1.0\r\n")
	keys := make([]string, 0, len(r.Headers)+1)
	keys = append(keys, "cseq")
	for k := range r.Headers {
		if k == "cseq" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(canonicalHeader(k))
		b.WriteString(": ")
		if k == "cseq" {
			b.WriteString(strconv.Itoa(r.CSeq))
		} else {
			b.WriteString(r.Headers[k])
		}
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	out := []byte(b.String())
	if len(r.Body) > 0 {
		out = append(out, r.Body...)
	}
	return out
}

// canonicalHeader — lowercase kalitni odatdagi ko'rinishga qaytaradi.
var canonicalHeaderMap = map[string]string{
	"www-authenticate": "WWW-Authenticate",
	"content-base":     "Content-Base",
	"content-type":     "Content-Type",
	"cseq":             "CSeq",
}

func canonicalHeader(k string) string {
	if v, ok := canonicalHeaderMap[k]; ok {
		return v
	}
	parts := strings.Split(k, "-")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "-")
}

// ParseResponse — javobni o'qiydi. Tarmoq uzilganda io.EOF qaytaradi.
func ParseResponse(br *bufio.Reader) (*Response, error) {
	line, err := readLine(br)
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 || parts[0] != "RTSP/1.0" {
		return nil, fmt.Errorf("rtsp: yomon status satri: %q", line)
	}
	status, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("rtsp: status son emas: %q: %w", parts[1], err)
	}
	reason := ""
	if len(parts) == 3 {
		reason = parts[2]
	}
	headers, body, err := readHeadersBody(br)
	if err != nil {
		return nil, err
	}
	cseq, err := parseCSeq(headers)
	if err != nil {
		return nil, err
	}
	return &Response{Status: status, Reason: reason, CSeq: cseq, Headers: headers, Body: body}, nil
}

// ParseRequest — so'rovni o'qiydi (test sun'iy serveri uchun).
func ParseRequest(br *bufio.Reader) (*Request, error) {
	line, err := readLine(br)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(line, " ")
	if len(parts) != 3 || !strings.HasPrefix(parts[2], "RTSP/") {
		return nil, fmt.Errorf("rtsp: yomon so'rov satri: %q", line)
	}
	headers, body, err := readHeadersBody(br)
	if err != nil {
		return nil, err
	}
	cseq, err := parseCSeq(headers)
	if err != nil {
		return nil, err
	}
	return &Request{Method: parts[0], URL: parts[1], CSeq: cseq, Headers: headers, Body: body}, nil
}

func parseCSeq(headers Headers) (int, error) {
	v := headers["cseq"]
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, fmt.Errorf("rtsp: CSeq son emas: %q: %w", v, err)
	}
	return n, nil
}

func readHeadersBody(br *bufio.Reader) (Headers, []byte, error) {
	headers := Headers{}
	for {
		line, err := readLine(br)
		if err != nil {
			return nil, nil, err
		}
		if line == "" {
			break
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			return nil, nil, fmt.Errorf("rtsp: sarlavhada ikki nuqta yo'q: %q", line)
		}
		headers[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	var body []byte
	if cl := headers["content-length"]; cl != "" {
		n, err := strconv.Atoi(strings.TrimSpace(cl))
		if err != nil || n < 0 {
			return nil, nil, fmt.Errorf("rtsp: Content-Length yomon: %q", cl)
		}
		if n > maxBodyLen {
			return nil, nil, fmt.Errorf("rtsp: Content-Length juda katta: %d", n)
		}
		body = make([]byte, n)
		if _, err := io.ReadFull(br, body); err != nil {
			return nil, nil, fmt.Errorf("rtsp: body o'qilmadi: %w", err)
		}
	}
	return headers, body, nil
}

func readLine(br *bufio.Reader) (string, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		if err == io.EOF && line != "" {
			return "", fmt.Errorf("rtsp: satr tugallanmagan: %w", err)
		}
		return "", err
	}
	if len(line) > maxLineLen {
		return "", fmt.Errorf("rtsp: satr juda uzun (%d bayt)", len(line))
	}
	return strings.TrimRight(line, "\r\n"), nil
}
