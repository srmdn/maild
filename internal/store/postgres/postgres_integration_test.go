package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/srmdn/maild/internal/domain"
	"github.com/srmdn/maild/internal/migrate"
)

// MAILD_TEST_DSN must point to a Postgres dedicated to tests (e.g.
// postgres://maild:maild@localhost:5432/maild_test?sslmode=disable).
// When unset the integration tests are skipped so `make test` stays CI-safe.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("MAILD_TEST_DSN")
	if dsn == "" {
		t.Skip("MAILD_TEST_DSN not set; skipping store integration tests")
	}
	ctx := context.Background()
	store, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if err := migrate.Up(ctx, store.DB()); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	truncateAll(t, store.DB())
	return store
}

func truncateAll(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`TRUNCATE TABLE
		message_attempts, messages, suppressions, unsubscribes, smtp_accounts,
		webhook_events, workspace_policies, metering_events, domains,
		user_api_keys, user_workspaces, users, workspaces, schema_migrations
		RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func TestStoreSuppressionsAndUnsubscribes(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.EnsureDefaultWorkspace(ctx); err != nil {
		t.Fatal(err)
	}

	if ok, _ := store.IsSuppressed(ctx, 1, "a@x.com"); ok {
		t.Fatal("expected not suppressed initially")
	}
	if err := store.AddSuppression(ctx, 1, "a@x.com", "manual"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := store.IsSuppressed(ctx, 1, "a@x.com"); !ok {
		t.Fatal("expected suppressed after add")
	}
	if err := store.AddSuppression(ctx, 1, "a@x.com", "other"); err != nil {
		t.Fatal(err)
	}

	if ok, _ := store.IsUnsubscribed(ctx, 1, "b@x.com"); ok {
		t.Fatal("expected not unsubscribed initially")
	}
	if err := store.AddUnsubscribe(ctx, 1, "b@x.com", "user_unsubscribed"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := store.IsUnsubscribed(ctx, 1, "b@x.com"); !ok {
		t.Fatal("expected unsubscribed after add")
	}
}

func TestStoreSMTPAccounts(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.EnsureDefaultWorkspace(ctx); err != nil {
		t.Fatal(err)
	}

	primary := []byte("ciphertext-primary")
	if err := store.UpsertSMTPAccountEncrypted(ctx, 1, "primary", primary); err != nil {
		t.Fatal(err)
	}
	// first account becomes active automatically
	p, found, err := store.GetSMTPAccountEncrypted(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !found || string(p) != string(primary) {
		t.Fatalf("expected active primary; found=%v", found)
	}

	if err := store.UpsertSMTPAccountEncrypted(ctx, 1, "secondary", []byte("c2")); err != nil {
		t.Fatal(err)
	}
	p, _, _ = store.GetSMTPAccountEncrypted(ctx, 1)
	if string(p) != string(primary) {
		t.Fatal("active should stay primary after adding secondary")
	}

	list, err := store.ListSMTPAccounts(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(list))
	}

	if err := store.SetActiveSMTPAccount(ctx, 1, "secondary"); err != nil {
		t.Fatal(err)
	}
	p, _, _ = store.GetSMTPAccountEncrypted(ctx, 1)
	if string(p) != "c2" {
		t.Fatal("secondary should be active now")
	}

	if err := store.SetActiveSMTPAccount(ctx, 1, "does-not-exist"); err == nil {
		t.Fatal("expected error activating unknown account")
	}
}

func TestStoreMessagesAndAttempts(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.EnsureDefaultWorkspace(ctx); err != nil {
		t.Fatal(err)
	}

	m, err := store.CreateMessage(ctx, domain.Message{
		WorkspaceID: 1, FromEmail: "from@x.com", ToEmail: "to@x.com",
		Subject: "Hi", BodyText: "body", Status: "queued",
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID == 0 {
		t.Fatal("expected message id")
	}
	got, err := store.GetMessage(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "Hi" {
		t.Fatalf("subject mismatch: %q", got.Subject)
	}

	if err := store.SetMessageStatus(ctx, m.ID, "sent"); err != nil {
		t.Fatal(err)
	}
	if st, _ := store.GetMessage(ctx, m.ID); st.Status != "sent" {
		t.Fatalf("status mismatch: %q", st.Status)
	}

	if ok, _ := store.TransitionMessageStatus(ctx, m.ID, "sent", "delivered"); !ok {
		t.Fatal("expected transition sent->delivered")
	}
	if ok, _ := store.TransitionMessageStatus(ctx, m.ID, "queued", "sent"); ok {
		t.Fatal("expected false for invalid transition")
	}

	n, err := store.NextAttemptNo(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected attempt 1, got %d", n)
	}
	if err := store.InsertAttempt(ctx, m.ID, 1, "smtp.host", "ok", true); err != nil {
		t.Fatal(err)
	}
	n, _ = store.NextAttemptNo(ctx, m.ID)
	if n != 2 {
		t.Fatalf("expected attempt 2, got %d", n)
	}
	attempts, err := store.ListMessageAttempts(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || !attempts[0].Success {
		t.Fatalf("attempts mismatch: %+v", attempts)
	}

	msgs, err := store.ListMessages(ctx, 1, 10, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	count, err := store.CountMessagesSince(ctx, 1, "x.com", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}
}

func TestStoreWorkspacePolicy(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.EnsureDefaultWorkspace(ctx); err != nil {
		t.Fatal(err)
	}

	if _, found, _ := store.GetWorkspacePolicy(ctx, 1); found {
		t.Fatal("expected no policy initially")
	}

	pol := domain.WorkspacePolicy{
		WorkspaceID:               1,
		RateLimitWorkspacePerHour: 400,
		RateLimitDomainPerHour:    200,
		BlockedRecipientDomains:   []string{"mailinator.com", "tempmail.com"},
	}
	if err := store.UpsertWorkspacePolicy(ctx, pol); err != nil {
		t.Fatal(err)
	}
	got, found, err := store.GetWorkspacePolicy(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got.RateLimitWorkspacePerHour != 400 || got.RateLimitDomainPerHour != 200 {
		t.Fatalf("policy mismatch: %+v", got)
	}
	if len(got.BlockedRecipientDomains) != 2 || got.BlockedRecipientDomains[0] != "mailinator.com" {
		t.Fatalf("blocked domains mismatch: %v", got.BlockedRecipientDomains)
	}
}

func TestStoreMetering(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.EnsureDefaultWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	for _, e := range []domain.MeteringEvent{
		{WorkspaceID: 1, EventType: "sent", Quantity: 1},
		{WorkspaceID: 1, EventType: "sent", Quantity: 2},
		{WorkspaceID: 1, EventType: "bounce", Quantity: 1},
	} {
		if err := store.InsertMeteringEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	items, err := store.MeteringSummary(ctx, 1, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	byType := map[string]int64{}
	for _, it := range items {
		byType[it.EventType] = it.Total
	}
	if byType["sent"] != 3 || byType["bounce"] != 1 {
		t.Fatalf("metering mismatch: %v", byType)
	}
}

func TestStoreWebhookEvents(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.EnsureDefaultWorkspace(ctx); err != nil {
		t.Fatal(err)
	}

	processed, err := store.InsertWebhookEvent(ctx, domain.WebhookEvent{
		WorkspaceID: 1, EventType: "bounce", Email: "a@x.com", Reason: "r",
		Status: "processed", AttemptCount: 1, RawPayload: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	dead, err := store.InsertWebhookEvent(ctx, domain.WebhookEvent{
		WorkspaceID: 1, EventType: "bounce", Email: "b@x.com",
		Status: "dead_letter", LastError: "boom", RawPayload: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	all, err := store.ListWebhookEvents(ctx, 1, 10, "", time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 events, got %d", len(all))
	}

	deadLetters, err := store.ListWebhookEvents(ctx, 1, 10, "dead_letter", time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(deadLetters) != 1 {
		t.Fatalf("expected 1 dead letter, got %d", len(deadLetters))
	}

	byID, err := store.ListWebhookDeadLettersByID(ctx, 1, []int64{dead.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(byID) != 1 || byID[0].ID != dead.ID {
		t.Fatalf("dead-letter by id mismatch: %+v", byID)
	}

	if err := store.UpdateWebhookEventReplayResult(ctx, dead.ID, "replayed", 1, ""); err != nil {
		t.Fatal(err)
	}
	_ = processed
	all, _ = store.ListWebhookEvents(ctx, 1, 10, "", time.Time{}, time.Time{})
	var foundStatus string
	for _, e := range all {
		if e.ID == dead.ID {
			foundStatus = e.Status
		}
	}
	if foundStatus != "replayed" {
		t.Fatalf("expected status replayed, got %q", foundStatus)
	}
}

func TestStoreUsersAndWorkspaces(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	u, err := store.CreateUser(ctx, "u@x.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == 0 || u.PasswordHash != "hash" {
		t.Fatalf("bad user: %+v", u)
	}
	got, err := store.GetUserByEmail(ctx, "u@x.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != u.ID {
		t.Fatal("user mismatch")
	}
	if _, err := store.GetUserByEmail(ctx, "missing@x.com"); err == nil {
		t.Fatal("expected error for missing user")
	}
	if _, err := store.CreateUser(ctx, "u@x.com", "h2"); err == nil {
		t.Fatal("expected duplicate email error")
	}
	if exists, _ := store.EmailExists(ctx, "u@x.com"); !exists {
		t.Fatal("expected user to exist")
	}

	ws, err := store.CreateWorkspaceForUser(ctx, u.ID, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if ws == 0 {
		t.Fatal("expected workspace id")
	}
	uw, err := store.GetUserWorkspace(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if uw.WorkspaceName != "acme" || uw.Role != "admin" {
		t.Fatalf("user workspace mismatch: %+v", uw)
	}

	if seen, _ := store.GetOnboardingSeen(ctx, u.ID, ws); seen {
		t.Fatal("expected onboarding not seen")
	}
	if err := store.DismissOnboarding(ctx, u.ID, ws); err != nil {
		t.Fatal(err)
	}
	if seen, _ := store.GetOnboardingSeen(ctx, u.ID, ws); !seen {
		t.Fatal("expected onboarding seen")
	}

	smtp, dom, pol, msg, err := store.GetOnboardingChecklistItems(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	if smtp || dom || pol || msg {
		t.Fatalf("expected all checklist items false: %v %v %v %v", smtp, dom, pol, msg)
	}
}

func TestStoreUserAPIKeys(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	u, err := store.CreateUser(ctx, "k@x.com", "hash")
	if err != nil {
		t.Fatal(err)
	}

	k, err := store.CreateAPIKey(ctx, u.ID, "k1", "hash1")
	if err != nil {
		t.Fatal(err)
	}
	if k.ID == 0 || k.Name != "k1" {
		t.Fatalf("bad api key: %+v", k)
	}
	keys, err := store.ListAPIKeys(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if _, err := store.CreateAPIKey(ctx, u.ID, "k1", "hash2"); err == nil {
		t.Fatal("expected duplicate key name error")
	}

	if err := store.DeleteAPIKey(ctx, u.ID, k.ID); err != nil {
		t.Fatal(err)
	}
	keys, _ = store.ListAPIKeys(ctx, u.ID)
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys after delete, got %d", len(keys))
	}
	if err := store.DeleteAPIKey(ctx, u.ID, 999999); err == nil {
		t.Fatal("expected error deleting nonexistent key")
	}
}
