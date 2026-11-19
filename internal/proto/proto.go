package proto

// Kontrakt tiplari — docs/protocol.md §4 (agent → server) va §5 (server → agent).
//
// Bu paketda MANTIQ YO'Q: faqat strukturalar va Decode. Kontrakt bilan sinxronlik
// golden testlar orqali kafolatlanadi (testdata/protocol/*.json — protocol.md
// misollaridan ko'chirilgan). Yangi maydon qo'shilganda: avval kontrakt, keyin
// fixture, keyin struktura (TASKS §0.2).
//
// Envelope: {"type": "...", "seq": 1, "ts": "...", "data": { ... }} (§2).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Envelope — barcha WS xabarlari uchun umumli qoplama (§2).
type Envelope struct {
	Type string          `json:"type"`
	Seq  int64           `json:"seq"`
	TS   time.Time       `json:"ts"`
	Data json.RawMessage `json:"data"`
}

// MsgType — qabul qilingan xabar turlari (§4, §5).
// ProtocolVersion — kontrakt versiyasi (§1: hozir 1). Agent hello'da yuboradi.
const ProtocolVersion = 1

const (
	// agent → server
	MsgAuth          = "auth"
	MsgHello         = "hello"
	MsgCamsSync      = "cams_sync"
	MsgCamStatus     = "cam_status"
	MsgCodecProbe    = "codec_probe"
	MsgMediaReady    = "media_ready"
	MsgMediaStopped  = "media_stopped"
	MsgMediaStats    = "media_stats"
	MsgHB            = "hb"
	MsgDiscoveryDone = "discovery_done"
	MsgSelftestDone  = "selftest_done"
	MsgSecretRotated = "secret_rotated"
	MsgCodecFixDone  = "codec_fix_done"
	MsgPong          = "pong"

	// server → agent
	MsgWelcome      = "welcome"
	MsgCamsRequest  = "cams_request"
	MsgCamsAck      = "cams_ack"
	MsgMediaStart   = "media_start"
	MsgMediaStop    = "media_stop"
	MsgMediaRefresh = "media_refresh"
	MsgCodecFix     = "codec_fix"
	MsgSecretRotate = "secret_rotate"
	MsgConfigPush   = "config_push"
	MsgError        = "error"
	MsgPing         = "ping"
)

// Decode — xabar `data` bo'lini turli strukturaga o'qiydi.
// Noma'lum maydonlar xato hisoblanadi: kontrakt va kod bir-biridan ortda qolmasin.
func Decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, fmt.Errorf("xabar %s: %w", typeName[T](), err)
	}
	return v, nil
}

func typeName[T any]() string {
	var v T
	return fmt.Sprintf("%T", v)
}

// Encode — struktura/pointerdan to'liq JSON (testlar va yuborish uchun).
// NewEnvelope — envelope'ni yig'adi: data JSON'ga kodlanadi, ts = hozir (UTC).
// Agent va devserver shu yordamchidan foydalanadi (§2).
func NewEnvelope(typ string, seq int64, data any) (Envelope, error) {
	raw, err := Encode(data)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Type: typ, Seq: seq, TS: time.Now().UTC(), Data: raw}, nil
}

func Encode(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("JSON kodlash: %w", err)
	}
	return b, nil
}

// --- §3.3 handshake ---

// Auth — ulanishdan keyin ≤5 s ichida yuboriladi.
type Auth struct {
	AgentID string `json:"agent_id"`
	Secret  string `json:"secret"`
}

// --- §4 agent → server ---

// Caps — qurilma imkoniyatlari (§4.1, caps probe natijasi).
type Caps struct {
	GPU           string `json:"gpu"` // nvenc|vaapi|amf|none
	CPUCores      int    `json:"cpu_cores"`
	RAMMB         int    `json:"ram_mb"`
	FFmpegVersion string `json:"ffmpeg_version"`
	HEVCDecode    bool   `json:"hevc_decode"`
}

// Hello — birinchi xabar (auth'dan keyin).
type Hello struct {
	ProtocolVersion int       `json:"protocol_version"`
	AgentVersion    string    `json:"agent_version"`
	OS              string    `json:"os"`
	Hostname        string    `json:"hostname"`
	LanIP           string    `json:"lan_ip"`
	BootID          string    `json:"boot_id"`
	StartedAt       time.Time `json:"started_at"`
	CamsEpoch       int64     `json:"cams_epoch"`
	Caps            Caps      `json:"caps"`
}

// CameraProfilePair — main/sub juftliklari uchun.
type CameraProfilePair struct {
	Main string `json:"main"`
	Sub  string `json:"sub"`
}

// CameraBoolPair — B-frame bayroqlari.
type CameraBoolPair struct {
	Main bool `json:"main"`
	Sub  bool `json:"sub"`
}

// CameraEntry — cams_sync'dagi bitta kamera (§4.2).
type CameraEntry struct {
	Op         string            `json:"op"` // upsert|delete
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	RTSPMasked string            `json:"rtsp_masked"`
	Profiles   CameraProfilePair `json:"profiles"`
	Codecs     CameraProfilePair `json:"codecs"`
	BFrame     CameraBoolPair    `json:"bframe"`
	OnvifOK    bool              `json:"onvif_ok"`
	Status     string            `json:"status"`
	Position   int               `json:"position"`
	Enabled    bool              `json:"enabled"`
}

// CamsSync — to'liq/delta kamera ro'yxati, chunk'lab (§4.2).
type CamsSync struct {
	Epoch      int64         `json:"epoch"`
	Full       bool          `json:"full"`
	SeqNo      int           `json:"seq_no"`
	ChunkCount int           `json:"chunk_count"`
	Done       bool          `json:"done"`
	Cameras    []CameraEntry `json:"cameras"`
}

// CamStatus — kamera holati o'zgarishi (§4.4).
type CamStatus struct {
	CameraID  string    `json:"camera_id"`
	Status    string    `json:"status"` // online|offline|error
	Error     string    `json:"error"`
	CheckedAt time.Time `json:"checked_at"`
	RTTMs     int64     `json:"rtt_ms"`
}

// CodecProbe — SDP tahlili natijasi (§4.5).
type CodecProbe struct {
	CameraID    string            `json:"camera_id"`
	Codecs      CameraProfilePair `json:"codecs"`
	BFrame      CameraBoolPair    `json:"bframe"`
	ProfilesSDP CameraProfilePair `json:"profiles_sdp"`
	OnvifSwitch bool              `json:"onvif_switch_ok"`
	Reason      *string           `json:"reason"`
}

// MediaReady — publish ochildi (§4.6).
type MediaReady struct {
	SessionID   string  `json:"session_id"`
	CameraID    string  `json:"camera_id"`
	Profile     string  `json:"profile"`
	Kind        string  `json:"kind"`
	PublishID   string  `json:"publish_id"`
	Path        string  `json:"path"`
	Ingest      string  `json:"ingest"`
	Transport   string  `json:"transport"`
	Transcoded  bool    `json:"transcoded"`
	Backend     string  `json:"backend"`
	FPS         float64 `json:"fps"`
	BitrateKbps int     `json:"bitrate_kbps"`
	BytesSent   int64   `json:"bytes_sent"`
	ReadyMs     int64   `json:"ready_ms"`
}

// MediaStopped — publish yopildi (§4.6, reason kodlari §13).
type MediaStopped struct {
	SessionID string `json:"session_id"`
	CameraID  string `json:"camera_id"`
	PublishID string `json:"publish_id"`
	Reason    string `json:"reason"`
}

// PublishStat — media_stats'dagi bitta faol publish (§4.6).
type PublishStat struct {
	PublishID     string  `json:"publish_id"`
	CameraID      string  `json:"camera_id"`
	BytesSent     int64   `json:"bytes_sent"`
	FPS           float64 `json:"fps"`
	BitrateKbps   int     `json:"bitrate_kbps"`
	RTTMs         int64   `json:"rtt_ms"`
	PacketLossPct float64 `json:"packet_loss_pct"`
	Transport     string  `json:"transport"`
}

// MediaStats — har 10 s da yuboriladi (§4.6).
type MediaStats struct {
	Publishes []PublishStat `json:"publishes"`
}

// HB — yengil holat heartbeat'i (§4.8).
type HB struct {
	UptimeS    int64 `json:"uptime_s"`
	Publishes  int   `json:"publishes"`
	MemMB      int   `json:"mem_mb"`
	CamsTotal  int   `json:"cams_total"`
	CamsOnline int   `json:"cams_online"`
}

// DiscoveredCamera — kashf etilgan qurilma (§4.7, §16.2).
type DiscoveredCamera struct {
	MAC          string            `json:"mac"`
	Serial       string            `json:"serial"`
	IP           string            `json:"ip"`
	Port         int               `json:"port"`
	HTTPPort     int               `json:"http_port"`
	Vendor       string            `json:"vendor"`
	Model        string            `json:"model"`
	OnvifXAddr   string            `json:"onvif_xaddr"`
	RTSPProbeOK  bool              `json:"rtsp_probe_ok"`
	Codecs       CameraProfilePair `json:"codecs"`
	AlreadyKnown bool              `json:"already_known"`
	AuthRequired bool              `json:"auth_required"`
}

// DiscoveryDone — skan natijalari (§4.7).
type DiscoveryDone struct {
	ScanID   string             `json:"scan_id"`
	Mode     string             `json:"mode"` // onvif|rtsp_scan
	TimeoutS int                `json:"timeout_s"`
	Found    []DiscoveredCamera `json:"found"`
	Errors   []string           `json:"errors"`
}

// SelftestCheck — bitta tekshiruv natijasi (§4.7).
type SelftestCheck struct {
	Name        string `json:"name"`
	OK          bool   `json:"ok"`
	MS          int64  `json:"ms"`
	BitrateKbps *int   `json:"bitrate_kbps,omitempty"`
}

// SelftestDone — self-test natijalari (§4.7).
type SelftestDone struct {
	ScanID string          `json:"scan_id"`
	Checks []SelftestCheck `json:"checks"`
}

// SecretRotated — secret_rotate javobi (§5.6).
type SecretRotated struct {
	OK bool `json:"ok"`
}

// CodecFixDone — codec_fix natijasi (§5.5).
type CodecFixDone struct {
	CameraID string  `json:"camera_id"`
	OK       bool    `json:"ok"`
	Reason   *string `json:"reason"`
}

// --- §5 server → agent ---

// Compat — versiya mosligi (§1).
type Compat struct {
	OK            bool    `json:"ok"`
	ServerVersion *string `json:"server_version,omitempty"`
	MinSupported  *int    `json:"min_supported,omitempty"`
	MaxSupported  *int    `json:"max_supported,omitempty"`
}

// Limits — agent uchun chegaralar (§4.1, §10.4).
type Limits struct {
	MaxUploads         *int `json:"max_uploads,omitempty"`
	MaxLocalTranscodes *int `json:"max_local_transcodes,omitempty"`
	MediaTTLS          *int `json:"media_ttl_s,omitempty"`
}

// Settings — probe/log sozlamalari (§4.1, §5.7).
type Settings struct {
	ProbePacingParallel *int    `json:"probe_pacing_parallel,omitempty"`
	CodecProbeIntervalH *int    `json:"codec_probe_interval_h,omitempty"`
	LogLevel            *string `json:"log_level,omitempty"`
}

// Welcome — auth'dan keyin server javobi (§4.1).
type Welcome struct {
	ServerVersion   string   `json:"server_version"`
	ProtocolVersion int      `json:"protocol_version"`
	Compat          Compat   `json:"compat"`
	LastSeq         int64    `json:"last_seq"`
	CamsEpoch       int64    `json:"cams_epoch"`
	Limits          Limits   `json:"limits"`
	Settings        Settings `json:"settings"`
	NeedsFullSync   bool     `json:"needs_full_sync"`
}

// CamsRequest — server so'raydi (§5.1).
type CamsRequest struct {
	Full      bool     `json:"full"`
	CameraIDs []string `json:"camera_ids,omitempty"`
}

// CamsAck — cams_sync tasdig'i (§4.3).
type CamsAck struct {
	Epoch   int64   `json:"epoch"`
	OK      bool    `json:"ok"`
	Applied int     `json:"applied"`
	Reason  *string `json:"reason"`
}

// PublishURLs — ingest URL'lari (§5.2).
type PublishURLs struct {
	WHIP   string `json:"whip"`
	RTSP   string `json:"rtsp"`
	RTSPTC string `json:"rtsp_tc"`
}

// Publish — token + URL'lar (§5.2, §7).
type Publish struct {
	Token      string      `json:"token"`
	URLs       PublishURLs `json:"urls"`
	HeaderName string      `json:"header_name"`
	QueryName  string      `json:"query_name"`
}

// TranscodeParams — maqsadli oqim parametrlari (§5.2).
type TranscodeParams struct {
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	FPS         int    `json:"fps"`
	BitrateKbps int    `json:"bitrate_kbps"`
	MaxrateKbps int    `json:"maxrate_kbps"`
	GopS        int    `json:"gop_s"`
	Profile     string `json:"profile"`
}

// Transcode — rejim va backend (§5.2).
type Transcode struct {
	Mode    string          `json:"mode"`    // auto|server|client|off
	By      string          `json:"by"`      // server|client|none
	Backend string          `json:"backend"` // gpu|cpu
	Params  TranscodeParams `json:"params"`
}

// MediaStart — publish boshlash buyrug'i (§5.2).
type MediaStart struct {
	SessionID string    `json:"session_id"`
	CameraID  string    `json:"camera_id"`
	Profile   string    `json:"profile"`
	Kind      string    `json:"kind"`
	TTLS      int       `json:"ttl_s"`
	Ingest    string    `json:"ingest"`
	Publish   Publish   `json:"publish"`
	Transcode Transcode `json:"transcode"`
	Reason    string    `json:"reason"`
}

// MediaStop — publish to'xtatish (§5.3).
type MediaStop struct {
	SessionID string `json:"session_id"`
	CameraID  string `json:"camera_id"`
	Reason    string `json:"reason"`
}

// MediaRefresh — publish TTL uzaytirish (§5.4).
type MediaRefresh struct {
	SessionID string `json:"session_id"`
	CameraID  string `json:"camera_id"`
	ExtendS   int    `json:"extend_s"`
}

// CodecFixTarget — ONVIF orqali profil sozlash maqsadi (§5.5).
type CodecFixTarget struct {
	Profile     string `json:"profile"`
	Codec       string `json:"codec"`
	NoBFrames   bool   `json:"no_bframes"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	FPS         int    `json:"fps"`
	BitrateKbps int    `json:"bitrate_kbps"`
	GopS        int    `json:"gop_s"`
}

// CodecFix — buyruq (§5.5).
type CodecFix struct {
	CameraID string         `json:"camera_id"`
	Target   CodecFixTarget `json:"target"`
	ApplyAll bool           `json:"apply_all"`
}

// SecretRotate — yangi secret (§5.6).
type SecretRotate struct {
	NewSecret string `json:"new_secret"`
	GraceS    int    `json:"grace_s"`
}

// ConfigPush — limit/sozlama yangilanishi (§5.7).
type ConfigPush struct {
	Limits   Limits   `json:"limits"`
	Settings Settings `json:"settings"`
}

// ErrorMsg — server xabari (§5.8).
type ErrorMsg struct {
	Code      string  `json:"code"`
	Message   string  `json:"message"`
	SessionID *string `json:"session_id,omitempty"`
	CameraID  *string `json:"camera_id,omitempty"`
}
