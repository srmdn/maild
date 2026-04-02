package service

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/srmdn/maild/internal/domain"
)

func TestIntegrationRetryReplayFailoverFlow(t *testing.T) {
	now := time.Now().UTC().Add(-30 * time.Minute)
	store := &integrationStore{
		messages: map[int64]domain.Message{
			101: {ID: 101, WorkspaceID: 1, Status: "failed"},
			102: {ID: 102, WorkspaceID: 1, Status: "failed"},
			103: {ID: 103, WorkspaceID: 1, Status: "sent"},
		},
		webhookEvents: map[int64]domain.WebhookEvent{
			201: {
				ID:           201,
				WorkspaceID:  1,
				EventType:    "bounce",
				Email:        "bounced@example.com",
				Reason:       "hard_bounce",
				Status:       "dead_letter",
				AttemptCount: 1,
			},
			202: {
				ID:           202,
				WorkspaceID:  1,
				EventType:    "unsubscribe",
				Email:        "optout@example.com",
				Reason:       "user_unsubscribe",
				Status:       "dead_letter",
				AttemptCount: 1,
			},
		},
		accounts: []domain.SMTPAccountSummary{
			{Name: "primary", Active: true, UpdatedAt: now},
			{Name: "standby", Active: false, UpdatedAt: now},
		},
		failures:     5,
		suppressions: make(map[string]bool),
		unsubscribes: make(map[string]bool),
	}
	queue := &integrationQueue{}
	svc := NewMessageService(
		store,
		queue,
		nil,
		nil,
		nil,
		nil,
		3,
		2,
		0,
		0,
		true,
		3,
		5*time.Minute,
		1*time.Minute,
	)

	retryResult, err := svc.RetryMessages(context.Background(), 1, []int64{101, 102}, 0)
	if err != nil {
		t.Fatalf("RetryMessages error = %v", err)
	}
	if retryResult.Retried != 2 || retryResult.Failed != 0 || retryResult.Skipped != 0 {
		t.Fatalf("unexpected retry result: %#v", retryResult)
	}
	if len(queue.enqueued) != 2 {
		t.Fatalf("enqueued count = %d, want 2", len(queue.enqueued))
	}
	for _, id := range []int64{101, 102} {
		if store.messages[id].Status != "queued" {
			t.Fatalf("message %d status = %q, want queued", id, store.messages[id].Status)
		}
	}

	replayResult, err := svc.ReplayWebhookDeadLetters(context.Background(), 1, []int64{201, 202}, 0)
	if err != nil {
		t.Fatalf("ReplayWebhookDeadLetters error = %v", err)
	}
	if replayResult.Replayed != 2 || replayResult.Failed != 0 {
		t.Fatalf("unexpected replay result: %#v", replayResult)
	}
	if !store.suppressions["bounced@example.com"] {
		t.Fatalf("expected bounced@example.com in suppressions map")
	}
	if !store.unsubscribes["optout@example.com"] {
		t.Fatalf("expected optout@example.com in unsubscribes map")
	}
	if store.webhookEvents[201].Status != "replayed" || store.webhookEvents[202].Status != "replayed" {
		t.Fatalf("expected webhook events to be replayed")
	}

	switched, toProvider, err := svc.tryAutoFailover(context.Background(), 1, "primary")
	if err != nil {
		t.Fatalf("tryAutoFailover error = %v", err)
	}
	if !switched || toProvider != "standby" {
		t.Fatalf("unexpected failover result: switched=%v toProvider=%q", switched, toProvider)
	}
	if active := store.activeAccountName(); active != "standby" {
		t.Fatalf("active account = %q, want standby", active)
	}
}

type integrationStore struct {
	messages      map[int64]domain.Message
	webhookEvents map[int64]domain.WebhookEvent
	accounts      []domain.SMTPAccountSummary
	failures      int64
	suppressions  map[string]bool
	unsubscribes  map[string]bool
}

func (s *integrationStore) EnsureDefaultWorkspace(context.Context) error { return nil }
func (s *integrationStore) IsSuppressed(context.Context, int64, string) (bool, error) {
	return false, nil
}
func (s *integrationStore) AddSuppression(_ context.Context, _ int64, email, _ string) error {
	s.suppressions[email] = true
	return nil
}
func (s *integrationStore) IsUnsubscribed(context.Context, int64, string) (bool, error) {
	return false, nil
}
func (s *integrationStore) AddUnsubscribe(_ context.Context, _ int64, email, _ string) error {
	s.unsubscribes[email] = true
	return nil
}
func (s *integrationStore) UpsertSMTPAccountEncrypted(context.Context, int64, string, []byte) error {
	return nil
}
func (s *integrationStore) GetSMTPAccountEncrypted(context.Context, int64) ([]byte, bool, error) {
	return nil, false, nil
}
func (s *integrationStore) SetActiveSMTPAccount(_ context.Context, _ int64, name string) error {
	found := false
	for i := range s.accounts {
		if s.accounts[i].Name == name {
			found = true
		}
		s.accounts[i].Active = s.accounts[i].Name == name
		if s.accounts[i].Active {
			s.accounts[i].UpdatedAt = time.Now().UTC()
		}
	}
	if !found {
		return errors.New("account not found")
	}
	return nil
}
func (s *integrationStore) ListSMTPAccounts(context.Context, int64) ([]domain.SMTPAccountSummary, error) {
	out := make([]domain.SMTPAccountSummary, len(s.accounts))
	copy(out, s.accounts)
	return out, nil
}
func (s *integrationStore) CountFailedAttemptsByProviderSince(context.Context, int64, string, time.Time) (int64, error) {
	return s.failures, nil
}
func (s *integrationStore) CreateMessage(context.Context, domain.Message) (domain.Message, error) {
	return domain.Message{}, nil
}
func (s *integrationStore) GetMessage(_ context.Context, id int64) (domain.Message, error) {
	m, ok := s.messages[id]
	if !ok {
		return domain.Message{}, errors.New("message not found")
	}
	return m, nil
}
func (s *integrationStore) SetMessageStatus(_ context.Context, id int64, status string) error {
	m, ok := s.messages[id]
	if !ok {
		return errors.New("message not found")
	}
	m.Status = status
	s.messages[id] = m
	return nil
}
func (s *integrationStore) TransitionMessageStatus(_ context.Context, id int64, fromStatus, toStatus string) (bool, error) {
	m, ok := s.messages[id]
	if !ok {
		return false, errors.New("message not found")
	}
	if m.Status != fromStatus {
		return false, nil
	}
	m.Status = toStatus
	s.messages[id] = m
	return true, nil
}
func (s *integrationStore) NextAttemptNo(context.Context, int64) (int, error) { return 0, nil }
func (s *integrationStore) InsertAttempt(context.Context, int64, int, string, string, bool) error {
	return nil
}
func (s *integrationStore) ListMessageAttempts(context.Context, int64) ([]domain.MessageAttempt, error) {
	return nil, nil
}
func (s *integrationStore) ListMessages(_ context.Context, workspaceID int64, limit int, _, _ time.Time) ([]domain.Message, error) {
	out := make([]domain.Message, 0, len(s.messages))
	for _, m := range s.messages {
		if m.WorkspaceID == workspaceID {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (s *integrationStore) CountMessagesSince(context.Context, int64, string, time.Time) (int64, error) {
	return 0, nil
}
func (s *integrationStore) UpsertWorkspacePolicy(context.Context, domain.WorkspacePolicy) error {
	return nil
}
func (s *integrationStore) GetWorkspacePolicy(context.Context, int64) (domain.WorkspacePolicy, bool, error) {
	return domain.WorkspacePolicy{}, false, nil
}
func (s *integrationStore) InsertMeteringEvent(context.Context, domain.MeteringEvent) error {
	return nil
}
func (s *integrationStore) MeteringSummary(context.Context, int64, time.Time, time.Time) ([]domain.MeteringSummaryItem, error) {
	return nil, nil
}
func (s *integrationStore) ExportMessageLogsCSV(context.Context, int64, int) (string, error) {
	return "", nil
}
func (s *integrationStore) InsertWebhookEvent(context.Context, domain.WebhookEvent) (domain.WebhookEvent, error) {
	return domain.WebhookEvent{}, nil
}
func (s *integrationStore) ListWebhookEvents(context.Context, int64, int, string, time.Time, time.Time) ([]domain.WebhookEvent, error) {
	return nil, nil
}
func (s *integrationStore) ListWebhookDeadLetters(_ context.Context, workspaceID int64, limit int) ([]domain.WebhookEvent, error) {
	out := make([]domain.WebhookEvent, 0, len(s.webhookEvents))
	for _, e := range s.webhookEvents {
		if e.WorkspaceID == workspaceID && e.Status == "dead_letter" {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (s *integrationStore) ListWebhookDeadLettersByID(_ context.Context, workspaceID int64, ids []int64) ([]domain.WebhookEvent, error) {
	out := make([]domain.WebhookEvent, 0, len(ids))
	for _, id := range ids {
		e, ok := s.webhookEvents[id]
		if !ok {
			continue
		}
		if e.WorkspaceID != workspaceID || e.Status != "dead_letter" {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
func (s *integrationStore) UpdateWebhookEventReplayResult(_ context.Context, id int64, status string, attemptCount int, lastError string) error {
	e, ok := s.webhookEvents[id]
	if !ok {
		return errors.New("webhook event not found")
	}
	e.Status = status
	e.AttemptCount = attemptCount
	e.LastError = lastError
	s.webhookEvents[id] = e
	return nil
}

func (s *integrationStore) activeAccountName() string {
	for _, a := range s.accounts {
		if a.Active {
			return a.Name
		}
	}
	return ""
}

type integrationQueue struct {
	enqueued []int64
}

func (q *integrationQueue) Enqueue(_ context.Context, id int64) error {
	q.enqueued = append(q.enqueued, id)
	return nil
}

func (q *integrationQueue) Dequeue(context.Context, time.Duration) (int64, bool, error) {
	return 0, false, nil
}
