package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
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
		FROM projects WHERE id = $1
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

func (s *Store) ListProjects(ctx context.Context) ([]model.Project, error) {
	const q = `
		SELECT id, key, name, description, created_at, updated_at
		FROM projects ORDER BY created_at ASC
	`
	rows, err := s.db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Project{}
	for rows.Next() {
		var p model.Project
		if err := rows.Scan(&p.ID, &p.Key, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
