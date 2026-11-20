// S2.4: start/refresh/stop/ready/stopped amallari (faqat loop goroutine'sida).
package media

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"central-vms/server/internal/gpu"
	"central-vms/server/internal/proto"
	"central-vms/server/internal/registry"
)

func (m *Manager) handle(st *loopState, c *cmd) {
	switch c.op {
	case "start":
		c.res, c.err = m.start(st, c)
	case "refresh":
		c.err = m.refresh(st, c)
	case "stop":
		m.deps.Lg.Info("DBG stop cmd", "sess", c.sessionID, "found", st.sessions[c.sessionID] != nil,
			"pubID", func() string {
				if x := st.sessions[c.sessionID]; x != nil {
					return x.pubID
				}
				return ""
			}())
		c.err = m.stopSession(st, c.sessionID, c.reason)
	case "ready":
		m.mediaReady(st, c)
	case "stopped":
		m.mediaStopped(st, c)
	case "tick":
		m.tick(st)
	case "stats":
		// stats allaqachon muhofaza qilingan; tick bilan bir xil ish
	}
}

// decideTranscode — sof funksiya: kamera siyosati + server imkoniyatidan
// rejimni aniqlash (testlar GPU fixture'lari bilan).
func decideTranscode(mode string, caps gpu.Caps) (pubMode, backend, by string) {
	switch mode {
	case "off":
		return "direct", "none", "none"
	case "server":
		if caps.Backend != gpu.BackendNone {
			return "server_tc", string(caps.Backend), "server"
		}
		return "direct", "none", "none" // server GPU yo'q → fallback (root §5 zanjir)
	case "client":
		return "direct", "none", "client"
	default: // auto
		if caps.Backend != gpu.BackendNone {
			return "server_tc", string(caps.Backend), "server"
		}
		return "direct", "none", "none"
	}
}

func (m *Manager) start(st *loopState, c *cmd) (*StartResult, error) {
	cam := c.cam
	if !cam.Enabled {
		return nil, fmt.Errorf("kamera o'chirilgan")
	}
	cl, err := m.deps.Reg.GetClientByID(cam.ClientID)
	if err != nil {
		return nil, fmt.Errorf("client topilmadi: %w", err)
	}
	if !m.deps.Agent.Online(cl.AgentID) {
		return nil, fmt.Errorf("agent offline")
	}
	total, err := m.deps.Store.CountActiveSessions(0)
	if err != nil {
		return nil, err
	}
	if m.cfg.GlobalMax > 0 && total >= m.cfg.GlobalMax {
		return nil, ErrFull
	}
	mine, err := m.deps.Store.CountActiveSessions(c.userID)
	if err != nil {
		return nil, err
	}
	if m.cfg.UserMax > 0 && mine >= m.cfg.UserMax {
		return nil, ErrFull
	}
	if c.kind == "preview" && m.cfg.PreviewMax > 0 {
		var pv int
		for _, s := range st.sessions {
			if s.kind == "preview" && (s.state == "pending" || s.state == "publishing" || s.state == "playing") {
				pv++
			}
		}
		if pv >= m.cfg.PreviewMax {
			return nil, ErrFull
		}
	}

	profile := c.profile
	if profile == "" {
		profile = cam.PlayProfile
	}
	pubMode, backend, by := decideTranscode(cam.TranscodeMode, m.deps.GPU)
	// server_tc: agent main profilni yuboradi (sifat manbai), sub'ga
	// FFmpeg o'zi tushiradi — dedup kaliti shu.
	pubProfile := profile
	if pubMode == "server_tc" {
		pubProfile = "main"
	}
	ingest := "rtsp"

	now := m.deps.Clock.Now()
	sess := &session{
		id: randHex(8), cameraID: cam.ID, agentID: cl.AgentID, userID: c.userID,
		profile: profile, kind: c.kind, state: "pending",
		expiresAt: now.Add(m.cfg.SessionTTL), backend: backend,
		transcoded: pubMode == "server_tc",
	}
	// FK tartibi: avval session (media_tokens.session_id REFERENCES sessions),
	// keyin tokenlar.
	if err := m.deps.Store.InsertSession(sess.id, cam.ID, c.userID, profile, c.kind,
		sess.expiresAt.UTC().Format(time.RFC3339)); err != nil {
		return nil, err
	}
	pub := m.getOrCreatePub(st, cam, cl.AgentID, pubProfile, pubMode, ingest, now)
	if pub == nil {
		_ = m.deps.Store.SetSessionState(sess.id, "failed")
		return nil, fmt.Errorf("publish token yaratib bo'lmadi")
	}
	readTTL := m.cfg.SessionTTL + 2*m.cfg.RefreshExt
	readPath := pub.path
	if pubMode == "server_tc" {
		readPath = "tc/" + cl.AgentID + "/" + cam.ID
	}
	raw, _, err := m.deps.Tokens.Mint(sess.id, readPath, "read", readTTL)
	if err != nil {
		_ = m.deps.Store.SetSessionState(sess.id, "failed")
		m.releasePubLocked(st, pub)
		return nil, err
	}
	sess.readToken = raw
	sess.pubID = pub.id
	sess.readerPath = pub.path
	if pubMode == "server_tc" {
		sess.readerPath = "tc/" + cl.AgentID + "/" + cam.ID
	}
	st.sessions[sess.id] = sess
	pub.refcount++
	pub.sessions = append(pub.sessions, sess.id)

	if err := m.deps.Agent.Send(cl.AgentID, proto.MsgMediaStart, proto.MediaStart{
		SessionID: sess.id, CameraID: cam.ID, Profile: pubProfile, Kind: c.kind,
		TTLS:      int(m.cfg.PublishTTL.Seconds()),
		Ingest:    ingest,
		Publish:   proto.Publish{Token: pub.token, URLs: m.publishURLs(pub.path, ingest), HeaderName: "Authorization", QueryName: "token"},
		Transcode: proto.Transcode{Mode: cam.TranscodeMode, By: by, Backend: backend, Params: proto.TranscodeParams{Profile: pubProfile}},
		Reason:    "user_start",
	}); err != nil {
		m.endSessionLocked(st, sess, "agent_send_failed", true)
		m.releasePubLocked(st, pub)
		return nil, fmt.Errorf("media_start yuborib bo'lmadi: %w", err)
	}
	sess.state = "publishing"
	_ = m.deps.Store.SetSessionState(sess.id, "publishing")

	return &StartResult{
		SessionID:  sess.id,
		PlayURL:    m.whepURL(sess.readerPath),
		RTSPURL:    m.rtspURL(sess.readerPath),
		ExpiresAt:  sess.expiresAt,
		Transcoded: sess.transcoded,
		Backend:    backend,
	}, nil
}

func (m *Manager) getOrCreatePub(st *loopState, cam registry.Camera, agentID, profile, pubMode, ingest string, now time.Time) *publish {
	key := pubKey(cam.ID, profile, pubMode)
	if id, ok := st.byKey[key]; ok {
		p := st.pubs[id]
		p.idleOn = false // oldingi idle hisob bekor bo'ldi
		return p
	}
	// Agent HAR DOIM originalni cam/... ga yuboradi; server_tc'da FFmpeg
	// uni o'qib tc/... ga yozadi (S2.7) — o'quvchi esa tc/... dan oladi.
	path := "cam/" + agentID + "/" + cam.ID
	p := &publish{
		id: randHex(8), agentID: agentID, cameraID: cam.ID, path: path, profile: profile,
		ingest: ingest, mode: pubMode, started: now, readyTO: now.Add(m.cfg.MediaReadyTO),
	}
	raw, _, err := m.deps.Tokens.Mint("", path, "publish", m.cfg.PublishTTL+5*time.Minute)
	if err != nil {
		m.deps.Lg.Warn("publish token mint", "err", err)
		return nil
	}
	p.token = raw
	if err := m.deps.MTX.EnsurePath(path, PathConf{Source: "publisher"}); err != nil {
		m.deps.Lg.Warn("ensure path", "path", path, "err", err)
	}
	st.pubs[p.id] = p
	st.byKey[key] = p.id
	return p
}

func (m *Manager) mediaReady(st *loopState, c *cmd) {
	p := m.pubByIDLocked(st, c.ready.PublishID)
	if p == nil {
		p = m.pubBySessionLocked(st, c.ready.SessionID)
	}
	if p == nil {
		return
	}
	if p.publishID == "" {
		p.publishID = c.ready.PublishID
	}
	if c.ready.Path != "" {
		p.path = c.ready.Path // agent xabar bergan path — bir xil kelishi shart
	}
	if !p.ready {
		p.ready = true
		m.deps.Lg.Info("media_ready", "session", c.ready.SessionID, "path", p.path,
			"ready_ms", c.ready.ReadyMs, "publish_id", c.ready.PublishID)
	}
	for _, sid := range p.sessions {
		s := st.sessions[sid]
		if s == nil || s.state == "playing" {
			continue
		}
		s.state = "playing"
		if c.ready.Path != "" && !s.transcoded {
			s.readerPath = c.ready.Path
		}
		_ = m.deps.Store.SetSessionState(sid, "playing")
	}
}

func (m *Manager) mediaStopped(st *loopState, c *cmd) {
	p := m.pubBySessionLocked(st, c.stopped.SessionID)
	if p == nil {
		return
	}
	m.deps.Lg.Info("media_stopped", "publish", p.id, "reason", c.stopped.Reason)
	for _, sid := range append([]string(nil), p.sessions...) {
		if s := st.sessions[sid]; s != nil {
			m.endSessionLocked(st, s, "agent_stopped:"+c.stopped.Reason, true)
		}
	}
	m.dropPubLocked(st, p) // refcount hisobiga qaramay darhol to'xtatiladi
}

func (m *Manager) refresh(st *loopState, c *cmd) error {
	s := st.sessions[c.sessionID]
	if s == nil || s.state == "ended" || s.state == "failed" {
		return fmt.Errorf("session %q yo'q yoki tugagan", c.sessionID)
	}
	ext := time.Duration(c.extendS) * time.Second
	if ext <= 0 {
		ext = m.cfg.RefreshExt
	}
	s.expiresAt = m.deps.Clock.Now().Add(ext)
	s.state = "playing"
	_ = m.deps.Store.SetSessionState(s.id, "playing")
	_ = m.deps.Store.TouchSessionTTL(s.id, s.expiresAt.UTC().Format(time.RFC3339))
	return m.deps.Tokens.Extend(s.readToken, ext)
}

// stopSession — sessiyani yopish; publish refcount kamayadi, zero bo'lsa
// idle hisob boshlanadi (root §16.2: readers=0 → IdleStop keyin to'xtatish).
func (m *Manager) stopSession(st *loopState, sessionID, reason string) error {
	s := st.sessions[sessionID]
	if s == nil {
		// Allaqachon tugagan — idempotent (UI ikki marta bosishi mumkin).
		return nil
	}
	m.endSessionLocked(st, s, reason, true)
	return nil
}

func (m *Manager) endSessionLocked(st *loopState, s *session, reason string, dbState bool) {
	if s.state == "ended" || s.state == "failed" {
		return
	}
	final := "ended"
	if reason == "media_ready_timeout" || reason == "media_offline" || reason == "agent_send_failed" {
		final = "failed"
	}
	s.state = final
	if dbState {
		_ = m.deps.Store.SetSessionState(s.id, final)
	}
	delete(st.sessions, s.id) // xotiradan (DB tarixi qoladi)
	// Publish ro'yxatidan ham olib tashlash + refcount kamaytirish —
	// aks holda keyingi Stop/mediaStopped "yo'q" deb adashadi.
	if p := st.pubs[s.pubID]; p != nil {
		out := p.sessions[:0]
		for _, sid := range p.sessions {
			if sid != s.id {
				out = append(out, sid)
			}
		}
		p.sessions = out
		m.releasePubLocked(st, p)
	}
	m.deps.Lg.Info("session yakunlandi", "session", s.id, "state", final, "reason", reason)
}

func (m *Manager) releasePubLocked(st *loopState, p *publish) {
	if p.refcount > 0 {
		p.refcount--
	}
	if p.refcount == 0 {
		p.idleOn = true
		p.stopAt = m.deps.Clock.Now().Add(m.cfg.IdleStop)
	}
}

func (m *Manager) dropPubLocked(st *loopState, p *publish) {
	for _, sid := range p.sessions {
		if s := st.sessions[sid]; s != nil && s.state != "ended" && s.state != "failed" {
			m.endSessionLocked(st, s, "publish_dropped", true)
		}
		delete(st.sessions, sid)
	}
	delete(st.pubs, p.id)
	delete(st.byKey, pubKey(p.cameraID, p.profile, p.mode))
	m.stopPublish(st, p, "dropped")
}

func (m *Manager) stopPublish(st *loopState, p *publish, reason string) {
	if p.stopSent {
		return
	}
	p.stopSent = true
	if m.deps.Agent.Online(p.agentID) {
		_ = m.deps.Agent.Send(p.agentID, proto.MsgMediaStop, proto.MediaStop{
			SessionID: p.firstSession(), CameraID: p.cameraID, Reason: reason})
	}
	_ = m.deps.MTX.DeletePath(p.path)
}

func (p *publish) firstSession() string {
	if len(p.sessions) > 0 {
		return p.sessions[0]
	}
	return p.id
}

func pubKey(cameraID, profile, mode string) string { return cameraID + "|" + profile + "|" + mode }

func (m *Manager) pubByIDLocked(st *loopState, publishID string) *publish {
	if publishID == "" {
		return nil
	}
	for _, p := range st.pubs {
		if p.publishID == publishID || p.id == publishID {
			return p
		}
	}
	return nil
}

func (m *Manager) pubBySessionLocked(st *loopState, sessionID string) *publish {
	for _, p := range st.pubs {
		for _, sid := range p.sessions {
			if sid == sessionID {
				return p
			}
		}
	}
	// media_ready'da agent o'z publish_id'sini yuboradi — session topilmasa
	// publish'ni publish_id bo'yicha izlash mumkin (pastdagi variant).
	return nil
}

func pathFor(pubMode, agentID, cameraID string) string {
	if pubMode == "server_tc" {
		return "tc/" + agentID + "/" + cameraID
	}
	return "cam/" + agentID + "/" + cameraID
}

func (m *Manager) host() string {
	if m.deps.PublicHost != "" {
		return m.deps.PublicHost
	}
	return "127.0.0.1"
}

func (m *Manager) urls() (whepBase, rtspBase string) {
	wp, rp := m.deps.WHEPPort, m.deps.RTSPPort
	if wp == 0 {
		wp = 8889
	}
	if rp == 0 {
		rp = 8554
	}
	return m.deps.MTX.URLs(m.host(), wp, rp)
}

func (m *Manager) whepURL(path string) string {
	wb, _ := m.urls()
	return wb + "/" + path + "/whep"
}

func (m *Manager) rtspURL(path string) string {
	_, rb := m.urls()
	return rb + "/" + path
}

func (m *Manager) publishURLs(path, ingest string) proto.PublishURLs {
	wb, rb := m.urls()
	// WHIP — WHEP o'rniga whip (agent publish uchun).
	return proto.PublishURLs{WHIP: wb + "/" + path + "/whip", RTSP: rb + "/" + path}
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
