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
	CreateMessage(ctx context.Context, m domain.Message) (domain.Message, error)
	GetMessage(ctx context.Context, id int64) (domain.Message, error)
	SetMessageStatus(ctx context.Context, id int64, status string) error
	TransitionMessageStatus(ctx context.Context, id int64, fromStatus, toStatus string) (bool, error)
	NextAttemptNo(ctx context.Context, messageID int64) (int, error)
	InsertAttempt(ctx context.Context, messageID int64, attemptNo int, provider, response string, success bool) error
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
}

func NewMessageService(store MessageStore, queue MessageQueue, sender *smtpclient.Client, sealer *crypto.Sealer, limiter RateLimiter, blockedRecipientDomains map[string]struct{}, maxAttempts int) *MessageService {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	return &MessageService{
		store:                   store,
		queue:                   queue,
		sender:                  sender,
		sealer:                  sealer,
		limiter:                 limiter,
		blockedRecipientDomains: blockedRecipientDomains,
		maxAttempts:             maxAttempts,
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
		return m, nil
	}

	if err := s.queue.Enqueue(ctx, m.ID); err != nil {
		return domain.Message{}, err
	}

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

	err = s.sender.Send(creds, m.ToEmail, m.Subject, m.BodyText)
	if err == nil {
		if err := s.store.InsertAttempt(ctx, m.ID, attemptNo, smtpclient.ProviderName(creds), "accepted by smtp server", true); err != nil {
			return err
		}
		return s.store.SetMessageStatus(ctx, m.ID, "sent")
	}

	safeErr := sanitize.SMTPError(err.Error())
	if insertErr := s.store.InsertAttempt(ctx, m.ID, attemptNo, smtpclient.ProviderName(creds), safeErr, false); insertErr != nil {
		return insertErr
	}

	if attemptNo >= s.maxAttempts {
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
