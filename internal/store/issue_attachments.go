package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bradleymackey/track-slash/internal/model"
)

type CreateIssueAttachmentParams struct {
	IssueID         uuid.UUID
	StorageObjectID uuid.UUID
	CreatedByID     uuid.UUID
}

type IssueAttachmentsCursor struct {
	Number int `json:"n"`
}

type ListIssueAttachmentsParams struct {
	IssueID uuid.UUID
	Cursor  *IssueAttachmentsCursor
	Limit   int
}

type issueAttachmentScanner interface {
	Scan(dest ...any) error
}

func scanIssueAttachment(row issueAttachmentScanner) (model.IssueAttachment, error) {
	var out model.IssueAttachment
	var objectDeletedAt sql.NullTime
	var objectProjectID, objectOwnerUserID uuid.NullUUID
	err := row.Scan(
		&out.ID, &out.ProjectID, &out.IssueID, &out.StorageObjectID, &out.CreatedByID, &out.CreatedAt, &out.UpdatedAt,
		&out.Object.ID, &objectProjectID, &out.Object.Number, &objectOwnerUserID, &out.Object.Backend, &out.Object.Bucket,
		&out.Object.ObjectKey, &out.Object.Filename, &out.Object.ContentType, &out.Object.ByteSize,
		&out.Object.SHA256, &out.Object.CreatedByID, &out.Object.CreatedAt, &out.Object.UpdatedAt, &objectDeletedAt,
	)
	if err != nil {
		return model.IssueAttachment{}, err
	}
	if objectDeletedAt.Valid {
		out.Object.DeletedAt = &objectDeletedAt.Time
	}
	if objectProjectID.Valid {
		out.Object.ProjectID = objectProjectID.UUID
	}
	if objectOwnerUserID.Valid {
		id := objectOwnerUserID.UUID
		out.Object.OwnerUserID = &id
	}
	if out.Object.Number > 0 {
		out.Object.Ref = model.StorageObjectRef(out.Object.Number)
	}
	return out, nil
}

func issueAttachmentSelect() string {
	return `
		SELECT ia.id, ia.project_id, ia.issue_id, ia.storage_object_id, ia.created_by_id, ia.created_at, ia.updated_at,
		       so.id, so.project_id, so.number, so.owner_user_id, so.backend, so.bucket, so.object_key,
		       so.filename, so.content_type, so.byte_size, so.sha256, so.created_by_id,
		       so.created_at, so.updated_at, so.deleted_at
		FROM issue_attachments ia
		JOIN issues i ON i.id = ia.issue_id
		JOIN projects p ON p.id = ia.project_id
		JOIN storage_objects so ON so.id = ia.storage_object_id
	`
}

func (s *Store) CreateIssueAttachment(ctx context.Context, p CreateIssueAttachmentParams) (model.IssueAttachment, error) {
	var out model.IssueAttachment
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		issue, err := getIssueForChangelog(ctx, tx, p.IssueID, false)
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
		if issue.ProjectID != object.ProjectID {
			return fmt.Errorf("issue and storage object belong to different projects: %w", ErrConflict)
		}

		out, err = scanIssueAttachment(tx.QueryRow(ctx, `
			WITH inserted AS (
				INSERT INTO issue_attachments (project_id, issue_id, storage_object_id, created_by_id)
				VALUES ($1, $2, $3, $4)
				RETURNING id, project_id, issue_id, storage_object_id, created_by_id, created_at, updated_at
			)
			SELECT ins.id, ins.project_id, ins.issue_id, ins.storage_object_id, ins.created_by_id, ins.created_at, ins.updated_at,
			       so.id, so.project_id, so.number, so.owner_user_id, so.backend, so.bucket, so.object_key,
			       so.filename, so.content_type, so.byte_size, so.sha256, so.created_by_id,
			       so.created_at, so.updated_at, so.deleted_at
			FROM inserted ins
			JOIN issues i ON i.id = ins.issue_id
			JOIN projects p ON p.id = ins.project_id
			JOIN storage_objects so ON so.id = ins.storage_object_id
			WHERE i.deleted_at IS NULL AND p.deleted_at IS NULL AND so.deleted_at IS NULL
		`, issue.ProjectID, issue.ID, object.ID, p.CreatedByID))
		if err != nil {
			if mapped := mapIssueAttachmentWriteError(err); mapped != nil {
				return mapped
			}
			return err // defensive: non-pg or unmapped pg error
		}

		targetRef, targetTitle := changelogTarget(issue)
		return appendProjectChangelog(ctx, tx, appendProjectChangelogParams{
			ProjectID:   out.ProjectID,
			Entity:      "issue_attachment",
			Op:          "insert",
			EntityID:    out.ID,
			IssueID:     &issue.ID,
			TargetRef:   targetRef,
			TargetTitle: targetTitle,
			Summary:     fmt.Sprintf("Attached %s to %s", out.Object.Filename, issue.Identifier),
		})
	})
	if err != nil {
		return model.IssueAttachment{}, err
	}
	return out, nil
}

func (s *Store) ListIssueAttachments(ctx context.Context, p ListIssueAttachmentsParams) ([]model.IssueAttachment, bool, error) {
	if _, err := s.ProjectIDForIssue(ctx, p.IssueID); err != nil {
		return nil, false, err
	}
	args := []any{p.IssueID}
	q := issueAttachmentSelect() + `
		WHERE ia.issue_id = $1 AND i.deleted_at IS NULL AND p.deleted_at IS NULL AND so.deleted_at IS NULL
	`
	if p.Cursor != nil {
		args = append(args, p.Cursor.Number)
		q += fmt.Sprintf(" AND so.number > $%d", len(args))
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(" ORDER BY so.number ASC LIMIT $%d", len(args))
	return scanIssueAttachmentRows(ctx, s, q, args, p.Limit)
}

func (s *Store) GetIssueAttachmentByObjectNumber(ctx context.Context, issueID uuid.UUID, objectNumber int) (model.IssueAttachment, error) {
	q := issueAttachmentSelect() + `
		WHERE ia.issue_id = $1 AND so.number = $2
		  AND i.deleted_at IS NULL AND p.deleted_at IS NULL AND so.deleted_at IS NULL
	`
	out, err := scanIssueAttachment(s.db.QueryRow(ctx, q, issueID, objectNumber))
	if err != nil {
		if isNoRows(err) {
			return model.IssueAttachment{}, ErrNotFound
		}
		return model.IssueAttachment{}, err
	}
	return out, nil
}

func (s *Store) DeleteIssueAttachment(ctx context.Context, issueID, storageObjectID uuid.UUID) (model.IssueAttachment, error) {
	var out model.IssueAttachment
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		out, err = scanIssueAttachment(tx.QueryRow(ctx, issueAttachmentSelect()+`
			WHERE ia.issue_id = $1 AND ia.storage_object_id = $2
			  AND i.deleted_at IS NULL AND p.deleted_at IS NULL AND so.deleted_at IS NULL
			FOR UPDATE OF ia, so
		`, issueID, storageObjectID))
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		issue, err := getIssueForChangelog(ctx, tx, issueID, false)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			DELETE FROM issue_attachments
			WHERE issue_id = $1 AND storage_object_id = $2
		`, issueID, storageObjectID)
		if err != nil {
			return err // defensive: delete has no expected constraint mapping
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}

		var deletedAt time.Time
		err = tx.QueryRow(ctx, `
			UPDATE storage_objects
			SET deleted_at = now(),
			    updated_at = GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')
			WHERE id = $1 AND deleted_at IS NULL
			RETURNING updated_at, deleted_at
		`, storageObjectID).Scan(&out.Object.UpdatedAt, &deletedAt)
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: selected object was live and locked above
		}
		out.Object.DeletedAt = &deletedAt

		targetRef, targetTitle := changelogTarget(issue)
		return appendProjectChangelog(ctx, tx, appendProjectChangelogParams{
			ProjectID:   out.ProjectID,
			Entity:      "issue_attachment",
			Op:          "delete",
			EntityID:    out.ID,
			IssueID:     &issue.ID,
			TargetRef:   targetRef,
			TargetTitle: targetTitle,
			Summary:     fmt.Sprintf("Removed attachment %s from %s", out.Object.Filename, issue.Identifier),
		})
	})
	if err != nil {
		return model.IssueAttachment{}, err
	}
	return out, nil
}

func scanIssueAttachmentRows(ctx context.Context, s *Store, q string, args []any, limit int) ([]model.IssueAttachment, bool, error) {
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := make([]model.IssueAttachment, 0, limit)
	for rows.Next() {
		item, err := scanIssueAttachment(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

func mapIssueAttachmentWriteError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	switch pgErr.Code {
	case "23503":
		return fmt.Errorf("invalid issue attachment reference: %w", ErrConflict)
	case "23505":
		return fmt.Errorf("issue attachment already exists: %w", ErrConflict)
	}
	return nil
}
