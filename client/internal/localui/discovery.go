package localui

import (
	"encoding/json"
	"io"
	"net/http"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/discovery"
)

// Kashf etish ekranining API'si (docs/protocol.md §16.3, §17).
// Skan natijalari faqat lokal UI'da ko'rinadi — serverga yuborilmaydi (§16.4).
// Skan va qo'shish tarmoq/yozuv amali — PIN talab qilinadi (§10.3).

// DiscoveryService — skan boshqaruvi (agent tomonda real implementatsiya;
// nil bo'lsa endpoint'lar 501 qaytaradi).
type DiscoveryService interface {
	StartScan() error                 // skanni FONDA boshlaydi (darhol qaytadi)
	Progress() discovery.Progress     // jarayon holati (UI poll uchun)
	Results() []discovery.MergeResult // hozirgacha topilganlar (jonli o'sib boradi)
	AddSelected(keys []string, in discovery.BulkAddInput) (discovery.BulkAddReport, error)
}

// uiDevice — jadval uchun soddalashtirilgan ko'rinish (§16.2 ro'yxati).
type uiDevice struct {
	Key          string `json:"key"`
	IP           string `json:"ip"`
	Port         int    `json:"port"`
	Model        string `json:"model"`
	Name         string `json:"name"`
	MAC          string `json:"mac"`
	HasONVIF     bool   `json:"has_onvif"`
	AlreadyKnown bool   `json:"already_known"`
	ExistingID   string `json:"existing_id,omitempty"`
}

// addRequest — POST /api/ui/discovery/add jismi.
type addRequest struct {
	Keys        []string `json:"keys"`
	NamePattern string   `json:"name_pattern"`
	Password    string   `json:"password"`
	Enabled     bool     `json:"enabled"`
}

func (s *Server) discoveryRoutes() {
	s.mux.HandleFunc("POST /api/ui/discovery/scan", s.handleDiscoveryScan)
	s.mux.HandleFunc("GET /api/ui/discovery/results", s.handleDiscoveryResults)
	s.mux.HandleFunc("POST /api/ui/discovery/add", s.handleDiscoveryAdd)
}

func (s *Server) discoveryReady(w http.ResponseWriter) bool {
	if s.discovery == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "discovery_not_ready"})
		return false
	}
	return true
}

func (s *Server) handleDiscoveryScan(w http.ResponseWriter, r *http.Request) {
	if !s.discoveryReady(w) {
		return
	}
	if err := s.discovery.StartScan(); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "scan_failed", "detail": err.Error()})
		return
	}
	// 202: skan fonda yuradi — natijalar GET /results'dan jonli o'qiladi.
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
}

func (s *Server) handleDiscoveryResults(w http.ResponseWriter, r *http.Request) {
	if !s.discoveryReady(w) {
		return
	}
	res := s.discovery.Results()
	prog := s.discovery.Progress()
	devs := make([]uiDevice, 0, len(res))
	for _, m := range res {
		devs = append(devs, uiDevice{
			Key:          discovery.DeviceKey(m.Device),
			IP:           m.IP,
			Port:         m.Port,
			Model:        m.Model,
			Name:         m.Name,
			MAC:          m.MAC,
			HasONVIF:     len(m.XAddrs) > 0,
			AlreadyKnown: m.AlreadyKnown,
			ExistingID:   m.ExistingID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devs, "count": len(devs),
		"progress": prog})
}

func (s *Server) handleDiscoveryAdd(w http.ResponseWriter, r *http.Request) {
	if !s.discoveryReady(w) {
		return
	}
	var req addRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_json"})
		return
	}
	if len(req.Keys) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "keys_bo_sh"})
		return
	}
	rep, err := s.discovery.AddSelected(req.Keys, discovery.BulkAddInput{
		NamePattern: req.NamePattern,
		Password:    req.Password,
		Enabled:     req.Enabled,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "add_failed", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"added":  len(rep.Added),
		"failed": len(rep.Failed),
		"ids":    addedIDs(rep.Added),
		"errors": repFailed(rep.Failed),
	})
}

// addedIDs — muvaffaqiyatli qo'shilgan kamera id'lari.
func addedIDs(added []cams.Camera) []string {
	ids := make([]string, 0, len(added))
	for _, c := range added {
		ids = append(ids, c.ID)
	}
	return ids
}

// repFailed — xato hisobotini UI'ga ko'rsatish uchun soddalashtirish.
func repFailed(fails []discovery.BulkAddFailure) []map[string]string {
	out := make([]map[string]string, 0, len(fails))
	for _, f := range fails {
		out = append(out, map[string]string{"ip": f.Device.IP, "error": f.Err})
	}
	return out
}
