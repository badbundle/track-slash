package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bradleymackey/track-slash/internal/model"
)

type CreateIssueParams struct {
	ProjectID   uuid.UUID
	Title       string
	Description string
	AssigneeID  *uuid.UUID
	ReporterID  *uuid.UUID
}

func (s *Store) CreateIssue(ctx context.Context, p CreateIssueParams) (model.Issue, error) {
	var out model.Issue
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			number     int
			projectKey string
		)
		err := tx.QueryRow(ctx, `
			SELECT next_issue_number, key
			FROM projects WHERE id = $1
			FOR UPDATE
		`, p.ProjectID).Scan(&number, &projectKey)
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}

		err = tx.QueryRow(ctx, `
			INSERT INTO issues (project_id, number, title, description, assignee_id, reporter_id)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id, project_id, number, title, description, status,
			          assignee_id, reporter_id, created_at, updated_at
		`, p.ProjectID, number, p.Title, p.Description, p.AssigneeID, p.ReporterID).
			Scan(&out.ID, &out.ProjectID, &out.Number, &out.Title, &out.Description, &out.Status,
				&out.AssigneeID, &out.ReporterID, &out.CreatedAt, &out.UpdatedAt)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" {
				return fmt.Errorf("invalid assignee/reporter: %w", ErrConflict)
			}
			return err
		}

		_, err = tx.Exec(ctx, `
			UPDATE projects
			SET next_issue_number = next_issue_number + 1,
			    updated_at = now()
			WHERE id = $1
		`, p.ProjectID)
		if err != nil {
			return err
		}

		out.Identifier = fmt.Sprintf("%s-%d", projectKey, out.Number)
		return nil
	})
	if err != nil {
		return model.Issue{}, err
	}
	return out, nil
}

func (s *Store) GetIssue(ctx context.Context, id uuid.UUID) (model.Issue, error) {
	const q = `
		SELECT i.id, i.project_id, i.number, p.key, i.title, i.description, i.status,
		       i.assignee_id, i.reporter_id, i.created_at, i.updated_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		WHERE i.id = $1
	`
	var iss model.Issue
	var key string
	err := s.db.QueryRow(ctx, q, id).Scan(
		&iss.ID, &iss.ProjectID, &iss.Number, &key, &iss.Title, &iss.Description, &iss.Status,
		&iss.AssigneeID, &iss.ReporterID, &iss.CreatedAt, &iss.UpdatedAt,
	)
	if err != nil {
		if isNoRows(err) {
			return model.Issue{}, ErrNotFound
		}
		return model.Issue{}, err
	}
	iss.Identifier = fmt.Sprintf("%s-%d", key, iss.Number)
	return iss, nil
}

type ListIssuesParams struct {
	ProjectID uuid.UUID
	Status    model.Status // empty = all
}

func (s *Store) ListIssues(ctx context.Context, p ListIssuesParams) ([]model.Issue, error) {
	args := []any{p.ProjectID}
	q := `
		SELECT i.id, i.project_id, i.number, pr.key, i.title, i.description, i.status,
		       i.assignee_id, i.reporter_id, i.created_at, i.updated_at
		FROM issues i
		JOIN projects pr ON pr.id = i.project_id
		WHERE i.project_id = $1
	`
	if p.Status != "" {
		args = append(args, string(p.Status))
		q += " AND i.status = $2"
	}
	q += " ORDER BY i.number ASC"

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Issue{}
	for rows.Next() {
		var iss model.Issue
		var key string
		if err := rows.Scan(
			&iss.ID, &iss.ProjectID, &iss.Number, &key, &iss.Title, &iss.Description, &iss.Status,
			&iss.AssigneeID, &iss.ReporterID, &iss.CreatedAt, &iss.UpdatedAt,
		); err != nil {
			return nil, err
		}
		iss.Identifier = fmt.Sprintf("%s-%d", key, iss.Number)
		out = append(out, iss)
	}
	return out, rows.Err()
}

type UpdateIssueParams struct {
	Title       *string
	Description *string
	Status      *model.Status
	AssigneeID  *uuid.UUID
	ClearAssignee bool
}

func (s *Store) UpdateIssue(ctx context.Context, id uuid.UUID, p UpdateIssueParams) (model.Issue, error) {
	sets := []string{}
	args := []any{}
	i := 1
	if p.Title != nil {
		sets = append(sets, fmt.Sprintf("title = $%d", i))
		args = append(args, *p.Title)
		i++
	}
	if p.Description != nil {
		sets = append(sets, fmt.Sprintf("description = $%d", i))
		args = append(args, *p.Description)
		i++
	}
	if p.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", i))
		args = append(args, string(*p.Status))
		i++
	}
	if p.ClearAssignee {
		sets = append(sets, "assignee_id = NULL")
	} else if p.AssigneeID != nil {
		sets = append(sets, fmt.Sprintf("assignee_id = $%d", i))
		args = append(args, *p.AssigneeID)
		i++
	}

	if len(sets) == 0 {
		return s.GetIssue(ctx, id)
	}

	sets = append(sets, "updated_at = now()")
	args = append(args, id)
	q := fmt.Sprintf(`
		UPDATE issues SET %s WHERE id = $%d
	`, strings.Join(sets, ", "), i)

	tag, err := s.db.Exec(ctx, q, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return model.Issue{}, fmt.Errorf("invalid assignee: %w", ErrConflict)
		}
		return model.Issue{}, err
	}
	if tag.RowsAffected() == 0 {
		return model.Issue{}, ErrNotFound
	}
	return s.GetIssue(ctx, id)
}
