package localui

import (
	"context"
	"net/http"
	"time"

	"central-vms/client/internal/cams"
)

// Kamera preview (snapshot) endpoint'lari — UI talabi (2026-11-19):
// kamera qo'shilganda/ulanganda/holat o'zgarganda BIR marta rasm olinadi;
// rasmga bosilsa qayta yangilanadi. Doimiy oqim emas (§17 ga kiritilgan
// kengaytma).

// SnapshotSource — agent tomondagi snapshot ombori (preview paketi).
type SnapshotSource interface {
	File(id string) (string, bool) // JPEG fayl yo'li
	At(id string) *time.Time       // oxirgi yangilanish (nil → yo'q)
	Capture(ctx context.Context, id string) error
	Remove(id string)
}

func (s *Server) snapshotRoutes() {
	s.mux.HandleFunc("GET /api/ui/cams/{id}/snapshot", s.handleSnapshotGet)
	s.mux.HandleFunc("POST /api/ui/cams/{id}/snapshot", s.handleSnapshotRefresh)
}

func (s *Server) handleSnapshotGet(w http.ResponseWriter, r *http.Request) {
	if s.snap == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "snapshots_not_ready"})
		return
	}
	path, ok := s.snap.File(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no_snapshot"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
}

// handleSnapshotRefresh — rasmni bosganda: darhol yangi kadr oladi.
// Yozuv amali — PIN talab qilinadi (§10.3).
func (s *Server) handleSnapshotRefresh(w http.ResponseWriter, r *http.Request) {
	if s.snap == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "snapshots_not_ready"})
		return
	}
	id := r.PathValue("id")
	if err := s.snap.Capture(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "snapshot_failed", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"at": time.Now().UTC().Format(time.RFC3339)})
}

// camWithSnap — ro'yxat JSON'iga snapshot vaqtini qo'shadi (model o'zgarmaydi).
type camWithSnap struct {
	cams.Camera
	SnapshotAt *time.Time `json:"snapshot_at,omitempty"`
}

func (s *Server) decorateCams(list []cams.Camera) []camWithSnap {
	out := make([]camWithSnap, 0, len(list))
	for _, c := range list {
		var at *time.Time
		if s.snap != nil {
			at = s.snap.At(c.ID)
		}
		out = append(out, camWithSnap{Camera: c, SnapshotAt: at})
	}
	return out
}
