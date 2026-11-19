package publish

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"

	"central-vms/client/internal/rtspclient"
)

// §13: yopilish/xato reason kodlari — bitta joyda, UI xabarlari bilan.

// Side — xato qaysi tomonda yuz berdi (kamera pull yoki server publish).
type Side int

const (
	SideCamera Side = iota // kamera RTSP tomoni
	SideServer             // server/MediaMTX tomoni (publish)
)

// §13 to'liq kodlar ro'yxati.
const (
	ReasonCameraOffline     = "camera_offline"
	ReasonCameraRejected    = "camera_rejected"
	ReasonTokenRejected     = "token_rejected"
	ReasonPublishFailed     = "publish_failed"
	ReasonCodecMismatch     = "codec_mismatch"
	ReasonBFrameUnsupported = "bframe_unsupported"
	ReasonTranscodeFailed   = "transcode_failed"
	ReasonLimitExceeded     = "limit_exceeded"
	ReasonTTLExpired        = "ttl_expired"
	ReasonNetwork           = "network"
	ReasonAgentRestart      = "agent_restart"
	ReasonShutdown          = "shutdown"
	ReasonNoViewers         = "no_viewers"
	ReasonServerStop        = "server_stop"
	ReasonUserClose         = "user_close"
	ReasonPageHidden        = "page_hidden"
	ReasonIdleTTL           = "idle_ttl"
)

// Soxta (sentinel) xatolar — yuqoridagi kodlarga birma-bir bog'lanadi.
var (
	ErrLimitExceeded     = errors.New("publish: max_uploads band")
	ErrCodecMismatch     = errors.New("publish: SDP'da kutilgan kodek yo'q")
	ErrBFrameUnsupported = errors.New("publish: High+B-frame tuzatib bo'lmadi")
	ErrTranscodeFailed   = errors.New("publish: FFmpeg ochilmadi/encoders yo'q")
)

// reasonText — §13 UI xabarlari (foydalanuvchiga ko'rsatilmaydiganlar bo'sh).
var reasonText = map[string]string{
	ReasonCameraOffline:     "Kamera javob bermayapti",
	ReasonCameraRejected:    "Kamera paroli yoki RTSP yo'li xato",
	ReasonTokenRejected:     "Ruxsat rad etildi (token)",
	ReasonPublishFailed:     "Oqimni yubolib bo'lmadi",
	ReasonCodecMismatch:     "Kodek o'zgardi, qayta tekshirilmoqda",
	ReasonBFrameUnsupported: "Kamerada B-frame o'chirilsin (qo'llanma)",
	ReasonTranscodeFailed:   "Transcode ishlamadi (joy GPU/CPU)",
	ReasonLimitExceeded:     "Joy upload chegarasi band",
	ReasonNetwork:           "Server bilan bog'lanish yo'q",
	// yashirin (normal/ichki) — map'da bo'sh matn bilan (UI ko'rsatmaydi).
	ReasonTTLExpired:   "",
	ReasonAgentRestart: "",
	ReasonShutdown:     "",
	ReasonNoViewers:    "",
	ReasonServerStop:   "",
	ReasonUserClose:    "",
	ReasonPageHidden:   "",
	ReasonIdleTTL:      "",
	"":                 "",
}

// ReasonText — UI ko'rsatadigan matn; yashirin kod → ""; noma'lum → fallback.
func ReasonText(reason string) string {
	if t, ok := reasonText[reason]; ok {
		return t
	}
	if reason == "" {
		return ""
	}
	return "Noma'lum xato"
}

// ValidReason — §13 ro'yxatidagi kodlarmi.
func ValidReason(reason string) bool {
	switch reason {
	case ReasonCameraOffline, ReasonCameraRejected, ReasonTokenRejected,
		ReasonPublishFailed, ReasonCodecMismatch, ReasonBFrameUnsupported,
		ReasonTranscodeFailed, ReasonLimitExceeded, ReasonTTLExpired,
		ReasonNetwork, ReasonAgentRestart, ReasonShutdown, ReasonNoViewers,
		ReasonServerStop, ReasonUserClose, ReasonPageHidden, ReasonIdleTTL:
		return true
	}
	return false
}

// ReasonFor — xato + tomon → §13 kodi. Aniq sabab topilmasa "" qaytadi
// (chaqiruvchi o'zi fallback tanlaydi, masalan publish_failed).
func ReasonFor(err error, side Side) string {
	if err == nil {
		return ""
	}
	// 1) Aniq sentinel kodlar.
	switch {
	case errors.Is(err, ErrLimitExceeded):
		return ReasonLimitExceeded
	case errors.Is(err, ErrCodecMismatch):
		return ReasonCodecMismatch
	case errors.Is(err, ErrBFrameUnsupported):
		return ReasonBFrameUnsupported
	case errors.Is(err, ErrTranscodeFailed):
		return ReasonTranscodeFailed
	}
	// 2) Publish retry: ichki sabab aniqroq bo'lsa — o'sha, yo'qsa publish_failed.
	var pe *PublishError
	if errors.As(err, &pe) {
		if r := ReasonFor(pe.Cause, side); r != "" && r != ReasonPublishFailed {
			return r
		}
		return ReasonPublishFailed
	}
	// 3) Protokol status javoblari.
	var se *rtspclient.StatusError
	if errors.As(err, &se) {
		if side == SideServer {
			if se.Code == 401 || se.Code == 403 {
				return ReasonTokenRejected
			}
			return ReasonNetwork
		}
		if se.Code == 401 || se.Code == 403 || se.Code == 404 {
			return ReasonCameraRejected
		}
		return ReasonCameraOffline
	}
	// 4) Tarmoq xatolari (ulanib bo'lmadi / taymaut).
	if isNetError(err) {
		if side == SideServer {
			return ReasonNetwork
		}
		return ReasonCameraOffline
	}
	return ""
}

func isNetError(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.ETIMEDOUT) ||
		errors.Is(err, os.ErrDeadlineExceeded) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, context.Canceled)
}
