// S2.4: tick loop — taymautlar (ready 5 s, TTL, idle stop, MediaMTX
// watchdog) va graceful shutdown.
package media

import (
	"time"

	"central-vms/server/internal/proto"
)

func (m *Manager) tick(st *loopState) {
	now := m.deps.Clock.Now()

	// MediaMTX watchdog: o'lik bo'lsa — barcha faol sessiyalar failed
	// (root §7.1 media_offline; UI qayta urinishni taklif qiladi).
	if !m.deps.MTX.Alive() {
		for _, s := range snapshotSessions(st) {
			if s.state == "pending" || s.state == "publishing" || s.state == "playing" {
				m.endSessionLocked(st, s, "media_offline", true)
			}
		}
		for _, p := range snapshotPubs(st) {
			m.dropPubLocked(st, p)
		}
		return
	}

	// Sessiya TTL: muddat o'tsa — yopish (watchdog; refresh yetishmasa).
	for _, s := range snapshotSessions(st) {
		if (s.state == "publishing" || s.state == "playing") && now.After(s.expiresAt) {
			_ = m.stopSession(st, s.id, "session_ttl")
		}
	}

	for _, p := range snapshotPubs(st) {
		// media_ready taymauti (5 s): agent javob bermadi → failed.
		if !p.ready && now.After(p.readyTO) {
			m.deps.Lg.Warn("media_ready taymauti", "publish", p.id, "path", p.path)
			for _, sid := range append([]string(nil), p.sessions...) {
				if s := st.sessions[sid]; s != nil {
					m.endSessionLocked(st, s, "media_ready_timeout", true)
				}
			}
			m.dropPubLocked(st, p)
			continue
		}
		// Transcode qayta urinishi (manba kechikishi) — 3 marta, keyin
		// o'quvchi direct (cam) path'ga qaytadi.
		if p.mode == "server_tc" && !p.tcStarted && p.ready && m.deps.Transcode != nil {
			if p.tcTries < 3 && !now.Before(p.tcNextTry) {
				m.startTranscode(st, p)
			} else if p.tcTries >= 3 {
				// O'quvchi tokeni tc/ path uchun mint qilingan — direct'ga
				// fallback tokenni buzadi; shuning uchun sessiyani yopamiz
				// (UI qayta urinishni taklif qiladi).
				m.deps.Lg.Warn("transcode urinishlari tugadi", "camera", p.cameraID)
				for _, sid := range append([]string(nil), p.sessions...) {
					if s := st.sessions[sid]; s != nil {
						m.endSessionLocked(st, s, "transcode_failed", true)
					}
				}
				m.dropPubLocked(st, p)
				continue
			}
		}
		// Idle stop: refcount=0 va readers=0 → upload to'xtatish (§16.2).
		// Sessiyalar biriktirilgan publish'da o'quvchilar yo'qolib qolsa
		// (brauzer yopildi, o'ldi, tarmoq uzildi) — ularni ham yopamiz:
		// UI pagehide'da o'zi yopadi, lekin server ishonchi bu.
		if p.refcount > 0 {
			if n, err := m.deps.MTX.Readers(p.path); err == nil {
				switch {
				case n > 0:
					p.sawReaders = true
					p.zeroSince = time.Time{}
				case p.sawReaders:
					// Deraza keng: UI WHEP'ni qayta ochishi ~10-25 s
					// (transcode'li) bo'lishi mumkin — shuning uchun
					// 3×IdleStop. Qisqa uzilish jonli ko'rishni
					// buzmasligi kerak (2026-11-20 dev'da 1× juda
					// agressiv chiqdi: tirik sessiyalarni yopdi).
					if p.zeroSince.IsZero() {
						p.zeroSince = now
					} else if !now.Before(p.zeroSince.Add(3 * m.cfg.IdleStop)) {
						for _, sid := range append([]string(nil), p.sessions...) {
							if x := st.sessions[sid]; x != nil {
								m.endSessionLocked(st, x, "idle_no_readers", true)
							}
						}
						m.dropPubLocked(st, p)
						continue
					}
				}
			} else {
				m.deps.Lg.Warn("readers poll", "path", p.path, "err", err)
			}
		}

		if p.refcount == 0 && p.idleOn && !now.Before(p.stopAt) {
			if n, err := m.deps.MTX.Readers(p.path); err == nil && n == 0 {
				m.dropPubLocked(st, p)
				continue
			} else if err != nil {
				m.deps.Lg.Warn("readers poll", "path", p.path, "err", err)
			}
			p.stopAt = now.Add(m.cfg.IdleStop) // o'quvchi bor — qayta tekshirish
		}
	}
}

// shutdownAll — Close()da: barcha publish'larni to'xtatish, sessiyalarni
// DB'da ended qilish (graceful restart; §7.1).
func (m *Manager) shutdownAll(st *loopState) {
	for _, p := range snapshotPubs(st) {
		if m.deps.Agent.Online(p.agentID) {
			for _, sid := range p.stopTargets() {
				_ = m.deps.Agent.Send(p.agentID, proto.MsgMediaStop, proto.MediaStop{
					SessionID: sid, CameraID: p.cameraID, Reason: "server_shutdown"})
			}
		}
	}
	for _, s := range snapshotSessions(st) {
		m.endSessionLocked(st, s, "server_shutdown", true)
	}
	if m.deps.Transcode != nil {
		m.deps.Transcode.StopAll()
	}
	clear(st.pubs)
	clear(st.byKey)
}

func snapshotSessions(st *loopState) []*session {
	out := make([]*session, 0, len(st.sessions))
	for _, s := range st.sessions {
		out = append(out, s)
	}
	return out
}

func snapshotPubs(st *loopState) []*publish {
	out := make([]*publish, 0, len(st.pubs))
	for _, p := range st.pubs {
		out = append(out, p)
	}
	return out
}
