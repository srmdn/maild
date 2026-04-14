package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/srmdn/maild/internal/domain"
)

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (domain.User, error) {
	row := s.db.QueryRowContext(
		ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id, email, password_hash, created_at`,
		email, passwordHash,
	)
	var u domain.User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	return u, err
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email = $1`,
		email,
	)
	var u domain.User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, errors.New("user not found")
	}
	return u, err
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (domain.User, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE id = $1`,
		id,
	)
	var u domain.User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, errors.New("user not found")
	}
	return u, err
}

func (s *Store) CreateWorkspaceForUser(ctx context.Context, userID int64, workspaceName string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var workspaceID int64
	err = tx.QueryRowContext(
		ctx,
		`INSERT INTO workspaces (name) VALUES ($1) RETURNING id`,
		workspaceName,
	).Scan(&workspaceID)
	if err != nil {
		return 0, err
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO user_workspaces (user_id, workspace_id, role) VALUES ($1, $2, 'admin')`,
		userID, workspaceID,
	)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return workspaceID, nil
}

func (s *Store) GetUserWorkspace(ctx context.Context, userID int64) (domain.UserWithWorkspace, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT u.id, u.email, u.created_at, w.id, w.name, uw.role
		 FROM users u
		 JOIN user_workspaces uw ON uw.user_id = u.id
		 JOIN workspaces w ON w.id = uw.workspace_id
		 WHERE u.id = $1
		 LIMIT 1`,
		userID,
	)
	var u domain.UserWithWorkspace
	var createdAt time.Time
	err := row.Scan(&u.ID, &u.Email, &createdAt, &u.WorkspaceID, &u.WorkspaceName, &u.Role)
	u.CreatedAt = createdAt
	if errors.Is(err, sql.ErrNoRows) {
		return domain.UserWithWorkspace{}, errors.New("workspace not found for user")
	}
	return u, err
}

func (s *Store) EmailExists(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)`,
		email,
	).Scan(&exists)
	return exists, err
}

func (s *Store) GetOnboardingSeen(ctx context.Context, userID, workspaceID int64) (bool, error) {
	var seenAt sql.NullTime
	err := s.db.QueryRowContext(
		ctx,
		`SELECT onboarding_seen_at FROM user_workspaces WHERE user_id = $1 AND workspace_id = $2`,
		userID, workspaceID,
	).Scan(&seenAt)
	if err != nil {
		return false, err
	}
	return seenAt.Valid && !seenAt.Time.IsZero(), nil
}

func (s *Store) DismissOnboarding(ctx context.Context, userID, workspaceID int64) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE user_workspaces SET onboarding_seen_at = now() WHERE user_id = $1 AND workspace_id = $2`,
		userID, workspaceID,
	)
	return err
}

func (s *Store) GetOnboardingChecklistItems(ctx context.Context, workspaceID int64) (smtp, domain, policy, message bool, err error) {
	err = s.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM smtp_accounts WHERE workspace_id = $1 AND is_active = TRUE)`,
		workspaceID,
	).Scan(&smtp)
	if err != nil {
		return
	}
	err = s.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM domains WHERE workspace_id = $1 AND verified = TRUE)`,
		workspaceID,
	).Scan(&domain)
	if err != nil {
		return
	}
	err = s.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM workspace_policies WHERE workspace_id = $1)`,
		workspaceID,
	).Scan(&policy)
	if err != nil {
		return
	}
	err = s.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM messages WHERE workspace_id = $1 AND status IN ('sent','delivered','failed'))`,
		workspaceID,
	).Scan(&message)
	return
}

func (s *Store) CreateAPIKey(ctx context.Context, userID int64, name, keyHash string) (domain.UserAPIKey, error) {
	row := s.db.QueryRowContext(
		ctx,
		`INSERT INTO user_api_keys (user_id, name, key_hash) VALUES ($1, $2, $3) RETURNING id, user_id, name, last_used_at, created_at`,
		userID, name, keyHash,
	)
	var k domain.UserAPIKey
	var lastUsed sql.NullTime
	err := row.Scan(&k.ID, &k.UserID, &k.Name, &lastUsed, &k.CreatedAt)
	if err != nil {
		return domain.UserAPIKey{}, err
	}
	if lastUsed.Valid {
		k.LastUsedAt = &lastUsed.Time
	}
	return k, nil
}

func (s *Store) ListAPIKeys(ctx context.Context, userID int64) ([]domain.UserAPIKey, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, user_id, name, last_used_at, created_at FROM user_api_keys WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.UserAPIKey
	for rows.Next() {
		var k domain.UserAPIKey
		var lastUsed sql.NullTime
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &lastUsed, &k.CreatedAt); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			k.LastUsedAt = &lastUsed.Time
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAPIKey(ctx context.Context, userID, keyID int64) error {
	result, err := s.db.ExecContext(
		ctx,
		`DELETE FROM user_api_keys WHERE id = $1 AND user_id = $2`,
		keyID, userID,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return errors.New("api key not found")
	}
	return nil
}
