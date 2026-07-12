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

type descriptionAttachmentScanner interface {
	Scan(dest ...any) error
}

type descriptionAttachmentFields struct {
	ID              uuid.UUID
	ProjectID       uuid.UUID
	ParentID        uuid.UUID
	StorageObjectID uuid.UUID
	CreatedByID     uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Object          model.StorageObject
}

func scanDescriptionAttachmentFields(row descriptionAttachmentScanner) (descriptionAttachmentFields, error) {
	var out descriptionAttachmentFields
	var objectDeletedAt sql.NullTime
	var objectProjectID, objectOwnerUserID uuid.NullUUID
	err := row.Scan(
		&out.ID, &out.ProjectID, &out.ParentID, &out.StorageObjectID, &out.CreatedByID, &out.CreatedAt, &out.UpdatedAt,
		&out.Object.ID, &objectProjectID, &out.Object.Number, &objectOwnerUserID, &out.Object.Backend, &out.Object.Bucket,
		&out.Object.ObjectKey, &out.Object.Filename, &out.Object.ContentType, &out.Object.ByteSize,
		&out.Object.SHA256, &out.Object.CreatedByID, &out.Object.CreatedAt, &out.Object.UpdatedAt, &objectDeletedAt,
	)
	if err != nil {
		return descriptionAttachmentFields{}, err
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

func scanDescriptionAttachmentRows[T any](ctx context.Context, s *Store, q string, args []any, limit int, scan func(descriptionAttachmentScanner) (T, error)) ([]T, bool, error) {
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := make([]T, 0, limit)
	for rows.Next() {
		item, err := scan(rows)
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

func softDeleteAttachedStorageObject(ctx context.Context, tx pgx.Tx, storageObjectID uuid.UUID, object *model.StorageObject) error {
	var deletedAt time.Time
	err := tx.QueryRow(ctx, `
		UPDATE storage_objects
		SET deleted_at = now(),
		    updated_at = GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING updated_at, deleted_at
	`, storageObjectID).Scan(&object.UpdatedAt, &deletedAt)
	if err != nil {
		if isNoRows(err) {
			return ErrNotFound
		}
		return err // defensive: caller selected and locked the live object
	}
	object.DeletedAt = &deletedAt
	return nil
}

func mapDescriptionAttachmentWriteError(err error, parent string) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	switch pgErr.Code {
	case "23503":
		return fmt.Errorf("invalid %s attachment reference: %w", parent, ErrConflict)
	case "23505":
		return fmt.Errorf("%s attachment already exists: %w", parent, ErrConflict)
	}
	return nil
}
