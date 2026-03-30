package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/srmdn/maild/internal/api"
	"github.com/srmdn/maild/internal/buildinfo"
	"github.com/srmdn/maild/internal/config"
)

type Server struct {
	http *http.Server
}

func New(cfg config.Config, logger *slog.Logger, apiHandler *api.Handler) *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/healthz", handleHealth)
	mux.HandleFunc("/readyz", handleReady)
	apiHandler.Register(mux)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           withAccessLog(logger, mux),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
	}

	return &Server{http: srv}
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

func handleReady(w http.ResponseWriter, _ *http.Request) {
	// Temporary readiness check for bootstrap phase.
	type response struct {
		Status string `json:"status"`
	}
	writeJSON(w, http.StatusOK, response{Status: "ready"})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
