// Package cams — kamera domeni: identifikator, holat, SDP tahlili (C2 davomida to'ldiriladi).
package cams

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Status — kamera holati (docs/protocol.md §10, §14).
type Status string

const (
	StatusUnknown Status = "unknown"
	StatusOnline  Status = "online"
	StatusOffline Status = "offline"
	StatusError   Status = "error"
	StatusMuted   Status = "muted"
)

// Camera — bitta kamera yozuvi (§3.2). Parol SAQLANMAYDI — faqat secret_ref (§10.1).
type Camera struct {
	ID       string `json:"id"` // agent generatsiya qilgan UUIDv4, o'zgarmas (§5)
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	MainPath string `json:"main_path"`
	SubPath  string `json:"sub_path,omitempty"`
	// User — RTSP autentifikatsiya foydalanuvchisi (1.0-draft.12: UI'da
	// maydon bor edi, lekin model/API yo'qqa chiqarardi va hardcode
	// "admin" ishlatilardi — admin bo'lmagan kamera ishlamasdi).
	User string `json:"user,omitempty"`

	OnvifXAddr string `json:"onvif_xaddr,omitempty"`
	OnvifOK    bool   `json:"onvif_ok"`             // ONVIF orqali sozlash mumkinmi (§4.2, C2.12)
	SecretRef  string `json:"secret_ref,omitempty"` // keystore'dagi ref (§10.1)

	Enabled  bool `json:"enabled"`
	Position int  `json:"position"`

	CodecMain  string `json:"codec_main,omitempty"`
	CodecSub   string `json:"codec_sub,omitempty"`
	BframeMain bool   `json:"bframe_main"`
	BframeSub  bool   `json:"bframe_sub"`

	Status Status `json:"status"`
	// So'roq vaqtlari: hech qachon bo'lmagan bo'lsa maydon umuman chiqmaydi
	// (UI'da "0001-01-01" ko'rinmasligi uchun).
	LastCheckAt  *time.Time `json:"last_check_at,omitempty"`
	CodecProbeAt *time.Time `json:"codec_probe_at,omitempty"`
}

// NewID — kamera identifikatori: UUIDv4, agent tomonidan bir marta generatsiya
// qilinadi va keyin O'ZGARMAYDI (docs/protocol.md §5, root §5).
func NewID() string { return uuid.NewString() }

// SecretRefFor — §10.1: "cam:<uuid>:main" / "cam:<uuid>:sub".
func SecretRefFor(id, profile string) string { return "cam:" + id + ":" + profile }

// Valid — minimal tekshiruv (UI CRUD va bulk add uchun).
func (c *Camera) Valid() error {
	if c.ID == "" {
		return errNoID
	}
	if c.Host == "" {
		return errNoHost
	}
	if c.Port == 0 {
		c.Port = 554 // RTSP default
	}
	if c.MainPath == "" {
		c.MainPath = "/cam/realmonitor?channel=1&subtype=0" // Dahua default (§16)
	}
	if c.Status == "" {
		c.Status = StatusUnknown
	}
	return nil
}

var (
	errNoID   = errors.New("kamera: id bo'sh")
	errNoHost = errors.New("kamera: host bo'sh")
)
