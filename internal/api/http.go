package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/srmdn/maild/internal/auth"
	"github.com/srmdn/maild/internal/domain"
	"github.com/srmdn/maild/internal/domaincheck"
	"github.com/srmdn/maild/internal/sanitize"
	"github.com/srmdn/maild/internal/service"
	"github.com/srmdn/maild/internal/webhooksig"
)

type Handler struct {
	messages               *service.MessageService
	domains                *service.DomainService
	apiKeyHeader           string
	adminAPIKey            string
	operatorAPIKey         string
	webhooksEnabled        bool
	webhookSignatureHeader string
	webhookTimestampHeader string
	webhookVerifier        *webhooksig.Verifier
	logger                 *slog.Logger
}

func NewHandler(
	messages *service.MessageService,
	domains *service.DomainService,
	apiKeyHeader, adminAPIKey, operatorAPIKey string,
	webhooksEnabled bool,
	webhookSignatureHeader, webhookTimestampHeader, webhookSigningSecret string,
	webhookMaxSkew time.Duration,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		messages:               messages,
		domains:                domains,
		apiKeyHeader:           apiKeyHeader,
		adminAPIKey:            adminAPIKey,
		operatorAPIKey:         operatorAPIKey,
		webhooksEnabled:        webhooksEnabled,
		webhookSignatureHeader: webhookSignatureHeader,
		webhookTimestampHeader: webhookTimestampHeader,
		webhookVerifier:        webhooksig.NewVerifier(webhookSigningSecret, webhookMaxSkew),
		logger:                 logger,
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
		"/v1/smtp-accounts/activate",
		withAPIKey(auth.RequireRole(auth.RoleAdmin)(h.activateSMTPAccount)),
	)
	mux.HandleFunc(
		"/v1/smtp-accounts/list",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.listSMTPAccounts)),
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
	mux.HandleFunc(
		"/v1/webhooks/logs",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.webhookLogs)),
	)
	mux.HandleFunc(
		"/v1/webhooks/replay",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.replayWebhookDeadLetters)),
	)
	mux.HandleFunc(
		"/v1/workspaces/policy",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.workspacePolicy)),
	)
	mux.HandleFunc(
		"/v1/analytics/summary",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.analyticsSummary)),
	)
	mux.HandleFunc(
		"/v1/analytics/export.csv",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.analyticsExportCSV)),
	)
	mux.HandleFunc(
		"/v1/billing/metering",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.billingMetering)),
	)
	mux.HandleFunc(
		"/ui/policy",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.workspacePolicyUI)),
	)
	if h.webhooksEnabled {
		mux.HandleFunc("/v1/webhooks/events", h.receiveWebhookEvent)
	}
}

type webhookEventRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	Type        string `json:"type"`
	Email       string `json:"email"`
	Reason      string `json:"reason"`
}

type replayWebhookDeadLettersRequest struct {
	WorkspaceID int64   `json:"workspace_id"`
	EventIDs    []int64 `json:"event_ids"`
	Limit       int     `json:"limit"`
}

func (h *Handler) receiveWebhookEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "invalid webhook payload", http.StatusBadRequest)
		return
	}

	if err := h.webhookVerifier.Verify(
		r.Header.Get(h.webhookTimestampHeader),
		r.Header.Get(h.webhookSignatureHeader),
		body,
		time.Now().UTC(),
	); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid webhook signature")
		h.logger.Warn("webhook_signature_invalid", "reason", err.Error())
		return
	}

	events, rejected, err := parseWebhookEvents(body)
	if err != nil {
		if _, dlqErr := h.messages.RecordWebhookDeadLetter(
			r.Context(),
			1,
			"unknown",
			"",
			"invalid_payload",
			err.Error(),
			string(body),
			1,
		); dlqErr != nil {
			writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(dlqErr))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":              "accepted",
			"processed_count":     0,
			"rejected_count":      1,
			"dead_lettered_count": 1,
			"total_count":         1,
		})
		h.logger.Warn("webhook_payload_dead_lettered", "reason", err.Error())
		return
	}

	processed := 0
	deadLettered := 0
	var firstAccepted webhookEventRequest
	rawPayload := string(body)

	for _, req := range events {
		if req.WorkspaceID == 0 {
			req.WorkspaceID = 1
		}

		eventType := strings.ToLower(strings.TrimSpace(req.Type))
		email := strings.TrimSpace(req.Email)
		if email == "" {
			rejected++
			continue
		}

		reason := strings.TrimSpace(req.Reason)
		event, err := h.messages.ProcessWebhookEvent(r.Context(), req.WorkspaceID, eventType, email, reason, rawPayload)
		if err != nil {
			writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
			h.logger.Warn("webhook_event_apply_failed", "type", eventType, "workspace_id", req.WorkspaceID, "email", email)
			return
		}
		if event.Status == "dead_letter" {
			deadLettered++
		}

		processed++
		if processed == 1 {
			firstAccepted = webhookEventRequest{
				WorkspaceID: req.WorkspaceID,
				Type:        eventType,
				Email:       email,
				Reason:      reason,
			}
		}
		h.logger.Info("webhook_event_processed", "type", eventType, "workspace_id", req.WorkspaceID, "email", email, "status", event.Status, "attempt_count", event.AttemptCount)
	}

	if rejected > 0 {
		rejectedWorkspaceID := int64(1)
		if len(events) > 0 && events[0].WorkspaceID > 0 {
			rejectedWorkspaceID = events[0].WorkspaceID
		}
		if _, err := h.messages.RecordWebhookDeadLetter(
			r.Context(),
			rejectedWorkspaceID,
			"unknown",
			"",
			"rejected_records",
			"webhook payload contained unsupported or incomplete records",
			rawPayload,
			1,
		); err != nil {
			writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
			return
		}
		deadLettered++
	}

	if processed == 0 {
		http.Error(w, "invalid webhook payload", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if processed == 1 && rejected == 0 {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":       "accepted",
			"workspace_id": firstAccepted.WorkspaceID,
			"type":         firstAccepted.Type,
			"email":        firstAccepted.Email,
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":              "accepted",
		"processed_count":     processed,
		"rejected_count":      rejected,
		"dead_lettered_count": deadLettered,
		"total_count":         processed + rejected,
	})
	if rejected > 0 {
		h.logger.Warn("webhook_event_partially_rejected", "processed", processed, "rejected", rejected)
	}
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

type activateSMTPAccountRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	Name        string `json:"name"`
}

func (h *Handler) activateSMTPAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req activateSMTPAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.WorkspaceID == 0 {
		req.WorkspaceID = 1
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	if err := h.messages.SetActiveSMTPAccount(r.Context(), req.WorkspaceID, req.Name); err != nil {
		writeError(w, http.StatusBadRequest, sanitize.HTTPInternalError(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"workspace_id": req.WorkspaceID,
		"name":         req.Name,
		"status":       "active",
	})
}

func (h *Handler) listSMTPAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceID, err := parseInt64Query(r, "workspace_id", 1)
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return
	}
	accounts, err := h.messages.SMTPAccounts(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"workspace_id": workspaceID,
		"count":        len(accounts),
		"accounts":     accounts,
	})
}

type upsertWorkspacePolicyRequest struct {
	WorkspaceID               int64    `json:"workspace_id"`
	RateLimitWorkspacePerHour int      `json:"rate_limit_workspace_per_hour"`
	RateLimitDomainPerHour    int      `json:"rate_limit_domain_per_hour"`
	BlockedRecipientDomains   []string `json:"blocked_recipient_domains"`
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

func (h *Handler) webhookLogs(w http.ResponseWriter, r *http.Request) {
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
	status := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("status")))

	events, err := h.messages.WebhookLogs(r.Context(), workspaceID, limit, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"workspace_id": workspaceID,
		"status":       status,
		"count":        len(events),
		"events":       events,
	})
}

func (h *Handler) replayWebhookDeadLetters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req replayWebhookDeadLettersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.WorkspaceID == 0 {
		req.WorkspaceID = 1
	}

	result, err := h.messages.ReplayWebhookDeadLetters(r.Context(), req.WorkspaceID, req.EventIDs, req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		h.logger.Warn("webhook_dead_letter_replay_failed", "workspace_id", req.WorkspaceID, "reason", err.Error())
		return
	}

	h.logger.Info(
		"webhook_dead_letter_replayed",
		"workspace_id", result.WorkspaceID,
		"requested", result.Requested,
		"replayed", result.Replayed,
		"failed", result.Failed,
		"source", result.ReplaySource,
	)
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) workspacePolicy(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		workspaceID, err := parseInt64Query(r, "workspace_id", 1)
		if err != nil {
			http.Error(w, "invalid workspace_id", http.StatusBadRequest)
			return
		}
		policy, err := h.messages.WorkspacePolicy(r.Context(), workspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(policy)
	case http.MethodPost:
		role, _ := auth.RoleFromContext(r.Context())
		if role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		var req upsertWorkspacePolicyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if req.WorkspaceID == 0 {
			req.WorkspaceID = 1
		}
		err := h.messages.UpsertWorkspacePolicy(r.Context(), domain.WorkspacePolicy{
			WorkspaceID:               req.WorkspaceID,
			RateLimitWorkspacePerHour: req.RateLimitWorkspacePerHour,
			RateLimitDomainPerHour:    req.RateLimitDomainPerHour,
			BlockedRecipientDomains:   req.BlockedRecipientDomains,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
			return
		}
		policy, err := h.messages.WorkspacePolicy(r.Context(), req.WorkspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(policy)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) workspacePolicyUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceID, err := parseInt64Query(r, "workspace_id", 1)
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return
	}
	policy, err := h.messages.WorkspacePolicy(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		return
	}

	joined := strings.Join(policy.BlockedRecipientDomains, ", ")
	html := `<!doctype html>
<html><head><meta charset="utf-8"><title>maild policy</title>
<style>body{font-family:ui-sans-serif,system-ui;margin:2rem;max-width:760px}code{background:#f2f2f2;padding:.1rem .3rem}input,textarea{width:100%;padding:.5rem;margin:.25rem 0 1rem}button{padding:.6rem 1rem}pre{background:#f8f8f8;padding:1rem;overflow:auto}</style>
</head><body>
<h1>Workspace Policy</h1>
<p>Workspace: <code>` + strconv.FormatInt(workspaceID, 10) + `</code></p>
<label>API Key (admin)</label>
<input id="apiKey" type="text" placeholder="change-me-admin" />
<form id="policyForm">
<label>Workspace Hourly Limit</label>
<input id="rateWorkspace" type="number" name="rate_limit_workspace_per_hour" value="` + strconv.Itoa(policy.RateLimitWorkspacePerHour) + `" />
<label>Domain Hourly Limit</label>
<input id="rateDomain" type="number" name="rate_limit_domain_per_hour" value="` + strconv.Itoa(policy.RateLimitDomainPerHour) + `" />
<label>Blocked Recipient Domains (comma-separated)</label>
<textarea id="blockedDomains" rows="4">` + joined + `</textarea>
<button type="submit">Save Policy</button>
</form>
<p id="status"></p>
<pre id="result"></pre>
<script>
const form = document.getElementById('policyForm');
form.addEventListener('submit', async (e) => {
  e.preventDefault();
  const apiKey = document.getElementById('apiKey').value.trim();
  const body = {
    workspace_id: ` + strconv.FormatInt(workspaceID, 10) + `,
    rate_limit_workspace_per_hour: Number(document.getElementById('rateWorkspace').value),
    rate_limit_domain_per_hour: Number(document.getElementById('rateDomain').value),
    blocked_recipient_domains: document.getElementById('blockedDomains').value.split(',').map(v => v.trim()).filter(Boolean)
  };
  const res = await fetch('/v1/workspaces/policy', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-API-Key': apiKey },
    body: JSON.stringify(body)
  });
  const text = await res.text();
  document.getElementById('status').textContent = 'HTTP ' + res.status;
  document.getElementById('result').textContent = text;
});
</script>
</body></html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

func (h *Handler) analyticsSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceID, err := parseInt64Query(r, "workspace_id", 1)
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return
	}
	from, to := parseFromTo(r)
	items, err := h.messages.MeteringSummary(r.Context(), workspaceID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"workspace_id": workspaceID,
		"from":         from.UTC().Format(time.RFC3339),
		"to":           to.UTC().Format(time.RFC3339),
		"items":        items,
	})
}

func (h *Handler) billingMetering(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceID, err := parseInt64Query(r, "workspace_id", 1)
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return
	}
	from, to := parseFromTo(r)
	items, err := h.messages.MeteringSummary(r.Context(), workspaceID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"workspace_id": workspaceID,
		"from":         from.UTC().Format(time.RFC3339),
		"to":           to.UTC().Format(time.RFC3339),
		"metering":     items,
	})
}

func (h *Handler) analyticsExportCSV(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceID, err := parseInt64Query(r, "workspace_id", 1)
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return
	}
	limit := 500
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	csvData, err := h.messages.ExportMessageLogsCSV(r.Context(), workspaceID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="maild_messages.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(csvData))
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
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

func parseFromTo(r *http.Request) (time.Time, time.Time) {
	now := time.Now().UTC()
	defaultFrom := now.Add(-24 * time.Hour)
	from := defaultFrom
	to := now
	if raw := strings.TrimSpace(r.URL.Query().Get("from")); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			from = t.UTC()
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("to")); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			to = t.UTC()
		}
	}
	if !to.After(from) {
		to = from.Add(24 * time.Hour)
	}
	return from, to
}
