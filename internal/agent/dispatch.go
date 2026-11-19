package agent

import (
	"log/slog"

	"central-vms/client/internal/keystore"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/redact"
	"central-vms/client/internal/statestore"
)

// Handlers — serverdan kelgan buyruqlarni qabul qilish (accept-then-execute, §2).
// Har bir handler kichik funksiya: asosiy ish keyingi modullarda (C2/C3) bajariladi.
type Handlers struct {
	KS   *keystore.Store
	St   *statestore.State
	Log  *slog.Logger
	Sess *Session

	// SetLogLevel — log darajasini dinamik o'zgartirish (config_push uchun).
	SetLogLevel func(level string) error
	// RefreshMedia — publish TTL uzaytirish (C3 da to'liq ishlaydi).
	RefreshMedia func(proto.MediaRefresh) error
	// CamsAck — cams_sync tasdig'i (§4.3) → Syncer'ga uzatiladi.
	CamsAck func(proto.CamsAck)
	// CamsRequest — server ro'yxatni qayta so'radi (§5.1) → agent sync yuboradi.
	CamsRequest func(proto.CamsRequest)
	// MediaStart/MediaStop/MediaCodecFix — publish sessiyalari (§5.2, C3.20).
	MediaStart    func(proto.MediaStart)
	MediaStop     func(proto.MediaStop)
	MediaCodecFix func(proto.CodecFix)
}

// Handle — xabar turiga qarab bo'lish. Noma'lum turlar jimgina o'tkaziladi (§1).
func (h *Handlers) Handle(env proto.Envelope) {
	switch env.Type {
	case proto.MsgSecretRotate:
		h.secretRotate(env)
	case proto.MsgConfigPush:
		h.configPush(env)
	case proto.MsgMediaRefresh:
		h.mediaRefresh(env)
	case proto.MsgMediaStart:
		decodeRun[proto.MediaStart](h, env, "media_start", func(m proto.MediaStart) {
			if h.MediaStart != nil {
				h.MediaStart(m)
			}
		})
	case proto.MsgMediaStop:
		decodeRun[proto.MediaStop](h, env, "media_stop", func(m proto.MediaStop) {
			if h.MediaStop != nil {
				h.MediaStop(m)
			}
		})
	case proto.MsgCodecFix:
		decodeRun[proto.CodecFix](h, env, "codec_fix", func(m proto.CodecFix) {
			if h.MediaCodecFix != nil {
				h.MediaCodecFix(m)
			}
		})
	case proto.MsgCamsAck:
		if h.CamsAck == nil {
			return
		}
		a, err := proto.Decode[proto.CamsAck](env.Data)
		if err != nil {
			h.Log.Warn("cams_ack noto'g'ri", "err", redact.String(err.Error()))
			return
		}
		h.CamsAck(a)
	case proto.MsgCamsRequest:
		if h.CamsRequest == nil {
			return
		}
		req, err := proto.Decode[proto.CamsRequest](env.Data)
		if err != nil {
			h.Log.Warn("cams_request noto'g'ri", "err", redact.String(err.Error()))
			return
		}
		h.CamsRequest(req)
	case proto.MsgPing:
		// Server ping yuborsa pong bilan javob beramiz (§2)
		if err := h.Sess.Send(proto.MsgPong, map[string]any{}); err != nil {
			h.Log.Warn("pong yuborib bo'lmadi", "err", redact.String(err.Error()))
		}
	default:
		// forward-compat: yangi turlar eski agentga kelganda xato bermaydi
	}
}

// decodeRun — umumiy decode + warn (handler nil bo'lsa jimgina o'tkazadi).
func decodeRun[T any](h *Handlers, env proto.Envelope, name string, fn func(T)) {
	m, err := proto.Decode[T](env.Data)
	if err != nil {
		h.Log.Warn(name+" noto'g'ri", "err", redact.String(err.Error()))
		return
	}
	fn(m)
}

// secretRotate — §5.6: yangi secretni keystore'ga atomik saqlash, keyin secret_rotated.
func (h *Handlers) secretRotate(env proto.Envelope) {
	msg, err := proto.Decode[proto.SecretRotate](env.Data)
	if err != nil || msg.NewSecret == "" {
		h.Log.Error("secret_rotate noto'g'ri", "err", "maydonlar yetishmaydi")
		h.ackRotate(false)
		return
	}
	h.KS.Put(keystore.RefAgentSecret, msg.NewSecret)
	if err := h.KS.Save(); err != nil {
		// Saqlanmasa eski secret ishlayveradi — server grace_s dan keyin eskisini bekor qiladi,
		// shuning uchun ok:false deb javob beramiz va UI ko'rsatadi.
		h.Log.Error("secret saqlanmadi", "err", redact.String(err.Error()))
		h.ackRotate(false)
		return
	}
	h.Log.Info("agent_secret almashtirildi", "grace_s", msg.GraceS)
	h.ackRotate(true)
}

func (h *Handlers) ackRotate(ok bool) {
	if h.Sess == nil {
		return
	}
	if err := h.Sess.Send(proto.MsgSecretRotated, proto.SecretRotated{OK: ok}); err != nil {
		h.Log.Warn("secret_rotated yuborib bo'lmadi", "err", redact.String(err.Error()))
	}
}

// configPush — §5.7: limitlar va sozlamalarni qo'llash (ack yo'q, jimgina qabul).
func (h *Handlers) configPush(env proto.Envelope) {
	msg, err := proto.Decode[proto.ConfigPush](env.Data)
	if err != nil {
		h.Log.Warn("config_push noto'g'ri", "err", redact.String(err.Error()))
		return
	}
	if msg.Settings.LogLevel != nil && h.SetLogLevel != nil {
		if err := h.SetLogLevel(*msg.Settings.LogLevel); err != nil {
			h.Log.Warn("log_level qo'llanmadi", "err", redact.String(err.Error()))
		}
	}
	if h.St != nil && msg.Settings.ProbePacingParallel != nil {
		// probe siyosati C2/C3 da ishlatiladi — hozircha state'ga yozmaymiz,
		// faqat qayd qilamiz (keyinchalik config bilan birlashtiriladi)
		h.Log.Info("config_push qabul qilindi", "probe_parallel", *msg.Settings.ProbePacingParallel)
	}
}

// mediaRefresh — §5.4: publish TTL uzaytirish. C3 da registry ishlaydi.
func (h *Handlers) mediaRefresh(env proto.Envelope) {
	msg, err := proto.Decode[proto.MediaRefresh](env.Data)
	if err != nil {
		h.Log.Warn("media_refresh noto'g'ri", "err", redact.String(err.Error()))
		return
	}
	if h.RefreshMedia == nil {
		return // C3 gacha sessiyalar yo'q
	}
	if err := h.RefreshMedia(msg); err != nil {
		h.Log.Warn("media_refresh bajarilmadi", "session_id", msg.SessionID,
			"err", redact.String(err.Error()))
	}
}
