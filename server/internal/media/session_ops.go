// S2.4: start/refresh/stop/ready/stopped amallari (faqat loop goroutine'sida).
package media

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"central-vms/server/internal/gpu"
	"central-vms/server/internal/proto"
	"central-vms/server/internal/registry"
)

// uiEvent — UI'ga hodisa (UI nil bo'lsa jim). Faqat loop goroutine'sidan
// chaqiriladi — bloklovchi emas (wsui o'zi sekin mijozni yopadi).
func (m *Manager) uiEvent(typ string, data any) {
	if m.deps.UI != nil {
		m.deps.UI.Broadcast(typ, data)
	}
}

func (m *Manager) sessionEvent(st *loopState, s *session, state, reason string) {
	m.uiEvent(EvSessionState, map[string]any{
		"session_id": s.id, "camera_id": s.cameraID, "user_id": s.userID,
		"state": state, "reason": reason, "transcoded": s.transcoded, "backend": s.backend,
	})
	var active int
	for _, x := range st.sessions {
		if x.state == "pending" || x.state == "publishing" || x.state == "playing" {
			active++
		}
	}
	m.uiEvent(EvSessionCount, map[string]any{
		"active": active, "user_max": m.cfg.UserMax, "global_max": m.cfg.GlobalMax,
	})
}

// ErrNotOwner — sessiya boshqa foydalanuvchiniki (IDOR himoyasi,
// 2026-11-21 server auditi: sessiya ID'lari wsui Broadcast'ida
// filtrsiz tarqaladi, shuning uchun ID'ni bilgan har qanday foydalanuvchi
// begga sessiyasini to'xtatib/yozib qo'ya olmasligi kerak).
var ErrNotOwner = errors.New("sessiya sizniki emas")

// ownsSession — sessiya egasi so'rov egasi bilan bir xilmi.
func (m *Manager) ownsSession(st *loopState, c *cmd) bool {
	s := st.sessions[c.sessionID]
	return s != nil && s.userID == c.userID
}

func (m *Manager) handle(st *loopState, c *cmd) {
	switch c.op {
	case "start":
		c.res, c.err = m.start(st, c)
	case "refresh":
		if c.checkOwner && !m.ownsSession(st, c) {
			c.err = ErrNotOwner
			break
		}
		c.err = m.refresh(st, c)
	case "stop":
		if c.checkOwner && !m.ownsSession(st, c) {
			c.err = ErrNotOwner
			break
		}
		c.err = m.stopSession(st, c.sessionID, c.reason)
	case "ready":
		m.mediaReady(st, c)
	case "stopped":
		m.mediaStopped(st, c)
	case "tick":
		m.tick(st)
	case "list":
		c.ids = m.listSessions(st, c)
	case "tcfailed":
		m.transcodeFailed(st, c)
	case "tcend":
		m.transcodeEnded(st, c)
	case "stats":
		// stats allaqachon muhofaza qilingan; tick bilan bir xil ish
	}
}

// decideTranscode — sof funksiya: kamera siyosati + server imkoniyatidan
// rejimni aniqlash (testlar GPU fixture'lari bilan).
func decideTranscode(mode string, caps gpu.Caps, codecIn string, browserHEVC bool) (pubMode, backend, by string) {
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
	default: // auto — transcode FAQAT kerak bo'lganda (CPU/limit tejamkorligi)
		// H.264 brauzerlarda universal; H.265 esa faqat brauzer HEVC'ni
		// qo'llab-quvvatlasa transcodesiz o'ynaladi (aks holda H.264 ga
		// o'girish shart — HLS fallback ham HEVC'da ishonchsiz, §16.4).
		need := strings.EqualFold(codecIn, "h265") && !browserHEVC
		if !need {
			return "direct", "none", "none"
		}
		if caps.Backend != gpu.BackendNone {
			return "server_tc", string(caps.Backend), "server"
		}
		return "direct", "none", "client" // serverda GPU yo'q → agent zaxirasi
	}
}

func (m *Manager) start(st *loopState, c *cmd) (*StartResult, error) {
	cam := c.cam
	if !cam.Enabled {
		return nil, ErrDisabled
	}
	cl, err := m.deps.Reg.GetClientByID(cam.ClientID)
	if err != nil {
		return nil, fmt.Errorf("client topilmadi: %w", err)
	}
	if !m.deps.Agent.Online(cl.AgentID) {
		return nil, ErrAgentOffline
	}
	total, err := m.deps.Store.CountActiveSessions(0)
	if err != nil {
		return nil, err
	}
	if m.cfg.GlobalMax > 0 && total >= m.cfg.GlobalMax {
		return nil, &LimitError{Kind: "global_limit"}
	}
	mine, err := m.deps.Store.CountActiveSessions(c.userID)
	if err != nil {
		return nil, err
	}
	if m.cfg.UserMax > 0 && mine >= m.cfg.UserMax {
		return nil, &LimitError{Kind: "user_limit"}
	}
	if c.kind == "preview" && m.cfg.PreviewMax > 0 {
		var pv int
		for _, s := range st.sessions {
			if s.kind == "preview" && (s.state == "pending" || s.state == "publishing" || s.state == "playing") {
				pv++
			}
		}
		if pv >= m.cfg.PreviewMax {
			return nil, &LimitError{Kind: "preview_limit"}
		}
	}

	profile := c.profile
	if profile == "" {
		profile = cam.PlayProfile
	}
	needCodec := camCodecFor(cam, c.profile)
	pubMode, backend, by := decideTranscode(cam.TranscodeMode, m.deps.GPU, needCodec, c.browserHEVC)
	if pubMode == "server_tc" && m.deps.Transcode == nil {
		pubMode, backend, by = "direct", "none", "none" // transcode yo'q → fallback
	}
	// server_tc: agent main profilni yuboradi (sifat manbai), sub'ga
	// FFmpeg o'zi tushiradi — dedup kaliti shu.
	pubProfile := profile
	if pubMode == "server_tc" {
		pubProfile = "main"
	}
	// §5.2/13: ingest qiymatlari — "whip" | "rtsp_tcp". Avval bu yerda
	// "rtsp" yubatilardi: kontrakt bunday qiymatni bilmaydi va haqiqiy
	// vmsagent media_start'ni "publish: noma'lum ingest" deb rad etardi
	// (2026-11-20: barcha preview'lar publish_failed bilan tugardi).
	// rtsp_tcp tanlandi: u H.264 va H.265'ni birdek o'tkazadi, agentda
	// WHIP muxer bo'lmasligi mumkin (whip — keyinchalik, agent BUni
	// caps'da e'lon qilganda).
	ingest := "rtsp_tcp"

	now := m.deps.Clock.Now()
	sess := &session{
		id: randHex(8), cameraID: cam.ID, agentID: cl.AgentID, userID: c.userID,
		profile: profile, kind: c.kind, state: "pending", browserHEVC: c.browserHEVC,
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
	m.sessionEvent(st, sess, "publishing", "user_start")
	// Publish ALLAQACHON ko'tarilgan (boshqa sessiya ochib ketgan):
	// agent media_ready'ni publish uchun BITTA marta yuboradi, shuning
	// uchun bu sessiyani o'zimiz playing qilamiz — aks holdu 5 s
	// media_ready taymautida o'lar edi.
	if pub.ready {
		sess.state = "playing"
		_ = m.deps.Store.SetSessionState(sess.id, "playing")
		m.sessionEvent(st, sess, "playing", "")
	}

	codec := "h264" // transcode natijasi doim H.264 (browser/hamsodalik)
	if !sess.transcoded {
		codec = camCodecFor(cam, profile)
	}
	return &StartResult{
		SessionID:  sess.id,
		WHEPURL:    m.whepURL(sess.readerPath),
		RTSPURL:    m.rtspURL(sess.readerPath),
		Token:      sess.readToken,
		TTL:        int(m.cfg.SessionTTL.Seconds()),
		Codec:      codec,
		ExpiresAt:  sess.expiresAt,
		Transcoded: sess.transcoded,
		Backend:    backend,
	}, nil
}

func (m *Manager) getOrCreatePub(st *loopState, cam registry.Camera, agentID, profile, pubMode, ingest string, now time.Time) *publish {
	key := pubKey(cam.ID, profile)
	if id, ok := st.byKey[key]; ok {
		p := st.pubs[id]
		p.idleOn = false // oldingi idle hisob bekor bo'ldi
		// Rejim ko'tarilishi: direct publish'da o'tirgan oqimga
		// transcode talab qilingan sessiya qo'shilsa — shu oqimning
		// ustida FFmpeg qo'shiladi (agentga qayta media_start shart emas).
		if pubMode == "server_tc" && p.mode != "server_tc" {
			m.upgradePubToTC(p, now)
		}
		return p
	}
	// Agent HAR DOIM originalni cam/... ga yuboradi; server_tc'da FFmpeg
	// uni o'qib tc/... ga yozadi (S2.7) — o'quvchi esa tc/... dan oladi.
	path := "cam/" + agentID + "/" + cam.ID
	p := &publish{
		id: randHex(8), agentID: agentID, cameraID: cam.ID, path: path, profile: profile,
		ingest: ingest, mode: pubMode, started: now, readyTO: now.Add(m.cfg.MediaReadyTO),
		codecIn: codecForProfile(cam, profile), caps: m.deps.GPU,
	}
	if pubMode == "server_tc" {
		p.tcPath = "tc/" + agentID + "/" + cam.ID
		if tok, _, err := m.deps.Tokens.Mint("", p.tcPath, "publish", 24*time.Hour); err != nil {
			m.deps.Lg.Warn("tc publish token", "err", err)
		} else {
			p.tcPubTok = tok
		}
		if tok, _, err := m.deps.Tokens.Mint("", path, "read", 24*time.Hour); err != nil {
			m.deps.Lg.Warn("src read token", "err", err)
		} else {
			p.srcReadTok = tok
		}
		m.ensureMTXPath(p.tcPath, "ensure tc path")
	}
	raw, _, err := m.deps.Tokens.Mint("", path, "publish", m.cfg.PublishTTL+5*time.Minute)
	if err != nil {
		m.deps.Lg.Warn("publish token mint", "err", err)
		return nil
	}
	p.token = raw
	m.ensureMTXPath(path, "ensure path")
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
	// server_tc: manba tayyor → FFmpeg'ni ishga tushiramiz (bitta kamera =
	// bitta jarayon; qo'shimcha o'quvchilar shu jarayonni ishlatadi).
	if p.mode == "server_tc" && m.deps.Transcode != nil && !p.tcStarted {
		m.startTranscode(st, p)
	}
	if c.ready.Path != "" && c.ready.Path != p.path {
		// Agent boshqa path haqida xabar berdi: o'quvchi URL'lari
		// buzilmasligi uchun o'zimiznikida qolamiz va ogohlantiramiz
		// (1.0-draft.12: avval bu qiymat so'zsiz qabul qilingani uchun
		// "rtsp://host/main" kabi yaroqsiz URL'lar chiqardi).
		m.deps.Lg.Warn("media_ready path mos emas", "publish", p.id,
			"kutilgan", p.path, "kelgan", c.ready.Path)
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
		m.sessionEvent(st, s, "playing", "")
		if c.ready.Path != "" && !s.transcoded {
			s.readerPath = c.ready.Path
		}
		_ = m.deps.Store.SetSessionState(sid, "playing")
	}
}

// upgradePubToTC — tirik direct publish'ni transcode rejimiga ko'tarish:
// tc path tokenlari + MediaMTX path + transcode navbati.
func (m *Manager) upgradePubToTC(p *publish, now time.Time) {
	if m.deps.Transcode == nil {
		return
	}
	p.mode, p.caps = "server_tc", m.deps.GPU
	p.tcPath = "tc/" + p.agentID + "/" + p.cameraID
	p.tcTries, p.tcStarted = 0, false
	p.tcNextTry = now
	if p.tcPubTok == "" {
		if tok, _, err := m.deps.Tokens.Mint("", p.tcPath, "publish", 24*time.Hour); err != nil {
			m.deps.Lg.Warn("tc publish token", "err", err)
		} else {
			p.tcPubTok = tok
		}
	}
	if p.srcReadTok == "" {
		if tok, _, err := m.deps.Tokens.Mint("", p.path, "read", 24*time.Hour); err != nil {
			m.deps.Lg.Warn("src read token", "err", err)
		} else {
			p.srcReadTok = tok
		}
	}
	m.ensureMTXPath(p.tcPath, "ensure tc path")
	m.deps.Lg.Info("publish transcode rejimiga ko'tarildi", "camera", p.cameraID,
		"path", p.tcPath, "ready", p.ready)
}

// startTranscode — FFmpeg'ni ishga tushirish (muvaffaqiyatsiz bo'lsa
// qayta urinish vaqti belgilanadi).
func (m *Manager) startTranscode(st *loopState, p *publish) {
	p.tcStarted, p.tcTries = true, p.tcTries+1
	_, rb := m.urls()
	spec := TranscodeSpec{
		CameraID: p.cameraID, SrcPath: p.path, DstPath: p.tcPath,
		SrcToken: p.srcReadTok, DstToken: p.tcPubTok, CodecIn: p.codecIn,
		Caps: p.caps, RTSPBase: rb,
	}
	if err := m.deps.Transcode.Start(spec); err != nil {
		p.tcStarted = false
		p.tcNextTry = m.deps.Clock.Now().Add(time.Second)
		m.deps.Lg.Warn("transcode boshlanmadi — qayta urilinadi", "camera", p.cameraID,
			"try", p.tcTries, "err", err)
		return
	}
	// Transcode tayyor bo'lsa o'quvchi tc/ path'ga o'tadi — lekin FAQAT
	// H.265'ni o'ynay olmaydigan sessiyalar: HEVC'li brauzer original
	// (cam/) oqimda qoladi, sifat buzilmaydi.
	for _, sid := range p.sessions {
		s := st.sessions[sid]
		if s == nil || s.browserHEVC {
			continue
		}
		s.transcoded, s.readerPath = true, p.tcPath
	}
}

// tcHealthy — transcode shuncha vaqt yashasa, uzilishni vaqtinchalik
// deb hisoblaymiz: urish byudjeti qayta to'ldiriladi.
const tcHealthy = 10 * time.Second

// tcRestartDecision — sof qaror: uzilgan transcode'ni qayta ko'tarish,
// qachon va urish hisobini tozalash.
func tcRestartDecision(elapsed time.Duration, hasSessions bool) (restart bool, delay time.Duration, resetTries bool) {
	if !hasSessions {
		return false, 0, false // publish'da o'quvchi qolmagan — idle logic yopadi
	}
	return true, time.Second, elapsed >= tcHealthy
}

// transcodeEnded — FFmpeg jarayoni tugadi (exit kodi qanday bo'lishidan
// qat'iy nazar). Avval bu xabar yo'q edi: publish tirik qolganda
// transcode o'lsa tc/... path' yo'qolib, o'quvchilar 404 olardi va
// preview biror marta uzilsa butunlay tiklanmas edi (2026-11-21 audit).
func (m *Manager) transcodeEnded(st *loopState, c *cmd) {
	for _, p := range st.pubs {
		if p.cameraID != c.cameraID || p.mode != "server_tc" {
			continue
		}
		p.tcStarted = false
		restart, delay, reset := tcRestartDecision(c.elapsed, len(p.sessions) > 0)
		if !restart {
			continue
		}
		if reset {
			p.tcTries = 0
		}
		p.tcNextTry = m.deps.Clock.Now().Add(delay)
		m.deps.Lg.Info("transcode uzildi — qayta ko'tariladi", "camera", p.cameraID,
			"yashadi", c.elapsed.Round(time.Second).String(), "exit_xato", c.failed,
			"try", p.tcTries)
	}
}

// OnTranscodeEnd — Transcoder jarayon tugash habari (bloklamaydi).
func (m *Manager) OnTranscodeEnd(cameraID string, elapsed time.Duration, failed bool) {
	m.dropCmd(&cmd{op: "tcend", cameraID: cameraID, elapsed: elapsed, failed: failed})
}

// transcodeFailed — Transcoder xabar berdi: bu backend ishlamadi.
// Zanjir bo'yicha keyingi imkoniyatga o'tamiz (server GPU → server CPU).
func (m *Manager) transcodeFailed(st *loopState, c *cmd) {
	for _, p := range st.pubs {
		if p.cameraID != c.cameraID || p.mode != "server_tc" {
			continue
		}
		p.tcStarted = false
		p.tcNextTry = m.deps.Clock.Now()
		if next, ok := m.nextCaps(p.caps); ok {
			p.caps = next
			m.deps.Lg.Info("transcode backend almashtirildi", "camera", p.cameraID, "backend", next.Backend)
		}
	}
}

// nextCaps — zanjir bo'yicha keyingi ishlash mumkin bo'lgan backend
// (nvenc → vaapi → amf → cpu); Transcoder "yomon" deb belgilaganlari
// chiqarib tashlanadi.
func (m *Manager) nextCaps(cur gpu.Caps) (gpu.Caps, bool) {
	chain := []gpu.Backend{gpu.BackendNVENC, gpu.BackendVAAPI, gpu.BackendAMF, gpu.BackendSoft}
	skip := cur.Backend
	for _, b := range chain {
		if b == skip || b == gpu.BackendNone {
			continue
		}
		if m.deps.Transcode != nil && m.deps.Transcode.IsBad(b) {
			continue
		}
		for _, e := range cur.Encoders {
			want := map[gpu.Backend]string{
				gpu.BackendNVENC: "h264_nvenc", gpu.BackendVAAPI: "h264_vaapi",
				gpu.BackendAMF: "h264_amf", gpu.BackendSoft: "libx264",
			}[b]
			if e == want {
				nc := cur
				nc.Backend, nc.Encoder = b, want
				if b != gpu.BackendNVENC && b != gpu.BackendVAAPI {
					nc.HEVCDecoder = ""
				}
				return nc, true
			}
		}
	}
	return cur, false
}

// OnTranscodeFailure — Transcoder qaygi (jarayon darhol yiqilsa).
func (m *Manager) OnTranscodeFailure(cameraID string, backend gpu.Backend) {
	m.dropCmd(&cmd{op: "tcfailed", cameraID: cameraID})
}

// dropCmd — bloklamaydigan yuborish (goroutine callback'idan; loop band
// bo'lsa xabar tushib qoladi — keyingi tick qayta urinishni davom ettiradi).
func (m *Manager) dropCmd(c *cmd) {
	c.done = make(chan struct{})
	select {
	case m.cmd <- c:
	default:
	}
}

func (m *Manager) mediaStopped(st *loopState, c *cmd) {
	p := m.pubBySessionLocked(st, c.stopped.SessionID)
	if p == nil {
		return
	}
	m.deps.Lg.Info("media_stopped", "publish", p.id, "reason", c.stopped.Reason)
	// Sababni kameraga yozib qo'yamiz: operator UI'da "nima uchun oqim
	// yo'q" degan savolga javob ko'rinishi uchun (1.0-draft.12).
	if m.deps.Reg != nil && p.cameraID != "" && oqimXatosi(c.stopped.Reason) {
		_ = m.deps.Reg.SetCameraError(p.cameraID, "oqim to'xtadi: "+c.stopped.Reason)
	}
	for _, sid := range append([]string(nil), p.sessions...) {
		if s := st.sessions[sid]; s != nil {
			m.endSessionLocked(st, s, "agent_stopped:"+c.stopped.Reason, true)
		}
	}
	m.dropPubLocked(st, p) // refcount hisobiga qaramay darhol to'xtatiladi
}

func (m *Manager) listSessions(st *loopState, c *cmd) []string {
	var out []string
	for _, s := range st.sessions {
		if c.userID != 0 && s.userID != c.userID {
			continue
		}
		if c.cameraID != "" && s.cameraID != c.cameraID {
			continue
		}
		if c.kind != "" && s.kind != c.kind {
			continue
		}
		out = append(out, s.id)
	}
	return out
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
		m.sessionEvent(st, s, final, reason)
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
		p.ended = append(p.ended, s.id)
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
	delete(st.byKey, pubKey(p.cameraID, p.profile))
	if p.tcPath != "" {
		_ = m.deps.MTX.DeletePath(p.tcPath)
	}
	if m.deps.Transcode != nil {
		m.deps.Transcode.Stop(p.cameraID, "publish_dropped")
	}
	m.stopPublish(st, p, "dropped")
}

func (m *Manager) stopPublish(st *loopState, p *publish, reason string) {
	if p.stopSent {
		return
	}
	p.stopSent = true
	// Har sessiya ID'si uchun alohida media_stop: agent publish'ni
	// session_id bo'yicha kuzatadi (protocol.md §9). firstSession()
	// yetmas edi — tugagan sessiyalar ro'yxatdan o'chirilgan bo'ladi.
	if m.deps.Agent.Online(p.agentID) {
		for _, sid := range p.stopTargets() {
			_ = m.deps.Agent.Send(p.agentID, proto.MsgMediaStop, proto.MediaStop{
				SessionID: sid, CameraID: p.cameraID, Reason: reason})
		}
	}
	_ = m.deps.MTX.DeletePath(p.path)
}

// stopTargets — media_stop yuboriladigan sessiya ID'lari (faol + tugagan,
// takrorlanmas). Kamida bitta ID bo'lishi shart (agent publish'ni taniydi).
func (p *publish) stopTargets() []string {
	seen := map[string]bool{}
	var out []string
	for _, sid := range append(append([]string(nil), p.sessions...), p.ended...) {
		if !seen[sid] {
			seen[sid] = true
			out = append(out, sid)
		}
	}
	if len(out) == 0 {
		out = []string{p.id}
	}
	return out
}

// pubKey — publish dedup kaliti. REJA/§9.1: agent kamera+profil bo'yicha
// BITTA original oqim ochadi (ingest bir xil), shuning uchun kalitda
// transcode rejimi BO'LMASLIGI kerak. Avval mode ham kalitda edi:
// avval hevc=true (direct), keyin hevc=false (server_tc) so'rovi kelganda
// server ikkinchi publish ochib, ikkinchi media_start yuborardi — agent
// esa uni dedup qilib refcount'ni o'stirdi-xolos, media_ready esa BITTA
// marta keladi. Natijada ikkinchi sessiya "publishing"da qotib, 5 s'dan
// keyin media_ready_timeout bilan o'lar, transcode umuman boshlanmas edi
// (2026-11-21 server auditining asosiy topilmasi).
func pubKey(cameraID, profile string) string { return cameraID + "|" + profile }

// camCodecFor — o'quvchiga xabar qilinadigan kodek (profil bo'yicha).
func camCodecFor(cam registry.Camera, profile string) string {
	if profile == "main" {
		return cam.CodecMain
	}
	return cam.CodecSub
}

// codecForProfile — transcode manbasining kodeki (cuvid/vaapi tanlovi uchun).
func codecForProfile(cam registry.Camera, profile string) string {
	if profile == "main" {
		return cam.CodecMain
	}
	return cam.CodecSub
}

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

// oqimXatosi — reason kodi muvaffaqiyatsizlikmi (UI'da ko'rsatiladi),
// yoki normal to'xtash (tomaqibilar ketdi / TTL tugadi / foydalanuvchi
// yopdi — §9.1).
func oqimXatosi(reason string) bool {
	switch reason {
	case "no_viewers", "ttl_expired", "user", "user_stop", "session_stop", "":
		return false
	default:
		return true
	}
}

// ensureMTXPath — MediaMTX'da publish yo'li mavjudligini ta'minlash.
// Konfiguratsiya (dev/prod mediamtx.yml) `~^cam/.+` va `~^tc/.+`
// patternlari bilan bizning yo'llarni qoplaydi — bunday holda API orqali
// qo'shishga hojat yo'q (v1.11.3'da `/` li nomlar bilan PUT ishlamaydi).
func (m *Manager) ensureMTXPath(path, label string) {
	if ok, err := m.deps.MTX.PathConfigured(path); err == nil && ok {
		return
	}
	if err := m.deps.MTX.EnsurePath(path, PathConf{Source: "publisher"}); err != nil {
		m.deps.Lg.Warn(label, "path", path, "err", err)
	}
}
