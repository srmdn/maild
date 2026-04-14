package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/srmdn/maild/internal/domain"
	"github.com/srmdn/maild/internal/store/postgres"
)

const (
	standardSessionTTL = 7 * 24 * time.Hour
	extendedSessionTTL = 30 * 24 * time.Hour
)

type AuthHandler struct {
	store          *postgres.Store
	sessionStore   *SessionStore
	cookieSecure   bool
	cookieDomain   string
	appEnv         string
	signupTemplate *template.Template
	loginTemplate  *template.Template
}

func NewAuthHandler(store *postgres.Store, sessionStore *SessionStore, appEnv string) *AuthHandler {
	secure := appEnv == "production"
	return &AuthHandler{
		store:        store,
		sessionStore: sessionStore,
		cookieSecure: secure,
		cookieDomain: "",
		appEnv:       appEnv,
	}
}

func (h *AuthHandler) SetTemplates(signup, login *template.Template) {
	h.signupTemplate = signup
	h.loginTemplate = login
}

type signupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	RememberMe bool   `json:"remember_me"`
}

type authResponse struct {
	User      domain.UserWithWorkspace `json:"user"`
	SessionID string                   `json:"-"`
}

func (h *AuthHandler) Signup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	password := strings.TrimSpace(req.Password)

	if email == "" || !isValidEmail(email) {
		writeError(w, http.StatusBadRequest, "valid email is required")
		return
	}
	if len(password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	exists, err := h.store.EmailExists(r.Context(), email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if exists {
		writeError(w, http.StatusConflict, "email already registered")
		return
	}

	hash := hashPassword(password)

	user, err := h.store.CreateUser(r.Context(), email, string(hash))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	workspaceName := strings.Split(email, "@")[0] + "'s workspace"
	workspaceID, err := h.store.CreateWorkspaceForUser(r.Context(), user.ID, workspaceName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workspace")
		return
	}

	sessionID, err := h.sessionStore.Create(r.Context(), user.ID, standardSessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	h.setSessionCookie(w, sessionID, int(standardSessionTTL/time.Second))

	resp := authResponse{
		User: domain.UserWithWorkspace{
			User:          user,
			WorkspaceID:   workspaceID,
			WorkspaceName: workspaceName,
			Role:          "admin",
		},
		SessionID: sessionID,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	password := strings.TrimSpace(req.Password)

	if email == "" || password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	user, err := h.store.GetUserByEmail(r.Context(), email)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	if !verifyPassword(password, user.PasswordHash) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	ttl := standardSessionTTL
	if req.RememberMe {
		ttl = extendedSessionTTL
	}

	sessionID, err := h.sessionStore.Create(r.Context(), user.ID, ttl)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	h.setSessionCookie(w, sessionID, int(ttl/time.Second))

	userWithWS, err := h.store.GetUserWorkspace(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get user workspace")
		return
	}

	resp := authResponse{
		User:      userWithWS,
		SessionID: sessionID,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID, err := h.getSessionID(r)
	if err == nil {
		_ = h.sessionStore.Delete(r.Context(), sessionID)
	}

	h.clearSessionCookie(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "logged out"})
}

func (h *AuthHandler) SignupPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.signupTemplate == nil {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := h.signupTemplate.Execute(&buf, nil); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.loginTemplate == nil {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := h.loginTemplate.Execute(&buf, nil); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID, err := h.getSessionID(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	userID, err := h.sessionStore.Get(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, ErrSessionExpired) {
			h.clearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, "session_expired")
			return
		}
		if errors.Is(err, ErrSessionNotFound) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	userWithWS, err := h.store.GetUserWorkspace(r.Context(), userID)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(userWithWS)
}

func (h *AuthHandler) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID, err := h.getSessionID(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		userID, err := h.sessionStore.Get(r.Context(), sessionID)
		if err != nil {
			if errors.Is(err, ErrSessionExpired) {
				h.clearSessionCookie(w)
				writeError(w, http.StatusUnauthorized, "session_expired")
				return
			}
			if errors.Is(err, ErrSessionNotFound) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			http.Error(w, "session error", http.StatusInternalServerError)
			return
		}

		userWithWS, err := h.store.GetUserWorkspace(r.Context(), userID)
		if err != nil {
			http.Error(w, "workspace not found", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userIDContextKey, userID)
		ctx = context.WithValue(ctx, workspaceIDContextKey, userWithWS.WorkspaceID)
		next(w, r.WithContext(ctx))
	}
}

func (h *AuthHandler) setSessionCookie(w http.ResponseWriter, sessionID string, maxAge int) {
	cookie := &http.Cookie{
		Name:     "maild_session",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
	http.SetCookie(w, cookie)
}

func (h *AuthHandler) clearSessionCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     "maild_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
	http.SetCookie(w, cookie)
}

func (h *AuthHandler) getSessionID(r *http.Request) (string, error) {
	cookie, err := r.Cookie("maild_session")
	if err != nil {
		return "", err
	}
	return cookie.Value, nil
}

func isValidEmail(email string) bool {
	return strings.Contains(email, "@") && len(email) >= 3
}

func hashPassword(password string) string {
	salt := "maild-salt-v1-"
	h := sha256.New()
	h.Write([]byte(salt + password))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func verifyPassword(password, hash string) bool {
	return hashPassword(password) == hash
}

const userIDContextKey ctxKey = "user_id"
const workspaceIDContextKey ctxKey = "workspace_id"

func UserIDFromContext(ctx context.Context) (int64, bool) {
	v := ctx.Value(userIDContextKey)
	id, ok := v.(int64)
	return id, ok
}

func WorkspaceIDFromContext(ctx context.Context) (int64, bool) {
	v := ctx.Value(workspaceIDContextKey)
	id, ok := v.(int64)
	return id, ok
}
