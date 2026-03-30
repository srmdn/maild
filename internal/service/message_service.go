package service

import (
	"context"
	"errors"
	"time"

	"github.com/srmdn/maild/internal/domain"
)

type MessageStore interface {
	EnsureDefaultWorkspace(ctx context.Context) error
	IsSuppressed(ctx context.Context, workspaceID int64, email string) (bool, error)
	CreateMessage(ctx context.Context, m domain.Message) (domain.Message, error)
	GetMessage(ctx context.Context, id int64) (domain.Message, error)
	SetMessageStatus(ctx context.Context, id int64, status string) error
	NextAttemptNo(ctx context.Context, messageID int64) (int, error)
	InsertAttempt(ctx context.Context, messageID int64, attemptNo int, provider, response string, success bool) error
}

type MessageQueue interface {
	Enqueue(ctx context.Context, id int64) error
	Dequeue(ctx context.Context, timeout time.Duration) (int64, bool, error)
}

type MailSender interface {
	ProviderName() string
	Send(toEmail, subject, body string) error
}

type MessageService struct {
	store       MessageStore
	queue       MessageQueue
	sender      MailSender
	maxAttempts int
}

func NewMessageService(store MessageStore, queue MessageQueue, sender MailSender, maxAttempts int) *MessageService {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	return &MessageService{
		store:       store,
		queue:       queue,
		sender:      sender,
		maxAttempts: maxAttempts,
	}
}

func (s *MessageService) Bootstrap(ctx context.Context) error {
	return s.store.EnsureDefaultWorkspace(ctx)
}

func (s *MessageService) QueueMessage(ctx context.Context, workspaceID int64, fromEmail, toEmail, subject, body string) (domain.Message, error) {
	suppressed, err := s.store.IsSuppressed(ctx, workspaceID, toEmail)
	if err != nil {
		return domain.Message{}, err
	}

	status := "queued"
	if suppressed {
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

	if err := s.store.SetMessageStatus(ctx, m.ID, "sending"); err != nil {
		return err
	}

	attemptNo, err := s.store.NextAttemptNo(ctx, m.ID)
	if err != nil {
		return err
	}

	err = s.sender.Send(m.ToEmail, m.Subject, m.BodyText)
	if err == nil {
		if err := s.store.InsertAttempt(ctx, m.ID, attemptNo, s.sender.ProviderName(), "accepted by smtp server", true); err != nil {
			return err
		}
		return s.store.SetMessageStatus(ctx, m.ID, "sent")
	}

	if insertErr := s.store.InsertAttempt(ctx, m.ID, attemptNo, s.sender.ProviderName(), err.Error(), false); insertErr != nil {
		return insertErr
	}

	if attemptNo >= s.maxAttempts {
		return s.store.SetMessageStatus(ctx, m.ID, "failed")
	}

	if err := s.store.SetMessageStatus(ctx, m.ID, "queued"); err != nil {
		return err
	}

	time.Sleep(2 * time.Second)
	return s.queue.Enqueue(ctx, m.ID)
}

func (s *MessageService) PopQueue(ctx context.Context, timeout time.Duration) (int64, bool, error) {
	return s.queue.Dequeue(ctx, timeout)
}

var ErrBadRequest = errors.New("bad request")
