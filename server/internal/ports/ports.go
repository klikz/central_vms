// Package ports — ishga tushirish oldidan port tekshiruvi (REJA §2.1/1,
// TASKS S0). Dev mashinada tashqi MediaMTX (premier-cv) 8554/8888/8889 TCP
// va 8189/9997 UDP portlarini ushlaydi — VMS ularni egallab bo'lmasligini
// oldindan ANIQ xato bilan bildiradi; tashqi jarayonni o'zi to'xtatmaydi.
package ports

import (
	"fmt"
	"net"
	"strings"
)

// Conflict — band port haqida aniq ma'lumot.
type Conflict struct {
	Proto string // tcp | udp
	Port  int
	Owner string // taxminiy izoh (bo'sh bo'lsa ham xato baribir chiqadi)
}

func (c Conflict) Error() string {
	own := ""
	if c.Owner != "" {
		own = " — " + c.Owner
	}
	return fmt.Sprintf("port band: %s/%d%s", c.Proto, c.Port, own)
}

// Check — ro'yxatdagi portlarning bo'shligini tekshiradi.
// TCP: 127.0.0.1:port ga listen (bind muvaffaqiyatsiz → band).
// UDP: net.ListenUDP orqali (shu tizimda UDP bindi TCP'dan mustaqil —
// tashqi MediaMTX UDP 8189'ni ushlab turgan holat shu yo'l bilan aniqlanadi).
// Qaytariladigan xatoda BARCHA konfliktlar sanaladi.
func Check(tcpPorts, udpPorts []int) error {
	var bad []string
	for _, p := range tcpPorts {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			bad = append(bad, Conflict{Proto: "tcp", Port: p, Owner: hint(p)}.Error())
			continue
		}
		_ = ln.Close()
	}
	for _, p := range udpPorts {
		uc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: p})
		if err != nil {
			bad = append(bad, Conflict{Proto: "udp", Port: p, Owner: hint(p)}.Error())
			continue
		}
		_ = uc.Close()
	}
	if len(bad) > 0 {
		return fmt.Errorf("port tekshiruvi: %s — yechim: tashqi MediaMTX xizmatini to'xtating "+
			"(masalan: sudo systemctl stop premier-cv-mediamtx)", strings.Join(bad, "; "))
	}
	return nil
}

// hint — tanish xizmatlar uchun qisqa maslahat (best-effort, hal qiluvchi emas).
func hint(p int) string {
	switch p {
	case 8554, 8888, 8889, 9997:
		return "MediaMTX ishlayapti (RTSP/HLS/WebRTC/API)"
	case 8189:
		return "MediaMTX WebRTC ICE yoki vmsd auth-hook"
	case 8081:
		return "vmsd API porti band"
	default:
		return ""
	}
}
