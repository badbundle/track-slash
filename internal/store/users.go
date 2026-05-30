package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bradleymackey/track-slash/internal/model"
)

func (s *Store) CreateUser(ctx context.Context, email, name string) (model.User, error) {
	username := UsernameFromEmail(email)
	return s.CreateUserProfile(ctx, username, email, name)
}

func (s *Store) CreateUserProfile(ctx context.Context, username, email, name string) (model.User, error) {
	username, err := NormalizeUsername(username)
	if err != nil {
		return model.User{}, err
	}
	const q = `
		INSERT INTO users (username, email, name)
		VALUES ($1, $2, $3)
		RETURNING id, username, COALESCE(email, ''), name, is_admin, created_at
	`
	var u model.User
	err = s.db.QueryRow(ctx, q, username, email, name).Scan(&u.ID, &u.Username, &u.Email, &u.Name, &u.IsAdmin, &u.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.User{}, ErrConflict
		}
		return model.User{}, err
	}
	return u, nil
}

func (s *Store) GetUser(ctx context.Context, id uuid.UUID) (model.User, error) {
	const q = `SELECT id, username, COALESCE(email, ''), name, is_admin, created_at FROM users WHERE id = $1 AND deleted_at IS NULL`
	var u model.User
	err := s.db.QueryRow(ctx, q, id).Scan(&u.ID, &u.Username, &u.Email, &u.Name, &u.IsAdmin, &u.CreatedAt)
	if err != nil {
		if isNoRows(err) {
			return model.User{}, ErrNotFound
		}
		return model.User{}, err
	}
	return u, nil
}

func (s *Store) CreateOrUpdateAdminUser(ctx context.Context, email, name string) (model.User, error) {
	username := UsernameFromEmail(email)
	const q = `
		INSERT INTO users (username, email, name, is_admin)
		VALUES ($1, $2, $3, true)
		ON CONFLICT (email) DO UPDATE
		SET username = EXCLUDED.username, name = EXCLUDED.name, is_admin = true, deleted_at = NULL
		RETURNING id, username, COALESCE(email, ''), name, is_admin, created_at
	`
	var u model.User
	if err := s.db.QueryRow(ctx, q, username, email, name).Scan(&u.ID, &u.Username, &u.Email, &u.Name, &u.IsAdmin, &u.CreatedAt); err != nil {
		return model.User{}, err
	}
	return u, nil
}

// UsersCursor is the keyset position for ListUsers. Encoded base64 JSON at the
// HTTP boundary; the schema is server-private.
type UsersCursor struct {
	CreatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"i"`
}

type ListUsersParams struct {
	Cursor *UsersCursor
	Limit  int // caller is expected to have clamped to a sane upper bound
}

// ListUsers returns up to Limit users plus a HasMore flag the caller turns
// into a next_cursor. Ordered (created_at ASC, id ASC) so the keyset is
// strictly monotonic.
func (s *Store) ListUsers(ctx context.Context, p ListUsersParams) ([]model.User, bool, error) {
	args := []any{}
	q := `SELECT id, username, COALESCE(email, ''), name, is_admin, created_at FROM users WHERE deleted_at IS NULL`
	if p.Cursor != nil {
		args = append(args, p.Cursor.CreatedAt, p.Cursor.ID)
		q += ` AND (created_at, id) > ($1, $2)`
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(` ORDER BY created_at ASC, id ASC LIMIT $%d`, len(args))

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	users := make([]model.User, 0, p.Limit)
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Name, &u.IsAdmin, &u.CreatedAt); err != nil {
			return nil, false, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(users) > p.Limit
	if hasMore {
		users = users[:p.Limit]
	}
	return users, hasMore, nil
}

func (s *Store) DeleteUser(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE users SET deleted_at = now()
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	if err != nil {
		// Defensive: soft-delete has no expected FK/check mapping.
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
