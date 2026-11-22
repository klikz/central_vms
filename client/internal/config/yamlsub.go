package config

import (
	"fmt"
	"strconv"
	"strings"
)

// yamlsub — §11 sxemasiga yetarli bo'lgan qismiy YAML o'quvchi.
//
// NEGA to'liq YAML kutubxonasi emas: hozirgi muhitda tarmoq yopiq (modul yuklab
// bo'lmadi). Shu sababli **faqat kerakli qism** qo'llab-quvvatlanadi va har bir
// cheklov aniq xato bilan rad etiladi (jimina noto'g'ri o'qimaslik uchun).
// Tarmoq bo'lsa `gopkg.in/yaml.v3` ga almashtirish mumkin — Config va Load()
// interfeysi o'zgarmaydi.
//
// Qo'llab-quvvatlanadi:
//   * izoh (`#` — qo'shtirnoq tashqarisida)
//   * uyum (mapping) — 2 bo'shliq indentatsiya (ixtiyoriy, lekin izchil bo'lishi shart)
//   * kalit: qiymat — satr, butun son, boolean (true/false), null
//   * ro'yxatlar: inline `[a, b]` va blok `- a`
//
// Qo'llab-quvvatlanmaydi (xato qaytaradi): anchor/alias (*x, &x), flow mapping {..},
// ko'p qatorli bloklar (|, >), tab indentatsiya, takroriy kalit.

type yamlError struct {
	line int
	msg  string
}

func (e *yamlError) Error() string {
	return fmt.Sprintf("yaml: qator %d: %s", e.line, e.msg)
}

type parser struct {
	lines []string
	i     int
}

// parseYAML — matnni map[string]any ga aylantiradi (yuqori darajali uyum kutiladi).
func parseYAML(src string) (map[string]any, error) {
	lines := strings.Split(src, "\n")
	p := &parser{lines: lines}
	m, err := p.parseMapping(0)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// parseMapping — `indent` darajasidagi kalit:qiymat juftliklarini o'qiydi.
func (p *parser) parseMapping(indent int) (map[string]any, error) {
	out := map[string]any{}
	for p.i < len(p.lines) {
		raw := p.lines[p.i]
		num := p.i + 1

		trimmed := stripComment(strings.TrimRight(raw, " \t\r"))
		if strings.TrimSpace(trimmed) == "" {
			p.i++
			continue
		}
		if strings.HasPrefix(raw, "\t") {
			return nil, &yamlError{num, "tab indentatsiya qo'llab-quvvatlanmaydi — bo'shliq ishlatilsin"}
		}
		if strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			return nil, &yamlError{num, "ro'yxat elementlari faqat kalit ostida bo'ladi"}
		}

		cur := indentOf(raw)
		if cur < indent {
			break // ota darajaga qaytdik
		}
		if cur > indent {
			return nil, &yamlError{num, "noto'g'ri indentatsiya (2 bo'shliq qadam kerak)"}
		}

		key, val, err := splitKeyValue(strings.TrimSpace(trimmed))
		if err != nil {
			return nil, &yamlError{num, err.Error()}
		}
		if _, dup := out[key]; dup {
			return nil, &yamlError{num, fmt.Sprintf("takroriy kalit %q", key)}
		}
		p.i++

		if val != "" {
			v, err := parseScalarOrList(val, num)
			if err != nil {
				return nil, err
			}
			out[key] = v
			continue
		}

		// Qiymat bo'sh — ichma-ich uyum yoki blok ro'yxat bo'lishi mumkin.
		// Bo'sh va faqat-izohli qatorlar OLDIRAB o'tiladi (2026-11-22:
		// `limits:` dan keyin darhol izoh kelsa, butun blok jim `nil`
		// deb o'qilar, keyingi qator esa "noto'g'ri indentatsiya" deb
		// chiqarardi — foydalanuvchi izohi konfiguratsiyani buzardi).
		for p.i < len(p.lines) {
			cand := stripComment(strings.TrimRight(p.lines[p.i], " \t\r"))
			if strings.TrimSpace(cand) == "" {
				p.i++
				continue
			}
			break
		}
		if p.i >= len(p.lines) {
			out[key] = nil
			continue
		}
		next := p.lines[p.i]
		nextTrim := stripComment(strings.TrimRight(next, " \t\r"))
		nextIndent := indentOf(next)
		if nextIndent <= indent {
			out[key] = nil
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(nextTrim), "- ") || strings.TrimSpace(nextTrim) == "-" {
			list, err := p.parseList(nextIndent)
			if err != nil {
				return nil, err
			}
			out[key] = list
			continue
		}
		sub, err := p.parseMapping(nextIndent)
		if err != nil {
			return nil, err
		}
		out[key] = sub
	}
	return out, nil
}

// parseList — blok ro'yxat (`- a`) ni o'qiydi.
func (p *parser) parseList(indent int) ([]any, error) {
	out := []any{}
	for p.i < len(p.lines) {
		raw := p.lines[p.i]
		num := p.i + 1
		trimmed := stripComment(strings.TrimRight(raw, " \t\r"))
		if strings.TrimSpace(trimmed) == "" {
			p.i++
			continue
		}
		if strings.HasPrefix(raw, "\t") {
			return nil, &yamlError{num, "tab indentatsiya qo'llab-quvvatlanmaydi — bo'shliq ishlatilsin"}
		}
		if indentOf(raw) != indent {
			break
		}
		body := strings.TrimSpace(trimmed)
		if !strings.HasPrefix(body, "-") {
			break
		}
		item := strings.TrimSpace(strings.TrimPrefix(body, "-"))
		if item == "" {
			return nil, &yamlError{num, "bo'sh ro'yxat elementi"}
		}
		v, err := parseScalarOrList(item, num)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.i++
	}
	return out, nil
}

func splitKeyValue(s string) (key string, val string, err error) {
	if strings.ContainsAny(s, "&*|>") && !strings.Contains(s, ":") {
		return "", "", fmt.Errorf("qo'llab-quvvatlanmaydigan YAML sintaksisi: %q", s)
	}
	idx := indexColonOutsideQuotes(s)
	if idx < 0 {
		return "", "", fmt.Errorf("kalit:qiymat kutildi, topilmadi: %q", s)
	}
	key = strings.TrimSpace(unquote(s[:idx]))
	val = strings.TrimSpace(s[idx+1:])
	if key == "" {
		return "", "", fmt.Errorf("bo'sh kalit")
	}
	if strings.HasPrefix(val, "{") {
		return "", "", fmt.Errorf("flow mapping qo'llab-quvvatlanmaydi: %q", val)
	}
	if val == "|" || val == ">" || strings.HasPrefix(val, "|") || strings.HasPrefix(val, ">") {
		return "", "", fmt.Errorf("ko'p qatorli blok qo'llab-quvvatlanmaydi")
	}
	if strings.HasPrefix(val, "*") || strings.HasPrefix(val, "&") {
		return "", "", fmt.Errorf("anchor/alias qo'llab-quvvatlanmaydi")
	}
	return key, val, nil
}

func indexColonOutsideQuotes(s string) int {
	var q rune
	for i, r := range s {
		switch {
		case q != 0:
			if r == q {
				q = 0
			}
		case r == '"' || r == '\'':
			q = r
		case r == ':':
			if i+1 == len(s) || s[i+1] == ' ' {
				return i
			}
		}
	}
	return -1
}

func parseScalarOrList(v string, line int) (any, error) {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "[") {
		if !strings.HasSuffix(v, "]") {
			return nil, &yamlError{line, "tugallanmagan inline ro'yxat"}
		}
		inner := strings.TrimSpace(v[1 : len(v)-1])
		if inner == "" {
			return []any{}, nil
		}
		var out []any
		for _, part := range splitTopLevelCommas(inner) {
			s, err := parseScalar(strings.TrimSpace(part), line)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		return out, nil
	}
	return parseScalar(v, line)
}

func splitTopLevelCommas(s string) []string {
	var out []string
	var q rune
	start := 0
	for i, r := range s {
		switch {
		case q != 0:
			if r == q {
				q = 0
			}
		case r == '"' || r == '\'':
			q = r
		case r == ',':
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func parseScalar(v string, line int) (any, error) {
	switch {
	case v == "" || v == "~" || v == "null":
		return "", nil
	case strings.EqualFold(v, "true"):
		return true, nil
	case strings.EqualFold(v, "false"):
		return false, nil
	}
	if unquoted, ok := unquoteChecked(v); ok {
		return unquoted, nil
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n, nil
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f, nil
	}
	if strings.ContainsAny(v, "{}[]") {
		return nil, &yamlError{line, fmt.Sprintf("qo'llab-quvvatlanmaydigan qiymat: %q", v)}
	}
	return v, nil
}

func unquote(s string) string {
	if u, ok := unquoteChecked(s); ok {
		return u
	}
	return s
}

func unquoteChecked(s string) (string, bool) {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u, true
		}
		return s[1 : len(s)-1], true
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), true
	}
	return s, false
}

func stripComment(s string) string {
	var q rune
	for i, r := range s {
		switch {
		case q != 0:
			if r == q {
				q = 0
			}
		case r == '"' || r == '\'':
			q = r
		case r == '#':
			if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
				return s[:i]
			}
		}
	}
	return s
}

func indentOf(s string) int {
	if strings.HasPrefix(s, "\t") {
		return -1 // tab — xato belgisi
	}
	n := 0
	for _, r := range s {
		if r == ' ' {
			n++
			continue
		}
		break
	}
	return n
}
