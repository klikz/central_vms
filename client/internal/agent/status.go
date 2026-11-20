package agent

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"central-vms/client/internal/clock"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/redact"
)

// cam_status / codec_probe yuborish (docs/protocol.md §4.4, §4.5).
//
// §4.4: hodisa-driven — holat o'zgarganda yuboriladi; uzluksiz xato
// (offline/error) holatida server muddatidan oldin "eski" deb belgilamasligi
// uchun 5 daqiqada bir takrorlanadi. online holati takrorlanmaydi.
// §4.5: codec_probe natijasi o'zgarganda yuboriladi (SDP tahlili).

// errRepeatWindow — uzluksiz xato holatining takrorlash oralig'i (§4.4).
const errRepeatWindow = 5 * time.Minute

// statusKey — takrorlash uchun solishtiriladigan imzo (holat + xato).
type statusKey struct {
	status string
	err    string
}

// StatusReporter — holat/codec xabarlarini sessiya orqali yuboradi va
// keraksiz takrorlanishni siqadi. Bitta yozuvchi (single-writer) emas —
// sessiya Send'ida lock bor; bu yerda faqat dedup-holat muhofaza qilinadi.
type StatusReporter struct {
	sess SyncSender
	clk  clock.Clock
	log  *slog.Logger

	mu      sync.Mutex
	last    map[string]statusKey // camera_id → oxirgi YUBORILGAN imzo
	lastErr map[string]time.Time // camera_id → oxirgi xato-holat yuborilgan vaqt
}

// NewStatusReporter — clk nil → Real, log nil → Discard.
func NewStatusReporter(sess SyncSender, clk clock.Clock, log *slog.Logger) *StatusReporter {
	if clk == nil {
		clk = clock.Real{}
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &StatusReporter{sess: sess, clk: clk, log: log,
		last: map[string]statusKey{}, lastErr: map[string]time.Time{}}
}

// SendCamStatus — §4.4. Qayta yuborish siyosati yuqorida. Yuborilmagan
// (siqilgan) holatda nil qaytadi va sender chaqirilmaydi.
func (r *StatusReporter) SendCamStatus(s proto.CamStatus) error {
	if s.CameraID == "" {
		return errors.New("cam_status: camera_id bo'sh")
	}
	key := statusKey{status: s.Status, err: s.Error}
	if !r.shouldSend(s.CameraID, key) {
		return nil // siqildi
	}
	if err := r.sess.Send(proto.MsgCamStatus, s); err != nil {
		return err
	}
	r.commit(s.CameraID, key)
	return nil
}

// shouldSend — dedup qarori (lock ostida, lekin holatni o'zgartirmaydi).
func (r *StatusReporter) shouldSend(id string, key statusKey) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	prev, seen := r.last[id]
	if !seen || prev != key {
		return true // birinchi marta yoki o'zgardi
	}
	// Holat bir xil: faqat uzluksiz xato holatini 5 daqiqada bir takrorlaymiz.
	if key.status == "online" {
		return false
	}
	at, ok := r.lastErr[id]
	if !ok {
		return true
	}
	return r.clk.Now().Sub(at) >= errRepeatWindow
}

// commit — muvaffaqiyatli yuborishdan keyin holatni yangilaydi.
func (r *StatusReporter) commit(id string, key statusKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last[id] = key
	if key.status != "online" {
		r.lastErr[id] = r.clk.Now()
	} else {
		delete(r.lastErr, id)
	}
}

// SendCodecProbe — §4.5. Natija yuboriladi; xato matni redaktasiyadan
// o'tkaziladi (parol/token tushib qolmasligi uchun, §10.2).
func (r *StatusReporter) SendCodecProbe(p proto.CodecProbe) error {
	if p.CameraID == "" {
		return errors.New("codec_probe: camera_id bo'sh")
	}
	if p.Reason != nil {
		c := redact.String(*p.Reason)
		p.Reason = &c
	}
	if err := r.sess.Send(proto.MsgCodecProbe, p); err != nil {
		return err
	}
	r.log.Info("codec_probe yuborildi", "camera_id", p.CameraID,
		"main", p.Codecs.Main, "sub", p.Codecs.Sub)
	return nil
}
