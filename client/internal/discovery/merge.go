package discovery

import (
	"fmt"
	"strings"
)

// Discovery natijalarini birlashtirish (docs/protocol.md §16.2).
// Unikal kalit: MAC (yoki serial), bo'lmasa ip+port. Qayta topilgan kamera
// already_known hisoblanadi va mavjud id saqlanadi (layout/favorites buzilmaydi).

// rtspDefaultPort — ip+port kaliti uchun standart RTSP porti.
const rtspDefaultPort = 554

// Known — allaqachon qo'shilgan kamera (lokal UI ro'yxatidan).
type Known struct {
	ID     string
	MAC    string
	Serial string
	IP     string
	Port   int
}

// MergeResult — birlashtirilgan bitta natija.
type MergeResult struct {
	Device
	AlreadyKnown bool
	ExistingID   string // saqlangan id (faqat AlreadyKnown'da)
}

// keyOf — unikal kalit: MAC > serial > ip:port. Bo'sh bo'lsa "" (moslashmaydi).
func keyOf(mac, serial, ip string, port int) string {
	if m := strings.ToLower(strings.TrimSpace(mac)); m != "" {
		return "mac:" + m
	}
	if s := strings.TrimSpace(serial); s != "" {
		return "serial:" + s
	}
	if ip = strings.TrimSpace(ip); ip != "" {
		if port == 0 {
			port = rtspDefaultPort
		}
		return fmt.Sprintf("ip:%s:%d", ip, port)
	}
	return ""
}

// DeviceKey — topilgan kameraning kaliti.
func DeviceKey(d Device) string { return keyOf(d.MAC, d.Serial, d.IP, d.Port) }

// KnownKey — mavjud kameraning kaliti.
func KnownKey(k Known) string { return keyOf(k.MAC, k.Serial, k.IP, k.Port) }

// Merge — skan natijalarini mavjud ro'yxat bilan birlashtiradi.
// Tartib saqlanadi; skan ichidagi takrorlar o'chiriladi; kalit bo'sh
// natijalar (hech qanday identifikatori yo'q) tashlab yuboriladi.
func Merge(scan []Device, known []Known) []MergeResult {
	// Mavjud kameralarni kalit bo'yicha indekslaymiz.
	index := make(map[string]string, len(known))
	for _, k := range known {
		if key := KnownKey(k); key != "" {
			index[key] = k.ID
		}
	}
	seen := make(map[string]bool, len(scan))
	out := make([]MergeResult, 0, len(scan))
	for _, d := range scan {
		key := DeviceKey(d)
		if key == "" {
			continue // identifikatorsiz natija — ehtiyotkorlik bilan tashlaymiz
		}
		if seen[key] {
			continue // skan ichida takror
		}
		seen[key] = true
		mr := MergeResult{Device: d}
		if id, ok := index[key]; ok {
			mr.AlreadyKnown = true
			mr.ExistingID = id
		}
		out = append(out, mr)
	}
	return out
}
