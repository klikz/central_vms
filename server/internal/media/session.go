// S2.4: sessiya menejeri — §7.1 holat mashinasi (pending → publishing →
// playing → stopping → ended/failed), publish refcount dedup, taymautlar
// (ready 5 s, TTL 5 min, idle stop 10 s), navbat (S2.5).
// Single-writer: barcha holat bitta goroutine'da; taymautlar tick loop'da
// fake clock bilan tekshiriladi (testlar deterministik).
package media

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"central-vms/server/internal/gpu"

	"central-vms/server/internal/proto"
	"central-vms/server/internal/registry"
)

// ErrFull — limitlar to'lgan (handler 429 beradi; navbat S2.5).
var ErrFull = errors.New("limit to'lgan")

// AgentSender — agentga media_start/media_stop yuborish (wsagent.Hub).
type AgentSender interface {
	Send(agentID, typ string, data any) error
	Online(agentID string) bool
}

// MediaAPI — MediaMTX control (Client shuni qanoatlantiradi).
type MediaAPI interface {
	EnsurePath(name string, conf PathConf) error
	DeletePath(name string) error
	Readers(path string) (int, error)
	Alive() bool
	// URLs — host/port'lardan WHEP/RTSP asoslarini quradi (sozlanuvchan).
	URLs(host string, whepPort, rtspPort int) (whepBase, rtspBase string)
}

// Store — sessions CRUD (registry.Registry shuni qanoatlantiradi).
type Store interface {
	InsertSession(id, cameraID string, userID int64, profile, kind, expiresAt string) error
	SetSessionState(id, state string) error
	TouchSessionTTL(id, expiresAt string) error
	CountActiveSessions(userID int64) (int, error)
}

// MgrConfig — §7.1 taymautlari va limitlar.
type MgrConfig struct {
	MediaReadyTO time.Duration // 5 s: media_ready kutiladi
	SessionTTL   time.Duration // 5 min
	RefreshExt   time.Duration // 2 min (media_refresh)
	PublishTTL   time.Duration // 60 s: refcount=0 bo'lsa idle hisob
	IdleStop     time.Duration // 10 s: readers=0 → upload to'xtatish
	UserMax      int           // 16
	GlobalMax    int           // 48
	PreviewMax   int           // 8 — bir vaqtdagi preview sessiyalari
	Tick         time.Duration // default 1 s
}

// Deps — menejer bog'liqliklari.
type Deps struct {
	Reg        *registry.Registry
	Store      Store
	Agent      AgentSender
	MTX        MediaAPI
	Tokens     *Tokens
	Clock      Clock
	Lg         *slog.Logger
	GPU        gpu.Caps // S2.1 aniqlangan imkoniyatlar
	PublicHost string   // agent/UI MediaMTX'ga qaysi IP orqali boradi
	WHEPPort   int      // 8889 (config.mediamtx.webrtc_port)
	RTSPPort   int      // 8554
}

// Manager — sessiyalar va publish'larning yagona egasi.
type Manager struct {
	cfg  MgrConfig
	deps Deps

	cmd chan *cmd

	stopCh chan struct{}
	done   chan struct{}

	mu       sync.Mutex // faqat stats (media_stats DB'dan tashqarida)
	stats    map[string]proto.PublishStat
	lastSeen map[string]time.Time
}

// StartResult — play/preview javobi (handler'ga).
type StartResult struct {
	SessionID  string    `json:"session_id"`
	PlayURL    string    `json:"play_url"`
	RTSPURL    string    `json:"rtsp_url"`
	ExpiresAt  time.Time `json:"expires_at"`
	Transcoded bool      `json:"transcoded"`
	Backend    string    `json:"backend"`
}

func NewManager(cfg MgrConfig, deps Deps) *Manager {
	if cfg.MediaReadyTO == 0 {
		cfg.MediaReadyTO = 5 * time.Second
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 5 * time.Minute
	}
	if cfg.RefreshExt == 0 {
		cfg.RefreshExt = 2 * time.Minute
	}
	if cfg.PublishTTL == 0 {
		cfg.PublishTTL = 60 * time.Second
	}
	if cfg.IdleStop == 0 {
		cfg.IdleStop = 10 * time.Second
	}
	if cfg.Tick == 0 {
		cfg.Tick = time.Second
	}
	if deps.Clock == nil {
		deps.Clock = realClock{}
	}
	m := &Manager{
		cfg: cfg, deps: deps,
		cmd: make(chan *cmd, 64), stopCh: make(chan struct{}), done: make(chan struct{}),
		stats: map[string]proto.PublishStat{}, lastSeen: map[string]time.Time{},
	}
	go m.loop()
	return m
}

// Close — loop'ni to'xtatadi (graceful shutdown).
func (m *Manager) Close() {
	close(m.stopCh)
	<-m.done
}

// --- tashqi API (bular loop'ga buyruq yuboradi) ---

// Start — sessiya ochish. camera to'liq registry yozuvi (handler oladi).
func (m *Manager) Start(cam registry.Camera, userID int64, profile, kind string) (*StartResult, error) {
	res := m.submit(&cmd{op: "start", cam: cam, userID: userID, profile: profile, kind: kind})
	if res.err != nil {
		return nil, res.err
	}
	return res.res, nil
}

// Refresh — TTL uzaytirish (media_refresh + token extend).
func (m *Manager) Refresh(sessionID string, extendS int) error {
	return m.submit(&cmd{op: "refresh", sessionID: sessionID, extendS: extendS}).err
}

// Stop — sessiyani yopish (user/close/ttl reason'lari).
func (m *Manager) Stop(sessionID, reason string) error {
	return m.submit(&cmd{op: "stop", sessionID: sessionID, reason: reason}).err
}

// OnMediaReady/Stopped/Stats — wsagent.MediaSink (hub goroutine'laridan
// chaqiriladi; loop'ga uzatamiz).
func (m *Manager) OnMediaReady(agentID string, mm proto.MediaReady) {
	m.submit(&cmd{op: "ready", agentID: agentID, ready: &mm})
}
func (m *Manager) OnMediaStopped(agentID string, mm proto.MediaStopped) {
	m.submit(&cmd{op: "stopped", agentID: agentID, stopped: &mm})
}
func (m *Manager) OnMediaStats(agentID string, mm proto.MediaStats) {
	m.mu.Lock()
	for _, p := range mm.Publishes {
		m.stats[p.PublishID] = p
		m.lastSeen[p.PublishID] = m.deps.Clock.Now()
	}
	m.mu.Unlock()
	m.submit(&cmd{op: "stats"})
}

// PublishStats — UI holat paneli uchun (S3).
func (m *Manager) PublishStats() map[string]proto.PublishStat {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]proto.PublishStat, len(m.stats))
	for k, v := range m.stats {
		out[k] = v
	}
	return out
}

type cmd struct {
	op        string // start|refresh|stop|ready|stopped|tick|stats
	cam       registry.Camera
	userID    int64
	profile   string
	kind      string
	sessionID string
	reason    string
	extendS   int
	agentID   string
	ready     *proto.MediaReady
	stopped   *proto.MediaStopped
	res       *StartResult
	err       error
	done      chan struct{}
}

func (m *Manager) submit(c *cmd) *cmd {
	c.done = make(chan struct{})
	m.cmd <- c // loop doim o'qiydi; MediaMTX chaqruvlari timeout'li (3 s)
	<-c.done
	return c
}

// --- ichki holat (faqat loop goroutine'sida) ---

type publish struct {
	id        string
	publishID string // agent media_ready'da bildirgan ID
	agentID   string
	cameraID  string
	path      string // cam/... yoki tc/...
	profile   string
	ingest    string
	mode      string // direct | server_tc
	token     string // publish token (agent/ffmpeg uchun)
	refcount  int
	sessions  []string
	ready     bool
	started   time.Time
	idleOn    bool      // refcount=0 bo'lsa — idle hisob faol (IdleStop=0 ham)
	stopAt    time.Time // idle hisob tugash vaqti
	readyTO   time.Time
	stopSent  bool
}

type session struct {
	id         string
	cameraID   string
	agentID    string
	userID     int64
	profile    string
	kind       string
	state      string // pending|publishing|playing|stopping|ended|failed
	pubID      string
	readerPath string // qaysi path'dan o'qiydi (server_tc bo'lsa tc/...)
	readToken  string
	expiresAt  time.Time
	transcoded bool
	backend    string
}

type loopState struct {
	sessions map[string]*session
	pubs     map[string]*publish
	byKey    map[string]string // "camID|profile|mode" → pubID
}

func (m *Manager) loop() {
	defer close(m.done)
	st := &loopState{
		sessions: map[string]*session{},
		pubs:     map[string]*publish{},
		byKey:    map[string]string{},
	}
	tk := time.NewTicker(m.cfg.Tick)
	defer tk.Stop()
	for {
		select {
		case <-m.stopCh:
			m.shutdownAll(st)
			return
		case c := <-m.cmd:
			m.handle(st, c)
			close(c.done)
		case <-tk.C:
			m.handle(st, &cmd{op: "tick"})
		}
	}
}
