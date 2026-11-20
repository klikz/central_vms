// Package devserver — mock vmsd (docs/protocol.md §18). Server tayyor
// bo'lmaganda agentning C1–C3 qismlarini tekshirish uchun ishlatiladi.
//
// Qamrov: enroll (§3.1), WS handshake (§3.3, §4.1), keyinchalik cams/media/codec.
package devserver

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"central-vms/client/internal/proto"
)

// Options — mock sozlamalari.
type Options struct {
	AuthTimeout time.Duration // auth/hello uchun limit (default 5 s, §3.3)
	MinVersion  int           // protocol_version oralig'i (§1)
	MaxVersion  int
	ServerVer   string

	// OnHello — agent hello yuborganda (testlar yuborilgan ma'lumotni tekshiradi).
	OnHello func(proto.Hello)
	// OnMessage — auth'dan keyin kelgan har bir xabar (hb, cams_sync, …).
	OnMessage func(agentID string, env proto.Envelope)
	// AutoCamsAck — cams_sync kelganda avtomatik cams_ack yuboradi (§4.3).
	// Testlarda qo'lda javob berish uchun false qilinadi (ack yo'qolsa retry).
	AutoCamsAck bool
	// AutoCamsEpoch — cams_sync epoch'ini solishtirish: true bo'lsa server
	// o'z saqlagan epochdan kichik/xato epoch'ni `stale_epoch` bilan rad etadi (§4.2).
	AutoCamsEpoch bool
	// OnClose — sessiya tugaganda (test sinxronizatsiyasi uchun).
	OnClose func(agentID string, code int)
	// PingInterval — server ping davri (§2 default 30 s; 0 → default).
	PingInterval time.Duration
	// StateFile — enroll qilingan agentlar saqlanadigan fayl (dev rejimi:
	// mock server qayta boshlansa agentlar yo'qolmasin). BO'SH bo'lsa xotirada.
	StateFile string
}

func (o *Options) withDefaults() {
	if o.AuthTimeout <= 0 {
		o.AuthTimeout = 5 * time.Second
	}
	if o.MinVersion == 0 {
		o.MinVersion = 1
	}
	if o.MaxVersion == 0 {
		o.MaxVersion = 1
	}
	if o.PingInterval <= 0 {
		o.PingInterval = 30 * time.Second
	}
	if o.ServerVer == "" {
		o.ServerVer = "devserver-mock"
	}
}

type stored struct {
	agentID string
	secret  string
}

// Server — holatli mock. Handler() orqali http.Handler beriladi.
type Server struct {
	o Options

	onWelcome func(agentID string) // skenariy hook'i (C1.14)

	mu    sync.Mutex
	byKey map[string]stored // join_key → agent (devserver avtomatik tasdiqlaydi)
	byID  map[string]string
	// camsSeen/camsEpoch — cams_sync hisobi (§4.2/§4.3, testlar uchun)
	camsSeen  map[string][]proto.CamsSync
	camsEpoch map[string]int64
	attempts  map[string][]time.Time
	online    []string              // auth'dan o'tgan agent_id'lar (testlar uchun)
	conns     map[string]*agentConn // faol ulanishlar (server tomonidan yuborish uchun)
}

func New(o Options) *Server {
	o.withDefaults()
	s := &Server{
		o:         o,
		byKey:     map[string]stored{},
		byID:      map[string]string{},
		attempts:  map[string][]time.Time{},
		conns:     map[string]*agentConn{},
		camsSeen:  map[string][]proto.CamsSync{},
		camsEpoch: map[string]int64{},
	}
	if o.StateFile != "" {
		s.loadState()
	}
	return s
}

// Handler — barcha marshrutlar.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "role": "devserver-mock"})
	})
	mux.HandleFunc("/api/agent/join", s.handleJoin)
	mux.HandleFunc("/api/agent/ws", s.handleWS)
	return mux
}

// OnlineAgents — auth'dan o'tgan agentlar (test tekshiruvi uchun).
func (s *Server) OnlineAgents() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.online...)
}

// handleJoin — §3.1: ulanish so‘rovi. Dev-mock server tasdiqni
// avtomatik bajaradi (haqiqiy serverda admin UI orqali tasdiqlanadi).
func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		JoinKey      string `json:"join_key"`
		AgentSecret  string `json:"agent_secret"`
		Hostname     string `json:"hostname"`
		Name         string `json:"name"`
		OS           string `json:"os"`
		LanIP        string `json:"lan_ip"`
		AgentVersion string `json:"agent_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json")
		return
	}
	if !s.allowRate(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	if len(in.JoinKey) < 32 || len(in.AgentSecret) < 32 {
		writeErr(w, http.StatusBadRequest, "bad_key")
		return
	}
	s.mu.Lock()
	if st, ok := s.byKey[in.JoinKey]; ok {
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"status": "approved", "agent_id": st.agentID})
		return
	}
	agentID := "ag_" + randHex(8)
	s.byKey[in.JoinKey] = stored{agentID: agentID, secret: in.AgentSecret}
	s.byID[agentID] = in.AgentSecret
	s.saveStateLocked()
	s.mu.Unlock()

	log.Printf("devserver: join OK agent_id=%s hostname=%s lan_ip=%s", agentID, in.Hostname, in.LanIP)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "approved", "agent_id": agentID, "request_id": 1,
	})
}

func (s *Server) secretFor(agentID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.byID[agentID]
	return v, ok
}

func (s *Server) markOnline(agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.online = append(s.online, agentID)
}

func (s *Server) allowRate(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	kept := s.attempts[ip][:0]
	for _, t := range s.attempts[ip] {
		if now.Sub(t) < 5*time.Minute {
			kept = append(kept, t)
		}
	}
	if len(kept) >= 10 {
		s.attempts[ip] = kept
		return false
	}
	s.attempts[ip] = append(kept, now)
	return true
}

func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		return strings.TrimSpace(strings.Split(xf, ",")[0])
	}
	return strings.Split(r.RemoteAddr, ":")[0]
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		log.Fatal(err)
	}
	const hexd = "0123456789abcdef"
	out := make([]byte, n*2)
	for i, v := range b {
		out[i*2] = hexd[v>>4]
		out[i*2+1] = hexd[v&0x0f]
	}
	return string(out)
}

func randToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		log.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// --- dev persistensiyasi (faqat mock uchun; haqiqiy serverda Postgres) ---

type persisted struct {
	Agents map[string]string    `json:"agents"` // agent_id → secret
	Joins  map[string]storedObj `json:"joins"`  // join_key → agent
}
type storedObj struct {
	AgentID string `json:"agent_id"`
	Secret  string `json:"secret"`
}

// loadState — fayldagi agentlarni xotiraga yuklaydi (fayl bo'lmasa jimgina qaytadi).
func (s *Server) loadState() {
	b, err := os.ReadFile(s.o.StateFile)
	if err != nil {
		return
	}
	var p persisted
	if json.Unmarshal(b, &p) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, secret := range p.Agents {
		s.byID[id] = secret
	}
	for key, st := range p.Joins {
		s.byKey[key] = stored{agentID: st.AgentID, secret: st.Secret}
	}
	log.Printf("devserver: %d agent yuklandi (%s)", len(p.Agents), s.o.StateFile)
}

// saveStateLocked — mu qflangan holda chaqiriladi.
func (s *Server) saveStateLocked() {
	if s.o.StateFile == "" {
		return
	}
	p := persisted{Agents: map[string]string{}, Joins: map[string]storedObj{}}
	for id, secret := range s.byID {
		p.Agents[id] = secret
	}
	for key, st := range s.byKey {
		p.Joins[key] = storedObj{AgentID: st.agentID, Secret: st.secret}
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	tmp := s.o.StateFile + ".tmp"
	if os.WriteFile(tmp, b, 0o600) != nil {
		return
	}
	_ = os.Rename(tmp, s.o.StateFile)
}

// handleCamsSync — kelgan cams_sync'ni hisoblab, kerak bo'lsa cams_ack yuboradi (§4.2/§4.3).
func (s *Server) handleCamsSync(agentID string, env proto.Envelope) {
	cs, err := proto.Decode[proto.CamsSync](env.Data)
	if err != nil {
		// mock: parse xatosini yopish o'rniga javob ham yubormaymiz — agent retry qiladi
		return
	}
	s.mu.Lock()
	s.camsSeen[agentID] = append(s.camsSeen[agentID], cs)
	stored := s.camsEpoch[agentID]
	if s.o.AutoCamsEpoch && cs.Epoch < stored {
		s.mu.Unlock()
		reason := "stale_epoch"
		_ = s.Send(agentID, proto.MsgCamsAck, proto.CamsAck{Epoch: cs.Epoch, OK: false, Reason: &reason})
		return
	}
	s.camsEpoch[agentID] = cs.Epoch
	applied := len(cs.Cameras)
	s.mu.Unlock()

	if s.o.AutoCamsAck {
		if err := s.Send(agentID, proto.MsgCamsAck, proto.CamsAck{Epoch: cs.Epoch, OK: true, Applied: applied}); err != nil {
			// Ulanish yopilgan — agent retry qiladi (§4.3), shuning uchun jimgina o'tamiz
			return
		}
	}
}

// CamsSyncSeen — agent yuborgan cams_sync chunk'lari (testlar uchun).
func (s *Server) CamsSyncSeen(agentID string) []proto.CamsSync {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]proto.CamsSync, len(s.camsSeen[agentID]))
	copy(out, s.camsSeen[agentID])
	return out
}

// SetCamsEpoch — serverning saqlagan epoch'i (stale_epoch sinovi uchun).
func (s *Server) SetCamsEpoch(agentID string, epoch int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.camsEpoch[agentID] = epoch
}

// Send — faol agentga server tomonidan xabar yuborish (cams_request, media_start,
// codec_fix, secret_rotate, … — C1.14/C2/C3 testlarida ishlatiladi).
// Yagona yozuvchi goroutine navbatiga qo'yadi (bir vaqtda ikki yozuv race bo'lardi).
// OnWelcome — welcome yuborilgandan keyin chaqiriladi (skenariy skripti, C1.14).
// Server so'ng media_start/config_push/secret_rotate kabilarni yuborishi mumkin.
func (s *Server) SetOnWelcome(fn func(agentID string)) {
	s.mu.Lock()
	s.onWelcome = fn
	s.mu.Unlock()
}

// --- C1.14: server→agent xabar yordamchilari (sketch sxemalari §5) ---

func (s *Server) SendMediaStart(agentID string, m proto.MediaStart) error {
	return s.Send(agentID, proto.MsgMediaStart, m)
}
func (s *Server) SendMediaStop(agentID string, m proto.MediaStop) error {
	return s.Send(agentID, proto.MsgMediaStop, m)
}
func (s *Server) SendMediaRefresh(agentID string, m proto.MediaRefresh) error {
	return s.Send(agentID, proto.MsgMediaRefresh, m)
}
func (s *Server) SendCodecFix(agentID string, m proto.CodecFix) error {
	return s.Send(agentID, proto.MsgCodecFix, m)
}
func (s *Server) SendConfigPush(agentID string, m proto.ConfigPush) error {
	return s.Send(agentID, proto.MsgConfigPush, m)
}
func (s *Server) SendSecretRotate(agentID string, m proto.SecretRotate) error {
	return s.Send(agentID, proto.MsgSecretRotate, m)
}

// CloseAfter — taymaut/uzilish simulyatsiyasi: agent d o'tgach kod bilan
// uziladi (§18: uzilish holatlari).
func (s *Server) CloseAfter(agentID string, d time.Duration, code int, reason string) {
	time.AfterFunc(d, func() {
		s.mu.Lock()
		ac := s.conns[agentID]
		s.mu.Unlock()
		if ac == nil || !ac.alive() {
			return
		}
		// Yopilish xabari yagona yozuvchi goroutine navbatiga qo'yiladi —
		// gorilla websocket'ga ikki gorindadan yozish race (§0.3).
		select {
		case ac.out <- outMsg{close: &[2]string{fmt.Sprint(code), reason}}:
		default:
			_ = ac.conn.Close() // navbat band — majburiy uzish ham yetarli
		}
	})
}

func (s *Server) Send(agentID, typ string, data any) error {
	env, err := proto.NewEnvelope(typ, 0, data)
	if err != nil {
		return err
	}
	s.mu.Lock()
	ac, ok := s.conns[agentID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("devserver: %s ulanishida emas", agentID)
	}
	// secret_rotate yuborsak, mock ham o'z bazasida yangilaydi (haqiqiy serverdek)
	if typ == proto.MsgSecretRotate {
		var sr proto.SecretRotate
		if json.Unmarshal(env.Data, &sr) == nil && sr.NewSecret != "" {
			s.mu.Lock()
			s.byID[agentID] = sr.NewSecret
			s.saveStateLocked()
			s.mu.Unlock()
		}
	}

	select {
	case ac.out <- outMsg{env: &env}:
		return nil
	case <-ac.done:
		return fmt.Errorf("devserver: %s ulanishi yopilgan", agentID)
	}
}

// WaitConnected — agent ulanishini qisqa kutish (testlar uchun).
//
// Eslatma: agent sessiyasi `RunOnce` da welcome'ni olgach darhol yopiladi,
// shuning uchun bu funksiya qisqa sinovlarda "ulanishni kutdim" deb yolg'on
// aytmasin — sessiya tugagan bo'lsa false qaytadi (chaqiruvchi o'zi hal qiladi).
func (s *Server) WaitConnected(agentID string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		ok := s.conns[agentID] != nil
		s.mu.Unlock()
		if ok {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// Connected — hozir ulanib turgan agentlar ro'yxati (test sinxronizatsiyasi).
func (s *Server) Connected() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.conns))
	for id := range s.conns {
		out = append(out, id)
	}
	return out
}

// agentConn — bitta ulanish + yagona yozuvchi goroutine navbati.
type agentConn struct {
	conn      *websocket.Conn
	out       chan outMsg
	done      chan struct{}
	aliveFlag atomic.Bool
}

type outMsg struct {
	env   *proto.Envelope
	ping  bool
	close *[2]string // [kod, sabab] — yagona yozuvchi orqali yopilish (race yo'q)
}

func (a *agentConn) alive() bool { return a.aliveFlag.Load() }

func (s *Server) setConn(agentID string, ac *agentConn) {
	s.mu.Lock()
	s.conns[agentID] = ac
	s.mu.Unlock()
}

func (s *Server) clearConn(agentID string) {
	s.mu.Lock()
	delete(s.conns, agentID)
	s.mu.Unlock()
}
