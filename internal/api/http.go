package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/srmdn/maild/internal/auth"
	"github.com/srmdn/maild/internal/domain"
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
		http.Error(w, "failed to queue message", http.StatusInternalServerError)
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
		http.Error(w, "failed to add suppression", http.StatusInternalServerError)
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

func writeJSON(w http.ResponseWriter, status int, payload domain.Message) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
