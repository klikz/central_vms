package main

import (
	"central-vms/client/internal/cams"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/publish"
	"central-vms/client/internal/redact"
	"time"
)

// C3.20: media hodisalarini WS orqali serverga yuborish (§4.6) va
// registry callback'larini mediaMgr'ga bog'lash.

// onReady — registry MarkReady hodisasi → media_ready (BIR marta, §9.1).
func (m *mediaMgr) onReady(ev publish.ReadyEvent) {
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
func (m *mediaMgr) onStopped(ev publish.StoppedEvent) {
	m.closeSess(ev.Key)
	m.sendWS(proto.MsgMediaStopped, proto.MediaStopped{
		SessionID: m.sessionOf(ev.Key, ev.PublishID), CameraID: ev.Key.CameraID,
		PublishID: ev.PublishID, Reason: ev.Reason,
	})
}

// sendStopped — reestrda entry bo'lmagan rad etilgan holatlar (§5.2).
func (m *mediaMgr) sendStopped(sessionID, cameraID, publishID, reason string) {
	m.sendWS(proto.MsgMediaStopped, proto.MediaStopped{
		SessionID: sessionID, CameraID: cameraID, PublishID: publishID, Reason: reason})
}

func (m *mediaMgr) sendWS(typ string, data any) {
	m.rt.mu.Lock()
	sess := m.rt.sess
	m.rt.mu.Unlock()
	if sess == nil {
		return
	}
	if err := sess.Send(typ, data); err != nil {
		m.rt.log.Warn("media hodisa yuborilmadi", "type", typ, "err", redact.String(err.Error()))
	}
}

// sessionOf — media_stopped uchun session_id: oxirgi so'rovchi yoki bo'sh.
func (m *mediaMgr) sessionOf(k publish.Key, publishID string) string {
	m.mu.Lock()
	s := m.sess[k]
	m.mu.Unlock()
	if s != nil {
		return s.sessionID
	}
	return ""
}

// backend — transport backend belgisi (§4.6: go-whip | go-rtsp | ffmpeg).
func (s *mediaSess) backend() string {
	switch s.plan.Ingest {
	case "whip":
		return "go-whip"
	case "rtsp_tcp":
		return "go-rtsp"
	default:
		return "ffmpeg"
	}
}

// camRTSPUser — kamera RTSP foydalanuvchisi (default "admin"; ONVIF
// foydalanuvchisi sozlamada parameter sifatida beriladi).
func camRTSPUser(c cams.Camera) string {
	if c.OnvifXAddr != "" {
		// ONVIF sozlangan kameralarda odatiy admin hisob ishlatiladi (§16).
		return "admin"
	}
	return "admin"
}

// statsSource — media_stats manbai: transport hisoblagichlari (§4.6).
func (m *mediaMgr) statsSource(k publish.Key) publish.Stat {
	m.mu.Lock()
	s := m.sess[k]
	m.mu.Unlock()
	if s == nil || s.tr == nil {
		return publish.Stat{}
	}
	return publish.Stat{BytesSent: s.tr.BytesSent(), Transport: s.tr.Transport()}
}

// statsEmit — media_stats'ni WS'ga yuborish.
func (m *mediaMgr) statsEmit(st proto.MediaStats) {
	m.sendWS(proto.MsgMediaStats, st)
}
