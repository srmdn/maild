package service

import (
	"context"
	"testing"
	"time"

	"github.com/srmdn/maild/internal/domain"
)

func TestQueueMessageSuppressedDoesNotEnqueue(t *testing.T) {
	store := &safetyStore{
		suppressed: true,
	}
	queue := &safetyQueue{}
	svc := NewMessageService(store, queue, nil, nil, allowAllLimiter{}, map[string]struct{}{}, 3, 2, 400, 200, false, 0, 0, 0)

	msg, err := svc.QueueMessage(context.Background(), 1, "from@example.com", "to@example.com", "subject", "body")
	if err != nil {
		t.Fatalf("QueueMessage error = %v", err)
	}
	if msg.Status != "suppressed" {
		t.Fatalf("status = %q, want suppressed", msg.Status)
	}
	if queue.enqueueCalls != 0 {
		t.Fatalf("enqueueCalls = %d, want 0", queue.enqueueCalls)
	}
}

func TestQueueMessageUnsubscribedDoesNotEnqueue(t *testing.T) {
	store := &safetyStore{
		unsubscribed: true,
	}
	queue := &safetyQueue{}
	svc := NewMessageService(store, queue, nil, nil, allowAllLimiter{}, map[string]struct{}{}, 3, 2, 400, 200, false, 0, 0, 0)

	msg, err := svc.QueueMessage(context.Background(), 1, "from@example.com", "to@example.com", "subject", "body")
	if err != nil {
		t.Fatalf("QueueMessage error = %v", err)
	}
	if msg.Status != "suppressed" {
		t.Fatalf("status = %q, want suppressed", msg.Status)
	}
	if queue.enqueueCalls != 0 {
		t.Fatalf("enqueueCalls = %d, want 0", queue.enqueueCalls)
	}
}

func TestProcessOneRechecksSuppressionBeforeSend(t *testing.T) {
	store := &safetyStore{
		messagesByID: map[int64]domain.Message{
			44: {ID: 44, WorkspaceID: 1, ToEmail: "to@example.com", Status: "queued"},
		},
		suppressed: true,
	}
	svc := NewMessageService(store, &safetyQueue{}, nil, nil, allowAllLimiter{}, map[string]struct{}{}, 3, 2, 400, 200, false, 0, 0, 0)

	if err := svc.ProcessOne(context.Background(), 44); err != nil {
		t.Fatalf("ProcessOne error = %v", err)
	}

	got := store.messagesByID[44]
	if got.Status != "suppressed" {
		t.Fatalf("status = %q, want suppressed", got.Status)
	}
	if store.transitionCalls != 0 {
		t.Fatalf("transitionCalls = %d, want 0", store.transitionCalls)
	}
}

type allowAllLimiter struct{}

func (allowAllLimiter) Allow(context.Context, int64, string) (bool, string, error) {
	return true, "", nil
}

type safetyQueue struct {
	enqueueCalls int
}

func (q *safetyQueue) Enqueue(context.Context, int64) error {
	q.enqueueCalls++
	return nil
}

func (q *safetyQueue) Dequeue(context.Context, time.Duration) (int64, bool, error) {
	return 0, false, nil
}

type safetyStore struct {
	suppressed     bool
	unsubscribed   bool
	messagesByID   map[int64]domain.Message
	nextMessageID  int64
	transitionCalls int
}

func (s *safetyStore) EnsureDefaultWorkspace(context.Context) error { return nil }
func (s *safetyStore) IsSuppressed(context.Context, int64, string) (bool, error) {
	return s.suppressed, nil
}
func (s *safetyStore) AddSuppression(context.Context, int64, string, string) error { return nil }
func (s *safetyStore) IsUnsubscribed(context.Context, int64, string) (bool, error) {
	return s.unsubscribed, nil
}
func (s *safetyStore) AddUnsubscribe(context.Context, int64, string, string) error { return nil }
func (s *safetyStore) UpsertSMTPAccountEncrypted(context.Context, int64, string, []byte) error {
	return nil
}
func (s *safetyStore) GetSMTPAccountEncrypted(context.Context, int64) ([]byte, bool, error) {
	return nil, false, nil
}
func (s *safetyStore) SetActiveSMTPAccount(context.Context, int64, string) error { return nil }
func (s *safetyStore) ListSMTPAccounts(context.Context, int64) ([]domain.SMTPAccountSummary, error) {
	return nil, nil
}
func (s *safetyStore) CountFailedAttemptsByProviderSince(context.Context, int64, string, time.Time) (int64, error) {
	return 0, nil
}
func (s *safetyStore) CreateMessage(_ context.Context, m domain.Message) (domain.Message, error) {
	s.nextMessageID++
	m.ID = s.nextMessageID
	if s.messagesByID == nil {
		s.messagesByID = make(map[int64]domain.Message)
	}
	s.messagesByID[m.ID] = m
	return m, nil
}
func (s *safetyStore) GetMessage(_ context.Context, id int64) (domain.Message, error) {
	return s.messagesByID[id], nil
}
func (s *safetyStore) SetMessageStatus(_ context.Context, id int64, status string) error {
	m := s.messagesByID[id]
	m.Status = status
	s.messagesByID[id] = m
	return nil
}
func (s *safetyStore) TransitionMessageStatus(_ context.Context, id int64, fromStatus, toStatus string) (bool, error) {
	s.transitionCalls++
	m := s.messagesByID[id]
	if m.Status != fromStatus {
		return false, nil
	}
	m.Status = toStatus
	s.messagesByID[id] = m
	return true, nil
}
func (s *safetyStore) NextAttemptNo(context.Context, int64) (int, error) { return 1, nil }
func (s *safetyStore) InsertAttempt(context.Context, int64, int, string, string, bool) error {
	return nil
}
func (s *safetyStore) ListMessageAttempts(context.Context, int64) ([]domain.MessageAttempt, error) {
	return nil, nil
}
func (s *safetyStore) ListMessages(context.Context, int64, int, time.Time, time.Time) ([]domain.Message, error) {
	return nil, nil
}
func (s *safetyStore) CountMessagesSince(context.Context, int64, string, time.Time) (int64, error) {
	return 0, nil
}
func (s *safetyStore) UpsertWorkspacePolicy(context.Context, domain.WorkspacePolicy) error { return nil }
func (s *safetyStore) GetWorkspacePolicy(context.Context, int64) (domain.WorkspacePolicy, bool, error) {
	return domain.WorkspacePolicy{}, false, nil
}
func (s *safetyStore) InsertMeteringEvent(context.Context, domain.MeteringEvent) error { return nil }
func (s *safetyStore) MeteringSummary(context.Context, int64, time.Time, time.Time) ([]domain.MeteringSummaryItem, error) {
	return nil, nil
}
func (s *safetyStore) ExportMessageLogsCSV(context.Context, int64, int) (string, error) {
	return "", nil
}
func (s *safetyStore) InsertWebhookEvent(context.Context, domain.WebhookEvent) (domain.WebhookEvent, error) {
	return domain.WebhookEvent{}, nil
}
func (s *safetyStore) ListWebhookEvents(context.Context, int64, int, string, time.Time, time.Time) ([]domain.WebhookEvent, error) {
	return nil, nil
}
func (s *safetyStore) ListWebhookDeadLetters(context.Context, int64, int) ([]domain.WebhookEvent, error) {
	return nil, nil
}
func (s *safetyStore) ListWebhookDeadLettersByID(context.Context, int64, []int64) ([]domain.WebhookEvent, error) {
	return nil, nil
}
func (s *safetyStore) UpdateWebhookEventReplayResult(context.Context, int64, string, int, string) error {
	return nil
}

