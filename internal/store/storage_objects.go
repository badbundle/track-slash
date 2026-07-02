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

type CreateStorageObjectParams struct {
	ID          uuid.UUID
	ProjectID   uuid.UUID
	Backend     string
	Bucket      string
	ObjectKey   string
	Filename    string
	ContentType string
	ByteSize    int64
	SHA256      string
	CreatedByID uuid.UUID
}

type StorageObjectsCursor struct {
	Number int `json:"n"`
}

type ListStorageObjectsParams struct {
	ProjectID uuid.UUID
	Cursor    *StorageObjectsCursor
	Limit     int
}

type storageObjectScanner interface {
	Scan(dest ...any) error
}

func scanStorageObject(row storageObjectScanner) (model.StorageObject, error) {
	var out model.StorageObject
	var deletedAt sql.NullTime
	err := row.Scan(
		&out.ID, &out.ProjectID, &out.Number, &out.Backend, &out.Bucket, &out.ObjectKey,
		&out.Filename, &out.ContentType, &out.ByteSize, &out.SHA256, &out.CreatedByID,
		&out.CreatedAt, &out.UpdatedAt, &deletedAt,
	)
	if err != nil {
		return model.StorageObject{}, err
	}
	if deletedAt.Valid {
		out.DeletedAt = &deletedAt.Time
	}
	out.Ref = model.StorageObjectRef(out.Number)
	return out, nil
}

func (s *Store) CreateStorageObject(ctx context.Context, p CreateStorageObjectParams) (model.StorageObject, error) {
	var out model.StorageObject
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var number int
		if err := tx.QueryRow(ctx, `
			SELECT next_object_number
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
		out, err = scanStorageObject(tx.QueryRow(ctx, `
			INSERT INTO storage_objects (
				id, project_id, number, backend, bucket, object_key, filename, content_type,
				byte_size, sha256, created_by_id
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING id, project_id, number, backend, bucket, object_key, filename, content_type,
			          byte_size, sha256, created_by_id, created_at, updated_at, deleted_at
		`, p.ID, p.ProjectID, number, p.Backend, p.Bucket, p.ObjectKey, p.Filename, p.ContentType, p.ByteSize, p.SHA256, p.CreatedByID))
		if err != nil {
			if mapped := mapStorageObjectWriteError(err); mapped != nil {
				return mapped
			}
			return err // defensive: non-pg or unmapped pg error
		}

		if _, err := tx.Exec(ctx, `
			UPDATE projects
			SET next_object_number = next_object_number + 1,
			    updated_at = now()
			WHERE id = $1
		`, p.ProjectID); err != nil {
			return err // defensive: project row was locked above
		}
		return nil
	})
	if err != nil {
		return model.StorageObject{}, err
	}
	return out, nil
}

func (s *Store) GetStorageObjectByProjectNumber(ctx context.Context, projectID uuid.UUID, number int) (model.StorageObject, error) {
	const q = `
		SELECT so.id, so.project_id, so.number, so.backend, so.bucket, so.object_key,
		       so.filename, so.content_type, so.byte_size, so.sha256, so.created_by_id,
		       so.created_at, so.updated_at, so.deleted_at
		FROM storage_objects so
		JOIN projects p ON p.id = so.project_id
		WHERE so.project_id = $1 AND so.number = $2 AND so.deleted_at IS NULL AND p.deleted_at IS NULL
	`
	out, err := scanStorageObject(s.db.QueryRow(ctx, q, projectID, number))
	if err != nil {
		if isNoRows(err) {
			return model.StorageObject{}, ErrNotFound
		}
		return model.StorageObject{}, err
	}
	return out, nil
}

func (s *Store) ListStorageObjects(ctx context.Context, p ListStorageObjectsParams) ([]model.StorageObject, bool, error) {
	if _, err := s.GetProject(ctx, p.ProjectID); err != nil {
		return nil, false, err
	}
	args := []any{p.ProjectID}
	q := `
		SELECT so.id, so.project_id, so.number, so.backend, so.bucket, so.object_key,
		       so.filename, so.content_type, so.byte_size, so.sha256, so.created_by_id,
		       so.created_at, so.updated_at, so.deleted_at
		FROM storage_objects so
		JOIN projects p ON p.id = so.project_id
		WHERE so.project_id = $1 AND so.deleted_at IS NULL AND p.deleted_at IS NULL
	`
	if p.Cursor != nil {
		args = append(args, p.Cursor.Number)
		q += fmt.Sprintf(" AND so.number > $%d", len(args))
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(" ORDER BY so.number ASC LIMIT $%d", len(args))

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := make([]model.StorageObject, 0, p.Limit)
	for rows.Next() {
		item, err := scanStorageObject(rows)
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

func (s *Store) DeleteStorageObject(ctx context.Context, id uuid.UUID) (model.StorageObject, error) {
	var out model.StorageObject
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		out, err = scanStorageObject(tx.QueryRow(ctx, `
			SELECT so.id, so.project_id, so.number, so.backend, so.bucket, so.object_key,
			       so.filename, so.content_type, so.byte_size, so.sha256, so.created_by_id,
			       so.created_at, so.updated_at, so.deleted_at
			FROM storage_objects so
			JOIN projects p ON p.id = so.project_id
			WHERE so.id = $1 AND so.deleted_at IS NULL AND p.deleted_at IS NULL
			FOR UPDATE OF so
		`, id))
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		var deletedAt time.Time
		err = tx.QueryRow(ctx, `
			UPDATE storage_objects
			SET deleted_at = now(),
			    updated_at = GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')
			WHERE id = $1 AND deleted_at IS NULL
			RETURNING updated_at, deleted_at
		`, id).Scan(&out.UpdatedAt, &deletedAt)
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: soft-delete has no expected FK/check mapping
		}
		out.DeletedAt = &deletedAt
		return nil
	})
	if err != nil {
		return model.StorageObject{}, err
	}
	return out, nil
}

func mapStorageObjectWriteError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	switch pgErr.Code {
	case "23503":
		return fmt.Errorf("invalid storage object reference: %w", ErrConflict)
	case "23505":
		return fmt.Errorf("storage object already exists: %w", ErrConflict)
	case "23514":
		return fmt.Errorf("invalid storage object metadata: %w", ErrConflict)
	default:
		return nil
	}
}
