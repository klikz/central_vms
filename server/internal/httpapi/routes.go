// Package httpapi — vmsd ning JSON API'si (docs/api.md; root §6).
// S1 qamrovi: auth, me, sites/clients/cameras o'qish, admin (users,
// enrollments, clients patch, cameras policy), audit. Play/sessions — S2.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"central-vms/server/internal/audit"
	"central-vms/server/internal/auth"
	"central-vms/server/internal/config"
	"central-vms/server/internal/media"
	"central-vms/server/internal/proto"
	"central-vms/server/internal/registry"
	"central-vms/server/internal/wsagent"
	"central-vms/server/internal/wsui"
)

// Deps — handler'larning bog'liqliklari (global o'zgaruvchi yo'q, TASKS §0.3).
type Deps struct {
	Cfg     config.Config
	Reg     *registry.Registry
	Tokens  *auth.Tokens
	Login   *auth.Rate // 5/5 min (root §9)
	Enroll  *auth.Rate // /api/agent/enroll rate-limit
	Audit   *audit.Log
	Enr     *wsagent.Enrollment
	Agent   *wsagent.Hub
	Media   *media.Manager // S2: sessiya menejeri (nil bo'lsa 503)
	MTXHook http.Handler   // /internal/auth (MediaMTX → vmsd)
	UI      *wsui.Hub      // S3: UI push kanali (/api/ui/ws)
	Lg      *slog.Logger

	// Mux — Routes() tomonidan o'rnatiladi (handler'larning ichki routeri).
	Mux http.Handler
}

// Routes — bitta mux: ommaviy endpoint'lar + JWT bilan himoyalangan guruhlar.
func Routes(d *Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/auth/login", d.handleLogin)
	mux.HandleFunc("POST /api/auth/refresh", d.handleRefresh)
	mux.HandleFunc("POST /api/auth/logout", d.handleLogout)
	mux.HandleFunc("POST /api/agent/enroll", d.handleEnroll)

	// Agent WS — hub o'zi handshake'ni boshqaradi (protocol.md §4/§18).
	mux.HandleFunc("/api/agent/ws", func(w http.ResponseWriter, r *http.Request) {
		d.Agent.ServeHTTP(w, r)
	})

	// UI push kanali (S3.0).
	if d.UI != nil {
		mux.Handle("/api/ui/ws", d.UI)
	}

	// MediaMTX auth-hook (protocol.md §15; MediaMTX 127.0.0.1:8189'ga yuboradi).
	if d.MTXHook != nil {
		mux.Handle("/internal/auth", d.MTXHook)
	}

	authMW := d.Tokens.Middleware
	mux.Handle("GET /api/me", authMW(http.HandlerFunc(d.handleMe)))
	mux.Handle("GET /api/sites", authMW(http.HandlerFunc(d.handleSites)))
	mux.Handle("GET /api/clients", authMW(http.HandlerFunc(d.handleClients)))
	mux.Handle("GET /api/clients/{id}/cameras", authMW(http.HandlerFunc(d.handleClientCameras)))
	mux.Handle("GET /api/cameras/{id}", authMW(http.HandlerFunc(d.handleCamera)))

	// Sessiyalar (S2.6).
	mux.Handle("POST /api/cameras/{id}/play", authMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.handleCameraPlay(w, r, "play")
	})))
	mux.Handle("POST /api/cameras/{id}/preview", authMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.handleCameraPlay(w, r, "preview")
	})))
	mux.Handle("POST /api/sessions/refresh", authMW(http.HandlerFunc(d.handleSessionRefresh)))
	mux.Handle("DELETE /api/sessions/{id}", authMW(http.HandlerFunc(d.handleSessionStop)))
	mux.Handle("GET /api/sessions", authMW(http.HandlerFunc(d.handleSessions)))

	// S1'da favorites/layouts o'qish/yozish (UI S3'da ulanadi).
	mux.Handle("GET /api/favorites", authMW(http.HandlerFunc(d.handleFavorites)))
	mux.Handle("POST /api/favorites/{cameraId}", authMW(http.HandlerFunc(d.handleFavoriteAdd)))
	mux.Handle("DELETE /api/favorites/{cameraId}", authMW(http.HandlerFunc(d.handleFavoriteDel)))
	mux.Handle("PUT /api/favorites/reorder", authMW(http.HandlerFunc(d.handleFavoritesReorder)))
	mux.Handle("GET /api/layouts", authMW(http.HandlerFunc(d.handleLayouts)))
	mux.Handle("POST /api/layouts", authMW(http.HandlerFunc(d.handleLayoutCreate)))
	mux.Handle("PUT /api/layouts/{id}", authMW(http.HandlerFunc(d.handleLayoutUpdate)))
	mux.Handle("DELETE /api/layouts/{id}", authMW(http.HandlerFunc(d.handleLayoutDelete)))
	mux.Handle("PUT /api/layouts/{id}/default", authMW(http.HandlerFunc(d.handleLayoutDefault)))

	admin := func(h http.HandlerFunc) http.Handler { return authMW(auth.RequireRole("admin", h)) }
	mux.Handle("GET /api/admin/users", admin(d.handleAdminUsers))
	mux.Handle("POST /api/admin/users", admin(d.handleAdminUserCreate))
	mux.Handle("PATCH /api/admin/users/{id}", admin(d.handleAdminUserPatch))
	mux.Handle("POST /api/admin/enrollments", admin(d.handleEnrollCreate))
	mux.Handle("PATCH /api/admin/clients/{id}", admin(d.handleClientPatch))
	mux.Handle("DELETE /api/admin/clients/{id}", admin(d.handleClientDelete))
	mux.Handle("PATCH /api/admin/cameras/{id}", admin(d.handleCameraPolicy))
	mux.Handle("POST /api/admin/clients/{id}/secret-rotate", admin(d.handleSecretRotate))
	mux.Handle("GET /api/admin/audit", admin(d.handleAudit))

	return mux
}

// --- yordamchilar ---

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, kind, detail string) {
	writeJSON(w, code, map[string]string{"error": kind, "detail": detail})
}

// regErr — registry xatolarini HTTP statusga aylantiradi.
func regErr(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case strings.Contains(err.Error(), registry.ErrNotFound.Error()):
		writeErr(w, http.StatusNotFound, "not_found", "")
	case strings.Contains(err.Error(), registry.ErrExists.Error()):
		writeErr(w, http.StatusConflict, "exists", "")
	case strings.Contains(err.Error(), registry.ErrBadInput.Error()):
		writeErr(w, http.StatusBadRequest, "bad_input", "")
	default:
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

// clientIP — proxy'siz ichki tarmoq: to'g'ridan-to'g'ri ulanish.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// pushLimits/protoSettings — agentga config_push uchun (protocol.md §5.7).
func pushLimits(c registry.Client) proto.Limits {
	mu, mt := c.MaxUploads, c.MaxTranscodes
	return proto.Limits{MaxUploads: &mu, MaxLocalTranscodes: &mt}
}

func protoSettings() proto.Settings { return proto.Settings{} }
