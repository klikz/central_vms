// Package statestore — state.json: atomik yozish/oadarish + cams_epoch (§3.2, §6).
//
// Parollar bu faylda SAQLANMAYDI — faqat secret_ref (§10.1).
// Yozish tartibi: state.json.tmp (0600) → sync → eski nusxa state.json.bak → rename.
package statestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"central-vms/client/internal/cams"
)

// Schema — state.json sxemasi versiyasi (§3.2). Maydon qo'shilsa 1 da qoladi,
// maydon o'chirilsa yoki ma'nosi o'zgarsa oshiriladi va migratsiya yoziladi.
const Schema = 1

// ErrSchema — fayl kelajak versiyasidan (agent eski) — ehtiyatkorlik bilan rad etamiz.
var ErrSchema = errors.New("state.json sxemasi bu agentdan yangi — agentni yangilang")

// State — state.json mazmuni.
type State struct {
	Schema    int    `json:"schema"`
	ServerURL string `json:"server_url"`
	AgentID   string `json:"agent_id,omitempty"`
	SecretRef string `json:"secret_ref,omitempty"` // keystore'dagi ref (§10.1)
	// JoinKey — §3.1: agentning doimiy ulanish kaliti (hex). So'rov
	// egalligini isbotlash uchun ishlatiladi; tasdiqdan keyin shu kalit
	// bilan agent_id so'raladi.
	JoinKey string `json:"join_key,omitempty"`
	// JoinStatus — oxirgi so'rov holati: pending | approved | rejected.
	JoinStatus string        `json:"join_status,omitempty"`
	CamsEpoch  int64         `json:"cams_epoch"`
	Cameras    []cams.Camera `json:"cameras"`
	// PendingDeletes — o'chirilgan kameralar id'si; serverga `cams_sync {op:"delete"}`
	// yetmagunicha saqlanadi (§6.5.3) — restart'dan keyin ham yo'qolmaydi.
	PendingDeletes []string `json:"pending_deletes,omitempty"`

	path string
	mu   sync.Mutex
}

// New — bo'sh holat (birinchi ishga tushish).
func New(path string) *State {
	return &State{Schema: Schema, path: path, Cameras: []cams.Camera{}}
}

// Load — state.json'ni o'qiydi. Bo'lmasa bo'sh holat qaytadi.
// Fayl buzilgan bo'lsa .bak dan tiklaydi (§6: "korrupt fayl tiklash").
func Load(path string) (*State, error) {
	st, err := readState(path)
	if errors.Is(err, fs.ErrNotExist) {
		return New(path), nil
	}
	if err == nil {
		return st, nil
	}
	// asosiy fayl buzilgan → .bak
	bak := path + ".bak"
	st2, err2 := readState(bak)
	if err2 == nil {
		st2.path = path
		return st2, fmt.Errorf("state.json buzilgan, .bak tiklandi: %w", err)
	}
	return nil, fmt.Errorf("state.json o'qilmadi: %w", err)
}

func readState(path string) (*State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}
	if s.Schema > Schema {
		return nil, fmt.Errorf("%w: fayl=%d, agent=%d", ErrSchema, s.Schema, Schema)
	}
	if s.Schema == 0 {
		s.Schema = Schema // eski/bo'sh fayl
	}
	if s.Cameras == nil {
		s.Cameras = []cams.Camera{}
	}
	normalizeCams(s.Cameras)
	s.path = path
	return &s, nil
}

// Save — atomik yozish. Bo'sh path bo'lsa xato.
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		return errors.New("statestore: path bo'sh")
	}
	s.Schema = Schema
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("statestore: json: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("statestore: papka: %w", err)
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("statestore: tmp: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return fmt.Errorf("statestore: yozish: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("statestore: sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("statestore: yopish: %w", err)
	}
	// Eski nusxa — korrupt bo'lsa tiklash uchun (§6)
	if _, err := os.Stat(s.path); err == nil {
		if err := os.Rename(s.path, s.path+".bak"); err != nil {
			return fmt.Errorf("statestore: .bak: %w", err)
		}
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("statestore: rename: %w", err)
	}
	return nil
}

// BumpEpoch — cams_epoch o'sadi va qaytaradi (§6.4: kamera ro'yxati o'zgarganda).
func (s *State) BumpEpoch() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CamsEpoch++
	return s.CamsEpoch
}

// SetCameras — ro'yxatni almashtiradi va epoch'ni o'stiradi.
func (s *State) SetCameras(list []cams.Camera) int64 {
	s.mu.Lock()
	s.Cameras = list
	s.mu.Unlock()
	return s.BumpEpoch()
}

// Touch — fayl muddatini yangilash uchun (bo'sh amaliyot, UI/monitoring).
func (s *State) Touched() time.Time { return time.Now().UTC() }

// normalizeCams — eski/nosoz yozuvlarni hozirgi shaklga keltiradi:
// status bo'sh bo'lsa "unknown", nol vaqt → nil (UI'da 0001-01-01 ko'rinmasin).
func normalizeCams(list []cams.Camera) {
	for i := range list {
		if list[i].Status == "" {
			list[i].Status = cams.StatusUnknown
		}
		if list[i].LastCheckAt != nil && list[i].LastCheckAt.IsZero() {
			list[i].LastCheckAt = nil
		}
		if list[i].CodecProbeAt != nil && list[i].CodecProbeAt.IsZero() {
			list[i].CodecProbeAt = nil
		}
	}
}

// AddPendingDelete — o'chirilgan id(lar)ni navbatga qo'shadi (takrorlanmaydi).
func (s *State) AddPendingDelete(ids []string) {
	// Chaqiruvchi (camsvc) qulfni ushlab turadi — shuning uchun bu yerda
	// qayta qulflamaymiz: State.mu sync.Mutex (reentrant emas).
	have := map[string]bool{}
	for _, id := range s.PendingDeletes {
		have[id] = true
	}
	for _, id := range ids {
		if id != "" && !have[id] {
			s.PendingDeletes = append(s.PendingDeletes, id)
			have[id] = true // bitta chaqiruv ichida ham takrorlanmasin
		}
	}
}

// ClearPendingDeletes — sync muvaffaqiyatli bo'lgach navbatni tozalash (§4.3).
func (s *State) ClearPendingDeletes() {
	// Qulf qo'yilmaydi: chaqiruvchi (agent.Syncer) Save() orqali qulf oladi;
	// sync.Mutex reentrant emas — shu yerda qulf deadlock bo'lardi.
	s.PendingDeletes = nil
}
