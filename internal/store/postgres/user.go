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
