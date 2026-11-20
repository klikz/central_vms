package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SetValues — konfiguratsiya faylida YUQORI DARAJALI kalitlarning qiymatini
// yangilaydi (lokal UI "Save" shu orqali saqlaydi).
//
// Nega qatorma-qator almashtirish va to'liq qayta yozmaslik: fayl izohlar
// (operator uchun muhim) va tartibini saqlab qolishi kerak — biz uni qayta
// serialize qilsak izohlar yo'qoladi.
//
// Qoidalar:
//   - kalit faylda bo'lsa: qiymat almashtiriladi, qator oxiridagi izoh
//     (`# ...`) saqlanadi;
//   - kalit bo'lmasa: fayl oxiriga qo'shiladi (yuqori darajali kalit —
//     oldingi blok indentatsiyasi 0 ga qaytadi);
//   - yozish atomik: vaqtincha fayl + rename (jarayon o'rtada o'lsa ham
//     eski fayl buzilmaydi).
func SetValues(path string, updates map[string]string) error {
	if len(updates) == 0 {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config o'qish: %w", err)
	}
	lines := strings.Split(string(raw), "\n")
	// Kalit nomlari xavfsiz: faqat [a-z_] (boshqa belgilar YAML'da
	// buzilishga olib keladi).
	for k := range updates {
		if !validKey(k) {
			return fmt.Errorf("config: kalit nomini yozib bo'lmaydi: %q", k)
		}
	}
	done := map[string]bool{}
	for i, ln := range lines {
		k, rest, ok := splitTopLevelKey(ln)
		if !ok {
			continue
		}
		v, want := updates[k]
		if !want || done[k] {
			continue
		}
		lines[i] = k + ": " + v + commentOf(rest)
		done[k] = true
	}
	missing := make([]string, 0, len(updates))
	for k := range updates {
		if !done[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		// Tartibli qo'shish (map tartibi tasodifiy bo'lmasligi uchun saralash).
		sortStrings(missing)
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		for _, k := range missing {
			lines = append(lines, k+": "+updates[k])
		}
		lines = append(lines, "")
	}
	out := strings.Join(lines, "\n")

	mode := os.FileMode(0o640)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp := path + ".tmp-new"
	if err := os.WriteFile(tmp, []byte(out), mode); err != nil {
		return fmt.Errorf("config yozish: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("config almashtirish: %w", err)
	}
	return nil
}

// splitTopLevelKey — "server_url: ws://x # izoh" → ("server_url", " ws://x # izoh", true).
// Indentatsiyali (blok ichidagi) satrlar qaytarilmaydi: biz faqat yuqori
// darajali kalitlarni tegamiz.
func splitTopLevelKey(ln string) (key, rest string, ok bool) {
	if ln == "" || (ln[0] == ' ' || ln[0] == '\t') || ln[0] == '#' {
		return "", "", false
	}
	i := strings.Index(ln, ":")
	if i <= 0 {
		return "", "", false
	}
	k := ln[:i]
	if !validKey(k) {
		return "", "", false
	}
	return k, ln[i+1:], true
}

func validKey(k string) bool {
	if k == "" {
		return false
	}
	for _, r := range k {
		if !(r >= 'a' && r <= 'z') && r != '_' {
			return false
		}
	}
	return true
}

// commentOf — qiymat qismidagi izohni qaytaradi (qo'shtirnoq ichidagi `#`
// izoh emas).
func commentOf(rest string) string {
	inQ := byte(0)
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if inQ != 0 {
			if c == inQ {
				inQ = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQ = c
			continue
		}
		if c == '#' && (i == 0 || rest[i-1] == ' ' || rest[i-1] == '\t') {
			return " " + strings.TrimRight(rest[i:], " ")
		}
	}
	return ""
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// DirOf — fayl yo'lining papkasi (xato xabarlari uchun yordamchi).
func DirOf(path string) string { return filepath.Dir(path) }
