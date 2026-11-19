package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/publish"
	"central-vms/client/internal/redact"
)

// C3.20: media sessiyalari boshqaruvchisi — media_start → registry +
// transport (whip/rtsp/ffmpeg) + kamera pull oqimi; hodisalar WS'ga (§4.6).

// transport — publish transportining kichik interfeysi (fake bilan testlanadi).
type transport interface {
	WriteAU(nals [][]byte, ts uint32) error
	BytesSent() int64
	Transport() string // "udp" | "tcp"
	Close() error
}

// mediaSess — bitta publish_key uchun ochilgan sessiya.
type mediaSess struct {
	key        publish.Key
	publishID  string
	sessionID  string // birinchi so'rovchi (§4.6 media_ready.session_id)
	kind       string
	transcoded bool // transcode.by=client (§4.6)
	plan       publish.StartPlan
	openedAt   time.Time
	cancel     context.CancelFunc
	tr         transport
}

// mediaMgr — registry hodisalari bilan bog'langan sessiyalar xaritasi.
type mediaMgr struct {
	rt  *runtime
	reg *publish.Registry

	// Inyeksiya nuqtalari (testlarda fake bilan almashtiriladi).
	openFn func(ctx context.Context, plan publish.StartPlan, cam cams.Camera, pass string) (transport, error)
	pullFn func(ctx context.Context, o cams.PullOptions, sink func(cams.AU) error) error

	mu   sync.Mutex
	sess map[publish.Key]*mediaSess
}

func newMediaMgr(rt *runtime, reg *publish.Registry) *mediaMgr {
	m := &mediaMgr{rt: rt, reg: reg, sess: map[publish.Key]*mediaSess{}}
	m.openFn = openTransport
	m.pullFn = func(ctx context.Context, o cams.PullOptions, sink func(cams.AU) error) error {
		o.Clock = clockReal()
		return cams.Pull(ctx, o, sink)
	}
	return m
}

// OnStart — §5.2: parse → kamera topish → registry (limit) → transport+oqim.
func (m *mediaMgr) OnStart(ms proto.MediaStart) {
	plan, err := publish.ParseStart(ms)
	if err != nil {
		m.rt.log.Warn("media_start parse", "camera_id", ms.CameraID, "err", redact.Err(err))
		m.sendStopped(ms.SessionID, ms.CameraID, "", publish.ReasonPublishFailed)
		return
	}
	cam, ok := m.findCamera(ms.CameraID)
	if !ok {
		m.sendStopped(ms.SessionID, ms.CameraID, "", publish.ReasonCameraRejected)
		return
	}
	res, err := m.reg.StartWithTTL(ms.SessionID, plan.Key, ms.Kind, ms.TTLS)
	if err != nil {
		m.sendStopped(ms.SessionID, ms.CameraID, "", publish.ReasonLimitExceeded)
		return
	}
	if !res.New {
		return // refcount o'sdi — transport allaqachon ochiq (§9.1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s2 := &mediaSess{key: plan.Key, publishID: res.PublishID, sessionID: ms.SessionID,
		kind: ms.Kind, transcoded: ms.Transcode.By == "client", plan: plan,
		openedAt: time.Now(), cancel: cancel}
	m.mu.Lock()
	m.sess[plan.Key] = s2
	m.mu.Unlock()
	go m.run(ctx, s2, cam)
}

// run — transport ochish + kamera oqimini uni ustiga yozish (§8, §9.3).
func (m *mediaMgr) run(ctx context.Context, s *mediaSess, cam cams.Camera) {
	pass := ""
	if cam.SecretRef != "" {
		if b, ok := m.rt.ks.Get(cam.SecretRef); ok {
			pass = b
		}
	}
	tr, err := m.openFn(ctx, s.plan, cam, pass)
	if err != nil {
		reason := publish.ReasonFor(err, publish.SideServer)
		if reason == "" {
			reason = publish.ReasonPublishFailed // §13: aniq bo'lmagan sabab
		}
		m.rt.log.Warn("transport ochilmadi", "camera_id", cam.ID, "err", redact.Err(err))
		m.reg.Remove(s.key, reason)
		return
	}
	m.mu.Lock()
	s.tr = tr
	m.mu.Unlock()

	first := true
	o := cams.PullOptions{URL: camRTSPURL(cam), User: camRTSPUser(cam), Pass: pass, Log: m.rt.log}
	perr := m.pullFn(ctx, o, func(au cams.AU) error {
		if err := tr.WriteAU(au.Nals, au.Timestamp); err != nil {
			return err
		}
		if first {
			first = false
			m.reg.MarkReady(s.key) // onReady hodisasi media_ready yuboradi (§4.6)
		}
		return nil
	})
	if perr != nil && ctx.Err() == nil {
		// Kamera oqimi uzildi (3 retry tugadi) → reason §13.
		reason := publish.ReasonFor(perr, publish.SideCamera)
		if reason == "" || errors.Is(perr, cams.ErrCameraOffline) {
			reason = publish.ReasonCameraOffline
		}
		m.reg.Remove(s.key, reason)
	}
}

// OnStop — §5.2/§9.1: sessiyani ayirish; refcount 0 → grace → yopilish.
// Ingest'ni agent sessiyalar xaritasidan olamiz (media_stop ingest maydoni
// majburiy emas, §5.2).
func (m *mediaMgr) OnStop(ms proto.MediaStop) {
	reason := ms.Reason
	if reason == "" {
		reason = publish.ReasonUserClose
	}
	m.mu.Lock()
	keys := make([]publish.Key, 0, 2)
	for k := range m.sess {
		if k.CameraID == ms.CameraID {
			keys = append(keys, k)
		}
	}
	m.mu.Unlock()
	if len(keys) == 0 {
		return
	}
	for _, k := range keys {
		m.reg.StopReason(ms.SessionID, k, reason)
	}
}

// OnRefresh — §9.2: TTL deadline'ini siljitish.
func (m *mediaMgr) OnRefresh(mr proto.MediaRefresh) {
	m.mu.Lock()
	var found *publish.Key
	for k, s := range m.sess {
		if (mr.SessionID != "" && s.sessionID == mr.SessionID) ||
			(mr.SessionID == "" && k.CameraID == mr.CameraID) {
			kk := k
			found = &kk
			break
		}
	}
	m.mu.Unlock()
	if found == nil {
		return
	}
	m.reg.Refresh(*found, mr.ExtendS)
}

// closeSess — transportni yopish (registry stopped hodisasidan keyin).
func (m *mediaMgr) closeSess(k publish.Key) {
	m.mu.Lock()
	s := m.sess[k]
	delete(m.sess, k)
	m.mu.Unlock()
	if s == nil {
		return
	}
	s.cancel()
	if s.tr != nil {
		_ = s.tr.Close()
	}
}

// findCamera — state'dagi kamera ro'yxatidan ID bo'yicha qidirish.
func (m *mediaMgr) findCamera(id string) (cams.Camera, bool) {
	m.rt.mu.Lock()
	defer m.rt.mu.Unlock()
	for _, c := range m.rt.st.Cameras {
		if c.ID == id {
			return c, true
		}
	}
	return cams.Camera{}, false
}
