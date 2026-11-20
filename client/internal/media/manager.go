package media

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/clock"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/publish"
	"central-vms/client/internal/redact"
)

// C3.20: media sessiyalari boshqaruvchisi — media_start → registry +
// transport (whip/rtsp/ffmpeg) + kamera pull oqimi; hodisalar WS'ga (§4.6).

// transport — publish transportining kichik interfeysi (fake bilan testlanadi).
type Transport interface {
	WriteAU(nals [][]byte, ts uint32) error
	BytesSent() int64
	Transport() string // "udp" | "tcp"
	Close() error
}

// sess — bitta publish_key uchun ochilgan sessiya.
type sess struct {
	key        publish.Key
	publishID  string
	sessionID  string // birinchi so'rovchi (§4.6 media_ready.session_id)
	kind       string
	transcoded bool // transcode.by=client (§4.6)
	plan       publish.StartPlan
	openedAt   time.Time
	cancel     context.CancelFunc
	tr         Transport
}

// Manager — registry hodisalari bilan bog'langan sessiyalar xaritasi.
type Manager struct {
	cfg Config
	reg *publish.Registry

	// Inyeksiya nuqtalari (testlarda fake bilan almashtiriladi).
	openFn func(ctx context.Context, plan publish.StartPlan, cam cams.Camera, pass string) (Transport, error)
	pullFn func(ctx context.Context, o cams.PullOptions, sink func(cams.AU) error) error

	mu   sync.Mutex
	sess map[publish.Key]*sess
}

// Config — boshqaruvchining tashqi bog'lanishlari (WS send, qidiruvlar).
type Config struct {
	Log          *slog.Logger
	Reg          *publish.Registry
	LookupCamera func(id string) (cams.Camera, bool)
	LookupSecret func(ref string) (string, bool)
	// Send — WS orqali serverga xabar (§4.6 hodisalari).
	Send func(typ string, data any) error
	// OpenFn/PullFn — injeksiya (testlarda fake); nil → real transportlar.
	OpenFn func(ctx context.Context, plan publish.StartPlan, cam cams.Camera, pass string) (Transport, error)
	PullFn func(ctx context.Context, o cams.PullOptions, sink func(cams.AU) error) error
}

// SetRegistry — registry keyinchalik bog'lanadi (o'zaro havola uchun).
func (m *Manager) SetRegistry(reg *publish.Registry) {
	m.reg = reg
	m.cfg.Reg = reg
}

// New — media sessiyalari boshqaruvchisi.
func New(cfg Config) *Manager {
	m := &Manager{cfg: cfg, reg: cfg.Reg, sess: map[publish.Key]*sess{}}
	m.openFn = cfg.OpenFn
	if m.openFn == nil {
		m.openFn = OpenTransport
	}
	m.pullFn = cfg.PullFn
	if m.pullFn == nil {
		m.pullFn = func(ctx context.Context, o cams.PullOptions, sink func(cams.AU) error) error {
			o.Clock = clock.Real{}
			return cams.Pull(ctx, o, sink)
		}
	}
	return m
}

// OnStart — §5.2: parse → kamera topish → registry (limit) → transport+oqim.
func (m *Manager) OnStart(ms proto.MediaStart) {
	plan, err := publish.ParseStart(ms)
	if err != nil {
		m.cfg.Log.Warn("media_start parse", "camera_id", ms.CameraID, "err", redact.Err(err))
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
	s2 := &sess{key: plan.Key, publishID: res.PublishID, sessionID: ms.SessionID,
		kind: ms.Kind, transcoded: ms.Transcode.By == "client", plan: plan,
		openedAt: time.Now(), cancel: cancel}
	m.mu.Lock()
	m.sess[plan.Key] = s2
	m.mu.Unlock()
	go m.run(ctx, s2, cam)
}

// run — transport ochish + kamera oqimini uni ustiga yozish (§8, §9.3).
func (m *Manager) run(ctx context.Context, s *sess, cam cams.Camera) {
	pass := ""
	if cam.SecretRef != "" && m.cfg.LookupSecret != nil {
		if b, ok := m.cfg.LookupSecret(cam.SecretRef); ok {
			pass = b
		}
	}
	tr, err := m.openFn(ctx, s.plan, cam, pass)
	if err != nil {
		reason := publish.ReasonFor(err, publish.SideServer)
		if reason == "" {
			reason = publish.ReasonPublishFailed // §13: aniq bo'lmagan sabab
		}
		m.cfg.Log.Warn("transport ochilmadi", "camera_id", cam.ID, "err", redact.Err(err))
		m.reg.Remove(s.key, reason)
		return
	}
	m.mu.Lock()
	s.tr = tr
	m.mu.Unlock()

	first := true
	user := cam.User
	if user == "" {
		user = "admin" // eski konfiglar/agentlar uchun standart (§10.2)
	}
	o := cams.PullOptions{URL: CamRTSPURL(cam), User: user, Pass: pass, Log: m.cfg.Log}
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
func (m *Manager) OnStop(ms proto.MediaStop) {
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
func (m *Manager) OnRefresh(mr proto.MediaRefresh) {
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
func (m *Manager) closeSess(k publish.Key) {
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

// findCamera — kamera ro'yxatidan ID bo'yicha qidirish.
func (m *Manager) findCamera(id string) (cams.Camera, bool) {
	if m.cfg.LookupCamera == nil {
		return cams.Camera{}, false
	}
	return m.cfg.LookupCamera(id)
}
