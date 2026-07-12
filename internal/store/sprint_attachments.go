package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bradleymackey/track-slash/internal/model"
)

type CreateSprintAttachmentParams struct {
	SprintID        uuid.UUID
	StorageObjectID uuid.UUID
	CreatedByID     uuid.UUID
}

type SprintAttachmentsCursor = IssueAttachmentsCursor

type ListSprintAttachmentsParams struct {
	SprintID uuid.UUID
	Cursor   *SprintAttachmentsCursor
	Limit    int
}

func (s *Store) CountSprintAttachments(ctx context.Context, sprintIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	out := make(map[uuid.UUID]int, len(sprintIDs))
	if len(sprintIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT sa.sprint_id, count(*)
		FROM sprint_attachments sa
		JOIN sprints sp ON sp.id = sa.sprint_id
		JOIN projects p ON p.id = sa.project_id
		JOIN storage_objects so ON so.id = sa.storage_object_id
		WHERE sa.sprint_id = ANY($1)
		  AND sp.deleted_at IS NULL AND p.deleted_at IS NULL AND so.deleted_at IS NULL
		GROUP BY sa.sprint_id
	`, sprintIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sprintID uuid.UUID
		var count int
		if err := rows.Scan(&sprintID, &count); err != nil {
			return nil, err
		}
		out[sprintID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanSprintAttachment(row descriptionAttachmentScanner) (model.SprintAttachment, error) {
	fields, err := scanDescriptionAttachmentFields(row)
	if err != nil {
		return model.SprintAttachment{}, err
	}
	return model.SprintAttachment{
		ID: fields.ID, ProjectID: fields.ProjectID, SprintID: fields.ParentID,
		StorageObjectID: fields.StorageObjectID, Object: fields.Object, CreatedByID: fields.CreatedByID,
		CreatedAt: fields.CreatedAt, UpdatedAt: fields.UpdatedAt,
	}, nil
}

func sprintAttachmentSelect() string {
	return `
		SELECT sa.id, sa.project_id, sa.sprint_id, sa.storage_object_id, sa.created_by_id, sa.created_at, sa.updated_at,
		       so.id, so.project_id, so.number, so.owner_user_id, so.backend, so.bucket, so.object_key,
		       so.filename, so.content_type, so.byte_size, so.sha256, so.created_by_id,
		       so.created_at, so.updated_at, so.deleted_at
		FROM sprint_attachments sa
		JOIN sprints sp ON sp.id = sa.sprint_id
		JOIN projects p ON p.id = sa.project_id
		JOIN storage_objects so ON so.id = sa.storage_object_id
	`
}

func (s *Store) CreateSprintAttachment(ctx context.Context, p CreateSprintAttachmentParams) (model.SprintAttachment, error) {
	var out model.SprintAttachment
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var sprint model.Sprint
		err := tx.QueryRow(ctx, `
			SELECT id, project_id, number, name
			FROM sprints
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, p.SprintID).Scan(&sprint.ID, &sprint.ProjectID, &sprint.Number, &sprint.Name)
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		sprint.Ref = model.SprintRef(sprint.Number)

		object, err := scanStorageObject(tx.QueryRow(ctx, `
			SELECT so.id, so.project_id, so.number, so.owner_user_id, so.backend, so.bucket, so.object_key,
			       so.filename, so.content_type, so.byte_size, so.sha256, so.created_by_id,
			       so.created_at, so.updated_at, so.deleted_at
			FROM storage_objects so
			JOIN projects p ON p.id = so.project_id
			WHERE so.id = $1 AND so.deleted_at IS NULL AND p.deleted_at IS NULL
			FOR UPDATE OF so
		`, p.StorageObjectID))
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		if sprint.ProjectID != object.ProjectID {
			return fmt.Errorf("sprint and storage object belong to different projects: %w", ErrConflict)
		}

		out, err = scanSprintAttachment(tx.QueryRow(ctx, `
			WITH inserted AS (
				INSERT INTO sprint_attachments (project_id, sprint_id, storage_object_id, created_by_id)
				VALUES ($1, $2, $3, $4)
				RETURNING id, project_id, sprint_id, storage_object_id, created_by_id, created_at, updated_at
			)
			SELECT ins.id, ins.project_id, ins.sprint_id, ins.storage_object_id, ins.created_by_id, ins.created_at, ins.updated_at,
			       so.id, so.project_id, so.number, so.owner_user_id, so.backend, so.bucket, so.object_key,
			       so.filename, so.content_type, so.byte_size, so.sha256, so.created_by_id,
			       so.created_at, so.updated_at, so.deleted_at
			FROM inserted ins
			JOIN sprints sp ON sp.id = ins.sprint_id
			JOIN projects p ON p.id = ins.project_id
			JOIN storage_objects so ON so.id = ins.storage_object_id
			WHERE sp.deleted_at IS NULL AND p.deleted_at IS NULL AND so.deleted_at IS NULL
		`, sprint.ProjectID, sprint.ID, object.ID, p.CreatedByID))
		if err != nil {
			if mapped := mapDescriptionAttachmentWriteError(err, "sprint"); mapped != nil {
				return mapped
			}
			return err // defensive: non-pg or unmapped pg error
		}

		return appendProjectChangelog(ctx, tx, appendProjectChangelogParams{
			ProjectID:   out.ProjectID,
			Entity:      "sprint_attachment",
			Op:          "insert",
			EntityID:    out.ID,
			TargetRef:   sprint.Ref,
			TargetTitle: sprint.Name,
			Summary:     fmt.Sprintf("Attached %s to %s", out.Object.Filename, sprint.Ref),
		})
	})
	if err != nil {
		return model.SprintAttachment{}, err
	}
	return out, nil
}

func (s *Store) ListSprintAttachments(ctx context.Context, p ListSprintAttachmentsParams) ([]model.SprintAttachment, bool, error) {
	if _, err := s.ProjectIDForSprint(ctx, p.SprintID); err != nil {
		return nil, false, err
	}
	args := []any{p.SprintID}
	q := sprintAttachmentSelect() + `
		WHERE sa.sprint_id = $1 AND sp.deleted_at IS NULL AND p.deleted_at IS NULL AND so.deleted_at IS NULL
	`
	if p.Cursor != nil {
		args = append(args, p.Cursor.Number)
		q += fmt.Sprintf(" AND so.number > $%d", len(args))
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(" ORDER BY so.number ASC LIMIT $%d", len(args))
	return scanSprintAttachmentRows(ctx, s, q, args, p.Limit)
}

func (s *Store) GetSprintAttachmentByObjectNumber(ctx context.Context, sprintID uuid.UUID, objectNumber int) (model.SprintAttachment, error) {
	q := sprintAttachmentSelect() + `
		WHERE sa.sprint_id = $1 AND so.number = $2
		  AND sp.deleted_at IS NULL AND p.deleted_at IS NULL AND so.deleted_at IS NULL
	`
	out, err := scanSprintAttachment(s.db.QueryRow(ctx, q, sprintID, objectNumber))
	if err != nil {
		if isNoRows(err) {
			return model.SprintAttachment{}, ErrNotFound
		}
		return model.SprintAttachment{}, err
	}
	return out, nil
}

func (s *Store) DeleteSprintAttachment(ctx context.Context, sprintID, storageObjectID uuid.UUID) (model.SprintAttachment, error) {
	var out model.SprintAttachment
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		out, err = scanSprintAttachment(tx.QueryRow(ctx, sprintAttachmentSelect()+`
			WHERE sa.sprint_id = $1 AND sa.storage_object_id = $2
			  AND sp.deleted_at IS NULL AND p.deleted_at IS NULL AND so.deleted_at IS NULL
			FOR UPDATE OF sa, so
		`, sprintID, storageObjectID))
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}

		var sprint model.Sprint
		err = tx.QueryRow(ctx, `SELECT number, name FROM sprints WHERE id = $1`, sprintID).Scan(&sprint.Number, &sprint.Name)
		if err != nil {
			return err
		}
		sprint.Ref = model.SprintRef(sprint.Number)

		tag, err := tx.Exec(ctx, `
			DELETE FROM sprint_attachments
			WHERE sprint_id = $1 AND storage_object_id = $2
		`, sprintID, storageObjectID)
		if err != nil {
			return err // defensive: delete has no expected constraint mapping
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}

		if err := softDeleteAttachedStorageObject(ctx, tx, storageObjectID, &out.Object); err != nil {
			return err
		}

		return appendProjectChangelog(ctx, tx, appendProjectChangelogParams{
			ProjectID:   out.ProjectID,
			Entity:      "sprint_attachment",
			Op:          "delete",
			EntityID:    out.ID,
			TargetRef:   sprint.Ref,
			TargetTitle: sprint.Name,
			Summary:     fmt.Sprintf("Removed attachment %s from %s", out.Object.Filename, sprint.Ref),
		})
	})
	if err != nil {
		return model.SprintAttachment{}, err
	}
	return out, nil
}

func scanSprintAttachmentRows(ctx context.Context, s *Store, q string, args []any, limit int) ([]model.SprintAttachment, bool, error) {
	return scanDescriptionAttachmentRows(ctx, s, q, args, limit, scanSprintAttachment)
}
