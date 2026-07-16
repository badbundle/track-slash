package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type StorageObjectDeletionStatus string

const (
	StorageObjectDeletionPending    StorageObjectDeletionStatus = "pending"
	StorageObjectDeletionProcessing StorageObjectDeletionStatus = "processing"
	StorageObjectDeletionFailed     StorageObjectDeletionStatus = "failed"
)

type StorageObjectDeletion struct {
	StorageObjectID uuid.UUID
	Backend         string
	Bucket          string
	ObjectKey       string
	Status          StorageObjectDeletionStatus
	AttemptCount    int
	NextAttemptAt   *time.Time
	LockedAt        *time.Time
	LastError       string
	FailedAt        *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type storageObjectDeletionScanner interface {
	Scan(dest ...any) error
}

func scanStorageObjectDeletion(row storageObjectDeletionScanner) (StorageObjectDeletion, error) {
	var out StorageObjectDeletion
	var nextAttemptAt, lockedAt, failedAt sql.NullTime
	err := row.Scan(
		&out.StorageObjectID, &out.Backend, &out.Bucket, &out.ObjectKey, &out.Status,
		&out.AttemptCount, &nextAttemptAt, &lockedAt, &out.LastError, &failedAt,
		&out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return StorageObjectDeletion{}, err
	}
	if nextAttemptAt.Valid {
		out.NextAttemptAt = &nextAttemptAt.Time
	}
	if lockedAt.Valid {
		out.LockedAt = &lockedAt.Time
	}
	if failedAt.Valid {
		out.FailedAt = &failedAt.Time
	}
	return out, nil
}

func storageObjectDeletionSelect() string {
	return `
		SELECT storage_object_id, backend, bucket, object_key, status, attempt_count,
		       next_attempt_at, locked_at, last_error, failed_at, created_at, updated_at
		FROM storage_object_deletions
	`
}

func (s *Store) GetStorageObjectDeletion(ctx context.Context, storageObjectID uuid.UUID) (StorageObjectDeletion, error) {
	out, err := scanStorageObjectDeletion(s.db.QueryRow(ctx, storageObjectDeletionSelect()+`
		WHERE storage_object_id = $1
	`, storageObjectID))
	if err != nil {
		if isNoRows(err) {
			return StorageObjectDeletion{}, ErrNotFound
		}
		return StorageObjectDeletion{}, err
	}
	return out, nil
}

func (s *Store) ClaimStorageObjectDeletions(ctx context.Context, limit int, leaseTimeout time.Duration) ([]StorageObjectDeletion, error) {
	if limit <= 0 || leaseTimeout <= 0 {
		return nil, ErrConflict
	}
	rows, err := s.db.Query(ctx, `
		WITH candidates AS (
			SELECT storage_object_id
			FROM storage_object_deletions
			WHERE (status = 'pending' AND next_attempt_at <= now())
			   OR (status = 'processing' AND locked_at <= now() - make_interval(secs => $2))
			ORDER BY COALESCE(next_attempt_at, locked_at), created_at
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE storage_object_deletions d
		SET status = 'processing',
		    attempt_count = d.attempt_count + 1,
		    next_attempt_at = NULL,
		    locked_at = now(),
		    failed_at = NULL,
		    updated_at = now()
		FROM candidates c
		WHERE d.storage_object_id = c.storage_object_id
		RETURNING d.storage_object_id, d.backend, d.bucket, d.object_key, d.status, d.attempt_count,
		          d.next_attempt_at, d.locked_at, d.last_error, d.failed_at, d.created_at, d.updated_at
	`, limit, leaseTimeout.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]StorageObjectDeletion, 0, limit)
	for rows.Next() {
		item, err := scanStorageObjectDeletion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) CompleteStorageObjectDeletion(ctx context.Context, storageObjectID uuid.UUID, attemptCount int) error {
	_, err := s.db.Exec(ctx, `
		DELETE FROM storage_object_deletions
		WHERE storage_object_id = $1 AND status = 'processing' AND attempt_count = $2
	`, storageObjectID, attemptCount)
	return err
}

func (s *Store) RetryStorageObjectDeletion(ctx context.Context, storageObjectID uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE storage_object_deletions
		SET status = 'pending',
		    next_attempt_at = $3,
		    locked_at = NULL,
		    last_error = $4,
		    failed_at = NULL,
		    updated_at = now()
		WHERE storage_object_id = $1 AND status = 'processing' AND attempt_count = $2
	`, storageObjectID, attemptCount, nextAttemptAt, lastError)
	return err
}

func (s *Store) FailStorageObjectDeletion(ctx context.Context, storageObjectID uuid.UUID, attemptCount int, lastError string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE storage_object_deletions
		SET status = 'failed',
		    next_attempt_at = NULL,
		    locked_at = NULL,
		    last_error = $3,
		    failed_at = now(),
		    updated_at = now()
		WHERE storage_object_id = $1 AND status = 'processing' AND attempt_count = $2
	`, storageObjectID, attemptCount, lastError)
	return err
}
