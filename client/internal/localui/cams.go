package localui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/camsvc"
)

// Kamera CRUD endpoint'lari (docs/protocol.md §17.1, §6).
// Yozuv amallari PIN talab qiladi (§10.3).

// camRequest — POST/PUT jismi.
type camRequest struct {
	ID       string  `json:"id,omitempty"`
	Name     string  `json:"name"`
	Host     string  `json:"host"`
	Port     int     `json:"port,omitempty"`
	MainPath string  `json:"main_path,omitempty"`
	SubPath  string  `json:"sub_path,omitempty"`
	User     string  `json:"user,omitempty"` // RTSP foydalanuvchisi (bo'sh → "admin")
	Enabled  *bool   `json:"enabled,omitempty"`
	Position int     `json:"position,omitempty"`
	Secret   *string `json:"secret,omitempty"` // nil → tegilmaydi, "" → o'chiriladi
}

func (s *Server) camRoutes() {
	s.mux.HandleFunc("GET /api/ui/cams", s.handleCamsList)
	s.mux.HandleFunc("POST /api/ui/cams", s.handleCamsCreate)
	s.mux.HandleFunc("PUT /api/ui/cams/{id}", s.handleCamsUpdate)
	s.mux.HandleFunc("DELETE /api/ui/cams", s.handleCamsDelete)
	s.mux.HandleFunc("POST /api/ui/cams/import", s.handleCamsImport)
	s.mux.HandleFunc("GET /api/ui/cams/export", s.handleCamsExport)
	s.mux.HandleFunc("POST /api/ui/cams/export-nonce", s.handleExportNonce)
}

// camsReady — xizmat ulanmagan bo'lsa 501 (§17.1).
func (s *Server) camsReady(w http.ResponseWriter) bool {
	if s.cams == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "cams_not_ready"})
		return false
	}
	return true
}

// nonceTTL — parol eksporti tasdiq nonce'ining yashash muddati.
const nonceTTL = 60 * time.Second

func (s *Server) handleCamsList(w http.ResponseWriter, r *http.Request) {
	if !s.camsReady(w) {
		return
	}
	list := s.cams.List()
	if list == nil {
		list = []cams.Camera{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"cams": s.decorateCams(list), "count": len(list)})
}

func (s *Server) handleCamsCreate(w http.ResponseWriter, r *http.Request) {
	if !s.camsReady(w) {
		return
	}
	req, err := decodeCamRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	created, err := s.cams.Create(reqToCamera(req), secretOf(req))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "create_failed", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleCamsUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.camsReady(w) {
		return
	}
	id := r.PathValue("id")
	req, err := decodeCamRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	updated, err := s.cams.Update(id, reqToCamera(req), req.Secret)
	if err != nil {
		if errors.Is(err, camsvc.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "update_failed", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleCamsDelete(w http.ResponseWriter, r *http.Request) {
	if !s.camsReady(w) {
		return
	}
	var in struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_json"})
		return
	}
	if len(in.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ids_empty"})
		return
	}
	n := s.cams.Delete(in.IDs)
	if s.snap != nil {
		for _, id := range in.IDs {
			s.snap.Remove(id)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

func (s *Server) handleCamsImport(w http.ResponseWriter, r *http.Request) {
	if !s.camsReady(w) {
		return
	}
	withSecrets := r.URL.Query().Get("with_secrets") == "1"
	defer r.Body.Close()
	rep, err := s.cams.ImportCSV(io.LimitReader(r.Body, 4<<20), withSecrets)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "csv_error", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// handleExportNonce — parol eksporti uchun bir martalik tasdiq oladi.
// UI/operator avval shu so'rovni yuboradi, keyin eksportda `nonce`
// bilan keladi. Bu qadam parollarni tasodifiy/avtomatik so'rov bilan
// chiqib ketishini oldini oladi (§10.2).
func (s *Server) handleExportNonce(w http.ResponseWriter, r *http.Request) {
	if !s.camsReady(w) {
		return
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "nonce_xato"})
		return
	}
	n := hex.EncodeToString(buf)
	s.nonceMu.Lock()
	if s.nonces == nil {
		s.nonces = map[string]time.Time{}
	}
	for k, at := range s.nonces { // eskirganlarni tozalash
		if time.Since(at) > nonceTTL {
			delete(s.nonces, k)
		}
	}
	s.nonces[n] = time.Now()
	s.nonceMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"nonce": n, "ttl_s": int(nonceTTL.Seconds())})
}

// consumeNonce — nonce'ni bir marta ishlatish uchun olish.
func (s *Server) consumeNonce(v string) bool {
	if v == "" {
		return false
	}
	s.nonceMu.Lock()
	defer s.nonceMu.Unlock()
	at, ok := s.nonces[v]
	if !ok {
		return false
	}
	delete(s.nonces, v)
	return time.Since(at) <= nonceTTL
}

func (s *Server) handleCamsExport(w http.ResponseWriter, r *http.Request) {
	if !s.camsReady(w) {
		return
	}
	withSecrets := r.URL.Query().Get("with_secrets") == "1"
	if withSecrets && !s.consumeNonce(r.URL.Query().Get("nonce")) {
		s.log.Warn("parol eksporti rad etildi: tasdiq nonce kerak",
			"maslahat", "avval POST /api/ui/cams/export-nonce so'rang")
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error":  "nonce_kerak",
			"detail": "parollarni eksport qilish uchun avval POST /api/ui/cams/export-nonce orqali bir martalik tasdiq oling",
		})
		return
	}
	if withSecrets {
		s.log.Warn("EKSPORT: barcha kamera parollari chiqarildi (with_secrets=1)")
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="cameras.csv"`)
	if err := s.cams.ExportCSV(w, withSecrets); err != nil {
		s.log.Error("CSV eksport xato", "err", err.Error())
	}
}

func decodeCamRequest(r *http.Request) (camRequest, error) {
	var req camRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, err2msg("bad_json: " + err.Error())
	}
	return req, nil
}

func reqToCamera(req camRequest) cams.Camera {
	c := cams.Camera{
		ID: req.ID, Name: req.Name, Host: req.Host, Port: req.Port,
		MainPath: req.MainPath, SubPath: req.SubPath, Position: req.Position,
		User:    req.User,
		Enabled: true,
	}

	if req.Enabled != nil {
		c.Enabled = *req.Enabled
	}
	return c
}

func secretOf(req camRequest) string {
	if req.Secret == nil {
		return ""
	}
	return *req.Secret
}

type err2msg string

func (e err2msg) Error() string { return string(e) }
