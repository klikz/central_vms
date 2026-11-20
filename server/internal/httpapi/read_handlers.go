// S1.14: o'qish/favorites/layouts endpoint'lari (viewer+r).
package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"central-vms/server/internal/auth"
)

func (d *Deps) handleSites(w http.ResponseWriter, r *http.Request) {
	sites, err := d.Reg.ListSites()
	if err != nil {
		regErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sites)
}

func (d *Deps) handleClients(w http.ResponseWriter, r *http.Request) {
	cs, err := d.Reg.ListClients()
	if err != nil {
		regErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

func (d *Deps) handleClientCameras(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_id", "")
		return
	}
	cams, err := d.Reg.ListCamerasByClient(id)
	if err != nil {
		regErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cams)
}

func (d *Deps) handleCamera(w http.ResponseWriter, r *http.Request) {
	cam, err := d.Reg.GetCamera(r.PathValue("id"))
	if err != nil {
		regErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cam)
}

func (d *Deps) handleFavorites(w http.ResponseWriter, r *http.Request) {
	cl, _ := auth.ClaimsFrom(r)
	ids, err := d.Reg.ListFavoriteCameraIDs(cl.Sub)
	if err != nil {
		regErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ids)
}

func (d *Deps) handleFavoriteAdd(w http.ResponseWriter, r *http.Request) {
	cl, _ := auth.ClaimsFrom(r)
	if err := d.Reg.AddFavorite(cl.Sub, r.PathValue("cameraId"), 0); err != nil {
		regErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *Deps) handleFavoriteDel(w http.ResponseWriter, r *http.Request) {
	cl, _ := auth.ClaimsFrom(r)
	if err := d.Reg.RemoveFavorite(cl.Sub, r.PathValue("cameraId")); err != nil {
		regErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *Deps) handleFavoritesReorder(w http.ResponseWriter, r *http.Request) {
	cl, _ := auth.ClaimsFrom(r)
	var ids []string
	if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", "")
		return
	}
	if err := d.Reg.ReorderFavorites(cl.Sub, ids); err != nil {
		regErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *Deps) handleLayouts(w http.ResponseWriter, r *http.Request) {
	cl, _ := auth.ClaimsFrom(r)
	ls, err := d.Reg.ListLayouts(cl.Sub)
	if err != nil {
		regErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ls)
}

type layoutBody struct {
	Name      string          `json:"name"`
	Grid      int             `json:"grid"`
	SlotsJSON json.RawMessage `json:"slots"`
}

func (b layoutBody) slotsStr() string {
	if len(b.SlotsJSON) == 0 {
		return "[]"
	}
	return string(b.SlotsJSON)
}

func (d *Deps) handleLayoutCreate(w http.ResponseWriter, r *http.Request) {
	cl, _ := auth.ClaimsFrom(r)
	var b layoutBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", "")
		return
	}
	id, err := d.Reg.CreateLayout(cl.Sub, b.Name, b.Grid, b.slotsStr())
	if err != nil {
		regErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (d *Deps) handleLayoutUpdate(w http.ResponseWriter, r *http.Request) {
	cl, _ := auth.ClaimsFrom(r)
	lid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_id", "")
		return
	}
	var b layoutBody
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &b); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_json", "")
			return
		}
	}
	if err := d.Reg.UpdateLayout(lid, cl.Sub, strPtr(b.Name), intPtr(b.Grid), slotsOrNil(b)); err != nil {
		regErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func slotsOrNil(b layoutBody) *string {
	if len(b.SlotsJSON) == 0 {
		return nil
	}
	s := b.slotsStr()
	return &s
}

func (d *Deps) handleLayoutDelete(w http.ResponseWriter, r *http.Request) {
	cl, _ := auth.ClaimsFrom(r)
	lid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_id", "")
		return
	}
	if err := d.Reg.DeleteLayout(lid, cl.Sub); err != nil {
		regErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *Deps) handleLayoutDefault(w http.ResponseWriter, r *http.Request) {
	cl, _ := auth.ClaimsFrom(r)
	lid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_id", "")
		return
	}
	if err := d.Reg.SetDefaultLayout(lid, cl.Sub); err != nil {
		regErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intPtr(i int) *int {
	if i == 0 {
		return nil
	}
	return &i
}
