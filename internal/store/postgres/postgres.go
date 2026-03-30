package postgres

import (
	"context"
	"database/sql"
	"errors"
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
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO smtp_accounts (workspace_id, name, encrypted_payload)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (workspace_id) DO UPDATE
		 SET name = EXCLUDED.name, encrypted_payload = EXCLUDED.encrypted_payload, updated_at = now()`,
		workspaceID, name, encryptedPayload,
	)
	return err
}

func (s *Store) GetSMTPAccountEncrypted(ctx context.Context, workspaceID int64) ([]byte, bool, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT encrypted_payload FROM smtp_accounts WHERE workspace_id = $1`,
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
