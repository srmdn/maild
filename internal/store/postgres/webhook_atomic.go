package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/srmdn/maild/internal/domain"
)

func (s *Store) ProcessWebhookEventAtomic(ctx context.Context, event domain.WebhookEvent) (domain.WebhookEvent, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WebhookEvent{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var created domain.WebhookEvent
	err = scanWebhookEvent(tx.QueryRowContext(ctx,
		`INSERT INTO webhook_events (workspace_id, identity_key, event_type, email, reason, status, attempt_count, last_error, raw_payload)
		 VALUES ($1, $2, $3, $4, $5, 'processing', 1, '', $6)
		 ON CONFLICT (workspace_id, identity_key) DO NOTHING
		 RETURNING id, workspace_id, event_type, identity_key, email, reason, status, attempt_count, last_error, raw_payload, created_at`,
		event.WorkspaceID, event.IdentityKey, event.EventType, nullableString(event.Email), nullableString(event.Reason), event.RawPayload,
	), &created)
	if errors.Is(err, sql.ErrNoRows) {
		if err := scanWebhookEvent(tx.QueryRowContext(ctx,
			`SELECT id, workspace_id, event_type, identity_key, email, reason, status, attempt_count, last_error, raw_payload, created_at
			 FROM webhook_events WHERE workspace_id = $1 AND identity_key = $2`, event.WorkspaceID, event.IdentityKey,
		), &created); err != nil {
			return domain.WebhookEvent{}, err
		}
		if err := tx.Commit(); err != nil {
			return domain.WebhookEvent{}, err
		}
		return created, nil
	}
	if err != nil {
		return domain.WebhookEvent{}, err
	}

	status, lastError, err := applyWebhookEventTx(ctx, tx, created)
	if err != nil {
		return domain.WebhookEvent{}, err
	}
	created.Status = status
	created.LastError = lastError
	if _, err := tx.ExecContext(ctx,
		`UPDATE webhook_events SET status = $2, last_error = $3 WHERE id = $1`, created.ID, status, lastError,
	); err != nil {
		return domain.WebhookEvent{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.WebhookEvent{}, err
	}
	return created, nil
}

func (s *Store) ReplayWebhookEventAtomic(ctx context.Context, workspaceID, eventID int64) (domain.WebhookEvent, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WebhookEvent{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var event domain.WebhookEvent
	err = scanWebhookEvent(tx.QueryRowContext(ctx,
		`SELECT id, workspace_id, event_type, identity_key, email, reason, status, attempt_count, last_error, raw_payload, created_at
		 FROM webhook_events WHERE workspace_id = $1 AND id = $2 AND status = 'dead_letter' FOR UPDATE SKIP LOCKED`, workspaceID, eventID,
	), &event)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return domain.WebhookEvent{}, false, err
		}
		return domain.WebhookEvent{}, false, nil
	}
	if err != nil {
		return domain.WebhookEvent{}, false, err
	}

	status, lastError, err := applyWebhookEventTx(ctx, tx, event)
	if err != nil {
		return domain.WebhookEvent{}, false, err
	}
	if status == "applied" {
		status = "replayed"
	}
	event.Status = status
	event.AttemptCount++
	event.LastError = lastError
	if _, err := tx.ExecContext(ctx,
		`UPDATE webhook_events SET status = $2, attempt_count = $3, last_error = $4 WHERE id = $1`,
		event.ID, event.Status, event.AttemptCount, event.LastError,
	); err != nil {
		return domain.WebhookEvent{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return domain.WebhookEvent{}, false, err
	}
	return event, true, nil
}

func applyWebhookEventTx(ctx context.Context, tx *sql.Tx, event domain.WebhookEvent) (string, string, error) {
	if strings.TrimSpace(event.Email) == "" {
		return "dead_letter", "replay failed: missing email", nil
	}
	eventType := classifyLegacyBounce(event.EventType, event.Reason)
	switch eventType {
	case "bounce_temporary":
		return "applied", "", nil
	case "bounce_permanent", "complaint":
		_, err := tx.ExecContext(ctx,
			`INSERT INTO suppressions (workspace_id, email, reason) VALUES ($1, $2, $3)
			 ON CONFLICT (workspace_id, email) DO UPDATE SET reason = EXCLUDED.reason`,
			event.WorkspaceID, normalizeEmail(event.Email), event.Reason,
		)
		return "applied", "", err
	case "unsubscribe":
		_, err := tx.ExecContext(ctx,
			`INSERT INTO unsubscribes (workspace_id, email, reason) VALUES ($1, $2, $3)
			 ON CONFLICT (workspace_id, email) DO UPDATE SET reason = EXCLUDED.reason`,
			event.WorkspaceID, normalizeEmail(event.Email), event.Reason,
		)
		return "applied", "", err
	case "bounce_unclassified", "bounce":
		return "dead_letter", "unclassified bounce; recipient was not suppressed", nil
	default:
		return "dead_letter", fmt.Sprintf("unsupported webhook event type: %s", eventType), nil
	}
}

func classifyLegacyBounce(eventType, reason string) string {
	if eventType != "bounce" {
		return eventType
	}
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "hard_bounce", "permanent", "permanent/general", "permanent/suppressed":
		return "bounce_permanent"
	case "soft_bounce", "transient", "temporary", "temporary/general", "transient/general":
		return "bounce_temporary"
	default:
		return "bounce_unclassified"
	}
}

type webhookEventScanner interface {
	Scan(dest ...any) error
}

func scanWebhookEvent(row webhookEventScanner, event *domain.WebhookEvent) error {
	var email, reason, lastError, raw sql.NullString
	err := row.Scan(
		&event.ID, &event.WorkspaceID, &event.EventType, &event.IdentityKey,
		&email, &reason, &event.Status, &event.AttemptCount, &lastError, &raw, &event.CreatedAt,
	)
	if err != nil {
		return err
	}
	if email.Valid {
		event.Email = email.String
	}
	if reason.Valid {
		event.Reason = reason.String
	}
	if lastError.Valid {
		event.LastError = lastError.String
	}
	if raw.Valid {
		event.RawPayload = raw.String
	}
	return nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
