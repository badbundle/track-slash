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
	Priority    model.IssuePriority
	AssigneeID  *uuid.UUID
	ReporterID  *uuid.UUID
}

type CreateSubIssueParams struct {
	ParentIssueID uuid.UUID
	Title         string
	Description   string
	Priority      model.IssuePriority
	AssigneeID    *uuid.UUID
	ReporterID    *uuid.UUID
}

type issueScanner interface {
	Scan(dest ...any) error
}

func scanIssue(row issueScanner) (model.Issue, error) {
	var iss model.Issue
	err := row.Scan(
		&iss.ID, &iss.ProjectID, &iss.OwnerUsername, &iss.ProjectKey, &iss.Number,
		&iss.Title, &iss.Description, &iss.Status, &iss.Priority, &iss.AssigneeID, &iss.ReporterID,
		&iss.SprintID, &iss.ParentIssueID, &iss.CreatedAt, &iss.UpdatedAt,
	)
	if err != nil {
		return model.Issue{}, err
	}
	iss.Identifier = fmt.Sprintf("%s-%d", iss.ProjectKey, iss.Number)
	return iss, nil
}

func issuePriorityOrDefault(priority model.IssuePriority) model.IssuePriority {
	if priority == "" {
		return model.PriorityP2
	}
	return priority
}

func (s *Store) CreateIssue(ctx context.Context, p CreateIssueParams) (model.Issue, error) {
	var out model.Issue
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			number        int
			projectKey    string
			ownerUsername string
		)
		err := tx.QueryRow(ctx, `
			SELECT p.next_issue_number, p.key, u.username
			FROM projects p
			JOIN users u ON u.id = p.owner_id
			WHERE p.id = $1 AND p.deleted_at IS NULL AND u.deleted_at IS NULL
			FOR UPDATE OF p
		`, p.ProjectID).Scan(&number, &projectKey, &ownerUsername)
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}

		priority := issuePriorityOrDefault(p.Priority)
		err = tx.QueryRow(ctx, `
			INSERT INTO issues (project_id, number, title, description, priority, assignee_id, reporter_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id, project_id, number, title, description, status, priority,
			          assignee_id, reporter_id, sprint_id, parent_issue_id, created_at, updated_at
		`, p.ProjectID, number, p.Title, p.Description, string(priority), p.AssigneeID, p.ReporterID).
			Scan(&out.ID, &out.ProjectID, &out.Number, &out.Title, &out.Description, &out.Status, &out.Priority,
				&out.AssigneeID, &out.ReporterID, &out.SprintID, &out.ParentIssueID, &out.CreatedAt, &out.UpdatedAt)
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

		out.ProjectKey = projectKey
		out.OwnerUsername = ownerUsername
		out.Identifier = fmt.Sprintf("%s-%d", projectKey, out.Number)
		return nil
	})
	if err != nil {
		return model.Issue{}, err
	}
	return out, nil
}

func (s *Store) CreateSubIssue(ctx context.Context, p CreateSubIssueParams) (model.Issue, error) {
	var out model.Issue
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			number        int
			projectKey    string
			ownerUsername string
			projectID     uuid.UUID
			parentIssueID *uuid.UUID
		)
		err := tx.QueryRow(ctx, `
			SELECT pr.next_issue_number, pr.key, u.username, i.project_id, i.parent_issue_id
			FROM issues i
			JOIN projects pr ON pr.id = i.project_id
			JOIN users u ON u.id = pr.owner_id
			WHERE i.id = $1 AND i.deleted_at IS NULL AND pr.deleted_at IS NULL
			  AND u.deleted_at IS NULL
			FOR UPDATE OF i, pr
		`, p.ParentIssueID).Scan(&number, &projectKey, &ownerUsername, &projectID, &parentIssueID)
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		if parentIssueID != nil {
			return fmt.Errorf("sub-issues cannot have sub-issues: %w", ErrConflict)
		}

		priority := issuePriorityOrDefault(p.Priority)
		err = tx.QueryRow(ctx, `
			INSERT INTO issues (project_id, number, title, description, priority, assignee_id, reporter_id, parent_issue_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id, project_id, number, title, description, status, priority,
			          assignee_id, reporter_id, sprint_id, parent_issue_id, created_at, updated_at
		`, projectID, number, p.Title, p.Description, string(priority), p.AssigneeID, p.ReporterID, p.ParentIssueID).
			Scan(&out.ID, &out.ProjectID, &out.Number, &out.Title, &out.Description, &out.Status, &out.Priority,
				&out.AssigneeID, &out.ReporterID, &out.SprintID, &out.ParentIssueID, &out.CreatedAt, &out.UpdatedAt)
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
		`, projectID)
		if err != nil {
			return err
		}

		out.ProjectKey = projectKey
		out.OwnerUsername = ownerUsername
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
		SELECT i.id, i.project_id, u.username, p.key, i.number, i.title, i.description, i.status, i.priority,
		       i.assignee_id, i.reporter_id, i.sprint_id, i.parent_issue_id, i.created_at, i.updated_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		JOIN users u ON u.id = p.owner_id
		WHERE i.id = $1 AND i.deleted_at IS NULL AND p.deleted_at IS NULL AND u.deleted_at IS NULL
	`
	iss, err := scanIssue(s.db.QueryRow(ctx, q, id))
	if err != nil {
		if isNoRows(err) {
			return model.Issue{}, ErrNotFound
		}
		return model.Issue{}, err
	}
	return iss, nil
}

func (s *Store) GetIssueByOwnerKeyNumber(ctx context.Context, ownerUsername, projectKey string, number int) (model.Issue, error) {
	const q = `
		SELECT i.id, i.project_id, u.username, p.key, i.number, i.title, i.description, i.status, i.priority,
		       i.assignee_id, i.reporter_id, i.sprint_id, i.parent_issue_id, i.created_at, i.updated_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		JOIN users u ON u.id = p.owner_id
		WHERE u.username = $1 AND p.key = $2 AND i.number = $3
		  AND i.deleted_at IS NULL AND p.deleted_at IS NULL AND u.deleted_at IS NULL
	`
	iss, err := scanIssue(s.db.QueryRow(ctx, q, ownerUsername, projectKey, number))
	if err != nil {
		if isNoRows(err) {
			return model.Issue{}, ErrNotFound
		}
		return model.Issue{}, err
	}
	return iss, nil
}

type ListIssuesParams struct {
	ProjectID uuid.UUID
	Status    model.Status // empty = all
	// AssigneeIDs filters to issues assigned to any supplied users. Empty = all.
	AssigneeIDs []uuid.UUID
	// SprintID filters by sprint. Backlog == true means "WHERE sprint_id IS NULL"
	// and SprintID is ignored. Both nil/false → no sprint filter.
	SprintID *uuid.UUID
	Backlog  bool
	Cursor   *IssuesCursor
	Limit    int
	// IncludeSubIssues is for personal/work views that should surface assigned
	// child work. Project planning lists keep the default top-level-only shape.
	IncludeSubIssues bool
}

// IssuesCursor keys off (project_id, number). Number is monotonic per project
// (see CreateIssue's FOR UPDATE counter) so it's a sufficient sole key.
type IssuesCursor struct {
	Number int `json:"n"`
}

// ListIssuesByIDs returns the issues matching the supplied id set, in no
// guaranteed order. Missing ids are silently skipped — callers diff against
// their request to detect deletions. Designed for batched WebSocket-driven
// refetches; ids should be capped by the caller.
func (s *Store) ListIssuesByIDs(ctx context.Context, ids []uuid.UUID) ([]model.Issue, error) {
	if len(ids) == 0 {
		return []model.Issue{}, nil
	}
	const q = `
		SELECT i.id, i.project_id, u.username, p.key, i.number, i.title, i.description, i.status, i.priority,
		       i.assignee_id, i.reporter_id, i.sprint_id, i.parent_issue_id, i.created_at, i.updated_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		JOIN users u ON u.id = p.owner_id
		WHERE i.id = ANY($1) AND i.deleted_at IS NULL AND p.deleted_at IS NULL AND u.deleted_at IS NULL
	`
	rows, err := s.db.Query(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.Issue, 0, len(ids))
	for rows.Next() {
		iss, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, iss)
	}
	return out, rows.Err()
}

func (s *Store) ListIssues(ctx context.Context, p ListIssuesParams) ([]model.Issue, bool, error) {
	args := []any{p.ProjectID}
	q := `
		SELECT i.id, i.project_id, u.username, pr.key, i.number, i.title, i.description, i.status, i.priority,
		       i.assignee_id, i.reporter_id, i.sprint_id, i.parent_issue_id, i.created_at, i.updated_at
		FROM issues i
		JOIN projects pr ON pr.id = i.project_id
		JOIN users u ON u.id = pr.owner_id
		WHERE i.project_id = $1 AND i.deleted_at IS NULL AND pr.deleted_at IS NULL AND u.deleted_at IS NULL
	`
	if !p.IncludeSubIssues {
		q += " AND i.parent_issue_id IS NULL"
	}
	if p.Status != "" {
		args = append(args, string(p.Status))
		q += fmt.Sprintf(" AND i.status = $%d", len(args))
	}
	if len(p.AssigneeIDs) > 0 {
		args = append(args, p.AssigneeIDs)
		q += fmt.Sprintf(" AND i.assignee_id = ANY($%d)", len(args))
	}
	switch {
	case p.Backlog:
		q += " AND i.sprint_id IS NULL"
	case p.SprintID != nil:
		args = append(args, *p.SprintID)
		q += fmt.Sprintf(" AND i.sprint_id = $%d", len(args))
	}
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
		iss, err := scanIssue(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, iss)
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

type ListSubIssuesForIssueParams struct {
	ParentIssueID uuid.UUID
	Cursor        *IssuesCursor
	Limit         int
}

func (s *Store) ListSubIssuesForIssue(ctx context.Context, p ListSubIssuesForIssueParams) ([]model.Issue, bool, error) {
	var parentIssueID *uuid.UUID
	if err := s.db.QueryRow(ctx, `
		SELECT i.parent_issue_id
		FROM issues i
		JOIN projects pr ON pr.id = i.project_id
		WHERE i.id = $1 AND i.deleted_at IS NULL AND pr.deleted_at IS NULL
	`, p.ParentIssueID).Scan(&parentIssueID); err != nil {
		if isNoRows(err) {
			return nil, false, ErrNotFound
		}
		// Defensive: only no-rows has a domain mapping here.
		return nil, false, err
	}
	if parentIssueID != nil {
		return nil, false, fmt.Errorf("sub-issues cannot have sub-issues: %w", ErrConflict)
	}

	args := []any{p.ParentIssueID}
	q := `
		SELECT i.id, i.project_id, u.username, pr.key, i.number, i.title, i.description, i.status, i.priority,
		       i.assignee_id, i.reporter_id, i.sprint_id, i.parent_issue_id, i.created_at, i.updated_at
		FROM issues i
		JOIN projects pr ON pr.id = i.project_id
		JOIN users u ON u.id = pr.owner_id
		WHERE i.parent_issue_id = $1 AND i.deleted_at IS NULL AND pr.deleted_at IS NULL AND u.deleted_at IS NULL
	`
	if p.Cursor != nil {
		args = append(args, p.Cursor.Number)
		q += fmt.Sprintf(" AND i.number > $%d", len(args))
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(" ORDER BY i.number ASC LIMIT $%d", len(args))

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		// Defensive: list query has no expected constraint mapping.
		return nil, false, err
	}
	defer rows.Close()

	out := make([]model.Issue, 0, p.Limit)
	for rows.Next() {
		iss, err := scanIssue(rows)
		if err != nil {
			// Defensive: selected columns match model fields.
			return nil, false, err
		}
		out = append(out, iss)
	}
	if err := rows.Err(); err != nil {
		// Defensive: scan/query failures after setup are DB/runtime faults.
		return nil, false, err
	}
	hasMore := len(out) > p.Limit
	if hasMore {
		out = out[:p.Limit]
	}
	return out, hasMore, nil
}

type UpdateIssueParams struct {
	Title         *string
	Description   *string
	Status        *model.Status
	Priority      *model.IssuePriority
	AssigneeID    *uuid.UUID
	ClearAssignee bool
	ReporterID    *uuid.UUID
	ClearReporter bool
	SprintID      *uuid.UUID
	ClearSprint   bool
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
	if p.Priority != nil {
		sets = append(sets, fmt.Sprintf("priority = $%d", i))
		args = append(args, string(*p.Priority))
		i++
	}
	if p.ClearAssignee {
		sets = append(sets, "assignee_id = NULL")
	} else if p.AssigneeID != nil {
		sets = append(sets, fmt.Sprintf("assignee_id = $%d", i))
		args = append(args, *p.AssigneeID)
		i++
	}
	if p.ClearReporter {
		sets = append(sets, "reporter_id = NULL")
	} else if p.ReporterID != nil {
		sets = append(sets, fmt.Sprintf("reporter_id = $%d", i))
		args = append(args, *p.ReporterID)
		i++
	}
	if p.ClearSprint {
		sets = append(sets, "sprint_id = NULL")
	} else if p.SprintID != nil {
		sets = append(sets, fmt.Sprintf("sprint_id = $%d", i))
		args = append(args, *p.SprintID)
		i++
	}

	if len(sets) == 0 {
		return s.GetIssue(ctx, id)
	}

	sets = append(sets, "updated_at = now()")
	args = append(args, id)
	q := fmt.Sprintf(`
		UPDATE issues SET %s WHERE id = $%d AND deleted_at IS NULL
	`, strings.Join(sets, ", "), i)

	validatePeople := (p.AssigneeID != nil && !p.ClearAssignee) || (p.ReporterID != nil && !p.ClearReporter)
	validateSprint := p.SprintID != nil && !p.ClearSprint
	// Cross-row validation runs in a transaction so the update uses the same
	// issue project observed by the member/sprint checks.
	if validatePeople || validateSprint {
		err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
			var issueProject, sprintProject uuid.UUID
			var parentIssueID *uuid.UUID
			var sprintStatus model.SprintStatus
			if err := tx.QueryRow(ctx, `SELECT project_id, parent_issue_id FROM issues WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&issueProject, &parentIssueID); err != nil {
				if isNoRows(err) {
					return ErrNotFound
				}
				return err
			}
			if p.AssigneeID != nil && !p.ClearAssignee {
				ok, err := issueProjectMemberExists(ctx, tx, issueProject, *p.AssigneeID)
				if err != nil {
					return err
				}
				if !ok {
					return ErrNotFound
				}
			}
			if p.ReporterID != nil && !p.ClearReporter {
				ok, err := issueProjectMemberExists(ctx, tx, issueProject, *p.ReporterID)
				if err != nil {
					return err
				}
				if !ok {
					return ErrNotFound
				}
			}
			if validateSprint {
				if parentIssueID != nil {
					return fmt.Errorf("sub-issues cannot be assigned to sprints: %w", ErrConflict)
				}
				if err := tx.QueryRow(ctx, `SELECT project_id, status FROM sprints WHERE id = $1 AND deleted_at IS NULL`, *p.SprintID).Scan(&sprintProject, &sprintStatus); err != nil {
					if isNoRows(err) {
						return fmt.Errorf("sprint not found: %w", ErrConflict)
					}
					return err
				}
				if issueProject != sprintProject {
					return fmt.Errorf("sprint belongs to a different project: %w", ErrConflict)
				}
				if sprintStatus == model.SprintStatusCompleted {
					return fmt.Errorf("cannot assign issue to completed sprint: %w", ErrConflict)
				}
			}
			_, err := tx.Exec(ctx, q, args...)
			return err
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" {
				return model.Issue{}, fmt.Errorf("invalid assignee, reporter, or sprint: %w", ErrConflict)
			}
			return model.Issue{}, err
		}
		return s.GetIssue(ctx, id)
	}

	tag, err := s.db.Exec(ctx, q, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return model.Issue{}, fmt.Errorf("invalid assignee or sprint: %w", ErrConflict)
		}
		return model.Issue{}, err
	}
	if tag.RowsAffected() == 0 {
		return model.Issue{}, ErrNotFound
	}
	return s.GetIssue(ctx, id)
}

func issueProjectMemberExists(ctx context.Context, tx pgx.Tx, projectID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM project_members pm
			JOIN users u ON u.id = pm.user_id
			WHERE pm.project_id = $1 AND pm.user_id = $2 AND u.deleted_at IS NULL
		)
	`, projectID, userID).Scan(&ok)
	return ok, err
}

func (s *Store) DeleteIssue(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE issues SET deleted_at = now(), updated_at = now()
		WHERE (id = $1 OR parent_issue_id = $1) AND deleted_at IS NULL
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
