package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"central-vms/server/internal/config"
	"central-vms/server/internal/httpapi"
	"central-vms/server/internal/version"
)

// newHTTPServer — bitta binary ichida: API/WS porti (cfg.Bind) va ichki
// auth-hook porti (MediaMTX → vmsd, protocol.md §15). S0'da faqat /healthz,
// statika va hook skeleti (501) mavjud.
func newHTTPServer(cfg config.Config, lg *slog.Logger, deps *httpapi.Deps) []*http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":             true,
			"version":        version.Version,
			"protocol":       protoVersion,
			"protocol_label": "1.0-draft.5",
		})
	})
	mux.Handle("/", staticHandler(cfg.WebDir, lg))
	if deps != nil && deps.Mux != nil {
		// API yo'llari statikadan oldin ulanadi (mux pattern'lari aniqroq).
		mux.Handle("/api/", deps.Mux)
	}

	hookMux := http.NewServeMux()
	hookMux.HandleFunc("/internal/auth", func(w http.ResponseWriter, _ *http.Request) {
		// S2: token→sessiya→path→user tekshiruvi (protocol.md §15).
		writeJSON(w, http.StatusNotImplemented, map[string]any{
			"error": "not_implemented", "detail": "auth-hook S2 bosqichida ishlaydi",
		})
	})

	srv := &http.Server{
		Addr:              cfg.Bind,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	hook := &http.Server{
		Addr:              cfg.MediaMTX.AuthHookBind,
		Handler:           hookMux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return []*http.Server{srv, hook}
}

// staticHandler — UI statikasi diskdan (embed YO'Q, root §4.1). build yo'q
// bo'lsa 503 + tushunarli matn (S3'da to'liq UI keladi).
func staticHandler(dir string, lg *slog.Logger) http.Handler {
	if _, err := os.Stat(dir); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "UI topilmadi (%s) — `make ui-build` bajarilganmi? (S3'da to'liq UI keladi)\n", dir)
		})
	}
	sub := os.DirFS(dir)
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/" {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p[1:]); err != nil {
			// SPA yo'llari (login/konsol) index.html'ga tushadi.
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func serveUntilSignal(ctx context.Context, lg *slog.Logger, cfg config.Config, servers []*http.Server) error {
	errCh := make(chan error, len(servers))
	for _, s := range servers {
		s := s
		ln, err := net.Listen("tcp", s.Addr)
		if err != nil {
			return fmt.Errorf("listen %s: %w", s.Addr, err)
		}
		lg.Info("tinglayapti", slog.String("addr", ln.Addr().String()))
		go func() {
			if err := s.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("serve %s: %w", s.Addr, err)
				return
			}
			errCh <- nil
		}()
	}

	select {
	case <-ctx.Done():
		lg.Info("to'xtatish signalini oldi — graceful shutdown")
	case err := <-errCh:
		if err != nil {
			return err
		}
	}
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		if err := s.Shutdown(sctx); err != nil {
			lg.Warn("shutdown", slog.String("addr", s.Addr), slog.Any("err", err))
		}
	}
	lg.Info("to'xtadi")
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
