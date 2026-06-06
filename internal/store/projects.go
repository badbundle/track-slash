package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bradleymackey/track-slash/internal/model"
)

type projectScanner interface {
	Scan(dest ...any) error
}

func scanProject(row projectScanner) (model.Project, error) {
	var p model.Project
	err := row.Scan(&p.ID, &p.OwnerID, &p.OwnerUsername, &p.Key, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (s *Store) CreateProject(ctx context.Context, key, name, description string) (model.Project, error) {
	ownerID, err := s.firstProjectOwner(ctx)
	if err != nil {
		return model.Project{}, err
	}
	return s.CreateProjectForUser(ctx, ownerID, key, name, description)
}

func (s *Store) CreateProjectForUser(ctx context.Context, userID uuid.UUID, key, name, description string) (model.Project, error) {
	var p model.Project
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		const projectQ = `
			WITH owner AS (
				SELECT id, username FROM users WHERE id = $1 AND deleted_at IS NULL
			),
			inserted AS (
				INSERT INTO projects (owner_id, key, name, description)
				SELECT id, $2, $3, $4 FROM owner
				RETURNING id, owner_id, key, name, description, created_at, updated_at
			)
			SELECT inserted.id, inserted.owner_id, owner.username, inserted.key,
			       inserted.name, inserted.description, inserted.created_at, inserted.updated_at
			FROM inserted
			JOIN owner ON owner.id = inserted.owner_id
		`
		var err error
		p, err = scanProject(tx.QueryRow(ctx, projectQ, userID, key, name, description))
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return ErrConflict
			}
			// Defensive: insert has no other expected pgcode mapping here.
			return err
		}

		tag, err := tx.Exec(ctx, `
			INSERT INTO project_members (project_id, user_id)
			SELECT $1, id FROM users WHERE id = $2 AND deleted_at IS NULL
			ON CONFLICT (project_id, user_id) DO NOTHING
		`, p.ID, userID)
		if err != nil {
			// Defensive: project row exists and user is selected from users; failure is a DB/runtime fault.
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return model.Project{}, err
	}
	return p, nil
}

func (s *Store) GetProject(ctx context.Context, id uuid.UUID) (model.Project, error) {
	const q = `
		SELECT p.id, p.owner_id, u.username, p.key, p.name, p.description, p.created_at, p.updated_at
		FROM projects p
		JOIN users u ON u.id = p.owner_id
		WHERE p.id = $1 AND p.deleted_at IS NULL AND u.deleted_at IS NULL
	`
	p, err := scanProject(s.db.QueryRow(ctx, q, id))
	if err != nil {
		if isNoRows(err) {
			return model.Project{}, ErrNotFound
		}
		return model.Project{}, err
	}
	return p, nil
}

func (s *Store) GetProjectByOwnerKey(ctx context.Context, ownerUsername, key string) (model.Project, error) {
	ownerUsername = strings.ToLower(strings.TrimSpace(ownerUsername))
	key = strings.ToUpper(strings.TrimSpace(key))
	const q = `
		SELECT p.id, p.owner_id, u.username, p.key, p.name, p.description, p.created_at, p.updated_at
		FROM projects p
		JOIN users u ON u.id = p.owner_id
		WHERE u.username = $1
		  AND p.key = $2
		  AND p.deleted_at IS NULL
		  AND u.deleted_at IS NULL
	`
	p, err := scanProject(s.db.QueryRow(ctx, q, ownerUsername, key))
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
	Cursor        *ProjectsCursor
	Limit         int
	VisibleToUser *uuid.UUID
}

func (s *Store) ListProjects(ctx context.Context, p ListProjectsParams) ([]model.Project, bool, error) {
	args := []any{}
	q := `
		SELECT projects.id, projects.owner_id, u.username, projects.key,
		       projects.name, projects.description, projects.created_at, projects.updated_at
		FROM projects
		JOIN users u ON u.id = projects.owner_id
		WHERE projects.deleted_at IS NULL AND u.deleted_at IS NULL
	`
	if p.VisibleToUser != nil {
		args = append(args, *p.VisibleToUser)
		q += fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM project_members pm
			WHERE pm.project_id = projects.id AND pm.user_id = $%d
		)`, len(args))
	}
	if p.Cursor != nil {
		args = append(args, p.Cursor.CreatedAt, p.Cursor.ID)
		q += fmt.Sprintf(` AND (projects.created_at, projects.id) > ($%d, $%d)`, len(args)-1, len(args))
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(` ORDER BY projects.created_at ASC, projects.id ASC LIMIT $%d`, len(args))

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := make([]model.Project, 0, p.Limit)
	for rows.Next() {
		pr, err := scanProject(rows)
		if err != nil {
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

func (s *Store) firstProjectOwner(ctx context.Context) (uuid.UUID, error) {
	var ownerID uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT id
		FROM users
		WHERE deleted_at IS NULL
		ORDER BY is_admin DESC, created_at ASC, id ASC
		LIMIT 1
	`).Scan(&ownerID)
	if err != nil {
		if isNoRows(err) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, err
	}
	return ownerID, nil
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
