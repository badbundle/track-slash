package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bradleymackey/track-slash/internal/model"
)

func (s *Store) CreateProject(ctx context.Context, key, name, description string) (model.Project, error) {
	const q = `
		INSERT INTO projects (key, name, description)
		VALUES ($1, $2, $3)
		RETURNING id, key, name, description, created_at, updated_at
	`
	var p model.Project
	err := s.db.QueryRow(ctx, q, key, name, description).
		Scan(&p.ID, &p.Key, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.Project{}, ErrConflict
		}
		return model.Project{}, err
	}
	return p, nil
}

func (s *Store) GetProject(ctx context.Context, id uuid.UUID) (model.Project, error) {
	const q = `
		SELECT id, key, name, description, created_at, updated_at
		FROM projects WHERE id = $1 AND deleted_at IS NULL
	`
	var p model.Project
	err := s.db.QueryRow(ctx, q, id).
		Scan(&p.ID, &p.Key, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if isNoRows(err) {
			return model.Project{}, ErrNotFound
		}
		return model.Project{}, err
	}
	return p, nil
}

type ProjectsCursor struct {
	CreatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"i"`
}

type ListProjectsParams struct {
	Cursor *ProjectsCursor
	Limit  int
}

func (s *Store) ListProjects(ctx context.Context, p ListProjectsParams) ([]model.Project, bool, error) {
	args := []any{}
	q := `
		SELECT id, key, name, description, created_at, updated_at
		FROM projects
		WHERE deleted_at IS NULL
	`
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

	out := make([]model.Project, 0, p.Limit)
	for rows.Next() {
		var pr model.Project
		if err := rows.Scan(&pr.ID, &pr.Key, &pr.Name, &pr.Description, &pr.CreatedAt, &pr.UpdatedAt); err != nil {
			return nil, false, err
		}
		out = append(out, pr)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(out) > p.Limit
	if hasMore {
		out = out[:p.Limit]
	}
	return out, hasMore, nil
}

func (s *Store) DeleteProject(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE projects SET deleted_at = now(), updated_at = now()
			WHERE id = $1 AND deleted_at IS NULL
		`, id)
		if err != nil {
			// Defensive: soft-delete has no expected FK/check mapping.
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, `
			UPDATE issues SET deleted_at = now(), updated_at = now()
			WHERE project_id = $1 AND deleted_at IS NULL
		`, id); err != nil {
			// Defensive: cascading soft-delete has no expected FK/check mapping.
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE sprints SET deleted_at = now(), updated_at = now()
			WHERE project_id = $1 AND deleted_at IS NULL
		`, id)
		if err != nil {
			// Defensive: cascading soft-delete has no expected FK/check mapping.
		}
		return err
	})
}
