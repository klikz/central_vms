package discovery

import (
	"fmt"
	"strings"

	"central-vms/client/internal/cams"
)

// Ommaviy qo'shish oqimi (docs/protocol.md §16.3).
// Tanlangan discovery natijalaridan kamera yaratadi: UUID (camsvc ichida),
// parol keystore'ga, cams_epoch++ va delta cams_sync (camsvc.commit) — bular
// CameraCreator orqali amalga oshadi (haqiqiysi: *camsvc.Service).

// CameraCreator — kamera yaratish qobiliyati (camsvc.Service mos keladi).
// Bu interfeys discovery paketini camsvc'ga bog'lamaydi (test real servis
// bilan tekshiriladi, lekin paket bog'liqligi yo'q).
type CameraCreator interface {
	Create(c cams.Camera, secret string) (cams.Camera, error)
}

// BulkAddInput — bitta amaliyot kirishi (§16.3).
type BulkAddInput struct {
	Devices     []Device          // tanlangan (already_known chaqiruvchi oldindan chiqaradi)
	NamePattern string            // bo'sh → "{model}-{index:02d}"
	Password    string            // umumiy parol
	PerDevice   map[string]string // device kaliti → parol (umumiydan ustun)
	Enabled     bool              // default true bo'lishi uchun chaqiruvchi sozlaydi
}

// BulkAddFailure — bitta kameraning qo'shilmaslik sababi.
type BulkAddFailure struct {
	Device Device
	Err    string
}

// BulkAddReport — amaliyot natijasi (qisman muvaffaqiyat hisoboti).
type BulkAddReport struct {
	Added  []cams.Camera
	Failed []BulkAddFailure
}

// BulkAdd — har kamera uchun: UUID + parol keystore'ga + epoch++ (creator
// ichida). Xatolar davom etadi — bitta yomon kamera butun amaliyotni
// to'xtatmaydi (§16.3 qisman hisobot).
func BulkAdd(creator CameraCreator, in BulkAddInput) BulkAddReport {
	rep := BulkAddReport{Added: []cams.Camera{}}
	pattern := strings.TrimSpace(in.NamePattern)
	if pattern == "" {
		pattern = "{model}-{index:02d}"
	}
	for i, d := range in.Devices {
		index := i + 1
		name := expandName(pattern, d, index)
		cam := cams.Camera{
			Name:     name,
			Host:     d.IP,
			Port:     d.Port,
			MainPath: "/cam/realmonitor?channel=1&subtype=0",
			Enabled:  in.Enabled,
			Status:   cams.StatusUnknown,
		}
		secret := in.Password
		if pw, ok := in.PerDevice[DeviceKey(d)]; ok {
			secret = pw
		}
		created, err := creator.Create(cam, secret)
		if err != nil {
			rep.Failed = append(rep.Failed, BulkAddFailure{Device: d, Err: err.Error()})
			continue
		}
		rep.Added = append(rep.Added, created)
	}
	return rep
}

// expandName — {model}, {ip}, {mac}, {name}, {index}/{n} va {index:02d}
// kabi {…:format} tokenlarini ochadi (§16.3).
func expandName(pattern string, d Device, index int) string {
	// {key} va {key:fmt} tokenlarini topib almashtiramiz.
	var b strings.Builder
	for {
		open := strings.IndexByte(pattern, '{')
		if open < 0 {
			b.WriteString(pattern)
			break
		}
		b.WriteString(pattern[:open])
		rest := pattern[open+1:]
		close := strings.IndexByte(rest, '}')
		if close < 0 {
			b.WriteString("{")
			b.WriteString(rest)
			break
		}
		token := rest[:close]
		pattern = rest[close+1:]
		key, format := token, ""
		if colon := strings.IndexByte(token, ':'); colon >= 0 {
			key, format = token[:colon], token[colon+1:]
		}
		b.WriteString(tokenValue(key, format, d, index))
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return fmt.Sprintf("kamera-%d", index)
	}
	return out
}

func tokenValue(key, format string, d Device, index int) string {
	switch key {
	case "model":
		if d.Model != "" {
			return d.Model
		}
		return d.IP // model yo'q → IP ma'noliroq
	case "name":
		if d.Name != "" {
			return d.Name
		}
		return d.IP
	case "ip":
		return d.IP
	case "mac":
		return d.MAC
	case "index", "n":
		return padInt(index, format)
	default:
		return "" // noma'lum token → bo'sh
	}
}

// padInt — "02d" kabi kenglik formatini qo'llaydi (bo'sh → oddiy son).
func padInt(n int, format string) string {
	s := itoa(n)
	// formatdan raqamli kenglikni olamiz (masalan "02d" → 2, "03" → 3).
	width := 0
	for _, r := range format {
		if r >= '0' && r <= '9' {
			width = width*10 + int(r-'0')
		}
	}
	if width > 0 && len(s) < width {
		s = strings.Repeat("0", width-len(s)) + s
	}
	return s
}

// itoa — kichik butun sonni satrga aylantiradi (manfiy emas).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
