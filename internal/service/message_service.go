package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/srmdn/maild/internal/crypto"
	"github.com/srmdn/maild/internal/domain"
	"github.com/srmdn/maild/internal/sanitize"
	"github.com/srmdn/maild/internal/smtpclient"
)

type MessageStore interface {
	EnsureDefaultWorkspace(ctx context.Context) error
	IsSuppressed(ctx context.Context, workspaceID int64, email string) (bool, error)
	AddSuppression(ctx context.Context, workspaceID int64, email, reason string) error
	IsUnsubscribed(ctx context.Context, workspaceID int64, email string) (bool, error)
	AddUnsubscribe(ctx context.Context, workspaceID int64, email, reason string) error
	UpsertSMTPAccountEncrypted(ctx context.Context, workspaceID int64, name string, encryptedPayload []byte) error
	GetSMTPAccountEncrypted(ctx context.Context, workspaceID int64) ([]byte, bool, error)
	SetActiveSMTPAccount(ctx context.Context, workspaceID int64, name string) error
	ListSMTPAccounts(ctx context.Context, workspaceID int64) ([]domain.SMTPAccountSummary, error)
	CountFailedAttemptsByProviderSince(ctx context.Context, workspaceID int64, provider string, since time.Time) (int64, error)
	CreateMessage(ctx context.Context, m domain.Message) (domain.Message, error)
	GetMessage(ctx context.Context, id int64) (domain.Message, error)
	SetMessageStatus(ctx context.Context, id int64, status string) error
	TransitionMessageStatus(ctx context.Context, id int64, fromStatus, toStatus string) (bool, error)
	NextAttemptNo(ctx context.Context, messageID int64) (int, error)
	InsertAttempt(ctx context.Context, messageID int64, attemptNo int, provider, response string, success bool) error
	ListMessageAttempts(ctx context.Context, messageID int64) ([]domain.MessageAttempt, error)
	ListMessages(ctx context.Context, workspaceID int64, limit int) ([]domain.Message, error)
	CountMessagesSince(ctx context.Context, workspaceID int64, recipientDomain string, since time.Time) (int64, error)
	UpsertWorkspacePolicy(ctx context.Context, p domain.WorkspacePolicy) error
	GetWorkspacePolicy(ctx context.Context, workspaceID int64) (domain.WorkspacePolicy, bool, error)
	InsertMeteringEvent(ctx context.Context, e domain.MeteringEvent) error
	MeteringSummary(ctx context.Context, workspaceID int64, from, to time.Time) ([]domain.MeteringSummaryItem, error)
	ExportMessageLogsCSV(ctx context.Context, workspaceID int64, limit int) (string, error)
	InsertWebhookEvent(ctx context.Context, e domain.WebhookEvent) (domain.WebhookEvent, error)
	ListWebhookEvents(ctx context.Context, workspaceID int64, limit int, status string) ([]domain.WebhookEvent, error)
	ListWebhookDeadLetters(ctx context.Context, workspaceID int64, limit int) ([]domain.WebhookEvent, error)
	ListWebhookDeadLettersByID(ctx context.Context, workspaceID int64, ids []int64) ([]domain.WebhookEvent, error)
	UpdateWebhookEventReplayResult(ctx context.Context, id int64, status string, attemptCount int, lastError string) error
}

type MessageQueue interface {
	Enqueue(ctx context.Context, id int64) error
	Dequeue(ctx context.Context, timeout time.Duration) (int64, bool, error)
}

type RateLimiter interface {
	Allow(ctx context.Context, workspaceID int64, recipientDomain string) (bool, string, error)
}

type MessageService struct {
	store                   MessageStore
	queue                   MessageQueue
	sender                  *smtpclient.Client
	sealer                  *crypto.Sealer
	limiter                 RateLimiter
	blockedRecipientDomains map[string]struct{}
	maxAttempts             int
	webhookApplyMaxAttempts int
	defaultWorkspaceRate    int
	defaultDomainRate       int
	autoFailoverEnabled     bool
	autoFailoverFailures    int
	autoFailoverWindow      time.Duration
	autoFailoverCooldown    time.Duration
}

func NewMessageService(store MessageStore, queue MessageQueue, sender *smtpclient.Client, sealer *crypto.Sealer, limiter RateLimiter, blockedRecipientDomains map[string]struct{}, maxAttempts int, webhookApplyMaxAttempts int, defaultWorkspaceRate int, defaultDomainRate int, autoFailoverEnabled bool, autoFailoverFailures int, autoFailoverWindow, autoFailoverCooldown time.Duration) *MessageService {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if webhookApplyMaxAttempts < 1 {
		webhookApplyMaxAttempts = 1
	}
	if autoFailoverFailures < 1 {
		autoFailoverFailures = 1
	}
	if autoFailoverWindow <= 0 {
		autoFailoverWindow = 5 * time.Minute
	}
	if autoFailoverCooldown < 0 {
		autoFailoverCooldown = 0
	}
	return &MessageService{
		store:                   store,
		queue:                   queue,
		sender:                  sender,
		sealer:                  sealer,
		limiter:                 limiter,
		blockedRecipientDomains: blockedRecipientDomains,
		maxAttempts:             maxAttempts,
		webhookApplyMaxAttempts: webhookApplyMaxAttempts,
		defaultWorkspaceRate:    defaultWorkspaceRate,
		defaultDomainRate:       defaultDomainRate,
		autoFailoverEnabled:     autoFailoverEnabled,
		autoFailoverFailures:    autoFailoverFailures,
		autoFailoverWindow:      autoFailoverWindow,
		autoFailoverCooldown:    autoFailoverCooldown,
	}
}

func (s *MessageService) Bootstrap(ctx context.Context) error {
	return s.store.EnsureDefaultWorkspace(ctx)
}

func (s *MessageService) QueueMessage(ctx context.Context, workspaceID int64, fromEmail, toEmail, subject, body string) (domain.Message, error) {
	recipientDomain := extractDomain(toEmail)
	if recipientDomain == "" {
		return domain.Message{}, ErrBadRequest
	}
	policy, err := s.workspacePolicy(ctx, workspaceID)
	if err != nil {
		return domain.Message{}, err
	}

	if policy.RateLimitWorkspacePerHour > 0 {
		count, err := s.store.CountMessagesSince(ctx, workspaceID, "", time.Now().UTC().Add(-1*time.Hour))
		if err != nil {
			return domain.Message{}, err
		}
		if count >= int64(policy.RateLimitWorkspacePerHour) {
			return domain.Message{}, fmt.Errorf("%w: tenant_workspace_policy_limit", ErrRateLimited)
		}
	}
	if policy.RateLimitDomainPerHour > 0 {
		count, err := s.store.CountMessagesSince(ctx, workspaceID, recipientDomain, time.Now().UTC().Add(-1*time.Hour))
		if err != nil {
			return domain.Message{}, err
		}
		if count >= int64(policy.RateLimitDomainPerHour) {
			return domain.Message{}, fmt.Errorf("%w: tenant_domain_policy_limit", ErrRateLimited)
		}
	}

	if isBlockedDomain(policy.BlockedRecipientDomains, recipientDomain) {
		return domain.Message{}, ErrBlockedRecipientDomain
	}

	if _, blocked := s.blockedRecipientDomains[recipientDomain]; blocked {
		return domain.Message{}, ErrBlockedRecipientDomain
	}

	allowed, reason, err := s.limiter.Allow(ctx, workspaceID, recipientDomain)
	if err != nil {
		return domain.Message{}, err
	}
	if !allowed {
		return domain.Message{}, fmt.Errorf("%w: %s", ErrRateLimited, reason)
	}

	suppressed, err := s.store.IsSuppressed(ctx, workspaceID, toEmail)
	if err != nil {
		return domain.Message{}, err
	}
	unsubscribed, err := s.store.IsUnsubscribed(ctx, workspaceID, toEmail)
	if err != nil {
		return domain.Message{}, err
	}

	status := "queued"
	if suppressed || unsubscribed {
		status = "suppressed"
	}

	m, err := s.store.CreateMessage(ctx, domain.Message{
		WorkspaceID: workspaceID,
		FromEmail:   fromEmail,
		ToEmail:     toEmail,
		Subject:     subject,
		BodyText:    body,
		Status:      status,
	})
	if err != nil {
		return domain.Message{}, err
	}

	if suppressed {
		_ = s.store.InsertMeteringEvent(ctx, domain.MeteringEvent{
			WorkspaceID: workspaceID,
			MessageID:   m.ID,
			EventType:   "message_suppressed",
			Quantity:    1,
			Metadata:    `{"source":"queue_message"}`,
		})
		return m, nil
	}

	if err := s.queue.Enqueue(ctx, m.ID); err != nil {
		return domain.Message{}, err
	}
	_ = s.store.InsertMeteringEvent(ctx, domain.MeteringEvent{
		WorkspaceID: workspaceID,
		MessageID:   m.ID,
		EventType:   "message_queued",
		Quantity:    1,
		Metadata:    `{"source":"queue_message"}`,
	})

	return m, nil
}

func (s *MessageService) ProcessOne(ctx context.Context, messageID int64) error {
	m, err := s.store.GetMessage(ctx, messageID)
	if err != nil {
		return err
	}

	if m.Status == "suppressed" || m.Status == "sent" {
		return nil
	}
	if m.Status != "queued" {
		return nil
	}

	suppressed, err := s.store.IsSuppressed(ctx, m.WorkspaceID, m.ToEmail)
	if err != nil {
		return err
	}
	unsubscribed, err := s.store.IsUnsubscribed(ctx, m.WorkspaceID, m.ToEmail)
	if err != nil {
		return err
	}
	if suppressed || unsubscribed {
		return s.store.SetMessageStatus(ctx, m.ID, "suppressed")
	}

	ok, err := s.store.TransitionMessageStatus(ctx, m.ID, "queued", "sending")
	if err != nil {
		return err
	}
	if !ok {
		// Another worker/process already claimed this queued message.
		return nil
	}

	attemptNo, err := s.store.NextAttemptNo(ctx, m.ID)
	if err != nil {
		return err
	}

	creds, err := s.resolveCredentials(ctx, m.WorkspaceID)
	if err != nil {
		return err
	}
	provider := smtpclient.ProviderName(creds)

	err = s.sender.Send(creds, m.ToEmail, m.Subject, m.BodyText)
	if err == nil {
		if err := s.store.InsertAttempt(ctx, m.ID, attemptNo, provider, "accepted by smtp server", true); err != nil {
			return err
		}
		_ = s.store.InsertMeteringEvent(ctx, domain.MeteringEvent{
			WorkspaceID: m.WorkspaceID,
			MessageID:   m.ID,
			EventType:   "message_sent",
			Quantity:    1,
			Metadata:    `{"provider":"` + provider + `"}`,
		})
		return s.store.SetMessageStatus(ctx, m.ID, "sent")
	}

	safeErr := sanitize.SMTPError(err.Error())
	if insertErr := s.store.InsertAttempt(ctx, m.ID, attemptNo, provider, safeErr, false); insertErr != nil {
		return insertErr
	}
	_ = s.store.InsertMeteringEvent(ctx, domain.MeteringEvent{
		WorkspaceID: m.WorkspaceID,
		MessageID:   m.ID,
		EventType:   "message_send_error",
		Quantity:    1,
		Metadata:    jsonMetadata(map[string]string{"provider": provider, "error": safeErr}),
	})
	if switched, toProvider, switchErr := s.tryAutoFailover(ctx, m.WorkspaceID, provider); switchErr == nil && switched {
		_ = s.store.InsertMeteringEvent(ctx, domain.MeteringEvent{
			WorkspaceID: m.WorkspaceID,
			MessageID:   m.ID,
			EventType:   "provider_auto_failover",
			Quantity:    1,
			Metadata:    jsonMetadata(map[string]string{"from": provider, "to": toProvider}),
		})
	}

	if attemptNo >= s.maxAttempts {
		_ = s.store.InsertMeteringEvent(ctx, domain.MeteringEvent{
			WorkspaceID: m.WorkspaceID,
			MessageID:   m.ID,
			EventType:   "message_failed",
			Quantity:    1,
			Metadata:    `{"provider":"` + provider + `"}`,
		})
		return s.store.SetMessageStatus(ctx, m.ID, "failed")
	}

	if err := s.store.SetMessageStatus(ctx, m.ID, "queued"); err != nil {
		return err
	}

	time.Sleep(backoffDuration(attemptNo))
	return s.queue.Enqueue(ctx, m.ID)
}

func (s *MessageService) PopQueue(ctx context.Context, timeout time.Duration) (int64, bool, error) {
	return s.queue.Dequeue(ctx, timeout)
}

func (s *MessageService) AddSuppression(ctx context.Context, workspaceID int64, email, reason string) error {
	return s.store.AddSuppression(ctx, workspaceID, email, reason)
}

func (s *MessageService) AddUnsubscribe(ctx context.Context, workspaceID int64, email, reason string) error {
	return s.store.AddUnsubscribe(ctx, workspaceID, email, reason)
}

func (s *MessageService) UpsertSMTPAccount(ctx context.Context, account domain.SMTPAccount) error {
	payload, err := json.Marshal(account)
	if err != nil {
		return err
	}
	encrypted, err := s.sealer.Seal(payload)
	if err != nil {
		return err
	}
	return s.store.UpsertSMTPAccountEncrypted(ctx, account.WorkspaceID, account.Name, encrypted)
}

func (s *MessageService) SetActiveSMTPAccount(ctx context.Context, workspaceID int64, name string) error {
	return s.store.SetActiveSMTPAccount(ctx, workspaceID, name)
}

func (s *MessageService) SMTPAccounts(ctx context.Context, workspaceID int64) ([]domain.SMTPAccountSummary, error) {
	return s.store.ListSMTPAccounts(ctx, workspaceID)
}

func (s *MessageService) UpsertWorkspacePolicy(ctx context.Context, policy domain.WorkspacePolicy) error {
	if policy.WorkspaceID == 0 {
		policy.WorkspaceID = 1
	}
	if policy.RateLimitWorkspacePerHour < 1 {
		policy.RateLimitWorkspacePerHour = s.defaultWorkspaceRate
	}
	if policy.RateLimitDomainPerHour < 1 {
		policy.RateLimitDomainPerHour = s.defaultDomainRate
	}
	policy.BlockedRecipientDomains = normalizeDomains(policy.BlockedRecipientDomains)
	return s.store.UpsertWorkspacePolicy(ctx, policy)
}

func (s *MessageService) WorkspacePolicy(ctx context.Context, workspaceID int64) (domain.WorkspacePolicy, error) {
	return s.workspacePolicy(ctx, workspaceID)
}

func (s *MessageService) MeteringSummary(ctx context.Context, workspaceID int64, from, to time.Time) ([]domain.MeteringSummaryItem, error) {
	return s.store.MeteringSummary(ctx, workspaceID, from, to)
}

func (s *MessageService) ExportMessageLogsCSV(ctx context.Context, workspaceID int64, limit int) (string, error) {
	return s.store.ExportMessageLogsCSV(ctx, workspaceID, limit)
}

func (s *MessageService) resolveCredentials(ctx context.Context, workspaceID int64) (smtpclient.Credentials, error) {
	payload, found, err := s.store.GetSMTPAccountEncrypted(ctx, workspaceID)
	if err != nil {
		return smtpclient.Credentials{}, err
	}
	if !found {
		return s.sender.DefaultCredentials(), nil
	}

	decrypted, err := s.sealer.Open(payload)
	if err != nil {
		return smtpclient.Credentials{}, err
	}

	var account domain.SMTPAccount
	if err := json.Unmarshal(decrypted, &account); err != nil {
		return smtpclient.Credentials{}, err
	}

	return smtpclient.Credentials{
		Host:     account.Host,
		Port:     account.Port,
		Username: account.Username,
		Password: account.Password,
		From:     account.FromEmail,
	}, nil
}

func (s *MessageService) ValidateSMTPAccount(ctx context.Context, workspaceID int64) (string, error) {
	creds, err := s.resolveCredentials(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	if err := s.sender.Validate(creds, 5*time.Second); err != nil {
		return "", err
	}
	return smtpclient.ProviderName(creds), nil
}

func (s *MessageService) MessageTimeline(ctx context.Context, messageID int64) (domain.Message, []domain.MessageAttempt, error) {
	m, err := s.store.GetMessage(ctx, messageID)
	if err != nil {
		return domain.Message{}, nil, err
	}
	attempts, err := s.store.ListMessageAttempts(ctx, messageID)
	if err != nil {
		return domain.Message{}, nil, err
	}
	return m, attempts, nil
}

func (s *MessageService) MessageLogs(ctx context.Context, workspaceID int64, limit int) ([]domain.Message, error) {
	return s.store.ListMessages(ctx, workspaceID, limit)
}

func (s *MessageService) RetryMessages(ctx context.Context, workspaceID int64, messageIDs []int64, limit int) (domain.MessageRetryResult, error) {
	if workspaceID == 0 {
		workspaceID = 1
	}
	retryIDs := normalizePositiveIDs(messageIDs)

	out := domain.MessageRetryResult{
		WorkspaceID: workspaceID,
		Outcomes:    make([]domain.MessageRetryOutcome, 0),
	}

	if len(retryIDs) > 0 {
		out.ReplaySource = "ids"
		out.Requested = len(retryIDs)
		for _, id := range retryIDs {
			msg, err := s.store.GetMessage(ctx, id)
			if err != nil {
				out.Failed++
				out.Outcomes = append(out.Outcomes, domain.MessageRetryOutcome{
					MessageID: id,
					Status:    "failed",
					Error:     "message not found",
				})
				continue
			}
			s.retryMessage(ctx, workspaceID, msg, &out)
		}
		return out, nil
	}

	if limit < 1 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	out.ReplaySource = "latest_failed"

	messages, err := s.store.ListMessages(ctx, workspaceID, limit)
	if err != nil {
		return domain.MessageRetryResult{}, err
	}
	for _, msg := range messages {
		if msg.Status != "failed" {
			continue
		}
		out.Requested++
		s.retryMessage(ctx, workspaceID, msg, &out)
	}
	return out, nil
}

func (s *MessageService) retryMessage(ctx context.Context, workspaceID int64, msg domain.Message, out *domain.MessageRetryResult) {
	if msg.WorkspaceID != workspaceID {
		out.Failed++
		out.Outcomes = append(out.Outcomes, domain.MessageRetryOutcome{
			MessageID: msg.ID,
			Status:    "failed",
			Error:     "workspace mismatch",
		})
		return
	}
	if msg.Status != "failed" {
		out.Skipped++
		out.Outcomes = append(out.Outcomes, domain.MessageRetryOutcome{
			MessageID: msg.ID,
			Status:    "skipped",
			Error:     "only failed messages can be retried",
		})
		return
	}

	ok, err := s.store.TransitionMessageStatus(ctx, msg.ID, "failed", "queued")
	if err != nil {
		out.Failed++
		out.Outcomes = append(out.Outcomes, domain.MessageRetryOutcome{
			MessageID: msg.ID,
			Status:    "failed",
			Error:     "status transition failed",
		})
		return
	}
	if !ok {
		out.Skipped++
		out.Outcomes = append(out.Outcomes, domain.MessageRetryOutcome{
			MessageID: msg.ID,
			Status:    "skipped",
			Error:     "message no longer failed",
		})
		return
	}

	if s.queue == nil {
		_ = s.store.SetMessageStatus(ctx, msg.ID, "failed")
		out.Failed++
		out.Outcomes = append(out.Outcomes, domain.MessageRetryOutcome{
			MessageID: msg.ID,
			Status:    "failed",
			Error:     "queue unavailable",
		})
		return
	}

	if err := s.queue.Enqueue(ctx, msg.ID); err != nil {
		_ = s.store.SetMessageStatus(ctx, msg.ID, "failed")
		out.Failed++
		out.Outcomes = append(out.Outcomes, domain.MessageRetryOutcome{
			MessageID: msg.ID,
			Status:    "failed",
			Error:     "queue enqueue failed",
		})
		return
	}

	_ = s.store.InsertMeteringEvent(ctx, domain.MeteringEvent{
		WorkspaceID: msg.WorkspaceID,
		MessageID:   msg.ID,
		EventType:   "message_retried",
		Quantity:    1,
		Metadata:    `{"source":"manual_retry"}`,
	})
	out.Retried++
	out.Outcomes = append(out.Outcomes, domain.MessageRetryOutcome{
		MessageID: msg.ID,
		Status:    "retried",
	})
}

func (s *MessageService) ProcessWebhookEvent(ctx context.Context, workspaceID int64, eventType, email, reason, rawPayload string) (domain.WebhookEvent, error) {
	if workspaceID == 0 {
		workspaceID = 1
	}
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	email = strings.TrimSpace(email)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		switch eventType {
		case "bounce", "complaint":
			reason = "provider_" + eventType
		case "unsubscribe":
			reason = "provider_unsubscribe"
		}
	}

	attempts, lastErr := s.applyWebhookEventWithRetry(ctx, workspaceID, eventType, email, reason)
	if lastErr == nil {
		return s.store.InsertWebhookEvent(ctx, domain.WebhookEvent{
			WorkspaceID:  workspaceID,
			EventType:    eventType,
			Email:        email,
			Reason:       reason,
			Status:       "applied",
			AttemptCount: attempts,
			RawPayload:   rawPayload,
		})
	}

	return s.store.InsertWebhookEvent(ctx, domain.WebhookEvent{
		WorkspaceID:  workspaceID,
		EventType:    eventType,
		Email:        email,
		Reason:       reason,
		Status:       "dead_letter",
		AttemptCount: attempts,
		LastError:    lastErr.Error(),
		RawPayload:   rawPayload,
	})
}

func (s *MessageService) RecordWebhookDeadLetter(ctx context.Context, workspaceID int64, eventType, email, reason, lastError, rawPayload string, attemptCount int) (domain.WebhookEvent, error) {
	if workspaceID == 0 {
		workspaceID = 1
	}
	if attemptCount < 1 {
		attemptCount = 1
	}
	eventType = strings.TrimSpace(strings.ToLower(eventType))
	if eventType == "" {
		eventType = "unknown"
	}
	return s.store.InsertWebhookEvent(ctx, domain.WebhookEvent{
		WorkspaceID:  workspaceID,
		EventType:    eventType,
		Email:        strings.TrimSpace(strings.ToLower(email)),
		Reason:       strings.TrimSpace(reason),
		Status:       "dead_letter",
		AttemptCount: attemptCount,
		LastError:    strings.TrimSpace(lastError),
		RawPayload:   rawPayload,
	})
}

func (s *MessageService) WebhookLogs(ctx context.Context, workspaceID int64, limit int, status string) ([]domain.WebhookEvent, error) {
	return s.store.ListWebhookEvents(ctx, workspaceID, limit, strings.TrimSpace(strings.ToLower(status)))
}

func (s *MessageService) ReplayWebhookDeadLetters(ctx context.Context, workspaceID int64, eventIDs []int64, limit int) (domain.WebhookReplayResult, error) {
	if workspaceID == 0 {
		workspaceID = 1
	}
	replayIDs := normalizePositiveIDs(eventIDs)

	var (
		events []domain.WebhookEvent
		err    error
		source = "latest"
	)
	if len(replayIDs) > 0 {
		source = "ids"
		events, err = s.store.ListWebhookDeadLettersByID(ctx, workspaceID, replayIDs)
	} else {
		if limit < 1 {
			limit = 20
		}
		if limit > 200 {
			limit = 200
		}
		events, err = s.store.ListWebhookDeadLetters(ctx, workspaceID, limit)
	}
	if err != nil {
		return domain.WebhookReplayResult{}, err
	}

	out := domain.WebhookReplayResult{
		WorkspaceID:  workspaceID,
		Requested:    len(events),
		ReplaySource: source,
		Outcomes:     make([]domain.WebhookReplayOutcome, 0, len(events)),
	}

	for _, event := range events {
		outcome := domain.WebhookReplayOutcome{
			EventID:      event.ID,
			Status:       "failed",
			AttemptCount: event.AttemptCount,
		}

		eventType := strings.ToLower(strings.TrimSpace(event.EventType))
		email := strings.TrimSpace(strings.ToLower(event.Email))
		reason := strings.TrimSpace(event.Reason)
		if email == "" {
			nextAttempts := event.AttemptCount + 1
			err := s.store.UpdateWebhookEventReplayResult(ctx, event.ID, "dead_letter", nextAttempts, "replay failed: missing email")
			if err != nil {
				return domain.WebhookReplayResult{}, err
			}
			out.Failed++
			outcome.AttemptCount = nextAttempts
			outcome.Error = "replay failed: missing email"
			out.Outcomes = append(out.Outcomes, outcome)
			continue
		}

		attempts, replayErr := s.applyWebhookEventWithRetry(ctx, event.WorkspaceID, eventType, email, reason)
		nextAttempts := event.AttemptCount + attempts
		if replayErr != nil {
			lastError := "replay failed: " + replayErr.Error()
			err := s.store.UpdateWebhookEventReplayResult(ctx, event.ID, "dead_letter", nextAttempts, lastError)
			if err != nil {
				return domain.WebhookReplayResult{}, err
			}
			out.Failed++
			outcome.AttemptCount = nextAttempts
			outcome.Error = lastError
			out.Outcomes = append(out.Outcomes, outcome)
			continue
		}

		if err := s.store.UpdateWebhookEventReplayResult(ctx, event.ID, "replayed", nextAttempts, ""); err != nil {
			return domain.WebhookReplayResult{}, err
		}
		out.Replayed++
		outcome.Status = "replayed"
		outcome.AttemptCount = nextAttempts
		out.Outcomes = append(out.Outcomes, outcome)
	}

	return out, nil
}

func (s *MessageService) applyWebhookEventWithRetry(ctx context.Context, workspaceID int64, eventType, email, reason string) (int, error) {
	var lastErr error
	for attempt := 1; attempt <= s.webhookApplyMaxAttempts; attempt++ {
		var err error
		switch eventType {
		case "bounce", "complaint":
			err = s.store.AddSuppression(ctx, workspaceID, email, reason)
		case "unsubscribe":
			err = s.store.AddUnsubscribe(ctx, workspaceID, email, reason)
		default:
			err = fmt.Errorf("unsupported webhook event type: %s", eventType)
		}

		if err == nil {
			return attempt, nil
		}

		lastErr = err
		if attempt < s.webhookApplyMaxAttempts {
			time.Sleep(webhookBackoffDuration(attempt))
		}
	}

	return s.webhookApplyMaxAttempts, lastErr
}

var ErrBadRequest = errors.New("bad request")
var ErrRateLimited = errors.New("rate limited")
var ErrBlockedRecipientDomain = errors.New("blocked recipient domain")

func extractDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[at+1:]))
}

func backoffDuration(attemptNo int) time.Duration {
	if attemptNo < 1 {
		attemptNo = 1
	}
	seconds := 1 << (attemptNo - 1)
	if seconds > 32 {
		seconds = 32
	}
	return time.Duration(seconds) * time.Second
}

func webhookBackoffDuration(attemptNo int) time.Duration {
	if attemptNo < 1 {
		attemptNo = 1
	}
	seconds := 1 << (attemptNo - 1)
	if seconds > 8 {
		seconds = 8
	}
	return time.Duration(seconds) * time.Second
}

func normalizePositiveIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func RateLimitReason(err error) string {
	const fallback = "rate_limit_exceeded"
	if err == nil || !errors.Is(err, ErrRateLimited) {
		return fallback
	}
	msg := err.Error()
	i := strings.Index(msg, ": ")
	if i < 0 || i+2 >= len(msg) {
		return fallback
	}
	reason := strings.TrimSpace(msg[i+2:])
	if reason == "" {
		return fallback
	}
	return reason
}

func FormatQueueError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrBlockedRecipientDomain) {
		return "blocked_recipient_domain"
	}
	if errors.Is(err, ErrRateLimited) {
		return fmt.Sprintf("rate_limited:%s", RateLimitReason(err))
	}
	if errors.Is(err, ErrBadRequest) {
		return "bad_request"
	}
	return "internal_error"
}

func (s *MessageService) workspacePolicy(ctx context.Context, workspaceID int64) (domain.WorkspacePolicy, error) {
	if workspaceID == 0 {
		workspaceID = 1
	}
	policy, found, err := s.store.GetWorkspacePolicy(ctx, workspaceID)
	if err != nil {
		return domain.WorkspacePolicy{}, err
	}
	if found {
		policy.BlockedRecipientDomains = normalizeDomains(policy.BlockedRecipientDomains)
		return policy, nil
	}
	defaults := make([]string, 0, len(s.blockedRecipientDomains))
	for d := range s.blockedRecipientDomains {
		defaults = append(defaults, d)
	}
	return domain.WorkspacePolicy{
		WorkspaceID:               workspaceID,
		RateLimitWorkspacePerHour: s.defaultWorkspaceRate,
		RateLimitDomainPerHour:    s.defaultDomainRate,
		BlockedRecipientDomains:   normalizeDomains(defaults),
	}, nil
}

func isBlockedDomain(list []string, domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	for _, d := range list {
		if strings.ToLower(strings.TrimSpace(d)) == domain {
			return true
		}
	}
	return false
}

func normalizeDomains(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, d := range in {
		v := strings.ToLower(strings.TrimSpace(d))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func jsonMetadata(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return ""
	}
	return string(raw)
}

func (s *MessageService) tryAutoFailover(ctx context.Context, workspaceID int64, currentProvider string) (bool, string, error) {
	if !s.autoFailoverEnabled {
		return false, "", nil
	}
	since := time.Now().UTC().Add(-s.autoFailoverWindow)
	failures, err := s.store.CountFailedAttemptsByProviderSince(ctx, workspaceID, currentProvider, since)
	if err != nil {
		return false, "", err
	}
	if failures < int64(s.autoFailoverFailures) {
		return false, "", nil
	}

	accounts, err := s.store.ListSMTPAccounts(ctx, workspaceID)
	if err != nil {
		return false, "", err
	}
	if len(accounts) < 2 {
		return false, "", nil
	}

	var active *domain.SMTPAccountSummary
	var standby *domain.SMTPAccountSummary
	for i := range accounts {
		a := accounts[i]
		if a.Active {
			active = &a
			continue
		}
		if standby == nil {
			tmp := a
			standby = &tmp
		}
	}
	if active == nil || standby == nil {
		return false, "", nil
	}
	if time.Since(active.UpdatedAt) < s.autoFailoverCooldown {
		return false, "", nil
	}

	if err := s.store.SetActiveSMTPAccount(ctx, workspaceID, standby.Name); err != nil {
		return false, "", err
	}
	return true, standby.Name, nil
}
