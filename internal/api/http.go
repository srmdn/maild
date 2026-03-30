package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/srmdn/maild/internal/auth"
	"github.com/srmdn/maild/internal/domain"
	"github.com/srmdn/maild/internal/sanitize"
	"github.com/srmdn/maild/internal/service"
)

type Handler struct {
	messages       *service.MessageService
	apiKeyHeader   string
	adminAPIKey    string
	operatorAPIKey string
}

func NewHandler(messages *service.MessageService, apiKeyHeader, adminAPIKey, operatorAPIKey string) *Handler {
	return &Handler{
		messages:       messages,
		apiKeyHeader:   apiKeyHeader,
		adminAPIKey:    adminAPIKey,
		operatorAPIKey: operatorAPIKey,
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	withAPIKey := func(next http.HandlerFunc) http.HandlerFunc {
		return auth.APIKeyMiddleware(h.apiKeyHeader, h.adminAPIKey, h.operatorAPIKey, next)
	}

	mux.HandleFunc(
		"/v1/messages",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.createMessage)),
	)
	mux.HandleFunc(
		"/v1/suppressions",
		withAPIKey(auth.RequireRole(auth.RoleAdmin)(h.createSuppression)),
	)
	mux.HandleFunc(
		"/v1/smtp-accounts",
		withAPIKey(auth.RequireRole(auth.RoleAdmin)(h.upsertSMTPAccount)),
	)
}

type createMessageRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	FromEmail   string `json:"from_email"`
	ToEmail     string `json:"to_email"`
	Subject     string `json:"subject"`
	BodyText    string `json:"body_text"`
}

func (h *Handler) createMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req createMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.WorkspaceID == 0 {
		req.WorkspaceID = 1
	}
	if strings.TrimSpace(req.FromEmail) == "" || strings.TrimSpace(req.ToEmail) == "" || strings.TrimSpace(req.Subject) == "" {
		http.Error(w, "from_email, to_email, and subject are required", http.StatusBadRequest)
		return
	}

	m, err := h.messages.QueueMessage(r.Context(), req.WorkspaceID, req.FromEmail, req.ToEmail, req.Subject, req.BodyText)
	if err != nil {
		if errors.Is(err, service.ErrBadRequest) {
			writeError(w, http.StatusBadRequest, "invalid recipient email")
			return
		}
		if errors.Is(err, service.ErrBlockedRecipientDomain) {
			writeError(w, http.StatusBadRequest, "recipient domain is blocked")
			return
		}
		if errors.Is(err, service.ErrRateLimited) {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		return
	}

	writeJSON(w, http.StatusCreated, m)
}

type createSuppressionRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	Email       string `json:"email"`
	Reason      string `json:"reason"`
}

func (h *Handler) createSuppression(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req createSuppressionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.WorkspaceID == 0 {
		req.WorkspaceID = 1
	}
	if strings.TrimSpace(req.Email) == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		req.Reason = "manual"
	}

	if err := h.messages.AddSuppression(r.Context(), req.WorkspaceID, req.Email, req.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"workspace_id": req.WorkspaceID,
		"email":        req.Email,
		"reason":       req.Reason,
		"status":       "suppressed",
	})
}

type upsertSMTPAccountRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	FromEmail   string `json:"from_email"`
}

func (h *Handler) upsertSMTPAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req upsertSMTPAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.WorkspaceID == 0 {
		req.WorkspaceID = 1
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = "default"
	}
	if strings.TrimSpace(req.Host) == "" || req.Port == 0 || strings.TrimSpace(req.FromEmail) == "" {
		http.Error(w, "host, port, and from_email are required", http.StatusBadRequest)
		return
	}

	err := h.messages.UpsertSMTPAccount(r.Context(), domain.SMTPAccount{
		WorkspaceID: req.WorkspaceID,
		Name:        req.Name,
		Host:        req.Host,
		Port:        req.Port,
		Username:    req.Username,
		Password:    req.Password,
		FromEmail:   req.FromEmail,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"workspace_id": req.WorkspaceID,
		"name":         req.Name,
		"host":         req.Host,
		"port":         req.Port,
		"from_email":   req.FromEmail,
		"status":       "saved_encrypted",
	})
}

func writeJSON(w http.ResponseWriter, status int, payload domain.Message) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
