package localui

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/camsvc"
	"central-vms/client/internal/config"
	"central-vms/client/internal/selftest"
)

//go:embed web
var webFS embed.FS

// favicon — brauzer /favicon.ico ni avtomatik so'raydi; 404 konsolda
// xato ko'rsatmasligi uchun SVG qaytariladi (§17).
var favicon = loadFavicon()

func loadFavicon() []byte {
	if b, err := webFS.ReadFile("web/favicon.svg"); err == nil {
		return b
	}
	return []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">` +
		`<rect width="32" height="32" rx="6" fill="#3b82f6"/></svg>`)
}

// Status — UI status chizig'i uchun holat (agent/publish modullari beradi).
type Status struct {
	Version      string `json:"version"`
	AgentID      string `json:"agent_id,omitempty"`
	ServerOnline bool   `json:"server_online"`
	CamsTotal    int    `json:"cams_total"`
	CamsOnline   int    `json:"cams_online"`
	Publishes    int    `json:"publishes"`
	CamsEpoch    int64  `json:"cams_epoch"`
	FFmpegOK     bool   `json:"ffmpeg_ok"`
	// §17 holat chizig'i: oxirgi heartbeat va oxirgi xato (redaktorlangan).
	// §1/C4.7: protokol versiyasi mos kelmasa — UI banneri.
	VersionMismatch bool   `json:"version_mismatch,omitempty"`
	ServerVersion   string `json:"server_version,omitempty"`
	ProtoMin        int    `json:"proto_min,omitempty"`
	ProtoMax        int    `json:"proto_max,omitempty"`
	LastHeartbeat   string `json:"last_heartbeat,omitempty"`
	LastError       string `json:"last_error,omitempty"`
	LastErrorAt     string `json:"last_error_at,omitempty"`
}

// Settings — UI'da ko'rinadigan/qayta yoziladigan sozlamalar (C1 davomida to'liq bo'ladi).
type Settings struct {
	ServerURL string `json:"server_url"`
	LogLevel  string `json:"log_level"`
}

// CamService — kamera CRUD (camsvc.Service bu interfeysni qanoatlantiradi).
// Nil bo'lsa /api/ui/cams* so'rovlari 501 qaytaradi (C2 gacha).
type CamService interface {
	List() []cams.Camera
	Create(c cams.Camera, secret string) (cams.Camera, error)
	Update(id string, patch cams.Camera, secret *string) (cams.Camera, error)
	Delete(ids []string) int
	ImportCSV(r io.Reader, withSecrets bool) (camsvc.CSVReport, error)
	ExportCSV(w io.Writer, withSecrets bool) error
}

// Deps — UI serveriga kirish bog'lamalari (kengaytirish oson bo'lsin).
type Deps struct {
	Status    func() Status
	LogLines  func(limit int) []string
	Cams      CamService
	Discovery DiscoveryService
	Snapshots SnapshotSource
	// C4.6: self-test paneli — Run barcha tekshiruvlarni bajaradi, Last
	// oxirgi natijalarni qaytaradi (ikkalari nil bo'lsa endpoint 501).
	SelftestRun  func(ctx context.Context) []selftest.Result
	SelftestLast func() []selftest.Result
}

// Server — lokal UI (:8082). Faqat 127.0.0.1 bind qilinishi shart (§10.3).
type Server struct {
	cfg          config.Config
	log          *slog.Logger
	version      string
	status       func() Status
	selftestRun  func(ctx context.Context) []selftest.Result
	selftestLast func() []selftest.Result
	logLines     func(limit int) []string
	cams         CamService
	discovery    DiscoveryService
	snap         SnapshotSource
	mux          *http.ServeMux
}

// NewServer — UI serverini quradi. Deps maydonlari nil bo'lsa bo'sh natija qaytariladi.
func NewServer(cfg config.Config, log *slog.Logger, version string, d Deps) (*Server, error) {
	if cfg.UIBind == "" {
		return nil, errors.New("localui: ui_bind bo'sh")
	}
	if d.Status == nil {
		d.Status = func() Status { return Status{Version: version} }
	}
	if d.LogLines == nil {
		d.LogLines = func(int) []string { return nil }
	}
	s := &Server{cfg: cfg, log: log, version: version, status: d.Status,
		selftestRun: d.SelftestRun, selftestLast: d.SelftestLast,
		logLines: d.LogLines, cams: d.Cams, discovery: d.Discovery, snap: d.Snapshots, mux: http.NewServeMux()}
	s.routes()
	return s, nil
}

func (s *Server) routes() {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		s.log.Error("statik fayllar o'qilmadi", "err", err)
		sub = fs.FS(embeddedFallback{})
	}
	fileServer := http.FileServer(http.FS(sub))

	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version})
	})
	s.mux.HandleFunc("/api/ui/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodErr(w)
			return
		}
		writeJSON(w, http.StatusOK, s.status())
	})
	s.mux.HandleFunc("/api/ui/settings", s.handleSettings)
	s.mux.HandleFunc("/api/ui/selftest", s.handleSelftest)
	s.camRoutes()
	s.discoveryRoutes()
	s.snapshotRoutes()

	// Jurnal ekrani (§17): oxirgi N ta YASHIRILGAN log qatori (logbuf buferi).
	s.mux.HandleFunc("/api/ui/log", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodErr(w)
			return
		}
		limit := 200
		if v := r.URL.Query().Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_limit"})
				return
			}
			if n > 1000 {
				n = 1000 // buferdan ham ko'p so'rasa maksimal 1000 qator (§17 zich ro'yxat)
			}
			limit = n
		}
		lines := s.logLines(limit)
		writeJSON(w, http.StatusOK, map[string]any{"lines": lines, "count": len(lines)})
	})

	// Favicon: brauzer avtomatik so'raydi — 404 konsolda chiqmasligi uchun.
	s.mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "max-age=86400")
		_, _ = w.Write(favicon)
	})

	// Noma'lum /api/* — HTML 404 emas, aniq JSON (xatoni darrov ko'rish uchun).
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "unknown_endpoint",
			"path":  redactPath(r.URL.Path),
		})
	})

	s.mux.HandleFunc("/", fileServer.ServeHTTP)
}

// redactPath — yo'lni loglanadigan qilib qaytaradi (so'roq qatoridagi token'ni olmaidi).
func redactPath(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i]
	}
	return p
}

// handleSelftest — POST: barcha tekshiruvlarni qayta bajar; GET: oxirgisi.
func (s *Server) handleSelftest(w http.ResponseWriter, r *http.Request) {
	if s.selftestRun == nil && s.selftestLast == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "self-test not configured"})
		return
	}
	var res []selftest.Result
	switch r.Method {
	case http.MethodPost:
		if s.selftestRun != nil {
			res = s.selftestRun(r.Context())
		}
	case http.MethodGet:
		if s.selftestLast != nil {
			res = s.selftestLast()
		}
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET or POST"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": res})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, Settings{ServerURL: s.cfg.ServerURL, LogLevel: s.cfg.LogLevel})
	case http.MethodPut, http.MethodPost:
		if !s.checkPin(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "pin_required"})
			return
		}
		var in Settings
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_json"})
			return
		}
		if in.ServerURL != "" {
			if !strings.HasPrefix(in.ServerURL, "ws://") && !strings.HasPrefix(in.ServerURL, "wss://") {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_server_url"})
				return
			}
			s.cfg.ServerURL = in.ServerURL
		}
		switch in.LogLevel {
		case "":
		case "debug", "info", "warn", "error":
			s.cfg.LogLevel = in.LogLevel
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_log_level"})
			return
		}
		writeJSON(w, http.StatusOK, Settings{ServerURL: s.cfg.ServerURL, LogLevel: s.cfg.LogLevel})
	default:
		methodErr(w)
	}
}

// checkPin — yozuv amallari uchun PIN (§10.3). Bo'sh PIN = talab qilinmaydi
// (faqat localhost bind holatida; config Validate buni tekshiradi).
func (s *Server) checkPin(r *http.Request) bool {
	if s.cfg.UIPin == "" {
		return true
	}
	got := r.Header.Get("X-Agent-Pin")
	if got == "" {
		if c, err := r.Cookie("vmsagent_pin"); err == nil {
			got = c.Value
		}
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.UIPin)) == 1
}

// Run — serverni ishga tushiradi va ctx bekor qilinganda mayin to'xtatadi (§9.4).
func (s *Server) Run(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("localui: %s da qulab bo'lmadi: %w", addr, err)
	}
	srv := &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("localui: to'xtatish: %w", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// javob yozib bo'lmadi — mijoz uzilgan, log darajasi debug
		_, _ = w.Write([]byte(`{"error":"encode_failed"}`))
	}
}

func methodErr(w http.ResponseWriter) {
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
}

// embeddedFallback — web/ papkasi topilmasa ham server ishlashi uchun (nodir holat).
type embeddedFallback struct{}

func (embeddedFallback) Open(name string) (fs.File, error)          { return nil, fs.ErrNotExist }
func (embeddedFallback) ReadDir(name string) ([]fs.DirEntry, error) { return nil, fs.ErrNotExist }
func (embeddedFallback) Glob(pattern string) ([]string, error)      { return nil, nil }
