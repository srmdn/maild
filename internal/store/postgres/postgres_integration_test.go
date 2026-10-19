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
		user_api_keys, user_workspaces, users, workspaces
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
	// matching is case-insensitive regardless of stored casing
	if err := store.AddSuppression(ctx, 1, "MIXED@x.com", "manual"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := store.IsSuppressed(ctx, 1, "mixed@x.com"); !ok {
		t.Fatal("expected suppression match to be case-insensitive")
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

func TestStoreWebhookAtomicIdempotencyAndReplay(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.EnsureDefaultWorkspace(ctx); err != nil {
		t.Fatal(err)
	}

	event := domain.WebhookEvent{
		WorkspaceID: 1,
		IdentityKey: "test-provider:event-42",
		EventType:   "bounce_permanent",
		Email:       "duplicate@example.com",
		Reason:      "permanent bounce",
		RawPayload:  `{"id":"event-42"}`,
	}
	results := make(chan domain.WebhookEvent, 2)
	errs := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			result, err := store.ProcessWebhookEventAtomic(ctx, event)
			results <- result
			errs <- err
		}()
	}
	close(start)
	first, second := <-results, <-results
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if first.ID == 0 || first.ID != second.ID {
		t.Fatalf("duplicate calls returned IDs %d and %d; want the same persisted event", first.ID, second.ID)
	}
	var eventCount, suppressionCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM webhook_events WHERE workspace_id = 1 AND identity_key = $1`, event.IdentityKey).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM suppressions WHERE workspace_id = 1 AND email = $1`, event.Email).Scan(&suppressionCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 || suppressionCount != 1 {
		t.Fatalf("duplicate delivery created %d events and %d suppressions; want one of each", eventCount, suppressionCount)
	}

	dead, err := store.InsertWebhookEvent(ctx, domain.WebhookEvent{
		WorkspaceID: 1, IdentityKey: "test-provider:replay-43", EventType: "complaint",
		Email: "replay@example.com", Reason: "complaint", Status: "dead_letter", AttemptCount: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := store.ReplayWebhookEventAtomic(ctx, 999, dead.ID); err != nil || claimed {
		t.Fatalf("cross-workspace replay = claimed %v, err %v; want not claimed", claimed, err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		CREATE FUNCTION fail_webhook_replay_completion() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.status = 'replayed' THEN RAISE EXCEPTION 'injected replay completion failure'; END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER fail_webhook_replay_completion BEFORE UPDATE ON webhook_events
		FOR EACH ROW EXECUTE FUNCTION fail_webhook_replay_completion();
	`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.DB().Exec(`DROP TRIGGER IF EXISTS fail_webhook_replay_completion ON webhook_events`)
		_, _ = store.DB().Exec(`DROP FUNCTION IF EXISTS fail_webhook_replay_completion()`)
	})
	if _, claimed, err := store.ReplayWebhookEventAtomic(ctx, 1, dead.ID); err == nil || claimed {
		t.Fatalf("injected partial replay = claimed %v, err %v; want rollback error", claimed, err)
	}
	var status string
	if err := store.DB().QueryRowContext(ctx, `SELECT status FROM webhook_events WHERE id = $1`, dead.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "dead_letter" {
		t.Fatalf("status after rolled-back replay = %q, want dead_letter", status)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM suppressions WHERE workspace_id = 1 AND email = 'replay@example.com'`).Scan(&suppressionCount); err != nil {
		t.Fatal(err)
	}
	if suppressionCount != 0 {
		t.Fatalf("rolled-back replay left %d suppressions, want 0", suppressionCount)
	}
	if _, err := store.DB().ExecContext(ctx, `DROP TRIGGER fail_webhook_replay_completion ON webhook_events`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `DROP FUNCTION fail_webhook_replay_completion()`); err != nil {
		t.Fatal(err)
	}

	claimedCount := 0
	claimedResults := make(chan bool, 2)
	replayErrors := make(chan error, 2)
	replayStart := make(chan struct{})
	for range 2 {
		go func() {
			<-replayStart
			_, claimed, err := store.ReplayWebhookEventAtomic(ctx, 1, dead.ID)
			claimedResults <- claimed
			replayErrors <- err
		}()
	}
	close(replayStart)
	for range 2 {
		if err := <-replayErrors; err != nil {
			t.Fatal(err)
		}
		if <-claimedResults {
			claimedCount++
		}
	}
	if claimedCount != 1 {
		t.Fatalf("sequential replay claimed %d times, want once", claimedCount)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM suppressions WHERE workspace_id = 1 AND email = 'replay@example.com'`).Scan(&suppressionCount); err != nil {
		t.Fatal(err)
	}
	if suppressionCount != 1 {
		t.Fatalf("replay created %d suppressions, want one", suppressionCount)
	}
}

func TestReplayPreservesExistingSuppressionTimestamp(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.EnsureDefaultWorkspace(ctx); err != nil {
		t.Fatal(err)
	}

	const email = "already-suppressed@example.com"
	if err := store.AddSuppression(ctx, 1, email, "original permanent bounce"); err != nil {
		t.Fatal(err)
	}
	originalCreatedAt := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Microsecond)
	if _, err := store.DB().ExecContext(ctx,
		`UPDATE suppressions SET created_at = $3 WHERE workspace_id = $1 AND email = $2`, 1, email, originalCreatedAt,
	); err != nil {
		t.Fatal(err)
	}

	dead, err := store.InsertWebhookEvent(ctx, domain.WebhookEvent{
		WorkspaceID: 1,
		IdentityKey: "test-provider:existing-suppression-replay",
		EventType:   "bounce_permanent",
		Email:       email,
		Reason:      "replayed permanent bounce",
		Status:      "dead_letter",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := store.ReplayWebhookEventAtomic(ctx, 1, dead.ID); err != nil || !claimed {
		t.Fatalf("ReplayWebhookEventAtomic() claimed = %t, err = %v; want a successful claim", claimed, err)
	}

	var count int
	var gotCreatedAt time.Time
	if err := store.DB().QueryRowContext(ctx,
		`SELECT count(*), min(created_at) FROM suppressions WHERE workspace_id = $1 AND email = $2`, 1, email,
	).Scan(&count, &gotCreatedAt); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("replay left %d suppression rows, want 1", count)
	}
	if !gotCreatedAt.Equal(originalCreatedAt) {
		t.Fatalf("suppression created_at after replay = %s, want original %s", gotCreatedAt, originalCreatedAt)
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

func TestSignupAfterDefaultWorkspaceBootstrap(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.EnsureDefaultWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := store.CreateUserWithWorkspace(ctx, "first@x.com", "hash", "first workspace")
	if err != nil {
		t.Fatal(err)
	}
	if first.WorkspaceID != 2 {
		t.Fatalf("first signup workspace ID = %d, want 2 after default workspace ID 1", first.WorkspaceID)
	}
	if first.Role != "admin" || first.ID == 0 || first.PasswordHash != "hash" {
		t.Fatalf("unexpected signup result: %+v", first)
	}

	if err := store.EnsureDefaultWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateUserWithWorkspace(ctx, "second@x.com", "hash", "second workspace")
	if err != nil {
		t.Fatal(err)
	}
	if second.WorkspaceID != 3 {
		t.Fatalf("second signup workspace ID = %d, want 3 after repeated bootstrap", second.WorkspaceID)
	}

	workspace, err := store.GetUserWorkspace(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.WorkspaceID != first.WorkspaceID || workspace.Role != "admin" {
		t.Fatalf("signup membership mismatch: %+v", workspace)
	}
}

func TestCreateUserWithWorkspaceRollsBackOnWorkspaceFailure(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.DB().ExecContext(ctx, `ALTER TABLE workspaces ADD CONSTRAINT test_reject_signup_workspace CHECK (name <> 'force-signup-rollback')`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.DB().ExecContext(context.Background(), `ALTER TABLE workspaces DROP CONSTRAINT IF EXISTS test_reject_signup_workspace`)
	})

	if _, err := store.CreateUserWithWorkspace(ctx, "rollback@x.com", "hash", "force-signup-rollback"); err == nil {
		t.Fatal("expected workspace insert to fail")
	}
	if exists, err := store.EmailExists(ctx, "rollback@x.com"); err != nil {
		t.Fatal(err)
	} else if exists {
		t.Fatal("user remained after signup transaction failed")
	}
}

func TestMessageQueueRecoveryRespectsRetryScheduleAndExpiresClaims(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.EnsureDefaultWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	stale, err := store.CreateMessage(ctx, domain.Message{WorkspaceID: 1, FromEmail: "from@x.com", ToEmail: "a@x.com", Subject: "stale", Status: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	futureAt := time.Now().Add(time.Hour)
	future, err := store.CreateMessage(ctx, domain.Message{WorkspaceID: 1, FromEmail: "from@x.com", ToEmail: "b@x.com", Subject: "future", Status: "queued", ScheduledAt: &futureAt})
	if err != nil {
		t.Fatal(err)
	}
	sending, err := store.CreateMessage(ctx, domain.Message{WorkspaceID: 1, FromEmail: "from@x.com", ToEmail: "c@x.com", Subject: "sending", Status: "sending"})
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	for _, id := range []int64{stale.ID, future.ID, sending.ID} {
		if _, err := store.DB().ExecContext(ctx, `UPDATE messages SET updated_at = $2 WHERE id = $1`, id, old); err != nil {
			t.Fatal(err)
		}
	}

	ids, err := store.ClaimQueuedMessagesForRecovery(ctx, time.Now().Add(-time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != stale.ID {
		t.Fatalf("recovered queued IDs = %v, want [%d]", ids, stale.ID)
	}
	if count, err := store.MarkStaleSendingUnknown(ctx, time.Now().Add(-15*time.Minute)); err != nil {
		t.Fatal(err)
	} else if count != 1 {
		t.Fatalf("marked unknown = %d, want 1", count)
	}
	got, err := store.GetMessage(ctx, sending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "unknown" {
		t.Fatalf("stale sending status = %q, want unknown", got.Status)
	}
	if updated, err := store.SetMessageRetryAt(ctx, sending.ID, time.Now()); err != nil {
		t.Fatal(err)
	} else if updated {
		t.Fatal("expired claim was requeued after being marked unknown")
	}
	got, err = store.GetMessage(ctx, future.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ScheduledAt == nil || !got.ScheduledAt.After(time.Now()) {
		t.Fatalf("future scheduled_at was not preserved: %v", got.ScheduledAt)
	}
}

func TestStoreUserAPIKeys(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	u, err := store.CreateUser(ctx, "k@x.com", "hash")
	if err != nil {
		t.Fatal(err)
	}

	ws, err := store.CreateWorkspaceForUser(ctx, u.ID, "keys workspace")
	if err != nil {
		t.Fatal(err)
	}
	k, err := store.CreateAPIKey(ctx, u.ID, ws, "k1", "hash1", "mk_demo1")
	if err != nil {
		t.Fatal(err)
	}
	if k.ID == 0 || k.Name != "k1" || k.WorkspaceID != ws || k.KeyPrefix != "mk_demo1" {
		t.Fatalf("bad api key: %+v", k)
	}
	keys, err := store.ListAPIKeys(ctx, u.ID, ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if _, err := store.CreateAPIKey(ctx, u.ID, ws, "k1", "hash2", "mk_demo2"); err == nil {
		t.Fatal("expected duplicate key name error")
	}

	identity, err := store.AuthenticateAPIKey(ctx, "hash1")
	if err != nil || identity.UserID != u.ID || identity.WorkspaceID != ws || identity.Role != "admin" {
		t.Fatalf("identity=%+v err=%v", identity, err)
	}
	keys, err = store.ListAPIKeys(ctx, u.ID, ws)
	if err != nil || keys[0].LastUsedAt == nil || keys[0].KeyPrefix != "mk_demo1" {
		t.Fatalf("last use/prefix: %+v %v", keys, err)
	}
	other, err := store.CreateWorkspaceForUser(ctx, u.ID, "other workspace")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteAPIKey(ctx, u.ID, other, k.ID); err == nil {
		t.Fatal("cross-workspace delete succeeded")
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE user_workspaces SET role = 'viewer' WHERE user_id=$1 AND workspace_id=$2`, u.ID, ws); err != nil {
		t.Fatal(err)
	}
	identity, err = store.AuthenticateAPIKey(ctx, "hash1")
	if err != nil || identity.Role != "viewer" {
		t.Fatalf("role change not observed: %+v %v", identity, err)
	}
	if _, err := store.CreateAPIKey(ctx, u.ID, ws, "viewer key", "viewerhash", "mk_view"); err == nil {
		t.Fatal("viewer could create key")
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM user_workspaces WHERE user_id=$1 AND workspace_id=$2`, u.ID, ws); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateAPIKey(ctx, "hash1"); err == nil {
		t.Fatal("removed membership still authenticates")
	}
	if err := store.DeleteAPIKey(ctx, u.ID, ws, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateAPIKey(ctx, "hash1"); err == nil {
		t.Fatal("revoked key still authenticates")
	}
	keys, _ = store.ListAPIKeys(ctx, u.ID, ws)
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys after delete, got %d", len(keys))
	}
	if err := store.DeleteAPIKey(ctx, u.ID, ws, 999999); err == nil {
		t.Fatal("expected error deleting nonexistent key")
	}
}
