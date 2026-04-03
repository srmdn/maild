package service

import (
	"context"
	"testing"
	"time"

	"github.com/srmdn/maild/internal/domain"
)

func TestTechnicalOnboardingChecklist(t *testing.T) {
	now := time.Now().UTC().Add(-30 * time.Minute)
	base := &integrationStore{
		messages: map[int64]domain.Message{
			10: {
				ID:          10,
				WorkspaceID: 1,
				ToEmail:     "user@example.com",
				Status:      "sent",
				CreatedAt:   now,
				UpdatedAt:   now.Add(2 * time.Minute),
			},
		},
		accounts: []domain.SMTPAccountSummary{
			{Name: "primary", Active: true, UpdatedAt: now},
			{Name: "standby", Active: false, UpdatedAt: now},
		},
		suppressions: make(map[string]bool),
		unsubscribes: make(map[string]bool),
	}
	store := &trackCStore{
		integrationStore: base,
		webhookLogs: []domain.WebhookEvent{
			{ID: 501, WorkspaceID: 1, Email: "user@example.com", Status: "applied", CreatedAt: now},
		},
	}

	svc := NewMessageService(store, nil, nil, nil, nil, nil, 3, 3, 500, 200, false, 0, 0, 0)
	checklist, err := svc.TechnicalOnboardingChecklist(context.Background(), 1)
	if err != nil {
		t.Fatalf("TechnicalOnboardingChecklist error = %v", err)
	}
	if checklist.Total != 6 {
		t.Fatalf("checklist.Total = %d, want 6", checklist.Total)
	}
	if checklist.Completed != 6 {
		t.Fatalf("checklist.Completed = %d, want 6", checklist.Completed)
	}
}

func TestIncidentBundle(t *testing.T) {
	now := time.Now().UTC().Add(-20 * time.Minute)
	message := domain.Message{
		ID:          42,
		WorkspaceID: 1,
		ToEmail:     "user@example.com",
		Status:      "failed",
		CreatedAt:   now,
		UpdatedAt:   now.Add(5 * time.Minute),
	}
	base := &integrationStore{
		messages: map[int64]domain.Message{
			42: message,
		},
		suppressions: make(map[string]bool),
		unsubscribes: make(map[string]bool),
	}
	store := &trackCStore{
		integrationStore: base,
		attemptsByID: map[int64][]domain.MessageAttempt{
			42: {
				{ID: 1, MessageID: 42, AttemptNo: 1, SMTPProvider: "primary", Success: false, CreatedAt: now.Add(1 * time.Minute)},
				{ID: 2, MessageID: 42, AttemptNo: 2, SMTPProvider: "primary", Success: false, CreatedAt: now.Add(2 * time.Minute)},
			},
		},
		webhookLogs: []domain.WebhookEvent{
			{ID: 700, WorkspaceID: 1, Email: "user@example.com", Status: "dead_letter", CreatedAt: now.Add(3 * time.Minute)},
			{ID: 701, WorkspaceID: 1, Email: "other@example.com", Status: "applied", CreatedAt: now.Add(3 * time.Minute)},
		},
	}

	svc := NewMessageService(store, nil, nil, nil, nil, nil, 3, 3, 0, 0, false, 0, 0, 0)
	bundle, err := svc.IncidentBundle(context.Background(), 1, 42)
	if err != nil {
		t.Fatalf("IncidentBundle error = %v", err)
	}
	if bundle.MessageID != 42 {
		t.Fatalf("bundle.MessageID = %d, want 42", bundle.MessageID)
	}
	if len(bundle.Attempts) != 2 {
		t.Fatalf("len(bundle.Attempts) = %d, want 2", len(bundle.Attempts))
	}
	if len(bundle.WebhookOutcomes) != 1 {
		t.Fatalf("len(bundle.WebhookOutcomes) = %d, want 1", len(bundle.WebhookOutcomes))
	}
	if bundle.Summary.FailedAttempts != 2 {
		t.Fatalf("bundle.Summary.FailedAttempts = %d, want 2", bundle.Summary.FailedAttempts)
	}
	if bundle.Summary.DeadLetterWebhooks != 1 {
		t.Fatalf("bundle.Summary.DeadLetterWebhooks = %d, want 1", bundle.Summary.DeadLetterWebhooks)
	}
}

type trackCStore struct {
	*integrationStore
	webhookLogs  []domain.WebhookEvent
	attemptsByID map[int64][]domain.MessageAttempt
}

func (s *trackCStore) ListMessageAttempts(_ context.Context, messageID int64) ([]domain.MessageAttempt, error) {
	if s.attemptsByID == nil {
		return nil, nil
	}
	out := append([]domain.MessageAttempt(nil), s.attemptsByID[messageID]...)
	return out, nil
}

func (s *trackCStore) ListWebhookEvents(_ context.Context, workspaceID int64, limit int, status string, from, to time.Time) ([]domain.WebhookEvent, error) {
	out := make([]domain.WebhookEvent, 0, len(s.webhookLogs))
	for _, e := range s.webhookLogs {
		if e.WorkspaceID != workspaceID {
			continue
		}
		if status != "" && e.Status != status {
			continue
		}
		if !from.IsZero() && e.CreatedAt.Before(from) {
			continue
		}
		if !to.IsZero() && !e.CreatedAt.Before(to) {
			continue
		}
		out = append(out, e)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
