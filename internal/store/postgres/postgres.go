package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/srmdn/maild/internal/domain"
)

type Store struct {
	db *sql.DB
}

func New(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	ctxPing, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctxPing); err != nil {
		return nil, err
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Check(ctx context.Context) bool {
	ctxPing, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.db.PingContext(ctxPing) == nil
}

func (s *Store) EnsureDefaultWorkspace(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO workspaces (id, name) VALUES (1, 'default') ON CONFLICT (id) DO NOTHING`)
	return err
}

func (s *Store) IsSuppressed(ctx context.Context, workspaceID int64, email string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM suppressions WHERE workspace_id = $1 AND email = $2)`, workspaceID, email).Scan(&exists)
	return exists, err
}

func (s *Store) AddSuppression(ctx context.Context, workspaceID int64, email, reason string) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO suppressions (workspace_id, email, reason)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (workspace_id, email) DO UPDATE SET reason = EXCLUDED.reason`,
		workspaceID, email, reason,
	)
	return err
}

func (s *Store) IsUnsubscribed(ctx context.Context, workspaceID int64, email string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM unsubscribes WHERE workspace_id = $1 AND email = $2)`, workspaceID, email).Scan(&exists)
	return exists, err
}

func (s *Store) AddUnsubscribe(ctx context.Context, workspaceID int64, email, reason string) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO unsubscribes (workspace_id, email, reason)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (workspace_id, email) DO UPDATE SET reason = EXCLUDED.reason`,
		workspaceID, email, reason,
	)
	return err
}

func (s *Store) UpsertDomainVerification(ctx context.Context, workspaceID int64, domain string, verified bool) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO domains (workspace_id, domain, verified)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (workspace_id, domain) DO UPDATE SET verified = EXCLUDED.verified`,
		workspaceID, domain, verified,
	)
	return err
}

func (s *Store) UpsertSMTPAccountEncrypted(ctx context.Context, workspaceID int64, name string, encryptedPayload []byte) error {
	var hasAny bool
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM smtp_accounts WHERE workspace_id = $1)`,
		workspaceID,
	).Scan(&hasAny); err != nil {
		return err
	}

	activate := !hasAny
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO smtp_accounts (workspace_id, name, encrypted_payload, is_active)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (workspace_id, name) DO UPDATE
		 SET encrypted_payload = EXCLUDED.encrypted_payload, updated_at = now()`,
		workspaceID, name, encryptedPayload, activate,
	)
	return err
}

func (s *Store) GetSMTPAccountEncrypted(ctx context.Context, workspaceID int64) ([]byte, bool, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT encrypted_payload
		 FROM smtp_accounts
		 WHERE workspace_id = $1
		 ORDER BY is_active DESC, updated_at DESC
		 LIMIT 1`,
		workspaceID,
	)
	var payload []byte
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return payload, true, nil
}

func (s *Store) SetActiveSMTPAccount(ctx context.Context, workspaceID int64, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `UPDATE smtp_accounts SET is_active = FALSE WHERE workspace_id = $1`, workspaceID); err != nil {
		return err
	}
	result, err := tx.ExecContext(
		ctx,
		`UPDATE smtp_accounts
		 SET is_active = TRUE, updated_at = now()
		 WHERE workspace_id = $1 AND name = $2`,
		workspaceID, strings.TrimSpace(name),
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return errors.New("smtp account not found")
	}
	return tx.Commit()
}

func (s *Store) ListSMTPAccounts(ctx context.Context, workspaceID int64) ([]domain.SMTPAccountSummary, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT workspace_id, name, is_active, to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		 FROM smtp_accounts
		 WHERE workspace_id = $1
		 ORDER BY is_active DESC, updated_at DESC`,
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.SMTPAccountSummary
	for rows.Next() {
		var a domain.SMTPAccountSummary
		if err := rows.Scan(&a.WorkspaceID, &a.Name, &a.Active, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) CreateMessage(ctx context.Context, m domain.Message) (domain.Message, error) {
	row := s.db.QueryRowContext(
		ctx,
		`INSERT INTO messages (workspace_id, from_email, to_email, subject, body_text, status, scheduled_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, workspace_id, from_email, to_email, subject, body_text, status, scheduled_at, created_at, updated_at`,
		m.WorkspaceID, m.FromEmail, m.ToEmail, m.Subject, m.BodyText, m.Status, m.ScheduledAt,
	)

	var created domain.Message
	err := row.Scan(
		&created.ID,
		&created.WorkspaceID,
		&created.FromEmail,
		&created.ToEmail,
		&created.Subject,
		&created.BodyText,
		&created.Status,
		&created.ScheduledAt,
		&created.CreatedAt,
		&created.UpdatedAt,
	)
	return created, err
}

func (s *Store) GetMessage(ctx context.Context, id int64) (domain.Message, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, workspace_id, from_email, to_email, subject, body_text, status, scheduled_at, created_at, updated_at
		 FROM messages WHERE id = $1`,
		id,
	)

	var m domain.Message
	err := row.Scan(
		&m.ID,
		&m.WorkspaceID,
		&m.FromEmail,
		&m.ToEmail,
		&m.Subject,
		&m.BodyText,
		&m.Status,
		&m.ScheduledAt,
		&m.CreatedAt,
		&m.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Message{}, errors.New("message not found")
	}
	return m, err
}

func (s *Store) SetMessageStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET status = $2, updated_at = now() WHERE id = $1`, id, status)
	return err
}

func (s *Store) TransitionMessageStatus(ctx context.Context, id int64, fromStatus, toStatus string) (bool, error) {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE messages
		 SET status = $3, updated_at = now()
		 WHERE id = $1 AND status = $2`,
		id, fromStatus, toStatus,
	)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (s *Store) NextAttemptNo(ctx context.Context, messageID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt_no), 0) + 1 FROM message_attempts WHERE message_id = $1`, messageID).Scan(&n)
	return n, err
}

func (s *Store) InsertAttempt(ctx context.Context, messageID int64, attemptNo int, provider, response string, success bool) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO message_attempts (message_id, attempt_no, smtp_provider, smtp_response, success)
		 VALUES ($1, $2, $3, $4, $5)`,
		messageID, attemptNo, provider, response, success,
	)
	return err
}

func (s *Store) ListMessageAttempts(ctx context.Context, messageID int64) ([]domain.MessageAttempt, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, message_id, attempt_no, smtp_provider, smtp_response, success, created_at
		 FROM message_attempts
		 WHERE message_id = $1
		 ORDER BY attempt_no ASC`,
		messageID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.MessageAttempt
	for rows.Next() {
		var a domain.MessageAttempt
		if err := rows.Scan(&a.ID, &a.MessageID, &a.AttemptNo, &a.SMTPProvider, &a.SMTPResponse, &a.Success, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ListMessages(ctx context.Context, workspaceID int64, limit int) ([]domain.Message, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, workspace_id, from_email, to_email, subject, body_text, status, scheduled_at, created_at, updated_at
		 FROM messages
		 WHERE workspace_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		workspaceID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Message
	for rows.Next() {
		var m domain.Message
		if err := rows.Scan(
			&m.ID, &m.WorkspaceID, &m.FromEmail, &m.ToEmail, &m.Subject, &m.BodyText, &m.Status, &m.ScheduledAt, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) CountMessagesSince(ctx context.Context, workspaceID int64, recipientDomain string, since time.Time) (int64, error) {
	var count int64
	query := `SELECT COUNT(1)
	          FROM messages
	          WHERE workspace_id = $1
	            AND created_at >= $2`
	args := []any{workspaceID, since}
	if recipientDomain != "" {
		query += ` AND lower(split_part(to_email, '@', 2)) = $3`
		args = append(args, strings.ToLower(strings.TrimSpace(recipientDomain)))
	}
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) UpsertWorkspacePolicy(ctx context.Context, p domain.WorkspacePolicy) error {
	blocked := strings.Join(p.BlockedRecipientDomains, ",")
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO workspace_policies (workspace_id, rate_limit_workspace_per_hour, rate_limit_domain_per_hour, blocked_recipient_domains, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (workspace_id) DO UPDATE
		 SET rate_limit_workspace_per_hour = EXCLUDED.rate_limit_workspace_per_hour,
		     rate_limit_domain_per_hour = EXCLUDED.rate_limit_domain_per_hour,
		     blocked_recipient_domains = EXCLUDED.blocked_recipient_domains,
		     updated_at = now()`,
		p.WorkspaceID, p.RateLimitWorkspacePerHour, p.RateLimitDomainPerHour, blocked,
	)
	return err
}

func (s *Store) GetWorkspacePolicy(ctx context.Context, workspaceID int64) (domain.WorkspacePolicy, bool, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT workspace_id, rate_limit_workspace_per_hour, rate_limit_domain_per_hour, blocked_recipient_domains
		 FROM workspace_policies
		 WHERE workspace_id = $1`,
		workspaceID,
	)
	var p domain.WorkspacePolicy
	var blocked string
	if err := row.Scan(&p.WorkspaceID, &p.RateLimitWorkspacePerHour, &p.RateLimitDomainPerHour, &blocked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.WorkspacePolicy{}, false, nil
		}
		return domain.WorkspacePolicy{}, false, err
	}
	p.BlockedRecipientDomains = splitDomains(blocked)
	return p, true, nil
}

func (s *Store) InsertMeteringEvent(ctx context.Context, e domain.MeteringEvent) error {
	var metadata any
	if strings.TrimSpace(e.Metadata) != "" {
		metadata = e.Metadata
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO metering_events (workspace_id, message_id, event_type, quantity, metadata)
		 VALUES ($1, NULLIF($2, 0), $3, $4, $5::jsonb)`,
		e.WorkspaceID, e.MessageID, e.EventType, e.Quantity, metadata,
	)
	return err
}

func (s *Store) MeteringSummary(ctx context.Context, workspaceID int64, from, to time.Time) ([]domain.MeteringSummaryItem, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT event_type, COALESCE(SUM(quantity), 0) AS total
		 FROM metering_events
		 WHERE workspace_id = $1
		   AND created_at >= $2
		   AND created_at < $3
		 GROUP BY event_type
		 ORDER BY event_type ASC`,
		workspaceID, from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.MeteringSummaryItem
	for rows.Next() {
		var item domain.MeteringSummaryItem
		if err := rows.Scan(&item.EventType, &item.Total); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ExportMessageLogsCSV(ctx context.Context, workspaceID int64, limit int) (string, error) {
	if limit < 1 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, from_email, to_email, subject, status, created_at, updated_at
		 FROM messages
		 WHERE workspace_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		workspaceID, limit,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type rowData struct {
		ID        int64
		FromEmail string
		ToEmail   string
		Subject   string
		Status    string
		CreatedAt time.Time
		UpdatedAt time.Time
	}
	records := make([]rowData, 0, limit)
	for rows.Next() {
		var r rowData
		if err := rows.Scan(&r.ID, &r.FromEmail, &r.ToEmail, &r.Subject, &r.Status, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return "", err
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("id,from_email,to_email,subject,status,created_at,updated_at\n")
	for _, r := range records {
		b.WriteString(
			strings.Join([]string{
				int64ToString(r.ID),
				csvEscape(r.FromEmail),
				csvEscape(r.ToEmail),
				csvEscape(r.Subject),
				csvEscape(r.Status),
				csvEscape(r.CreatedAt.UTC().Format(time.RFC3339)),
				csvEscape(r.UpdatedAt.UTC().Format(time.RFC3339)),
			}, ","),
		)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func (s *Store) InsertWebhookEvent(ctx context.Context, e domain.WebhookEvent) (domain.WebhookEvent, error) {
	row := s.db.QueryRowContext(
		ctx,
		`INSERT INTO webhook_events (workspace_id, event_type, email, reason, status, attempt_count, last_error, raw_payload)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, workspace_id, event_type, email, reason, status, attempt_count, last_error, raw_payload, created_at`,
		e.WorkspaceID,
		e.EventType,
		e.Email,
		e.Reason,
		e.Status,
		e.AttemptCount,
		e.LastError,
		e.RawPayload,
	)

	var out domain.WebhookEvent
	var raw sql.NullString
	var email sql.NullString
	var reason sql.NullString
	var lastError sql.NullString
	if err := row.Scan(
		&out.ID,
		&out.WorkspaceID,
		&out.EventType,
		&email,
		&reason,
		&out.Status,
		&out.AttemptCount,
		&lastError,
		&raw,
		&out.CreatedAt,
	); err != nil {
		return domain.WebhookEvent{}, err
	}
	if email.Valid {
		out.Email = email.String
	}
	if reason.Valid {
		out.Reason = reason.String
	}
	if lastError.Valid {
		out.LastError = lastError.String
	}
	if raw.Valid {
		out.RawPayload = raw.String
	}
	return out, nil
}

func splitDomains(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		d := strings.ToLower(strings.TrimSpace(p))
		if d == "" {
			continue
		}
		out = append(out, d)
	}
	return out
}

func int64ToString(v int64) string {
	return strconv.FormatInt(v, 10)
}

func csvEscape(v string) string {
	escaped := strings.ReplaceAll(v, `"`, `""`)
	return `"` + escaped + `"`
}

func (s *Store) ListWebhookEvents(ctx context.Context, workspaceID int64, limit int, status string) ([]domain.WebhookEvent, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, workspace_id, event_type, email, reason, status, attempt_count, last_error, raw_payload, created_at
		 FROM webhook_events
		 WHERE workspace_id = $1
		   AND ($2 = '' OR status = $2)
		 ORDER BY created_at DESC
		 LIMIT $3`,
		workspaceID, status, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.WebhookEvent
	for rows.Next() {
		var e domain.WebhookEvent
		var raw sql.NullString
		var email sql.NullString
		var reason sql.NullString
		var lastError sql.NullString
		if err := rows.Scan(
			&e.ID,
			&e.WorkspaceID,
			&e.EventType,
			&email,
			&reason,
			&e.Status,
			&e.AttemptCount,
			&lastError,
			&raw,
			&e.CreatedAt,
		); err != nil {
			return nil, err
		}
		if email.Valid {
			e.Email = email.String
		}
		if reason.Valid {
			e.Reason = reason.String
		}
		if lastError.Valid {
			e.LastError = lastError.String
		}
		if raw.Valid {
			e.RawPayload = raw.String
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
