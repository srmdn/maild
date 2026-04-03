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
		"/v1/ops/onboarding-checklist",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.onboardingChecklist)),
	)
	mux.HandleFunc(
		"/v1/incidents/bundle",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.incidentBundleExport)),
	)
	mux.HandleFunc(
		"/v1/messages/logs",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.messageLogs)),
	)
	mux.HandleFunc(
		"/v1/messages/retry",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.retryMessages)),
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
	mux.HandleFunc(
		"/ui/logs",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.messageLogsUI)),
	)
	mux.HandleFunc(
		"/ui",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.operatorDashboardUI)),
	)
	mux.HandleFunc(
		"/ui/onboarding",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.onboardingUI)),
	)
	mux.HandleFunc(
		"/ui/incidents",
		withAPIKey(auth.RequireRole(auth.RoleAdmin, auth.RoleOperator)(h.incidentUI)),
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

type retryMessagesRequest struct {
	WorkspaceID int64   `json:"workspace_id"`
	MessageIDs  []int64 `json:"message_ids"`
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

func (h *Handler) onboardingChecklist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceID, err := parseInt64Query(r, "workspace_id", 1)
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return
	}

	checklist, err := h.messages.TechnicalOnboardingChecklist(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		return
	}

	domainName := strings.TrimSpace(r.URL.Query().Get("domain"))
	dkimSelector := strings.TrimSpace(r.URL.Query().Get("dkim_selector"))
	if domainName != "" {
		item := domain.OnboardingChecklistItem{
			ID:          "domain_readiness_checked",
			Title:       "Check Domain Readiness",
			Description: "SPF, DKIM, and DMARC readiness has been validated for the sending domain.",
			Done:        false,
			Action:      "POST /v1/domains/readiness",
		}
		result, readinessErr := h.domains.CheckReadiness(r.Context(), workspaceID, domainName, dkimSelector)
		if readinessErr != nil {
			item.Evidence = "domain readiness check failed"
		} else {
			item.Done = result.Ready
			item.Evidence = "spf=" + strconv.FormatBool(result.SPFValid) +
				", dkim=" + strconv.FormatBool(result.DKIMValid) +
				", dmarc=" + strconv.FormatBool(result.DMARCValid)
		}
		checklist.Items = append(checklist.Items, item)
		checklist.Total = len(checklist.Items)
		checklist.Completed = 0
		for _, it := range checklist.Items {
			if it.Done {
				checklist.Completed++
			}
		}
	}

	writeJSON(w, http.StatusOK, checklist)
}

func (h *Handler) incidentBundleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceID, err := parseInt64Query(r, "workspace_id", 1)
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return
	}
	messageID, err := parseInt64Query(r, "message_id", 0)
	if err != nil || messageID <= 0 {
		http.Error(w, "message_id is required", http.StatusBadRequest)
		return
	}

	bundle, err := h.messages.IncidentBundle(r.Context(), workspaceID, messageID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") || strings.Contains(strings.ToLower(err.Error()), "workspace mismatch") {
			writeError(w, http.StatusNotFound, "message not found for workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="maild_incident_bundle_message_`+strconv.FormatInt(messageID, 10)+`.json"`)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(bundle)
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

	from, to := parseFromToOptional(r)
	messages, err := h.messages.MessageLogs(r.Context(), workspaceID, limit, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"workspace_id": workspaceID,
		"from":         formatRFC3339OrEmpty(from),
		"to":           formatRFC3339OrEmpty(to),
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
	from, to := parseFromToOptional(r)
	events, err := h.messages.WebhookLogs(r.Context(), workspaceID, limit, status, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"workspace_id": workspaceID,
		"from":         formatRFC3339OrEmpty(from),
		"to":           formatRFC3339OrEmpty(to),
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

func (h *Handler) retryMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req retryMessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.WorkspaceID == 0 {
		req.WorkspaceID = 1
	}

	result, err := h.messages.RetryMessages(r.Context(), req.WorkspaceID, req.MessageIDs, req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, sanitize.HTTPInternalError(err))
		h.logger.Warn("message_retry_failed", "workspace_id", req.WorkspaceID, "reason", err.Error())
		return
	}

	h.logger.Info(
		"message_retry_completed",
		"workspace_id", result.WorkspaceID,
		"requested", result.Requested,
		"retried", result.Retried,
		"skipped", result.Skipped,
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
<p><a href="/ui?workspace_id=` + strconv.FormatInt(workspaceID, 10) + `">Dashboard</a> · <a href="/ui/logs?workspace_id=` + strconv.FormatInt(workspaceID, 10) + `">Logs</a> · <a href="/ui/onboarding?workspace_id=` + strconv.FormatInt(workspaceID, 10) + `">Onboarding</a> · <a href="/ui/incidents?workspace_id=` + strconv.FormatInt(workspaceID, 10) + `">Incidents</a></p>
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

func (h *Handler) messageLogsUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceID, err := parseInt64Query(r, "workspace_id", 1)
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return
	}
	html := `<!doctype html>
<html><head><meta charset="utf-8"><title>maild logs</title>
<style>
body{font-family:ui-sans-serif,system-ui;margin:2rem;max-width:1200px}
code{background:#f2f2f2;padding:.1rem .3rem}
input,textarea{padding:.5rem;margin:.25rem .25rem .75rem 0}
button{padding:.6rem 1rem;margin-right:.5rem}
table{width:100%;border-collapse:collapse;margin-top:1rem}
th,td{border-bottom:1px solid #ddd;padding:.5rem;text-align:left;vertical-align:top}
tr:hover{background:#fafafa;cursor:pointer}
pre{background:#f8f8f8;padding:1rem;overflow:auto}
h1,h2{margin-top:1.6rem}
.row{display:flex;gap:1rem;flex-wrap:wrap}
.panel{flex:1 1 320px;border:1px solid #e9e9e9;padding:1rem;border-radius:.5rem;background:#fff}
.stat{display:inline-block;margin-right:.8rem}
.hint{color:#555;font-size:.92rem}
</style>
</head><body>
<p><a href="/ui?workspace_id=` + strconv.FormatInt(workspaceID, 10) + `">Dashboard</a> · <a href="/ui/onboarding?workspace_id=` + strconv.FormatInt(workspaceID, 10) + `">Onboarding</a> · <a href="/ui/incidents?workspace_id=` + strconv.FormatInt(workspaceID, 10) + `">Incidents</a> · <a href="/ui/policy?workspace_id=` + strconv.FormatInt(workspaceID, 10) + `">Policy</a></p>
<h1>Operator Console</h1>
<p>Workspace: <code>` + strconv.FormatInt(workspaceID, 10) + `</code></p>
<label>API Key</label><br />
<input id="apiKey" type="text" placeholder="change-me-operator" size="42" />
<p id="activeFilter" class="hint"></p>
<label>From (UTC)</label>
<input id="rangeFrom" type="datetime-local" />
<label>To (UTC)</label>
<input id="rangeTo" type="datetime-local" />
<button id="applyRange">Apply Range</button>
<button id="clearRange">Clear Range</button>
<div class="row">
  <section class="panel">
    <h2>Queue State</h2>
    <label>Limit</label>
    <input id="limit" type="number" value="50" min="1" max="500" />
    <button id="refresh">Refresh Logs</button>
    <div>
      <button id="filterFailed">Filter Failed</button>
      <button id="filterSuppressed">Filter Suppressed</button>
      <button id="clearMessageFilter">Clear Message Filter</button>
    </div>
    <p id="queueSummary"></p>
    <label>Selected Message ID</label>
    <input id="selectedMessageId" type="number" placeholder="click a row" />
    <button id="retrySelected">Retry Selected</button>
    <button id="retryChecked">Retry Checked Rows</button>
    <label>Bulk Message IDs (comma/newline)</label><br />
    <textarea id="bulkMessageIds" rows="3" placeholder="101,102,103"></textarea>
    <button id="retryBulkIds">Retry Bulk IDs</button>
  </section>
  <section class="panel">
    <h2>Suppression Tools</h2>
    <label>Email</label><br />
    <input id="suppressionEmail" type="email" placeholder="user@example.com" size="34" />
    <label>Reason</label><br />
    <input id="suppressionReason" type="text" placeholder="manual" size="34" />
    <div>
      <button id="addSuppression">Add Suppression</button>
      <button id="addUnsubscribe">Add Unsubscribe</button>
    </div>
  </section>
  <section class="panel">
    <h2>Domain Health</h2>
    <label>Domain</label><br />
    <input id="domainName" type="text" placeholder="maild.click" size="26" />
    <label>DKIM Selector</label><br />
    <input id="dkimSelector" type="text" placeholder="default" size="26" />
    <div>
      <button id="checkDomain">Run Readiness Check</button>
    </div>
    <pre id="domainResult"></pre>
  </section>
  <section class="panel">
    <h2>Onboarding Checklist</h2>
    <label>Domain (optional)</label><br />
    <input id="onboardingDomain" type="text" placeholder="maild.click" size="26" />
    <label>DKIM Selector (optional)</label><br />
    <input id="onboardingSelector" type="text" placeholder="default" size="26" />
    <div>
      <button id="loadOnboardingChecklist">Load Checklist</button>
    </div>
    <pre id="onboardingResult"></pre>
  </section>
  <section class="panel">
    <h2>Webhook Dead Letters</h2>
    <label>Limit</label>
    <input id="webhookLimit" type="number" value="20" min="1" max="200" />
    <button id="loadDeadLetters">Load Dead Letters</button>
    <button id="filterDeadLetter">Filter Dead Letter</button>
    <button id="clearWebhookFilter">Clear Webhook Filter</button>
    <label>Replay Event ID</label><br />
    <input id="webhookEventId" type="number" placeholder="optional single event id" />
    <button id="replayDeadLetters">Replay</button>
    <label>Bulk Event IDs (comma/newline)</label><br />
    <textarea id="bulkWebhookIds" rows="3" placeholder="201,202,203"></textarea>
    <button id="replayBulkIds">Replay Bulk IDs</button>
    <pre id="webhookResult"></pre>
  </section>
  <section class="panel">
    <h2>Incident Bundle</h2>
    <label>Message ID</label><br />
    <input id="incidentMessageId" type="number" placeholder="click a row or enter id" />
    <div>
      <button id="exportIncidentBundle">Export Incident Bundle</button>
    </div>
    <pre id="incidentBundleResult"></pre>
  </section>
</div>
<p id="status"></p>
<h2>Message Logs</h2>
<table>
  <thead>
    <tr>
      <th>Select</th>
      <th>ID</th>
      <th>To</th>
      <th>Subject</th>
      <th>Status</th>
      <th>Created</th>
      <th>Updated</th>
    </tr>
  </thead>
  <tbody id="rows"></tbody>
</table>
<h2>Timeline</h2>
<pre id="timeline">Select a row to view attempts.</pre>
<script>
const workspaceId = ` + strconv.FormatInt(workspaceID, 10) + `;
const statusEl = document.getElementById('status');
const rowsEl = document.getElementById('rows');
const timelineEl = document.getElementById('timeline');
const selectedIdEl = document.getElementById('selectedMessageId');
const queueSummaryEl = document.getElementById('queueSummary');
const domainResultEl = document.getElementById('domainResult');
const webhookResultEl = document.getElementById('webhookResult');
const onboardingResultEl = document.getElementById('onboardingResult');
const incidentBundleResultEl = document.getElementById('incidentBundleResult');
const activeFilterEl = document.getElementById('activeFilter');
const savedFilterKey = 'maild_operator_saved_filter';
let savedFilter = { message_status: '', webhook_status: '', from: '', to: '' };
const selectedMessageIDs = new Set();

function readHeaders() {
  const key = document.getElementById('apiKey').value.trim();
  return key ? { 'X-API-Key': key } : {};
}

function loadSavedFilter() {
  try {
    const raw = localStorage.getItem(savedFilterKey);
    if (!raw) return;
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === 'object') {
      savedFilter = {
        message_status: String(parsed.message_status || ''),
        webhook_status: String(parsed.webhook_status || ''),
        from: String(parsed.from || ''),
        to: String(parsed.to || ''),
      };
    }
  } catch (_) {}
}

function persistSavedFilter() {
  try {
    localStorage.setItem(savedFilterKey, JSON.stringify(savedFilter));
  } catch (_) {}
  updateActiveFilterLabel();
}

function updateActiveFilterLabel() {
  const parts = [];
  if (savedFilter.message_status) parts.push('messages=' + savedFilter.message_status);
  if (savedFilter.webhook_status) parts.push('webhooks=' + savedFilter.webhook_status);
  if (savedFilter.from) parts.push('from=' + savedFilter.from);
  if (savedFilter.to) parts.push('to=' + savedFilter.to);
  activeFilterEl.textContent = parts.length === 0 ? 'Saved filter: none' : 'Saved filter: ' + parts.join(', ');
}

function selectedIncidentMessageID() {
  const v = Number(document.getElementById('incidentMessageId').value || selectedIdEl.value || 0);
  if (!Number.isInteger(v) || v <= 0) return 0;
  return v;
}

function localDateTimeValueFromRFC3339(raw) {
  if (!raw) return '';
  const d = new Date(raw);
  if (Number.isNaN(d.getTime())) return '';
  const y = d.getUTCFullYear();
  const m = String(d.getUTCMonth() + 1).padStart(2, '0');
  const day = String(d.getUTCDate()).padStart(2, '0');
  const h = String(d.getUTCHours()).padStart(2, '0');
  const min = String(d.getUTCMinutes()).padStart(2, '0');
  return y + '-' + m + '-' + day + 'T' + h + ':' + min;
}

function rfc3339FromLocalDateTime(value) {
  if (!value) return '';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '';
  return d.toISOString();
}

function summarizeQueue(messages) {
  let queued = 0, sending = 0, sent = 0, failed = 0, suppressed = 0;
  for (const m of messages || []) {
    if (m.status === 'queued') queued++;
    else if (m.status === 'sending') sending++;
    else if (m.status === 'sent') sent++;
    else if (m.status === 'failed') failed++;
    else if (m.status === 'suppressed') suppressed++;
  }
  queueSummaryEl.innerHTML =
    '<span class="stat">queued: <code>' + queued + '</code></span>' +
    '<span class="stat">sending: <code>' + sending + '</code></span>' +
    '<span class="stat">sent: <code>' + sent + '</code></span>' +
    '<span class="stat">failed: <code>' + failed + '</code></span>' +
    '<span class="stat">suppressed: <code>' + suppressed + '</code></span>';
}

function parseIDList(raw) {
  const seen = new Set();
  const out = [];
  for (const part of String(raw || '').split(/[\s,]+/)) {
    if (!part) continue;
    const n = Number(part);
    if (!Number.isInteger(n) || n <= 0) continue;
    if (seen.has(n)) continue;
    seen.add(n);
    out.push(n);
  }
  return out;
}

async function loadLogs() {
  const limit = Number(document.getElementById('limit').value || 25);
  let url = '/v1/messages/logs?workspace_id=' + workspaceId + '&limit=' + limit;
  if (savedFilter.from) url += '&from=' + encodeURIComponent(savedFilter.from);
  if (savedFilter.to) url += '&to=' + encodeURIComponent(savedFilter.to);
  const res = await fetch(url, {
    headers: readHeaders(),
  });
  statusEl.textContent = 'Logs HTTP ' + res.status;
  if (!res.ok) {
    rowsEl.innerHTML = '';
    timelineEl.textContent = await res.text();
    return;
  }
  const data = await res.json();
  summarizeQueue(data.messages || []);
  let messages = data.messages || [];
  if (savedFilter.message_status) {
    messages = messages.filter((m) => m.status === savedFilter.message_status);
  }
  rowsEl.innerHTML = '';
  for (const m of messages) {
    const tr = document.createElement('tr');
    tr.innerHTML = '<td><input type="checkbox" data-mid="' + m.id + '"></td>' +
      '<td>' + m.id + '</td>' +
      '<td>' + m.to_email + '</td>' +
      '<td>' + m.subject + '</td>' +
      '<td>' + m.status + '</td>' +
      '<td>' + m.created_at + '</td>' +
      '<td>' + m.updated_at + '</td>';
    tr.addEventListener('click', (e) => {
      if (e.target && e.target.matches && e.target.matches('input[type="checkbox"]')) return;
      loadTimeline(m.id);
    });
    rowsEl.appendChild(tr);
    const cb = tr.querySelector('input[type="checkbox"]');
    cb.checked = selectedMessageIDs.has(m.id);
    cb.addEventListener('change', () => {
      if (cb.checked) selectedMessageIDs.add(m.id);
      else selectedMessageIDs.delete(m.id);
    });
  }
  if (messages.length === 0) {
    timelineEl.textContent = 'No messages found for this workspace.';
  }
}

async function loadTimeline(messageId) {
  selectedIdEl.value = String(messageId);
  document.getElementById('incidentMessageId').value = String(messageId);
  const res = await fetch('/v1/messages/timeline?message_id=' + messageId, {
    headers: readHeaders(),
  });
  if (!res.ok) {
    timelineEl.textContent = 'Timeline HTTP ' + res.status + '\n' + await res.text();
    return;
  }
  const data = await res.json();
  timelineEl.textContent = JSON.stringify(data, null, 2);
}

async function retryMessageIDs(ids) {
  if (!ids || ids.length === 0) {
    statusEl.textContent = 'No message IDs provided.';
    return;
  }
  const res = await fetch('/v1/messages/retry', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...readHeaders() },
    body: JSON.stringify({
      workspace_id: workspaceId,
      message_ids: ids
    }),
  });
  statusEl.textContent = 'Retry HTTP ' + res.status;
  timelineEl.textContent = await res.text();
}

document.getElementById('refresh').addEventListener('click', loadLogs);
document.getElementById('retrySelected').addEventListener('click', async () => {
  const messageId = Number(selectedIdEl.value || 0);
  if (!messageId) {
    statusEl.textContent = 'Select a message row first.';
    return;
  }
  await retryMessageIDs([messageId]);
});

document.getElementById('retryChecked').addEventListener('click', async () => {
  await retryMessageIDs(Array.from(selectedMessageIDs));
});

document.getElementById('retryBulkIds').addEventListener('click', async () => {
  const ids = parseIDList(document.getElementById('bulkMessageIds').value);
  await retryMessageIDs(ids);
});

async function postJSON(path, payload) {
  return fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...readHeaders() },
    body: JSON.stringify(payload),
  });
}

async function loadWebhookLogs() {
  const limit = Number(document.getElementById('webhookLimit').value || 20);
  let range = '';
  if (savedFilter.from) range += '&from=' + encodeURIComponent(savedFilter.from);
  if (savedFilter.to) range += '&to=' + encodeURIComponent(savedFilter.to);
  const status = savedFilter.webhook_status ? '&status=' + encodeURIComponent(savedFilter.webhook_status) : '';
  const res = await fetch('/v1/webhooks/logs?workspace_id=' + workspaceId + '&limit=' + limit + status + range, {
    headers: readHeaders(),
  });
  statusEl.textContent = 'Webhook logs HTTP ' + res.status;
  webhookResultEl.textContent = await res.text();
}

document.getElementById('addSuppression').addEventListener('click', async () => {
  const email = document.getElementById('suppressionEmail').value.trim();
  const reason = document.getElementById('suppressionReason').value.trim() || 'manual';
  if (!email) {
    statusEl.textContent = 'Suppression email is required.';
    return;
  }
  const res = await postJSON('/v1/suppressions', {
    workspace_id: workspaceId,
    email,
    reason,
  });
  statusEl.textContent = 'Suppression HTTP ' + res.status;
  timelineEl.textContent = await res.text();
});

document.getElementById('addUnsubscribe').addEventListener('click', async () => {
  const email = document.getElementById('suppressionEmail').value.trim();
  const reason = document.getElementById('suppressionReason').value.trim() || 'user_unsubscribed';
  if (!email) {
    statusEl.textContent = 'Unsubscribe email is required.';
    return;
  }
  const res = await postJSON('/v1/unsubscribes', {
    workspace_id: workspaceId,
    email,
    reason,
  });
  statusEl.textContent = 'Unsubscribe HTTP ' + res.status;
  timelineEl.textContent = await res.text();
});

document.getElementById('checkDomain').addEventListener('click', async () => {
  const domain = document.getElementById('domainName').value.trim();
  const dkimSelector = document.getElementById('dkimSelector').value.trim();
  if (!domain) {
    statusEl.textContent = 'Domain is required.';
    return;
  }
  const res = await postJSON('/v1/domains/readiness', {
    workspace_id: workspaceId,
    domain,
    dkim_selector: dkimSelector,
  });
  statusEl.textContent = 'Domain readiness HTTP ' + res.status;
  domainResultEl.textContent = await res.text();
});

document.getElementById('loadOnboardingChecklist').addEventListener('click', async () => {
  const domain = document.getElementById('onboardingDomain').value.trim();
  const selector = document.getElementById('onboardingSelector').value.trim();
  let url = '/v1/ops/onboarding-checklist?workspace_id=' + workspaceId;
  if (domain) {
    url += '&domain=' + encodeURIComponent(domain);
  }
  if (selector) {
    url += '&dkim_selector=' + encodeURIComponent(selector);
  }
  const res = await fetch(url, {
    headers: readHeaders(),
  });
  statusEl.textContent = 'Onboarding checklist HTTP ' + res.status;
  onboardingResultEl.textContent = await res.text();
});

document.getElementById('loadDeadLetters').addEventListener('click', loadWebhookLogs);

document.getElementById('replayDeadLetters').addEventListener('click', async () => {
  const eventID = Number(document.getElementById('webhookEventId').value || 0);
  const limit = Number(document.getElementById('webhookLimit').value || 20);
  const payload = {
    workspace_id: workspaceId,
    limit,
  };
  if (eventID > 0) {
    payload.event_ids = [eventID];
  }
  const res = await postJSON('/v1/webhooks/replay', payload);
  statusEl.textContent = 'Webhook replay HTTP ' + res.status;
  webhookResultEl.textContent = await res.text();
});

document.getElementById('replayBulkIds').addEventListener('click', async () => {
  const ids = parseIDList(document.getElementById('bulkWebhookIds').value);
  if (ids.length === 0) {
    statusEl.textContent = 'No webhook event IDs provided.';
    return;
  }
  const res = await postJSON('/v1/webhooks/replay', {
    workspace_id: workspaceId,
    event_ids: ids,
  });
  statusEl.textContent = 'Webhook replay HTTP ' + res.status;
  webhookResultEl.textContent = await res.text();
});

document.getElementById('exportIncidentBundle').addEventListener('click', async () => {
  const messageId = selectedIncidentMessageID();
  if (!messageId) {
    statusEl.textContent = 'Message ID is required for incident bundle export.';
    return;
  }
  const res = await fetch('/v1/incidents/bundle?workspace_id=' + workspaceId + '&message_id=' + messageId, {
    headers: readHeaders(),
  });
  statusEl.textContent = 'Incident bundle HTTP ' + res.status;
  incidentBundleResultEl.textContent = await res.text();
});

document.getElementById('filterFailed').addEventListener('click', () => {
  savedFilter.message_status = 'failed';
  persistSavedFilter();
  loadLogs();
});

document.getElementById('filterSuppressed').addEventListener('click', () => {
  savedFilter.message_status = 'suppressed';
  persistSavedFilter();
  loadLogs();
});

document.getElementById('clearMessageFilter').addEventListener('click', () => {
  savedFilter.message_status = '';
  persistSavedFilter();
  loadLogs();
});

document.getElementById('filterDeadLetter').addEventListener('click', () => {
  savedFilter.webhook_status = 'dead_letter';
  persistSavedFilter();
  loadWebhookLogs();
});

document.getElementById('clearWebhookFilter').addEventListener('click', () => {
  savedFilter.webhook_status = '';
  persistSavedFilter();
  loadWebhookLogs();
});

document.getElementById('applyRange').addEventListener('click', () => {
  const fromInput = document.getElementById('rangeFrom').value;
  const toInput = document.getElementById('rangeTo').value;
  savedFilter.from = rfc3339FromLocalDateTime(fromInput);
  savedFilter.to = rfc3339FromLocalDateTime(toInput);
  persistSavedFilter();
  loadLogs();
  loadWebhookLogs();
});

document.getElementById('clearRange').addEventListener('click', () => {
  savedFilter.from = '';
  savedFilter.to = '';
  document.getElementById('rangeFrom').value = '';
  document.getElementById('rangeTo').value = '';
  persistSavedFilter();
  loadLogs();
  loadWebhookLogs();
});

loadSavedFilter();
document.getElementById('rangeFrom').value = localDateTimeValueFromRFC3339(savedFilter.from);
document.getElementById('rangeTo').value = localDateTimeValueFromRFC3339(savedFilter.to);
updateActiveFilterLabel();
document.getElementById('incidentMessageId').value = selectedIdEl.value;
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

func parseFromToOptional(r *http.Request) (time.Time, time.Time) {
	var from time.Time
	var to time.Time
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
	if !from.IsZero() && !to.IsZero() && !to.After(from) {
		to = from.Add(24 * time.Hour)
	}
	return from, to
}

func formatRFC3339OrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
