// UI WS ulanish kaliti (ticket) — access token query'da yurmasligi uchun
// (2026-11-21 server auditi: token proksi loglariga tushardi).
package httpapi

import (
	"net/http"

	"central-vms/server/internal/auth"
)

func (d *Deps) handleWSTicket(w http.ResponseWriter, r *http.Request) {
	if d.UI == nil {
		writeErr(w, http.StatusServiceUnavailable, "ui_offline", "")
		return
	}
	cl, ok := auth.ClaimsFrom(r)
	if !ok || cl == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "")
		return
	}
	tok, ttl := d.UI.MintTicket(cl)
	if tok == "" {
		writeErr(w, http.StatusInternalServerError, "internal", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ticket": tok, "ttl_s": int(ttl.Seconds())})
}
