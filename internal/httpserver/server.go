package httpserver

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/srmdn/maild/internal/api"
	"github.com/srmdn/maild/internal/buildinfo"
	"github.com/srmdn/maild/internal/config"
	"github.com/srmdn/maild/internal/runtime"
)

type Server struct {
	http   *http.Server
	deps   *runtime.DependencyState
	static http.Handler
}

func New(cfg config.Config, logger *slog.Logger, deps *runtime.DependencyState, apiHandler *api.Handler, staticFS fs.FS) *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/healthz", handleHealth)
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		handleReady(w, r, deps)
	})
	apiHandler.Register(mux)

	if staticFS != nil {
		if err := api.LoadTemplates(staticFS); err != nil {
			logger.Warn("template loading failed", "err", err)
		} else {
			logger.Info("templates loaded successfully")
		}
		static, err := fs.Sub(staticFS, "web/static")
		if err == nil {
			mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
		}
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           withAccessLog(logger, mux),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
	}

	return &Server{http: srv, deps: deps}
}

func (s *Server) ListenAndServe() error {
	return s.http.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	type response struct {
		Name    string `json:"name"`
		Message string `json:"message"`
		Version string `json:"version"`
		TimeUTC string `json:"time_utc"`
	}

	writeJSON(w, http.StatusOK, response{
		Name:    "maild",
		Message: "lightweight outbound email operations platform",
		Version: buildinfo.Version,
		TimeUTC: time.Now().UTC().Format(time.RFC3339),
	})
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	type response struct {
		Status string `json:"status"`
	}
	writeJSON(w, http.StatusOK, response{Status: "ok"})
}

func handleReady(w http.ResponseWriter, _ *http.Request, deps *runtime.DependencyState) {
	type response struct {
		Status   string `json:"status"`
		Postgres bool   `json:"postgres"`
		Redis    bool   `json:"redis"`
	}
	postgres, redis, ready := deps.Snapshot()
	if !ready {
		writeJSON(w, http.StatusServiceUnavailable, response{
			Status:   "not_ready",
			Postgres: postgres,
			Redis:    redis,
		})
		return
	}
	writeJSON(w, http.StatusOK, response{
		Status:   "ready",
		Postgres: postgres,
		Redis:    redis,
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
