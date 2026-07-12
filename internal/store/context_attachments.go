package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bradleymackey/track-slash/internal/model"
)

type CreateContextAttachmentParams struct {
	ProjectID       uuid.UUID
	ContextID       uuid.UUID
	StorageObjectID uuid.UUID
	CreatedByID     uuid.UUID
}

type ContextAttachmentsCursor = IssueAttachmentsCursor

type ListContextAttachmentsParams struct {
	ContextID uuid.UUID
	Cursor    *ContextAttachmentsCursor
	Limit     int
}

func scanContextAttachment(row descriptionAttachmentScanner) (model.ContextAttachment, error) {
	fields, err := scanDescriptionAttachmentFields(row)
	if err != nil {
		return model.ContextAttachment{}, err
	}
	return model.ContextAttachment{
		ID: fields.ID, ProjectID: fields.ProjectID, ContextID: fields.ParentID,
		StorageObjectID: fields.StorageObjectID, Object: fields.Object,
		CreatedByID: fields.CreatedByID, CreatedAt: fields.CreatedAt, UpdatedAt: fields.UpdatedAt,
	}, nil
}

func contextAttachmentSelect() string {
	return `
		SELECT ca.id, ca.project_id, ca.context_id, ca.storage_object_id, ca.created_by_id, ca.created_at, ca.updated_at,
		       so.id, so.project_id, so.number, so.owner_user_id, so.backend, so.bucket, so.object_key,
		       so.filename, so.content_type, so.byte_size, so.sha256, so.created_by_id,
		       so.created_at, so.updated_at, so.deleted_at
		FROM context_attachments ca
		JOIN project_context pc ON pc.id = ca.context_id
		JOIN projects p ON p.id = ca.project_id
		JOIN storage_objects so ON so.id = ca.storage_object_id
	`
}

func (s *Store) CreateContextAttachment(ctx context.Context, p CreateContextAttachmentParams) (model.ContextAttachment, error) {
	var out model.ContextAttachment
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		contextItem, err := scanProjectContext(tx.QueryRow(ctx, `
			SELECT pc.id, pc.project_id, pc.number, pc.scope, pc.position, pc.title, pc.kind, pc.content_type, pc.body,
			       pc.source_filename, pc.created_by_id, pc.updated_by_id, pc.created_at, pc.updated_at
			FROM project_context pc
			JOIN projects p ON p.id = pc.project_id
			WHERE pc.id = $1 AND pc.project_id = $2 AND pc.scope = 'project' AND p.deleted_at IS NULL
			FOR UPDATE OF pc
		`, p.ContextID, p.ProjectID))
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}

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
		if contextItem.ProjectID != object.ProjectID {
			return fmt.Errorf("context and storage object belong to different projects: %w", ErrConflict)
		}

		out, err = scanContextAttachment(tx.QueryRow(ctx, `
			WITH inserted AS (
				INSERT INTO context_attachments (project_id, context_id, storage_object_id, created_by_id)
				VALUES ($1, $2, $3, $4)
				RETURNING id, project_id, context_id, storage_object_id, created_by_id, created_at, updated_at
			)
			SELECT ins.id, ins.project_id, ins.context_id, ins.storage_object_id, ins.created_by_id, ins.created_at, ins.updated_at,
			       so.id, so.project_id, so.number, so.owner_user_id, so.backend, so.bucket, so.object_key,
			       so.filename, so.content_type, so.byte_size, so.sha256, so.created_by_id,
			       so.created_at, so.updated_at, so.deleted_at
			FROM inserted ins
			JOIN storage_objects so ON so.id = ins.storage_object_id
		`, contextItem.ProjectID, contextItem.ID, object.ID, p.CreatedByID))
		if err != nil {
			if mapped := mapDescriptionAttachmentWriteError(err, "context"); mapped != nil {
				return mapped
			}
			return err
		}
		return appendProjectChangelog(ctx, tx, appendProjectChangelogParams{
			ProjectID: contextItem.ProjectID, Entity: "context_attachment", Op: "insert", EntityID: out.ID,
			TargetRef: changelogContextRef(contextItem), TargetTitle: contextItem.Title,
			Summary: fmt.Sprintf("Attached %s to context %s", out.Object.Filename, contextItem.Title),
		})
	})
	if err != nil {
		return model.ContextAttachment{}, err
	}
	return out, nil
}

func (s *Store) ListContextAttachments(ctx context.Context, p ListContextAttachmentsParams) ([]model.ContextAttachment, bool, error) {
	contextItem, err := s.GetProjectContext(ctx, p.ContextID)
	if err != nil {
		return nil, false, err
	}
	if contextItem.Scope != model.ProjectContextScopeProject {
		return nil, false, ErrNotFound
	}
	args := []any{p.ContextID}
	q := contextAttachmentSelect() + `
		WHERE ca.context_id = $1 AND pc.scope = 'project' AND p.deleted_at IS NULL AND so.deleted_at IS NULL
	`
	if p.Cursor != nil {
		args = append(args, p.Cursor.Number)
		q += fmt.Sprintf(" AND so.number > $%d", len(args))
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(" ORDER BY so.number ASC LIMIT $%d", len(args))
	return scanDescriptionAttachmentRows(ctx, s, q, args, p.Limit, scanContextAttachment)
}

func (s *Store) GetContextAttachmentByObjectNumber(ctx context.Context, contextID uuid.UUID, objectNumber int) (model.ContextAttachment, error) {
	out, err := scanContextAttachment(s.db.QueryRow(ctx, contextAttachmentSelect()+`
		WHERE ca.context_id = $1 AND so.number = $2 AND pc.scope = 'project'
		  AND p.deleted_at IS NULL AND so.deleted_at IS NULL
	`, contextID, objectNumber))
	if err != nil {
		if isNoRows(err) {
			return model.ContextAttachment{}, ErrNotFound
		}
		return model.ContextAttachment{}, err
	}
	return out, nil
}

func (s *Store) DeleteContextAttachment(ctx context.Context, contextID, storageObjectID uuid.UUID) (model.ContextAttachment, error) {
	var out model.ContextAttachment
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		out, err = scanContextAttachment(tx.QueryRow(ctx, contextAttachmentSelect()+`
			WHERE ca.context_id = $1 AND ca.storage_object_id = $2 AND pc.scope = 'project'
			  AND p.deleted_at IS NULL AND so.deleted_at IS NULL
			FOR UPDATE OF ca, so
		`, contextID, storageObjectID))
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		contextItem, err := scanProjectContext(tx.QueryRow(ctx, `
			SELECT id, project_id, number, scope, position, title, kind, content_type, body,
			       source_filename, created_by_id, updated_by_id, created_at, updated_at
			FROM project_context WHERE id = $1
		`, contextID))
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM context_attachments WHERE context_id = $1 AND storage_object_id = $2`, contextID, storageObjectID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err := softDeleteAttachedStorageObject(ctx, tx, storageObjectID, &out.Object); err != nil {
			return err
		}
		return appendProjectChangelog(ctx, tx, appendProjectChangelogParams{
			ProjectID: out.ProjectID, Entity: "context_attachment", Op: "delete", EntityID: out.ID,
			TargetRef: changelogContextRef(contextItem), TargetTitle: contextItem.Title,
			Summary: fmt.Sprintf("Removed attachment %s from context %s", out.Object.Filename, contextItem.Title),
		})
	})
	if err != nil {
		return model.ContextAttachment{}, err
	}
	return out, nil
}
