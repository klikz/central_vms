// Package camsvc — kamera CRUD va bulk amaliyotlar (docs/protocol.md §6, §16.3).
//
// state.json (statestore) + secrets.bin (keystore) bilan ishlaydi; har o'zgarishda
// cams_epoch o'sadi (§6.5) — serverga delta cams_sync yuborish shundan bilindi.
package camsvc

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/keystore"
	"central-vms/client/internal/statestore"
)

// ErrNotFound — kamera topilmadi (localui 404'ga map qiladi).
var ErrNotFound = errors.New("camera not found")

// Service — kamera operatsiyalari.
type Service struct {
	St *statestore.State
	KS *keystore.Store
	// OnChanged — ro‘yxat o‘zgarganda chaqiriladi: runtime darhol
	// cams_sync yuboradi (2026-11-20: avval o‘zgarish faqat qayta
	// ulanishda serverga yetib bordi, shuning uchun yangi kamera
	// serverda "ko‘rinmadi").
	OnChanged func()
}

// changed — o‘zgarish signalini beradi (nil xavfsiz).
func (s *Service) changed() {
	if s.OnChanged != nil {
		s.OnChanged()
	}
}

// New — xizmat quradi (ikkital ham majburiy).
func New(st *statestore.State, ks *keystore.Store) *Service {
	return &Service{St: st, KS: ks}
}

// List — barcha kameralar (position bo'yicha tartiblangan).
func (s *Service) List() []cams.Camera {
	out := make([]cams.Camera, len(s.St.Cameras))
	copy(out, s.St.Cameras)
	sortByPosition(out)
	return out
}

// Get — id bo'yicha.
func (s *Service) Get(id string) (cams.Camera, bool) {
	for _, c := range s.St.Cameras {
		if c.ID == id {
			return c, true
		}
	}
	return cams.Camera{}, false
}

// Create — yangi kamera: UUIDv4 agent tomonidan (§5), parol keystore'ga (§10.1).
func (s *Service) Create(c cams.Camera, secret string) (cams.Camera, error) {
	if c.User == "" {
		c.User = "admin" // §10.2: RTSP foydalanuvchisi standart "admin"
	}
	if c.ID == "" {
		c.ID = cams.NewID()
	}
	if _, exists := s.Get(c.ID); exists {
		return cams.Camera{}, fmt.Errorf("camera %s already exists", c.ID)
	}
	if err := c.Valid(); err != nil {
		return cams.Camera{}, err
	}
	if c.Position == 0 {
		c.Position = len(s.St.Cameras) + 1
	}
	if secret != "" {
		c.SecretRef = cams.SecretRefFor(c.ID, "main")
		s.KS.Put(c.SecretRef, secret)
	}
	s.St.Cameras = append(s.St.Cameras, c)
	s.St.BumpEpoch()
	if err := s.commit(); err != nil {
		return cams.Camera{}, err
	}
	s.changed()
	return c, nil
}

// Update — maydonlarni almashtirish (id o'zgarmaydi, §5).
// secret==nil → parol tegilmaydi; secret=="" → o'chiriladi; aks holda yangilanadi.
func (s *Service) Update(id string, patch cams.Camera, secret *string) (cams.Camera, error) {
	for i, c := range s.St.Cameras {
		if c.ID != id {
			continue
		}
		// id o'zgarmasligi kafolatlanadi
		patch.ID = c.ID
		if patch.Host == "" {
			patch.Host = c.Host
		}
		if err := patch.Valid(); err != nil {
			return cams.Camera{}, err
		}
		if secret != nil {
			ref := cams.SecretRefFor(id, "main")
			if *secret == "" {
				s.KS.Delete(ref)
				patch.SecretRef = ""
			} else {
				s.KS.Put(ref, *secret)
				patch.SecretRef = ref
			}
		} else {
			patch.SecretRef = c.SecretRef
		}
		if patch.Position == 0 {
			patch.Position = c.Position
		}
		if patch.User == "" {
			patch.User = c.User // tahrirda maydon bo'sh kelsa saqlanadi
		}
		s.St.Cameras[i] = patch
		s.St.BumpEpoch()
		if err := s.commit(); err != nil {
			return cams.Camera{}, err
		}
		s.changed()
		return patch, nil
	}
	return cams.Camera{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// SetStatus — kamera holatini yangilaydi (probe sikli natijasi).
// cams_epoch O'SMAYDI: holat agent-ichki kuzatuv (§12.2); serverga
// cam_status hodisasi alohida yuboriladi.
func (s *Service) SetStatus(id string, st cams.Status, at time.Time) error {
	for i := range s.St.Cameras {
		if s.St.Cameras[i].ID == id {
			s.St.Cameras[i].Status = st
			t := at
			s.St.Cameras[i].LastCheckAt = &t
			return s.St.Save()
		}
	}
	return fmt.Errorf("%w: %s", ErrNotFound, id)
}

// SetCodec — probe'dan olingan kodekni modelga yozadi va sync'ni
// harakatlantiradi (1.0-draft.12: kodek noma'lum bo'lsa agent H.265
// kamerani H.264 deb e'lon qilardi — oqim buzilar edi).
func (s *Service) SetCodec(id, main, sub string) error {
	for i := range s.St.Cameras {
		if s.St.Cameras[i].ID == id {
			changed := false
			if main != "" && s.St.Cameras[i].CodecMain != main {
				s.St.Cameras[i].CodecMain = main
				changed = true
			}
			if sub != "" && s.St.Cameras[i].CodecSub != sub {
				s.St.Cameras[i].CodecSub = sub
				changed = true
			}
			if !changed {
				return nil
			}
			if err := s.St.Save(); err != nil {
				return err
			}
			s.St.BumpEpoch()
			s.changed()
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Delete — bir nechta kamerani o'chirish; parollar ham tozalanadi (§10.1).
// O'chirilgan id qayta ishlatilmaydi (UUIDv4, §5).
func (s *Service) Delete(ids []string) int {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	kept := make([]cams.Camera, 0, len(s.St.Cameras))
	removed := 0
	for _, c := range s.St.Cameras {
		if want[c.ID] {
			removed++
			if c.SecretRef != "" {
				s.KS.Delete(c.SecretRef)
			}
			s.KS.Delete(cams.SecretRefFor(c.ID, "sub"))
			continue
		}
		kept = append(kept, c)
	}
	if removed == 0 {
		return 0
	}
	// §6.5.3: o'chirish cams_sync {op:"delete"} orqali serverga yetadi —
	// navbat state.json'da saqlanadi (restart'ga chidamli).
	var gone []string
	for _, c := range s.St.Cameras {
		if want[c.ID] {
			gone = append(gone, c.ID)
		}
	}
	s.St.AddPendingDelete(gone)
	s.St.Cameras = kept
	s.St.BumpEpoch()
	_ = s.commit()
	s.changed()
	return removed
}

// commit — keystore va state'ni ketma-ket saqlash (ikkisi ham atomik).
func (s *Service) commit() error {
	if err := s.KS.Save(); err != nil {
		return fmt.Errorf("secrets: %w", err)
	}
	if err := s.St.Save(); err != nil {
		return fmt.Errorf("state: %w", err)
	}
	return nil
}

func sortByPosition(list []cams.Camera) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].Position < list[j-1].Position; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// --- CSV (bulk add/import, §16.3) ---

// CSVReport — import natijasi.
type CSVReport struct {
	Created int      `json:"created"`
	Updated int      `json:"updated"`
	Errors  []string `json:"errors"`
}

// ImportCSV — qatorlarni o'qiydi: name,host,port,main_path,sub_path,enabled[,secret][,id]
// `id` bo'lsa mavjud kamera yangilanadi (bulk edit), bo'lmasa yangi yaratiladi.
func (s *Service) ImportCSV(r io.Reader, withSecrets bool) (CSVReport, error) {
	rep := CSVReport{Errors: []string{}}
	rd := csv.NewReader(r)
	rd.FieldsPerRecord = -1
	rows, err := rd.ReadAll()
	if err != nil {
		return rep, fmt.Errorf("cannot read CSV: %w", err)
	}
	if len(rows) == 0 {
		return rep, errors.New("empty CSV")
	}
	header := normalizeHeader(rows[0])
	for i, row := range rows[1:] {
		c, secret, id, err := rowToCamera(header, row, withSecrets)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("qator %d: %v", i+2, err))
			continue
		}
		if id != "" {
			// withSecrets=false bo'lsa parolga umuman tegilmaydi (nil),
			// aks holda bo'sh qator parolni o'chirish degani.
			secArg := (*string)(nil)
			if withSecrets {
				secArg = secPtr(secret)
			}
			if _, err := s.Update(id, c, secArg); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("qator %d: %v", i+2, err))
				continue
			}
			rep.Updated++
			continue
		}
		if _, err := s.Create(c, secret); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("qator %d: %v", i+2, err))
			continue
		}
		rep.Created++
	}
	return rep, nil
}

func secPtr(v string) *string { return &v }

func normalizeHeader(row []string) map[string]int {
	m := map[string]int{}
	for i, h := range row {
		m[strings.ToLower(strings.TrimSpace(h))] = i
	}
	return m
}

func field(h map[string]int, row []string, name string) string {
	i, ok := h[name]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func rowToCamera(h map[string]int, row []string, withSecrets bool) (cams.Camera, string, string, error) {
	var c cams.Camera
	c.Name = field(h, row, "name")
	c.Host = field(h, row, "host")
	if c.Host == "" {
		return c, "", "", errors.New("host is empty")
	}
	if p := field(h, row, "port"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return c, "", "", fmt.Errorf("port: %v", err)
		}
		c.Port = n
	}
	c.MainPath = field(h, row, "main_path")
	c.SubPath = field(h, row, "sub_path")
	c.Enabled = field(h, row, "enabled") != "false"
	secret := ""
	if withSecrets {
		secret = field(h, row, "secret")
	}
	// Valid() Create/Update ichida chaqiriladi (u yerdan ID generatsiya qilingandan keyin)
	id := field(h, row, "id")
	return c, secret, id, nil
}

// ExportCSV — kameralarni CSV ga yozish. withSecrets=false bo'lsa parol o'rniga
// `***` qo'yiladi (§16.3: eksportda parollar mask'lanadi).
func (s *Service) ExportCSV(w io.Writer, withSecrets bool) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"id", "name", "host", "port", "main_path", "sub_path", "enabled", "secret"}); err != nil {
		return err
	}
	for _, c := range s.List() {
		secret := ""
		if c.SecretRef != "" {
			if v, ok := s.KS.Get(c.SecretRef); ok && withSecrets {
				secret = v
			} else {
				secret = "***"
			}
		}
		rec := []string{c.ID, c.Name, c.Host, strconv.Itoa(c.Port), c.MainPath, c.SubPath,
			strconv.FormatBool(c.Enabled), secret}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
