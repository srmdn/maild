package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/srmdn/maild/internal/domain"
)

func TestReplayWebhookDeadLettersSuccess(t *testing.T) {
	store := &replayStore{
		byID: map[int64]domain.WebhookEvent{
			41: {
				ID:           41,
				WorkspaceID:  2,
				EventType:    "bounce",
				Email:        "user@example.com",
				Reason:       "hard_bounce",
				Status:       "dead_letter",
				AttemptCount: 2,
			},
		},
	}

	svc := NewMessageService(store, nil, nil, nil, nil, nil, 1, 1, 0, 0, false, 0, 0, 0)
	result, err := svc.ReplayWebhookDeadLetters(context.Background(), 2, []int64{41}, 0)
	if err != nil {
		t.Fatalf("ReplayWebhookDeadLetters error = %v", err)
	}

	if result.WorkspaceID != 2 {
		t.Fatalf("workspace = %d, want 2", result.WorkspaceID)
	}
	if result.ReplaySource != "ids" {
		t.Fatalf("source = %q, want ids", result.ReplaySource)
	}
	if result.Requested != 1 || result.Replayed != 1 || result.Failed != 0 {
		t.Fatalf("unexpected result counts: %#v", result)
	}
	if len(result.Outcomes) != 1 {
		t.Fatalf("len(outcomes) = %d, want 1", len(result.Outcomes))
	}
	if result.Outcomes[0].Status != "replayed" {
		t.Fatalf("status = %q, want replayed", result.Outcomes[0].Status)
	}
	if result.Outcomes[0].AttemptCount != 3 {
		t.Fatalf("attempt_count = %d, want 3", result.Outcomes[0].AttemptCount)
	}
	if len(store.suppressionCalls) != 1 {
		t.Fatalf("suppression calls = %d, want 1", len(store.suppressionCalls))
	}
	upd := store.updated[41]
	if upd.status != "replayed" {
		t.Fatalf("updated status = %q, want replayed", upd.status)
	}
	if upd.attemptCount != 3 {
		t.Fatalf("updated attempt_count = %d, want 3", upd.attemptCount)
	}
	if upd.lastError != "" {
		t.Fatalf("updated last_error = %q, want empty", upd.lastError)
	}
}

func TestReplayWebhookDeadLettersFailureBookkeeping(t *testing.T) {
	store := &replayStore{
		deadLetters: []domain.WebhookEvent{
			{
				ID:           7,
				WorkspaceID:  1,
				EventType:    "unknown",
				Email:        "bad@example.com",
				Status:       "dead_letter",
				AttemptCount: 3,
			},
			{
				ID:           8,
				WorkspaceID:  1,
				EventType:    "bounce",
				Email:        "",
				Status:       "dead_letter",
				AttemptCount: 1,
			},
		},
		byID: map[int64]domain.WebhookEvent{},
	}

	svc := NewMessageService(store, nil, nil, nil, nil, nil, 1, 1, 0, 0, false, 0, 0, 0)
	result, err := svc.ReplayWebhookDeadLetters(context.Background(), 1, nil, 10)
	if err != nil {
		t.Fatalf("ReplayWebhookDeadLetters error = %v", err)
	}

	if result.ReplaySource != "latest" {
		t.Fatalf("source = %q, want latest", result.ReplaySource)
	}
	if result.Requested != 2 || result.Replayed != 0 || result.Failed != 2 {
		t.Fatalf("unexpected result counts: %#v", result)
	}
	if len(result.Outcomes) != 2 {
		t.Fatalf("len(outcomes) = %d, want 2", len(result.Outcomes))
	}

	first := store.updated[7]
	if first.status != "dead_letter" {
		t.Fatalf("event 7 status = %q, want dead_letter", first.status)
	}
	if first.attemptCount != 4 {
		t.Fatalf("event 7 attempt_count = %d, want 4", first.attemptCount)
	}
	if !strings.Contains(first.lastError, "unsupported webhook event type") {
		t.Fatalf("event 7 last_error = %q, expected unsupported type", first.lastError)
	}

	second := store.updated[8]
	if second.status != "dead_letter" {
		t.Fatalf("event 8 status = %q, want dead_letter", second.status)
	}
	if second.attemptCount != 2 {
		t.Fatalf("event 8 attempt_count = %d, want 2", second.attemptCount)
	}
	if !strings.Contains(second.lastError, "missing email") {
		t.Fatalf("event 8 last_error = %q, expected missing email", second.lastError)
	}
}

type replayStore struct {
	deadLetters      []domain.WebhookEvent
	byID             map[int64]domain.WebhookEvent
	updated          map[int64]replayUpdate
	suppressionCalls []suppressionCall
}

type replayUpdate struct {
	status       string
	attemptCount int
	lastError    string
}

type suppressionCall struct {
	workspaceID int64
	email       string
	reason      string
}

func (s *replayStore) EnsureDefaultWorkspace(context.Context) error { return nil }
func (s *replayStore) IsSuppressed(context.Context, int64, string) (bool, error) {
	return false, nil
}
func (s *replayStore) AddSuppression(_ context.Context, workspaceID int64, email, reason string) error {
	s.suppressionCalls = append(s.suppressionCalls, suppressionCall{
		workspaceID: workspaceID,
		email:       email,
		reason:      reason,
	})
	return nil
}
func (s *replayStore) IsUnsubscribed(context.Context, int64, string) (bool, error) {
	return false, nil
}
func (s *replayStore) AddUnsubscribe(context.Context, int64, string, string) error {
	return nil
}
func (s *replayStore) UpsertSMTPAccountEncrypted(context.Context, int64, string, []byte) error {
	return nil
}
func (s *replayStore) GetSMTPAccountEncrypted(context.Context, int64) ([]byte, bool, error) {
	return nil, false, nil
}
func (s *replayStore) SetActiveSMTPAccount(context.Context, int64, string) error { return nil }
func (s *replayStore) ListSMTPAccounts(context.Context, int64) ([]domain.SMTPAccountSummary, error) {
	return nil, nil
}
func (s *replayStore) CountFailedAttemptsByProviderSince(context.Context, int64, string, time.Time) (int64, error) {
	return 0, nil
}
func (s *replayStore) CreateMessage(context.Context, domain.Message) (domain.Message, error) {
	return domain.Message{}, nil
}
func (s *replayStore) GetMessage(context.Context, int64) (domain.Message, error) {
	return domain.Message{}, nil
}
func (s *replayStore) SetMessageStatus(context.Context, int64, string) error { return nil }
func (s *replayStore) TransitionMessageStatus(context.Context, int64, string, string) (bool, error) {
	return false, nil
}
func (s *replayStore) NextAttemptNo(context.Context, int64) (int, error) { return 0, nil }
func (s *replayStore) InsertAttempt(context.Context, int64, int, string, string, bool) error {
	return nil
}
func (s *replayStore) ListMessageAttempts(context.Context, int64) ([]domain.MessageAttempt, error) {
	return nil, nil
}
func (s *replayStore) ListMessages(context.Context, int64, int, time.Time, time.Time) ([]domain.Message, error) {
	return nil, nil
}
func (s *replayStore) CountMessagesSince(context.Context, int64, string, time.Time) (int64, error) {
	return 0, nil
}
func (s *replayStore) UpsertWorkspacePolicy(context.Context, domain.WorkspacePolicy) error {
	return nil
}
func (s *replayStore) GetWorkspacePolicy(context.Context, int64) (domain.WorkspacePolicy, bool, error) {
	return domain.WorkspacePolicy{}, false, nil
}
func (s *replayStore) InsertMeteringEvent(context.Context, domain.MeteringEvent) error { return nil }
func (s *replayStore) MeteringSummary(context.Context, int64, time.Time, time.Time) ([]domain.MeteringSummaryItem, error) {
	return nil, nil
}
func (s *replayStore) ExportMessageLogsCSV(context.Context, int64, int) (string, error) {
	return "", nil
}
func (s *replayStore) InsertWebhookEvent(context.Context, domain.WebhookEvent) (domain.WebhookEvent, error) {
	return domain.WebhookEvent{}, nil
}
func (s *replayStore) ListWebhookEvents(context.Context, int64, int, string, time.Time, time.Time) ([]domain.WebhookEvent, error) {
	return nil, nil
}
func (s *replayStore) ListWebhookDeadLetters(_ context.Context, workspaceID int64, limit int) ([]domain.WebhookEvent, error) {
	if limit < len(s.deadLetters) {
		return s.deadLetters[:limit], nil
	}
	return s.deadLetters, nil
}
func (s *replayStore) ListWebhookDeadLettersByID(_ context.Context, workspaceID int64, ids []int64) ([]domain.WebhookEvent, error) {
	if s.byID == nil {
		return nil, nil
	}
	out := make([]domain.WebhookEvent, 0, len(ids))
	for _, id := range ids {
		if e, ok := s.byID[id]; ok && e.WorkspaceID == workspaceID && e.Status == "dead_letter" {
			out = append(out, e)
		}
	}
	return out, nil
}
func (s *replayStore) UpdateWebhookEventReplayResult(_ context.Context, id int64, status string, attemptCount int, lastError string) error {
	if s.updated == nil {
		s.updated = make(map[int64]replayUpdate)
	}
	if id == 0 {
		return errors.New("invalid id")
	}
	s.updated[id] = replayUpdate{
		status:       status,
		attemptCount: attemptCount,
		lastError:    lastError,
	}
	return nil
}
