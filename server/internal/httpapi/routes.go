// Package httpapi — vmsd ning JSON API'si (docs/api.md; root §6).
// S1 qamrovi: auth, me, sites/clients/cameras o'qish, admin (users,
// enrollments, clients patch, cameras policy), audit. Play/sessions — S2.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"reflect"

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
	Login   *auth.Rate // akkaunt+IP bo'yicha 5/5 min (root §9)
	LoginIP *auth.Rate // IP bo'yicha umumiy 40/5 min (akkaunt soni
	// bo'yicha tarqalgan brute-force'ni cheklash uchun; 2026-11-21 auditi)
	Audit   *audit.Log
	Join    *wsagent.Joiner // §3: ulanish so'rovlari (kod bekor qilindi)
	Agent   *wsagent.Hub
	Media   *media.Manager // S2: sessiya menejeri (nil bo'lsa 503)
	MTXHook http.Handler   // /internal/auth (MediaMTX → vmsd)
	UI      *wsui.Hub      // S3: UI push kanali (/api/ui/ws)
	Lg      *slog.Logger
	// HTTP — HTTP qatlam hisoblagichlari (metricsmw.go). newHTTPServer
	// Wrap orqali butun mux'ni o'raydi; /metrics Write orqali chiqaradi.
	HTTP *HTTPMetrics

	// Mux — Routes() tomonidan o'rnatiladi (handler'larning ichki routeri).
	Mux http.Handler
}

// Routes — bitta mux: ommaviy endpoint'lar + JWT bilan himoyalangan guruhlar.
func Routes(d *Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/auth/login", d.handleLogin)
	mux.HandleFunc("POST /api/auth/refresh", d.handleRefresh)
	mux.HandleFunc("POST /api/auth/logout", d.handleLogout)
	// §3.1: ulanish so'rovi (ochiq endpoint; IP rate-limit §3.1).
	mux.HandleFunc("POST /api/agent/join", d.handleJoin)

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
	// UI WS uchun bir martalik kalit (brauzer WS'ga header qo'yolmaydi).
	if d.UI != nil {
		mux.Handle("POST /api/ui/ws-ticket", authMW(http.HandlerFunc(d.handleWSTicket)))
	}
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
	mux.Handle("DELETE /api/admin/users/{id}", admin(d.handleUserDelete))
	// Ulanish so'rovlari va IP bo'yicha oldindan ruxsat (protocol.md §3.3).
	mux.Handle("GET /api/admin/joins", admin(d.handleJoinsList))
	mux.Handle("POST /api/admin/joins/{id}/approve", admin(d.handleJoinApprove))
	mux.Handle("POST /api/admin/joins/{id}/reject", admin(d.handleJoinReject))
	mux.Handle("DELETE /api/admin/joins/{id}", admin(d.handleJoinDelete))
	mux.Handle("GET /api/admin/preauth", admin(d.handlePreauthList))
	mux.Handle("POST /api/admin/preauth", admin(d.handlePreauthAdd))
	mux.Handle("DELETE /api/admin/preauth/{id}", admin(d.handlePreauthDelete))
	mux.Handle("PATCH /api/admin/clients/{id}", admin(d.handleClientPatch))
	mux.Handle("DELETE /api/admin/clients/{id}", admin(d.handleClientDelete))
	mux.Handle("PATCH /api/admin/cameras/{id}", admin(d.handleCameraPolicy))
	mux.Handle("POST /api/admin/clients/{id}/secret-rotate", admin(d.handleSecretRotate))
	mux.Handle("GET /api/admin/audit", admin(d.handleAudit))
	// api.md'da bor deb yozilgan, lekin umulqon register qilinmagan:
	mux.Handle("GET /api/admin/media/state", admin(d.handleMediaState))

	return mux
}

// --- yordamchilar ---

func writeJSON(w http.ResponseWriter, code int, v any) {
	// Bo'sh ro'yxat JSON'da `null` bo'lib chiqmasin — UI `.map()` qiladi.
	if v != nil {
		if rv := reflect.ValueOf(v); rv.Kind() == reflect.Slice && rv.IsNil() {
			v = reflect.MakeSlice(rv.Type(), 0, 0).Interface()
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, kind, detail string) {
	writeJSON(w, code, map[string]string{"error": kind, "detail": detail})
}

// fail — xatoni logga yozadi, mijozga faqat kod chiqadi. Ichki matn
// (SQLite xatolari, fayl yo'llari, SQL) mijozga chiqmasligi kerak
// (2026-11-21 server auditi).
func (d *Deps) fail(w http.ResponseWriter, code int, kind string, err error) {
	if d.Lg != nil && err != nil {
		if code >= 500 {
			d.Lg.Error(kind, "err", err)
		} else {
			d.Lg.Warn(kind, "err", err)
		}
	}
	writeErr(w, code, kind, "")
}

// regErr — registry xatolarini HTTP statusga aylantiradi.
// ESKISI: `strings.Contains(err.Error(), ...)` — ichki xato matnida
// "not found"/"exists" so'zi bo'lsa noto'g'ri status qaytardi va
// `internal` holatda SQLite matnini (fayl yo'llari, SQL) mijozga
// chiqarardi (2026-11-21 server auditi). Endi errors.Is + detail bo'sh,
// to'liq xato faqat logga.
func (d *Deps) regErr(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, registry.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "")
	case errors.Is(err, registry.ErrExists):
		writeErr(w, http.StatusConflict, "exists", "")
	case errors.Is(err, registry.ErrBadInput):
		writeErr(w, http.StatusBadRequest, "bad_input", "")
	default:
		if d.Lg != nil {
			d.Lg.Error("registry xatosi", "err", err)
		}
		writeErr(w, http.StatusInternalServerError, "internal", "")
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

// BodyLimit — so'rov tanasini qattiq cheklash. Barcha API so'rovlari
// kichik JSON (login, sessiya, admin CRUD); limit bo'lmasa ulkan
// `password` maydoni argon2id'ga (64 MB × 3) tushib CPU DoS bo'lardi
// va sekin body ulanishni abadiy ushlab turardi (2026-11-21 auditi).
func BodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		}
		next.ServeHTTP(w, r)
	})
}
