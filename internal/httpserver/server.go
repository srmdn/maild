package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/srmdn/maild/internal/api"
	"github.com/srmdn/maild/internal/auth"
	"github.com/srmdn/maild/internal/buildinfo"
	"github.com/srmdn/maild/internal/config"
	"github.com/srmdn/maild/internal/runtime"
)

type Server struct {
	http           *http.Server
	deps           *runtime.DependencyState
	authHandler    *auth.AuthHandler
	operatorAPIKey string
	staticFS       fs.FS
}

var (
	indexTemplate     *template.Template
	dashboardTemplate *template.Template
)

func New(cfg config.Config, logger *slog.Logger, deps *runtime.DependencyState, apiHandler *api.Handler, authHandler *auth.AuthHandler, staticFS fs.FS) *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/healthz", handleHealth)
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		handleReady(w, r, deps)
	})

	if authHandler != nil {
		mux.HandleFunc("/signup", authHandler.SignupPage)
		mux.HandleFunc("/login", authHandler.LoginPage)
		mux.HandleFunc("/logout", authHandler.Logout)
		mux.HandleFunc("/me", authHandler.Me)
		mux.HandleFunc("/api/v1/auth/signup", authHandler.Signup)
		mux.HandleFunc("/api/v1/auth/login", authHandler.Login)
		mux.HandleFunc("/dashboard", authHandler.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
			handleDashboard(w, r, authHandler, cfg.OperatorAPIKey)
		}))
	}

	apiHandler.Register(mux)

	if staticFS != nil {
		if err := api.LoadTemplates(staticFS); err != nil {
			logger.Warn("template loading failed", "err", err)
		} else {
			logger.Info("templates loaded successfully")
		}

		tmpl, err := loadPublicTemplates(staticFS)
		if err != nil {
			logger.Warn("public template loading failed", "err", err)
		} else {
			indexTemplate = tmpl["landing.html"]
			dashboardTemplate = tmpl["user_dashboard.html"]
			logger.Info("public templates loaded successfully")
		}

		static, err := fs.Sub(staticFS, "static")
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

	return &Server{
		http:           srv,
		deps:           deps,
		authHandler:    authHandler,
		operatorAPIKey: cfg.OperatorAPIKey,
		staticFS:       staticFS,
	}
}

func loadPublicTemplates(staticFS fs.FS) (map[string]*template.Template, error) {
	fsys, err := fs.Sub(staticFS, "templates")
	if err != nil {
		return nil, err
	}

	result := make(map[string]*template.Template)

	pages := []string{"landing.html", "user_dashboard.html", "signup.html", "login.html"}
	for _, page := range pages {
		tmpl := template.New(page)
		_, err := tmpl.ParseFS(fsys, page)
		if err != nil {
			return nil, err
		}
		result[page] = tmpl
	}

	return result, nil
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

	if r.Header.Get("Accept") != "" && !strings.Contains(r.Header.Get("Accept"), "text/html") {
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
		return
	}

	data := struct {
		Version string
	}{
		Version: buildinfo.Version,
	}

	var buf bytes.Buffer
	if err := indexTemplate.Execute(&buf, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func handleDashboard(w http.ResponseWriter, r *http.Request, authHandler *auth.AuthHandler, operatorAPIKey string) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	workspaceID, _ := auth.WorkspaceIDFromContext(r.Context())
	data := struct {
		UserID         int64
		WorkspaceID    int64
		OperatorAPIKey string
	}{
		UserID:         userID,
		WorkspaceID:    workspaceID,
		OperatorAPIKey: operatorAPIKey,
	}

	var buf bytes.Buffer
	if err := dashboardTemplate.Execute(&buf, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
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
