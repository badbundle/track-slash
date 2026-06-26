package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bradleymackey/track-slash/internal/model"
)

type CreateProjectContextParams struct {
	ProjectID      uuid.UUID
	Scope          model.ProjectContextScope
	Title          string
	Kind           model.ProjectContextKind
	ContentType    string
	Body           string
	SourceFilename *string
	CreatedByID    uuid.UUID
}

type CreateIssueContextParams struct {
	IssueID        uuid.UUID
	Title          string
	Kind           model.ProjectContextKind
	ContentType    string
	Body           string
	SourceFilename *string
	CreatedByID    uuid.UUID
}

type UpdateProjectContextParams struct {
	ID          uuid.UUID
	Title       *string
	Body        *string
	UpdatedByID uuid.UUID
}

type ProjectContextsCursor struct {
	Number int `json:"n"`
}

type ListProjectContextsParams struct {
	ProjectID uuid.UUID
	Cursor    *ProjectContextsCursor
	Limit     int
}

type ListContextsForIssueParams struct {
	IssueID uuid.UUID
	Cursor  *ProjectContextsCursor
	Limit   int
}

type ListIssuesForContextParams struct {
	ContextID uuid.UUID
	Cursor    *IssuesCursor
	Limit     int
}

type projectContextScanner interface {
	Scan(dest ...any) error
}

func scanProjectContext(row projectContextScanner) (model.ProjectContext, error) {
	var out model.ProjectContext
	err := row.Scan(
		&out.ID, &out.ProjectID, &out.Number, &out.Scope, &out.Title, &out.Kind, &out.ContentType, &out.Body,
		&out.SourceFilename, &out.CreatedByID, &out.UpdatedByID, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return model.ProjectContext{}, err
	}
	out.Ref = model.ProjectContextRef(out.Number)
	return out, nil
}

func scanProjectContextSummary(row projectContextScanner) (model.ProjectContextSummary, error) {
	var out model.ProjectContextSummary
	err := row.Scan(
		&out.ID, &out.ProjectID, &out.Number, &out.Scope, &out.Title, &out.Kind, &out.ContentType,
		&out.SourceFilename, &out.CreatedByID, &out.UpdatedByID, &out.LinkedIssueCount, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return model.ProjectContextSummary{}, err
	}
	out.Ref = model.ProjectContextRef(out.Number)
	return out, nil
}

func scanIssueContextLink(row projectContextScanner) (model.IssueContextLink, error) {
	var out model.IssueContextLink
	err := row.Scan(&out.ID, &out.ProjectID, &out.IssueID, &out.ContextID, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func projectContextKindOrDefault(kind model.ProjectContextKind) model.ProjectContextKind {
	if kind == "" {
		return model.ProjectContextKindText
	}
	return kind
}

func projectContextContentTypeOrDefault(contentType string) string {
	if contentType == "" {
		return "text/plain; charset=utf-8"
	}
	return contentType
}

func projectContextScopeOrDefault(scope model.ProjectContextScope) model.ProjectContextScope {
	if scope == "" {
		return model.ProjectContextScopeProject
	}
	return scope
}

func (s *Store) CreateProjectContext(ctx context.Context, p CreateProjectContextParams) (model.ProjectContext, error) {
	var out model.ProjectContext
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var number int
		if err := tx.QueryRow(ctx, `
			SELECT next_context_number
			FROM projects
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, p.ProjectID).Scan(&number); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: DB outage past the no-rows branch
		}

		var err error
		out, err = scanProjectContext(tx.QueryRow(ctx, `
			INSERT INTO project_context (
				project_id, number, scope, title, kind, content_type, body, source_filename, created_by_id, updated_by_id
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
			RETURNING id, project_id, number, scope, title, kind, content_type, body,
			          source_filename, created_by_id, updated_by_id, created_at, updated_at
		`, p.ProjectID, number, string(projectContextScopeOrDefault(p.Scope)), p.Title, string(projectContextKindOrDefault(p.Kind)), projectContextContentTypeOrDefault(p.ContentType), p.Body, p.SourceFilename, p.CreatedByID))
		if err != nil {
			if mapped := mapProjectContextWriteError(err); mapped != nil {
				return mapped
			}
			return err // defensive: non-pg or unmapped pg error
		}

		if _, err := tx.Exec(ctx, `
			UPDATE projects
			SET next_context_number = next_context_number + 1,
			    updated_at = now()
			WHERE id = $1
		`, p.ProjectID); err != nil {
			return err // defensive: project row was locked above
		}
		return nil
	})
	if err != nil {
		return model.ProjectContext{}, err
	}
	return out, nil
}

func (s *Store) CreateIssueContext(ctx context.Context, p CreateIssueContextParams) (model.ProjectContext, error) {
	var out model.ProjectContext
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var projectID uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT i.project_id
			FROM issues i
			JOIN projects p ON p.id = i.project_id
			WHERE i.id = $1 AND i.deleted_at IS NULL AND p.deleted_at IS NULL
		`, p.IssueID).Scan(&projectID); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: only no-rows has a domain mapping here
		}

		var number int
		if err := tx.QueryRow(ctx, `
			SELECT next_context_number
			FROM projects
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, projectID).Scan(&number); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: project ID came from the live issue row
		}

		var err error
		out, err = scanProjectContext(tx.QueryRow(ctx, `
			INSERT INTO project_context (
				project_id, number, scope, title, kind, content_type, body, source_filename, created_by_id, updated_by_id
			)
			VALUES ($1, $2, 'issue', $3, $4, $5, $6, $7, $8, $8)
			RETURNING id, project_id, number, scope, title, kind, content_type, body,
			          source_filename, created_by_id, updated_by_id, created_at, updated_at
		`, projectID, number, p.Title, string(projectContextKindOrDefault(p.Kind)), projectContextContentTypeOrDefault(p.ContentType), p.Body, p.SourceFilename, p.CreatedByID))
		if err != nil {
			if mapped := mapProjectContextWriteError(err); mapped != nil {
				return mapped
			}
			return err // defensive: non-pg or unmapped pg error
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO issue_context_links (project_id, issue_id, context_id)
			VALUES ($1, $2, $3)
		`, projectID, p.IssueID, out.ID); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				switch pgErr.Code {
				case "23505":
					return fmt.Errorf("context already linked: %w", ErrConflict)
				case "23503":
					return fmt.Errorf("invalid issue or context reference: %w", ErrConflict)
				}
			}
			return err // defensive: non-pg or unmapped pg error
		}

		if _, err := tx.Exec(ctx, `
			UPDATE projects
			SET next_context_number = next_context_number + 1,
			    updated_at = now()
			WHERE id = $1
		`, projectID); err != nil {
			return err // defensive: project row was locked above
		}
		return nil
	})
	if err != nil {
		return model.ProjectContext{}, err
	}
	return out, nil
}

func (s *Store) GetProjectContext(ctx context.Context, id uuid.UUID) (model.ProjectContext, error) {
	const q = `
		SELECT pc.id, pc.project_id, pc.number, pc.scope, pc.title, pc.kind, pc.content_type, pc.body,
		       pc.source_filename, pc.created_by_id, pc.updated_by_id, pc.created_at, pc.updated_at
		FROM project_context pc
		JOIN projects p ON p.id = pc.project_id
		WHERE pc.id = $1 AND p.deleted_at IS NULL
	`
	out, err := scanProjectContext(s.db.QueryRow(ctx, q, id))
	if err != nil {
		if isNoRows(err) {
			return model.ProjectContext{}, ErrNotFound
		}
		return model.ProjectContext{}, err
	}
	return out, nil
}

func (s *Store) GetProjectContextByProjectNumber(ctx context.Context, projectID uuid.UUID, number int) (model.ProjectContext, error) {
	const q = `
		SELECT pc.id, pc.project_id, pc.number, pc.scope, pc.title, pc.kind, pc.content_type, pc.body,
		       pc.source_filename, pc.created_by_id, pc.updated_by_id, pc.created_at, pc.updated_at
		FROM project_context pc
		JOIN projects p ON p.id = pc.project_id
		WHERE pc.project_id = $1 AND pc.number = $2 AND p.deleted_at IS NULL
	`
	out, err := scanProjectContext(s.db.QueryRow(ctx, q, projectID, number))
	if err != nil {
		if isNoRows(err) {
			return model.ProjectContext{}, ErrNotFound
		}
		return model.ProjectContext{}, err
	}
	return out, nil
}

func (s *Store) ListProjectContexts(ctx context.Context, p ListProjectContextsParams) ([]model.ProjectContextSummary, bool, error) {
	if _, err := s.GetProject(ctx, p.ProjectID); err != nil {
		return nil, false, err
	}
	args := []any{p.ProjectID}
	q := `
		SELECT pc.id, pc.project_id, pc.number, pc.scope, pc.title, pc.kind, pc.content_type,
		       pc.source_filename, pc.created_by_id, pc.updated_by_id,
		       COUNT(i.id)::INT AS linked_issue_count,
		       pc.created_at, pc.updated_at
		FROM project_context pc
		JOIN projects p ON p.id = pc.project_id
		LEFT JOIN issue_context_links icl ON icl.context_id = pc.id
		LEFT JOIN issues i ON i.id = icl.issue_id AND i.deleted_at IS NULL
		WHERE pc.project_id = $1 AND pc.scope = 'project' AND p.deleted_at IS NULL
	`
	if p.Cursor != nil {
		args = append(args, p.Cursor.Number)
		q += fmt.Sprintf(" AND pc.number > $%d", len(args))
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(`
		GROUP BY pc.id
		ORDER BY pc.number ASC
		LIMIT $%d
	`, len(args))

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := make([]model.ProjectContextSummary, 0, p.Limit)
	for rows.Next() {
		item, err := scanProjectContextSummary(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, item)
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

func (s *Store) UpdateProjectContext(ctx context.Context, p UpdateProjectContextParams) (model.ProjectContext, error) {
	const q = `
		UPDATE project_context pc
		SET title = COALESCE($2, title),
		    body = COALESCE($3, body),
		    updated_by_id = $4,
		    updated_at = GREATEST(clock_timestamp(), pc.updated_at + interval '1 microsecond')
		FROM projects pr
		WHERE pc.id = $1 AND pr.id = pc.project_id AND pr.deleted_at IS NULL
		RETURNING pc.id, pc.project_id, pc.number, pc.scope, pc.title, pc.kind, pc.content_type, pc.body,
		          pc.source_filename, pc.created_by_id, pc.updated_by_id, pc.created_at, pc.updated_at
	`
	out, err := scanProjectContext(s.db.QueryRow(ctx, q, p.ID, p.Title, p.Body, p.UpdatedByID))
	if err != nil {
		if isNoRows(err) {
			return model.ProjectContext{}, ErrNotFound
		}
		if mapped := mapProjectContextWriteError(err); mapped != nil {
			return model.ProjectContext{}, mapped
		}
		return model.ProjectContext{}, err
	}
	return out, nil
}

func (s *Store) DeleteProjectContext(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM project_context WHERE id = $1`, id)
	if err != nil {
		return err // defensive: delete has no expected FK/check mapping
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateIssueContextLink(ctx context.Context, issueID, contextID uuid.UUID) (model.IssueContextLink, error) {
	var out model.IssueContextLink
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var issueProject uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT i.project_id
			FROM issues i
			JOIN projects p ON p.id = i.project_id
			WHERE i.id = $1 AND i.deleted_at IS NULL AND p.deleted_at IS NULL
		`, issueID).Scan(&issueProject); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: only no-rows has a domain mapping here
		}

		var contextProject uuid.UUID
		var contextScope model.ProjectContextScope
		if err := tx.QueryRow(ctx, `
			SELECT pc.project_id, pc.scope
			FROM project_context pc
			JOIN projects p ON p.id = pc.project_id
			WHERE pc.id = $1 AND p.deleted_at IS NULL
		`, contextID).Scan(&contextProject, &contextScope); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: only no-rows has a domain mapping here
		}
		if issueProject != contextProject {
			return fmt.Errorf("issue and context belong to different projects: %w", ErrConflict)
		}
		if contextScope != model.ProjectContextScopeProject {
			return fmt.Errorf("issue-scoped context cannot be linked to another issue: %w", ErrConflict)
		}

		var err error
		out, err = scanIssueContextLink(tx.QueryRow(ctx, `
			INSERT INTO issue_context_links (project_id, issue_id, context_id)
			VALUES ($1, $2, $3)
			RETURNING id, project_id, issue_id, context_id, created_at, updated_at
		`, issueProject, issueID, contextID))
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				switch pgErr.Code {
				case "23505":
					return fmt.Errorf("context already linked: %w", ErrConflict)
				case "23503":
					return fmt.Errorf("invalid issue or context reference: %w", ErrConflict)
				}
			}
			return err // defensive: non-pg or unmapped pg error
		}
		return nil
	})
	if err != nil {
		return model.IssueContextLink{}, err
	}
	return out, nil
}

func (s *Store) DeleteIssueContextLink(ctx context.Context, issueID, contextID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		DELETE FROM issue_context_links
		WHERE issue_id = $1 AND context_id = $2
	`, issueID, contextID)
	if err != nil {
		return err // defensive: delete has no expected FK/check mapping
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListContextsForIssue(ctx context.Context, p ListContextsForIssueParams) ([]model.ProjectContext, bool, error) {
	if _, err := s.ProjectIDForIssue(ctx, p.IssueID); err != nil {
		return nil, false, err
	}
	args := []any{p.IssueID}
	q := `
		SELECT pc.id, pc.project_id, pc.number, pc.scope, pc.title, pc.kind, pc.content_type, pc.body,
		       pc.source_filename, pc.created_by_id, pc.updated_by_id, pc.created_at, pc.updated_at
		FROM issue_context_links icl
		JOIN project_context pc ON pc.id = icl.context_id
		JOIN projects p ON p.id = pc.project_id
		WHERE icl.issue_id = $1 AND p.deleted_at IS NULL
	`
	if p.Cursor != nil {
		args = append(args, p.Cursor.Number)
		q += fmt.Sprintf(" AND pc.number > $%d", len(args))
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(" ORDER BY pc.number ASC LIMIT $%d", len(args))

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := make([]model.ProjectContext, 0, p.Limit)
	for rows.Next() {
		item, err := scanProjectContext(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, item)
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

func (s *Store) ListIssuesForContext(ctx context.Context, p ListIssuesForContextParams) ([]model.Issue, bool, error) {
	if _, err := s.ProjectIDForProjectContext(ctx, p.ContextID); err != nil {
		return nil, false, err
	}
	args := []any{p.ContextID}
	q := `
		SELECT i.id, i.project_id, u.username, pr.key, i.number, i.title, i.description, i.status, i.close_reason, i.priority,
		       i.assignee_id, i.reporter_id, i.sprint_id, i.parent_issue_id, i.due_date, i.created_at, i.updated_at
		FROM issue_context_links icl
		JOIN issues i ON i.id = icl.issue_id
		JOIN projects pr ON pr.id = i.project_id
		JOIN users u ON u.id = pr.owner_id
		WHERE icl.context_id = $1 AND i.deleted_at IS NULL AND pr.deleted_at IS NULL AND u.deleted_at IS NULL
	`
	if p.Cursor != nil {
		args = append(args, p.Cursor.Number)
		q += fmt.Sprintf(" AND i.number > $%d", len(args))
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(" ORDER BY i.number ASC LIMIT $%d", len(args))

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := make([]model.Issue, 0, p.Limit)
	for rows.Next() {
		issue, err := scanIssue(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, issue)
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

func mapProjectContextWriteError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	switch pgErr.Code {
	case "23503":
		return fmt.Errorf("project or user not found: %w", ErrNotFound)
	case "23514":
		return fmt.Errorf("title/body outside allowed length: %w", ErrConflict)
	case "22P02":
		return fmt.Errorf("invalid context kind or scope: %w", ErrConflict)
	}
	return nil
}
