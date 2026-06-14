package store

import (
	"context"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
)

func (s *Store) GrantProjectAccess(ctx context.Context, projectID, userID uuid.UUID) (model.ProjectMember, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return model.ProjectMember{}, err
	}
	if _, err := s.GetUser(ctx, userID); err != nil {
		return model.ProjectMember{}, err
	}
	const q = `
		INSERT INTO project_members (project_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT (project_id, user_id) DO UPDATE SET user_id = EXCLUDED.user_id
		RETURNING project_id, user_id, created_at
	`
	var out model.ProjectMember
	if err := s.db.QueryRow(ctx, q, projectID, userID).Scan(&out.ProjectID, &out.UserID, &out.CreatedAt); err != nil {
		return model.ProjectMember{}, err
	}
	return out, nil
}

func (s *Store) RevokeProjectAccess(ctx context.Context, projectID, userID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		DELETE FROM project_members WHERE project_id = $1 AND user_id = $2
	`, projectID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListProjectMembers(ctx context.Context, projectID uuid.UUID) ([]model.ProjectMember, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	const q = `
		SELECT project_id, user_id, created_at
		FROM project_members
		WHERE project_id = $1
		ORDER BY created_at ASC, user_id ASC
	`
	rows, err := s.db.Query(ctx, q, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.ProjectMember{}
	for rows.Next() {
		var m model.ProjectMember
		if err := rows.Scan(&m.ProjectID, &m.UserID, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) ListProjectAssignees(ctx context.Context, projectID uuid.UUID) ([]model.ProjectAssignee, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	const q = `
		WITH assignees AS (
			SELECT u.id, u.username, u.name
			FROM project_members pm
			JOIN users u ON u.id = pm.user_id
			WHERE pm.project_id = $1 AND u.deleted_at IS NULL
			UNION
			SELECT u.id, u.username, u.name
			FROM issues i
			JOIN users u ON u.id = i.assignee_id
			WHERE i.project_id = $1 AND i.deleted_at IS NULL AND u.deleted_at IS NULL
		)
		SELECT id, username, name
		FROM assignees
		ORDER BY lower(name) ASC, lower(username) ASC, id ASC
	`
	rows, err := s.db.Query(ctx, q, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.ProjectAssignee{}
	for rows.Next() {
		var a model.ProjectAssignee
		if err := rows.Scan(&a.ID, &a.Username, &a.Name); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) UserCanAccessProject(ctx context.Context, user model.User, projectID uuid.UUID) (bool, error) {
	if user.IsAdmin {
		var exists bool
		err := s.db.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM projects WHERE id = $1 AND deleted_at IS NULL)
		`, projectID).Scan(&exists)
		return exists, err
	}
	var ok bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM project_members pm
			JOIN projects p ON p.id = pm.project_id
			WHERE pm.project_id = $1 AND pm.user_id = $2 AND p.deleted_at IS NULL
		)
	`, projectID, user.ID).Scan(&ok)
	return ok, err
}

func (s *Store) ProjectIDForIssue(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return s.lookupProjectID(ctx, `
		SELECT i.project_id
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		WHERE i.id = $1 AND i.deleted_at IS NULL AND p.deleted_at IS NULL
	`, id)
}

func (s *Store) ProjectIDForComment(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return s.lookupProjectID(ctx, `
		SELECT i.project_id
		FROM comments c
		JOIN issues i ON i.id = c.issue_id
		JOIN projects p ON p.id = i.project_id
		WHERE c.id = $1 AND i.deleted_at IS NULL AND p.deleted_at IS NULL
	`, id)
}

func (s *Store) ProjectIDForSprint(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return s.lookupProjectID(ctx, `
		SELECT s.project_id
		FROM sprints s
		JOIN projects p ON p.id = s.project_id
		WHERE s.id = $1 AND s.deleted_at IS NULL AND p.deleted_at IS NULL
	`, id)
}

func (s *Store) ProjectIDForIssueLink(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return s.lookupProjectID(ctx, `
		SELECT il.project_id
		FROM issue_links il
		JOIN projects p ON p.id = il.project_id
		WHERE il.id = $1 AND p.deleted_at IS NULL
	`, id)
}

func (s *Store) lookupProjectID(ctx context.Context, q string, id uuid.UUID) (uuid.UUID, error) {
	var projectID uuid.UUID
	if err := s.db.QueryRow(ctx, q, id).Scan(&projectID); err != nil {
		if isNoRows(err) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, err
	}
	return projectID, nil
}
