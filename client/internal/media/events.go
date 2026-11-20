package media

import (
	"central-vms/client/internal/proto"
	"central-vms/client/internal/publish"
	"central-vms/client/internal/redact"
	"time"
)

// C3.20: media hodisalarini WS orqali serverga yuborish (§4.6) va
// registry callback'larini Manager'ga bog'lash.

// onReady — registry MarkReady hodisasi → media_ready (BIR marta, §9.1).
func (m *Manager) OnReady(ev publish.ReadyEvent) {
	m.mu.Lock()
	s := m.sess[ev.Key]
	var st publish.Stat
	if s != nil && s.tr != nil {
		st = publish.Stat{BytesSent: s.tr.BytesSent(), Transport: s.tr.Transport()}
	}
	m.mu.Unlock()
	if s == nil {
		return
	}
	ready := publish.BuildReady(s.sessionID, s.kind, s.publishID, s.plan.Key.Profile,
		s.plan, st, s.transcoded, s.backend(), time.Since(s.openedAt).Milliseconds())
	m.sendWS(proto.MsgMediaReady, ready)
}

// onStopped — registry yopilish hodisasi → transportni yopish + media_stopped.
func (m *Manager) OnStopped(ev publish.StoppedEvent) {
	m.closeSess(ev.Key)
	m.sendWS(proto.MsgMediaStopped, proto.MediaStopped{
		SessionID: m.sessionOf(ev.Key, ev.PublishID), CameraID: ev.Key.CameraID,
		PublishID: ev.PublishID, Reason: ev.Reason,
	})
}

// sendStopped — reestrda entry bo'lmagan rad etilgan holatlar (§5.2).
func (m *Manager) sendStopped(sessionID, cameraID, publishID, reason string) {
	m.sendWS(proto.MsgMediaStopped, proto.MediaStopped{
		SessionID: sessionID, CameraID: cameraID, PublishID: publishID, Reason: reason})
}

func (m *Manager) sendWS(typ string, data any) {
	if m.cfg.Send == nil {
		return
	}
	if err := m.cfg.Send(typ, data); err != nil {
		m.cfg.Log.Warn("media hodisa yuborilmadi", "type", typ, "err", redact.String(err.Error()))
	}
}

// sessionOf — media_stopped uchun session_id: oxirgi so'rovchi yoki bo'sh.
func (m *Manager) sessionOf(k publish.Key, publishID string) string {
	m.mu.Lock()
	s := m.sess[k]
	m.mu.Unlock()
	if s != nil {
		return s.sessionID
	}
	return ""
}

// backend — transport backend belgisi (§4.6: go-whip | go-rtsp | ffmpeg).
func (s *sess) backend() string {
	switch s.plan.Ingest {
	case "whip":
		return "go-whip"
	case "rtsp_tcp":
		return "go-rtsp"
	default:
		return "ffmpeg"
	}
}

// statsSource — media_stats manbai: transport hisoblagichlari (§4.6).
func (m *Manager) StatsSource(k publish.Key) publish.Stat {
	m.mu.Lock()
	s := m.sess[k]
	m.mu.Unlock()
	if s == nil || s.tr == nil {
		return publish.Stat{}
	}
	return publish.Stat{BytesSent: s.tr.BytesSent(), Transport: s.tr.Transport()}
}

// statsEmit — media_stats'ni WS'ga yuborish.
func (m *Manager) StatsEmit(st proto.MediaStats) {
	m.sendWS(proto.MsgMediaStats, st)
}
