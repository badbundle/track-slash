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
	const q = `
		INSERT INTO users (email, name)
		VALUES ($1, $2)
		RETURNING id, email, name, created_at
	`
	var u model.User
	err := s.db.QueryRow(ctx, q, email, name).Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt)
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
	const q = `SELECT id, email, name, created_at FROM users WHERE id = $1`
	var u model.User
	err := s.db.QueryRow(ctx, q, id).Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt)
	if err != nil {
		if isNoRows(err) {
			return model.User{}, ErrNotFound
		}
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
	q := `SELECT id, email, name, created_at FROM users`
	if p.Cursor != nil {
		args = append(args, p.Cursor.CreatedAt, p.Cursor.ID)
		q += ` WHERE (created_at, id) > ($1, $2)`
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
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt); err != nil {
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
