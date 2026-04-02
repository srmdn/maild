package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/srmdn/maild/internal/domain"
)

func TestRetryMessagesByIDSuccess(t *testing.T) {
	store := &retryStore{
		messagesByID: map[int64]domain.Message{
			1: {ID: 1, WorkspaceID: 1, Status: "failed"},
		},
	}
	queue := &retryQueue{}
	svc := NewMessageService(store, queue, nil, nil, nil, nil, 1, 1, 0, 0, false, 0, 0, 0)

	result, err := svc.RetryMessages(context.Background(), 1, []int64{1}, 0)
	if err != nil {
		t.Fatalf("RetryMessages error = %v", err)
	}
	if result.ReplaySource != "ids" || result.Requested != 1 || result.Retried != 1 || result.Skipped != 0 || result.Failed != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0] != 1 {
		t.Fatalf("enqueued = %#v, want [1]", queue.enqueued)
	}
}

func TestRetryMessagesSkipsAndWorkspaceMismatch(t *testing.T) {
	store := &retryStore{
		messagesByID: map[int64]domain.Message{
			10: {ID: 10, WorkspaceID: 1, Status: "sent"},
			11: {ID: 11, WorkspaceID: 2, Status: "failed"},
		},
	}
	queue := &retryQueue{}
	svc := NewMessageService(store, queue, nil, nil, nil, nil, 1, 1, 0, 0, false, 0, 0, 0)

	result, err := svc.RetryMessages(context.Background(), 1, []int64{10, 11}, 0)
	if err != nil {
		t.Fatalf("RetryMessages error = %v", err)
	}
	if result.Requested != 2 || result.Retried != 0 || result.Skipped != 1 || result.Failed != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRetryMessagesQueueFailureRollsBack(t *testing.T) {
	store := &retryStore{
		messagesByID: map[int64]domain.Message{
			22: {ID: 22, WorkspaceID: 1, Status: "failed"},
		},
	}
	queue := &retryQueue{enqueueErr: errors.New("queue down")}
	svc := NewMessageService(store, queue, nil, nil, nil, nil, 1, 1, 0, 0, false, 0, 0, 0)

	result, err := svc.RetryMessages(context.Background(), 1, []int64{22}, 0)
	if err != nil {
		t.Fatalf("RetryMessages error = %v", err)
	}
	if result.Retried != 0 || result.Failed != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if store.messagesByID[22].Status != "failed" {
		t.Fatalf("status after queue failure = %q, want failed", store.messagesByID[22].Status)
	}
}

func TestRetryMessagesLatestFailedWindow(t *testing.T) {
	store := &retryStore{
		listMessages: []domain.Message{
			{ID: 30, WorkspaceID: 1, Status: "failed"},
			{ID: 31, WorkspaceID: 1, Status: "sent"},
			{ID: 32, WorkspaceID: 1, Status: "failed"},
		},
		messagesByID: map[int64]domain.Message{
			30: {ID: 30, WorkspaceID: 1, Status: "failed"},
			31: {ID: 31, WorkspaceID: 1, Status: "sent"},
			32: {ID: 32, WorkspaceID: 1, Status: "failed"},
		},
	}
	queue := &retryQueue{}
	svc := NewMessageService(store, queue, nil, nil, nil, nil, 1, 1, 0, 0, false, 0, 0, 0)

	result, err := svc.RetryMessages(context.Background(), 1, nil, 10)
	if err != nil {
		t.Fatalf("RetryMessages error = %v", err)
	}
	if result.ReplaySource != "latest_failed" || result.Requested != 2 || result.Retried != 2 || result.Failed != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(queue.enqueued) != 2 {
		t.Fatalf("enqueued = %#v, want 2 ids", queue.enqueued)
	}
}

type retryStore struct {
	messagesByID map[int64]domain.Message
	listMessages []domain.Message
}

func (s *retryStore) EnsureDefaultWorkspace(context.Context) error                { return nil }
func (s *retryStore) IsSuppressed(context.Context, int64, string) (bool, error)   { return false, nil }
func (s *retryStore) AddSuppression(context.Context, int64, string, string) error { return nil }
func (s *retryStore) IsUnsubscribed(context.Context, int64, string) (bool, error) { return false, nil }
func (s *retryStore) AddUnsubscribe(context.Context, int64, string, string) error { return nil }
func (s *retryStore) UpsertSMTPAccountEncrypted(context.Context, int64, string, []byte) error {
	return nil
}
func (s *retryStore) GetSMTPAccountEncrypted(context.Context, int64) ([]byte, bool, error) {
	return nil, false, nil
}
func (s *retryStore) SetActiveSMTPAccount(context.Context, int64, string) error { return nil }
func (s *retryStore) ListSMTPAccounts(context.Context, int64) ([]domain.SMTPAccountSummary, error) {
	return nil, nil
}
func (s *retryStore) CountFailedAttemptsByProviderSince(context.Context, int64, string, time.Time) (int64, error) {
	return 0, nil
}
func (s *retryStore) CreateMessage(context.Context, domain.Message) (domain.Message, error) {
	return domain.Message{}, nil
}
func (s *retryStore) GetMessage(_ context.Context, id int64) (domain.Message, error) {
	m, ok := s.messagesByID[id]
	if !ok {
		return domain.Message{}, errors.New("not found")
	}
	return m, nil
}
func (s *retryStore) SetMessageStatus(_ context.Context, id int64, status string) error {
	m, ok := s.messagesByID[id]
	if !ok {
		return errors.New("not found")
	}
	m.Status = status
	s.messagesByID[id] = m
	return nil
}
func (s *retryStore) TransitionMessageStatus(_ context.Context, id int64, fromStatus, toStatus string) (bool, error) {
	m, ok := s.messagesByID[id]
	if !ok {
		return false, errors.New("not found")
	}
	if m.Status != fromStatus {
		return false, nil
	}
	m.Status = toStatus
	s.messagesByID[id] = m
	return true, nil
}
func (s *retryStore) NextAttemptNo(context.Context, int64) (int, error) { return 0, nil }
func (s *retryStore) InsertAttempt(context.Context, int64, int, string, string, bool) error {
	return nil
}
func (s *retryStore) ListMessageAttempts(context.Context, int64) ([]domain.MessageAttempt, error) {
	return nil, nil
}
func (s *retryStore) ListMessages(context.Context, int64, int, time.Time, time.Time) ([]domain.Message, error) {
	return s.listMessages, nil
}
func (s *retryStore) CountMessagesSince(context.Context, int64, string, time.Time) (int64, error) {
	return 0, nil
}
func (s *retryStore) UpsertWorkspacePolicy(context.Context, domain.WorkspacePolicy) error { return nil }
func (s *retryStore) GetWorkspacePolicy(context.Context, int64) (domain.WorkspacePolicy, bool, error) {
	return domain.WorkspacePolicy{}, false, nil
}
func (s *retryStore) InsertMeteringEvent(context.Context, domain.MeteringEvent) error { return nil }
func (s *retryStore) MeteringSummary(context.Context, int64, time.Time, time.Time) ([]domain.MeteringSummaryItem, error) {
	return nil, nil
}
func (s *retryStore) ExportMessageLogsCSV(context.Context, int64, int) (string, error) {
	return "", nil
}
func (s *retryStore) InsertWebhookEvent(context.Context, domain.WebhookEvent) (domain.WebhookEvent, error) {
	return domain.WebhookEvent{}, nil
}
func (s *retryStore) ListWebhookEvents(context.Context, int64, int, string, time.Time, time.Time) ([]domain.WebhookEvent, error) {
	return nil, nil
}
func (s *retryStore) ListWebhookDeadLetters(context.Context, int64, int) ([]domain.WebhookEvent, error) {
	return nil, nil
}
func (s *retryStore) ListWebhookDeadLettersByID(context.Context, int64, []int64) ([]domain.WebhookEvent, error) {
	return nil, nil
}
func (s *retryStore) UpdateWebhookEventReplayResult(context.Context, int64, string, int, string) error {
	return nil
}

type retryQueue struct {
	enqueued   []int64
	enqueueErr error
}

func (q *retryQueue) Enqueue(_ context.Context, id int64) error {
	if q.enqueueErr != nil {
		return q.enqueueErr
	}
	q.enqueued = append(q.enqueued, id)
	return nil
}
func (q *retryQueue) Dequeue(context.Context, time.Duration) (int64, bool, error) {
	return 0, false, nil
}
