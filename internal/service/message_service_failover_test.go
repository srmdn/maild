package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/srmdn/maild/internal/domain"
)

func TestTryAutoFailoverDisabled(t *testing.T) {
	store := &autoFailoverStore{
		failures: 10,
		accounts: []domain.SMTPAccountSummary{
			{Name: "primary", Active: true, UpdatedAt: time.Now().UTC().Add(-30 * time.Minute)},
			{Name: "standby", Active: false, UpdatedAt: time.Now().UTC().Add(-30 * time.Minute)},
		},
	}
	svc := NewMessageService(store, nil, nil, nil, nil, nil, 1, 1, 0, 0, false, 3, 5*time.Minute, 1*time.Minute)

	switched, toProvider, err := svc.tryAutoFailover(context.Background(), 1, "primary")
	if err != nil {
		t.Fatalf("tryAutoFailover error = %v", err)
	}
	if switched || toProvider != "" {
		t.Fatalf("unexpected switch result: switched=%v to=%q", switched, toProvider)
	}
	if store.setActiveCalls != 0 {
		t.Fatalf("setActiveCalls = %d, want 0", store.setActiveCalls)
	}
}

func TestTryAutoFailoverThresholdNotMet(t *testing.T) {
	store := &autoFailoverStore{
		failures: 1,
		accounts: []domain.SMTPAccountSummary{
			{Name: "primary", Active: true, UpdatedAt: time.Now().UTC().Add(-30 * time.Minute)},
			{Name: "standby", Active: false, UpdatedAt: time.Now().UTC().Add(-30 * time.Minute)},
		},
	}
	svc := NewMessageService(store, nil, nil, nil, nil, nil, 1, 1, 0, 0, true, 3, 5*time.Minute, 1*time.Minute)

	switched, toProvider, err := svc.tryAutoFailover(context.Background(), 1, "primary")
	if err != nil {
		t.Fatalf("tryAutoFailover error = %v", err)
	}
	if switched || toProvider != "" {
		t.Fatalf("unexpected switch result: switched=%v to=%q", switched, toProvider)
	}
	if store.setActiveCalls != 0 {
		t.Fatalf("setActiveCalls = %d, want 0", store.setActiveCalls)
	}
}

func TestTryAutoFailoverCooldownBlocksSwitch(t *testing.T) {
	store := &autoFailoverStore{
		failures: 5,
		accounts: []domain.SMTPAccountSummary{
			{Name: "primary", Active: true, UpdatedAt: time.Now().UTC().Add(-10 * time.Second)},
			{Name: "standby", Active: false, UpdatedAt: time.Now().UTC().Add(-30 * time.Minute)},
		},
	}
	svc := NewMessageService(store, nil, nil, nil, nil, nil, 1, 1, 0, 0, true, 3, 5*time.Minute, 1*time.Minute)

	switched, toProvider, err := svc.tryAutoFailover(context.Background(), 1, "primary")
	if err != nil {
		t.Fatalf("tryAutoFailover error = %v", err)
	}
	if switched || toProvider != "" {
		t.Fatalf("unexpected switch result: switched=%v to=%q", switched, toProvider)
	}
	if store.setActiveCalls != 0 {
		t.Fatalf("setActiveCalls = %d, want 0", store.setActiveCalls)
	}
}

func TestTryAutoFailoverSwitchesToStandby(t *testing.T) {
	store := &autoFailoverStore{
		failures: 7,
		accounts: []domain.SMTPAccountSummary{
			{Name: "primary", Active: true, UpdatedAt: time.Now().UTC().Add(-20 * time.Minute)},
			{Name: "standby-a", Active: false, UpdatedAt: time.Now().UTC().Add(-30 * time.Minute)},
		},
	}
	svc := NewMessageService(store, nil, nil, nil, nil, nil, 1, 1, 0, 0, true, 3, 5*time.Minute, 1*time.Minute)

	switched, toProvider, err := svc.tryAutoFailover(context.Background(), 1, "primary")
	if err != nil {
		t.Fatalf("tryAutoFailover error = %v", err)
	}
	if !switched {
		t.Fatalf("switched = false, want true")
	}
	if toProvider != "standby-a" {
		t.Fatalf("toProvider = %q, want standby-a", toProvider)
	}
	if store.setActiveCalls != 1 {
		t.Fatalf("setActiveCalls = %d, want 1", store.setActiveCalls)
	}
	if store.lastSetActive != "standby-a" {
		t.Fatalf("lastSetActive = %q, want standby-a", store.lastSetActive)
	}
}

func TestTryAutoFailoverSetActiveError(t *testing.T) {
	store := &autoFailoverStore{
		failures: 7,
		accounts: []domain.SMTPAccountSummary{
			{Name: "primary", Active: true, UpdatedAt: time.Now().UTC().Add(-20 * time.Minute)},
			{Name: "standby-a", Active: false, UpdatedAt: time.Now().UTC().Add(-30 * time.Minute)},
		},
		setActiveErr: errors.New("db write failed"),
	}
	svc := NewMessageService(store, nil, nil, nil, nil, nil, 1, 1, 0, 0, true, 3, 5*time.Minute, 1*time.Minute)

	switched, toProvider, err := svc.tryAutoFailover(context.Background(), 1, "primary")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if switched || toProvider != "" {
		t.Fatalf("unexpected switch result: switched=%v to=%q", switched, toProvider)
	}
}

type autoFailoverStore struct {
	failures       int64
	accounts       []domain.SMTPAccountSummary
	setActiveErr   error
	setActiveCalls int
	lastSetActive  string
}

func (s *autoFailoverStore) EnsureDefaultWorkspace(context.Context) error { return nil }
func (s *autoFailoverStore) IsSuppressed(context.Context, int64, string) (bool, error) {
	return false, nil
}
func (s *autoFailoverStore) AddSuppression(context.Context, int64, string, string) error { return nil }
func (s *autoFailoverStore) IsUnsubscribed(context.Context, int64, string) (bool, error) {
	return false, nil
}
func (s *autoFailoverStore) AddUnsubscribe(context.Context, int64, string, string) error { return nil }
func (s *autoFailoverStore) UpsertSMTPAccountEncrypted(context.Context, int64, string, []byte) error {
	return nil
}
func (s *autoFailoverStore) GetSMTPAccountEncrypted(context.Context, int64) ([]byte, bool, error) {
	return nil, false, nil
}
func (s *autoFailoverStore) SetActiveSMTPAccount(_ context.Context, _ int64, name string) error {
	s.setActiveCalls++
	s.lastSetActive = name
	return s.setActiveErr
}
func (s *autoFailoverStore) ListSMTPAccounts(context.Context, int64) ([]domain.SMTPAccountSummary, error) {
	return s.accounts, nil
}
func (s *autoFailoverStore) CountFailedAttemptsByProviderSince(context.Context, int64, string, time.Time) (int64, error) {
	return s.failures, nil
}
func (s *autoFailoverStore) CreateMessage(context.Context, domain.Message) (domain.Message, error) {
	return domain.Message{}, nil
}
func (s *autoFailoverStore) GetMessage(context.Context, int64) (domain.Message, error) {
	return domain.Message{}, nil
}
func (s *autoFailoverStore) SetMessageStatus(context.Context, int64, string) error { return nil }
func (s *autoFailoverStore) TransitionMessageStatus(context.Context, int64, string, string) (bool, error) {
	return false, nil
}
func (s *autoFailoverStore) NextAttemptNo(context.Context, int64) (int, error) { return 0, nil }
func (s *autoFailoverStore) InsertAttempt(context.Context, int64, int, string, string, bool) error {
	return nil
}
func (s *autoFailoverStore) ListMessageAttempts(context.Context, int64) ([]domain.MessageAttempt, error) {
	return nil, nil
}
func (s *autoFailoverStore) ListMessages(context.Context, int64, int, time.Time, time.Time) ([]domain.Message, error) {
	return nil, nil
}
func (s *autoFailoverStore) CountMessagesSince(context.Context, int64, string, time.Time) (int64, error) {
	return 0, nil
}
func (s *autoFailoverStore) UpsertWorkspacePolicy(context.Context, domain.WorkspacePolicy) error {
	return nil
}
func (s *autoFailoverStore) GetWorkspacePolicy(context.Context, int64) (domain.WorkspacePolicy, bool, error) {
	return domain.WorkspacePolicy{}, false, nil
}
func (s *autoFailoverStore) InsertMeteringEvent(context.Context, domain.MeteringEvent) error {
	return nil
}
func (s *autoFailoverStore) MeteringSummary(context.Context, int64, time.Time, time.Time) ([]domain.MeteringSummaryItem, error) {
	return nil, nil
}
func (s *autoFailoverStore) ExportMessageLogsCSV(context.Context, int64, int) (string, error) {
	return "", nil
}
func (s *autoFailoverStore) InsertWebhookEvent(context.Context, domain.WebhookEvent) (domain.WebhookEvent, error) {
	return domain.WebhookEvent{}, nil
}
func (s *autoFailoverStore) ListWebhookEvents(context.Context, int64, int, string, time.Time, time.Time) ([]domain.WebhookEvent, error) {
	return nil, nil
}
func (s *autoFailoverStore) ListWebhookDeadLetters(context.Context, int64, int) ([]domain.WebhookEvent, error) {
	return nil, nil
}
func (s *autoFailoverStore) ListWebhookDeadLettersByID(context.Context, int64, []int64) ([]domain.WebhookEvent, error) {
	return nil, nil
}
func (s *autoFailoverStore) UpdateWebhookEventReplayResult(context.Context, int64, string, int, string) error {
	return nil
}
