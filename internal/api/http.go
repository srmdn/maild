package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/srmdn/maild/internal/auth"
	"github.com/srmdn/maild/internal/domain"
	"github.com/srmdn/maild/internal/domaincheck"
	"github.com/srmdn/maild/internal/sanitize"
	"github.com/srmdn/maild/internal/service"
)

type Handler struct {
	messages       *service.MessageService
	domains        *service.DomainService
	apiKeyHeader   string
	adminAPIKey    string
	operatorAPIKey string
	logger         *slog.Logger
}

func NewHandler(messages *service.MessageService, domains *service.DomainService, apiKeyHeader, adminAPIKey, operatorAPIKey string, logger *slog.Logger) *Handler {
	return &Handler{
		messages:       messages,
		domains:        domains,
		apiKeyHeader:   apiKeyHeader,
		adminAPIKey:    adminAPIKey,
		operatorAPIKey: operatorAPIKey,
		logger:         logger,
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
		"/v1/unsubscribes",
		withAPIKey(auth.RequireRole(auth.RoleAdmin)(h.createUnsubscribe)),
	)
	mux.HandleFunc(
		"/v1/smtp-accounts",
		withAPIKey(auth.RequireRole(auth.RoleAdmin)(h.upsertSMTPAccount)),
	)
	mux.HandleFunc(
		"/v1/domains/readiness",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.checkDomainReadiness)),
	)
	mux.HandleFunc(
		"/v1/smtp-accounts/validate",
		withAPIKey(auth.RequireRole(auth.RoleAdmin)(h.validateSMTPAccount)),
	)
	mux.HandleFunc(
		"/v1/messages/timeline",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.messageTimeline)),
	)
	mux.HandleFunc(
		"/v1/messages/logs",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.messageLogs)),
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
		h.logger.Warn("message_queue_failed", "workspace_id", req.WorkspaceID, "to_email", req.ToEmail, "reason", service.FormatQueueError(err))
		return
	}

	h.logger.Info("message_queued", "message_id", m.ID, "workspace_id", m.WorkspaceID, "to_email", m.ToEmail, "status", m.Status)
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
		h.logger.Warn("suppression_add_failed", "workspace_id", req.WorkspaceID, "email", req.Email)
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
	h.logger.Info("suppression_added", "workspace_id", req.WorkspaceID, "email", req.Email)
}

type createUnsubscribeRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	Email       string `json:"email"`
	Reason      string `json:"reason"`
}

func (h *Handler) createUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req createUnsubscribeRequest
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
		req.Reason = "user_unsubscribed"
	}

	if err := h.messages.AddUnsubscribe(r.Context(), req.WorkspaceID, req.Email, req.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		h.logger.Warn("unsubscribe_add_failed", "workspace_id", req.WorkspaceID, "email", req.Email)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"workspace_id": req.WorkspaceID,
		"email":        req.Email,
		"reason":       req.Reason,
		"status":       "unsubscribed",
	})
	h.logger.Info("unsubscribe_added", "workspace_id", req.WorkspaceID, "email", req.Email)
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
		h.logger.Warn("smtp_account_upsert_failed", "workspace_id", req.WorkspaceID, "name", req.Name, "host", req.Host)
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
	h.logger.Info("smtp_account_upserted", "workspace_id", req.WorkspaceID, "name", req.Name, "host", req.Host, "port", req.Port)
}

type checkDomainReadinessRequest struct {
	WorkspaceID  int64  `json:"workspace_id"`
	Domain       string `json:"domain"`
	DKIMSelector string `json:"dkim_selector"`
}

func (h *Handler) checkDomainReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req checkDomainReadinessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.WorkspaceID == 0 {
		req.WorkspaceID = 1
	}
	if strings.TrimSpace(req.Domain) == "" {
		http.Error(w, "domain is required", http.StatusBadRequest)
		return
	}

	result, err := h.domains.CheckReadiness(r.Context(), req.WorkspaceID, req.Domain, req.DKIMSelector)
	if err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		h.logger.Warn("domain_readiness_failed", "workspace_id", req.WorkspaceID, "domain", req.Domain)
		return
	}

	writeDomainReadinessJSON(w, http.StatusOK, result)
	h.logger.Info(
		"domain_readiness_checked",
		"workspace_id", req.WorkspaceID,
		"domain", result.Domain,
		"spf", result.SPFValid,
		"dkim", result.DKIMValid,
		"dmarc", result.DMARCValid,
		"ready", result.Ready,
	)
}

type validateSMTPAccountRequest struct {
	WorkspaceID int64 `json:"workspace_id"`
}

func (h *Handler) validateSMTPAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req validateSMTPAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.WorkspaceID == 0 {
		req.WorkspaceID = 1
	}

	provider, err := h.messages.ValidateSMTPAccount(r.Context(), req.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, sanitize.SMTPError(err.Error()))
		h.logger.Warn("smtp_account_validate_failed", "workspace_id", req.WorkspaceID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"workspace_id": req.WorkspaceID,
		"provider":     provider,
		"valid":        true,
	})
	h.logger.Info("smtp_account_validated", "workspace_id", req.WorkspaceID, "provider", provider)
}

func (h *Handler) messageTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	messageID, err := parseInt64Query(r, "message_id", 0)
	if err != nil || messageID == 0 {
		http.Error(w, "message_id is required", http.StatusBadRequest)
		return
	}

	message, attempts, err := h.messages.MessageTimeline(r.Context(), messageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message":  message,
		"attempts": attempts,
	})
}

func (h *Handler) messageLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspaceID, err := parseInt64Query(r, "workspace_id", 1)
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return
	}
	limitRaw := r.URL.Query().Get("limit")
	limit := 50
	if strings.TrimSpace(limitRaw) != "" {
		parsed, err := strconv.Atoi(limitRaw)
		if err != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = parsed
	}

	messages, err := h.messages.MessageLogs(r.Context(), workspaceID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"workspace_id": workspaceID,
		"count":        len(messages),
		"messages":     messages,
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

func writeDomainReadinessJSON(w http.ResponseWriter, status int, payload domaincheck.Result) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func parseInt64Query(r *http.Request, key string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}
