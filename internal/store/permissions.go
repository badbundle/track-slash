package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bradleymackey/track-slash/internal/model"
)

func (s *Store) GrantProjectAccess(ctx context.Context, projectID, userID uuid.UUID) (model.ProjectMember, error) {
	var out model.ProjectMember
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var project model.Project
		if err := tx.QueryRow(ctx, `
			SELECT p.id, p.owner_id, u.username, p.key, p.name, p.description, p.created_at, p.updated_at
			FROM projects p
			JOIN users u ON u.id = p.owner_id
			WHERE p.id = $1 AND p.deleted_at IS NULL AND u.deleted_at IS NULL
		`, projectID).Scan(&project.ID, &project.OwnerID, &project.OwnerUsername, &project.Key, &project.Name, &project.Description, &project.CreatedAt, &project.UpdatedAt); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		var username string
		if err := tx.QueryRow(ctx, `SELECT username FROM users WHERE id = $1 AND deleted_at IS NULL`, userID).Scan(&username); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO project_members (project_id, user_id)
			VALUES ($1, $2)
			ON CONFLICT (project_id, user_id) DO NOTHING
			RETURNING project_id, user_id, created_at
		`, projectID, userID).Scan(&out.ProjectID, &out.UserID, &out.CreatedAt)
		if err != nil {
			if !isNoRows(err) {
				return err
			}
			return tx.QueryRow(ctx, `
				SELECT project_id, user_id, created_at
				FROM project_members
				WHERE project_id = $1 AND user_id = $2
			`, projectID, userID).Scan(&out.ProjectID, &out.UserID, &out.CreatedAt)
		}
		return appendProjectChangelog(ctx, tx, appendProjectChangelogParams{
			ProjectID:   project.ID,
			Entity:      "project_member",
			Op:          "grant",
			EntityID:    userID,
			TargetRef:   project.Key,
			TargetTitle: project.Name,
			Summary:     fmt.Sprintf("Added @%s to project %s", username, project.Key),
		})
	})
	if err != nil {
		return model.ProjectMember{}, err
	}
	return out, nil
}

func (s *Store) RevokeProjectAccess(ctx context.Context, projectID, userID uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var project model.Project
		var username string
		if err := tx.QueryRow(ctx, `
			SELECT p.id, p.owner_id, owner.username, p.key, p.name, p.description, p.created_at, p.updated_at, member.username
			FROM project_members pm
			JOIN projects p ON p.id = pm.project_id
			JOIN users owner ON owner.id = p.owner_id
			JOIN users member ON member.id = pm.user_id
			WHERE pm.project_id = $1 AND pm.user_id = $2 AND p.deleted_at IS NULL
			FOR UPDATE OF pm
		`, projectID, userID).Scan(&project.ID, &project.OwnerID, &project.OwnerUsername, &project.Key, &project.Name, &project.Description, &project.CreatedAt, &project.UpdatedAt, &username); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		tag, err := tx.Exec(ctx, `
			DELETE FROM project_members WHERE project_id = $1 AND user_id = $2
		`, projectID, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return appendProjectChangelog(ctx, tx, appendProjectChangelogParams{
			ProjectID:   project.ID,
			Entity:      "project_member",
			Op:          "revoke",
			EntityID:    userID,
			TargetRef:   project.Key,
			TargetTitle: project.Name,
			Summary:     fmt.Sprintf("Removed @%s from project %s", username, project.Key),
		})
	})
}

func (s *Store) ListProjectMembers(ctx context.Context, projectID uuid.UUID) ([]model.ProjectMember, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	const q = `
		SELECT pm.project_id, pm.user_id, pm.created_at
		FROM project_members pm
		JOIN users u ON u.id = pm.user_id
		WHERE pm.project_id = $1 AND u.deleted_at IS NULL
		ORDER BY pm.created_at ASC, pm.user_id ASC
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
			SELECT u.id, u.username, u.name, u.profile_image_thumbnail_object_id
			FROM project_members pm
			JOIN users u ON u.id = pm.user_id
			WHERE pm.project_id = $1 AND u.deleted_at IS NULL
			UNION
			SELECT u.id, u.username, u.name, u.profile_image_thumbnail_object_id
			FROM issues i
			JOIN users u ON u.id = i.assignee_id
			WHERE i.project_id = $1 AND i.deleted_at IS NULL AND u.deleted_at IS NULL
		)
		SELECT id, username, name, profile_image_thumbnail_object_id
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
		var thumbnailID uuid.NullUUID
		if err := rows.Scan(&a.ID, &a.Username, &a.Name, &thumbnailID); err != nil {
			return nil, err
		}
		if thumbnailID.Valid {
			id := thumbnailID.UUID
			a.ProfileImageThumbnailObjectID = &id
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type SearchProjectMembersParams struct {
	ProjectID uuid.UUID
	Query     string
	Limit     int
}

func (s *Store) SearchProjectMembers(ctx context.Context, p SearchProjectMembersParams) ([]model.User, error) {
	if _, err := s.GetProject(ctx, p.ProjectID); err != nil {
		return nil, err
	}
	query := strings.ToLower(strings.TrimSpace(p.Query))
	const q = `
		SELECT u.id, u.username, COALESCE(u.email, ''), u.name, u.is_admin, u.created_at,
		       u.profile_image_object_id, u.profile_image_thumbnail_object_id
		FROM project_members pm
		JOIN projects p ON p.id = pm.project_id
		JOIN users u ON u.id = pm.user_id
		WHERE pm.project_id = $1
		  AND p.deleted_at IS NULL
		  AND u.deleted_at IS NULL
		  AND (
		      $2 = ''
		      OR lower(u.name) LIKE '%' || $2 || '%'
		      OR lower(u.username) LIKE '%' || $2 || '%'
		      OR lower(COALESCE(u.email, '')) LIKE '%' || $2 || '%'
		  )
		ORDER BY lower(u.name) ASC, lower(u.username) ASC, u.id ASC
		LIMIT $3
	`
	rows, err := s.db.Query(ctx, q, p.ProjectID, query, p.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
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

func (s *Store) ProjectIDForProjectContext(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return s.lookupProjectID(ctx, `
		SELECT pc.project_id
		FROM project_context pc
		JOIN projects p ON p.id = pc.project_id
		WHERE pc.id = $1 AND p.deleted_at IS NULL
	`, id)
}

func (s *Store) ProjectIDForIssueContextLink(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return s.lookupProjectID(ctx, `
		SELECT icl.project_id
		FROM issue_context_links icl
		JOIN projects p ON p.id = icl.project_id
		WHERE icl.id = $1 AND p.deleted_at IS NULL
	`, id)
}

func (s *Store) ProjectIDForIssueTag(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return s.lookupProjectID(ctx, `
		SELECT t.project_id
		FROM issue_tags t
		JOIN projects p ON p.id = t.project_id
		WHERE t.id = $1 AND p.deleted_at IS NULL
	`, id)
}

func (s *Store) ProjectIDForIssueTagLink(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return s.lookupProjectID(ctx, `
		SELECT l.project_id
		FROM issue_tag_links l
		JOIN projects p ON p.id = l.project_id
		WHERE l.id = $1 AND p.deleted_at IS NULL
	`, id)
}

func (s *Store) ProjectIDForIssueAttachment(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return s.lookupProjectID(ctx, `
		SELECT ia.project_id
		FROM issue_attachments ia
		JOIN issues i ON i.id = ia.issue_id
		JOIN projects p ON p.id = ia.project_id
		JOIN storage_objects so ON so.id = ia.storage_object_id
		WHERE ia.id = $1 AND i.deleted_at IS NULL AND p.deleted_at IS NULL AND so.deleted_at IS NULL
	`, id)
}

func (s *Store) ProjectIDForProjectAttachment(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return s.lookupProjectID(ctx, `
		SELECT pa.project_id
		FROM project_attachments pa
		JOIN projects p ON p.id = pa.project_id
		JOIN storage_objects so ON so.id = pa.storage_object_id
		WHERE pa.id = $1 AND p.deleted_at IS NULL AND so.deleted_at IS NULL
	`, id)
}

func (s *Store) ProjectIDForProjectChangelog(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return s.lookupProjectID(ctx, `
		SELECT e.project_id
		FROM project_changelog_entries e
		JOIN projects p ON p.id = e.project_id
		WHERE e.id = $1 AND p.deleted_at IS NULL
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
