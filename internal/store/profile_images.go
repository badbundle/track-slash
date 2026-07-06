package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bradleymackey/track-slash/internal/model"
)

type ReplaceUserProfileImageResult struct {
	User           model.User
	DeletedObjects []model.StorageObject
}

func (s *Store) ReplaceUserProfileImage(ctx context.Context, userID, objectID, thumbnailObjectID uuid.UUID) (ReplaceUserProfileImageResult, error) {
	if objectID == uuid.Nil || thumbnailObjectID == uuid.Nil || objectID == thumbnailObjectID {
		return ReplaceUserProfileImageResult{}, ErrConflict
	}
	var out ReplaceUserProfileImageResult
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		current, err := userForProfileImageUpdate(ctx, tx, userID)
		if err != nil {
			return err
		}
		if _, err := userStorageObjectForUpdate(ctx, tx, userID, objectID); err != nil {
			return err
		}
		if _, err := userStorageObjectForUpdate(ctx, tx, userID, thumbnailObjectID); err != nil {
			return err
		}

		next, err := scanUser(tx.QueryRow(ctx, `
			UPDATE users
			SET profile_image_object_id = $2,
			    profile_image_thumbnail_object_id = $3
			WHERE id = $1 AND deleted_at IS NULL
			RETURNING id, username, COALESCE(email, ''), name, is_admin, created_at, profile_image_object_id, profile_image_thumbnail_object_id
		`, userID, objectID, thumbnailObjectID))
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: user row was locked above
		}
		out.User = next

		var oldIDs []uuid.UUID
		if current.ProfileImageObjectID != nil && *current.ProfileImageObjectID != objectID && *current.ProfileImageObjectID != thumbnailObjectID {
			oldIDs = append(oldIDs, *current.ProfileImageObjectID)
		}
		if current.ProfileImageThumbnailObjectID != nil && *current.ProfileImageThumbnailObjectID != objectID && *current.ProfileImageThumbnailObjectID != thumbnailObjectID {
			oldIDs = append(oldIDs, *current.ProfileImageThumbnailObjectID)
		}
		deleted, err := softDeleteUserStorageObjects(ctx, tx, userID, oldIDs)
		if err != nil {
			return err
		}
		out.DeletedObjects = deleted
		return nil
	})
	if err != nil {
		return ReplaceUserProfileImageResult{}, err
	}
	return out, nil
}

func (s *Store) RemoveUserProfileImage(ctx context.Context, userID uuid.UUID) (ReplaceUserProfileImageResult, error) {
	var out ReplaceUserProfileImageResult
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		current, err := userForProfileImageUpdate(ctx, tx, userID)
		if err != nil {
			return err
		}
		if current.ProfileImageObjectID == nil && current.ProfileImageThumbnailObjectID == nil {
			out.User = current
			return nil
		}

		next, err := scanUser(tx.QueryRow(ctx, `
			UPDATE users
			SET profile_image_object_id = NULL,
			    profile_image_thumbnail_object_id = NULL
			WHERE id = $1 AND deleted_at IS NULL
			RETURNING id, username, COALESCE(email, ''), name, is_admin, created_at, profile_image_object_id, profile_image_thumbnail_object_id
		`, userID))
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: user row was locked above
		}
		out.User = next

		var oldIDs []uuid.UUID
		if current.ProfileImageObjectID != nil {
			oldIDs = append(oldIDs, *current.ProfileImageObjectID)
		}
		if current.ProfileImageThumbnailObjectID != nil {
			oldIDs = append(oldIDs, *current.ProfileImageThumbnailObjectID)
		}
		deleted, err := softDeleteUserStorageObjects(ctx, tx, userID, oldIDs)
		if err != nil {
			return err
		}
		out.DeletedObjects = deleted
		return nil
	})
	if err != nil {
		return ReplaceUserProfileImageResult{}, err
	}
	return out, nil
}

func (s *Store) GetUserProfileImageObject(ctx context.Context, userID uuid.UUID, thumbnail bool) (model.StorageObject, error) {
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return model.StorageObject{}, err
	}
	objectID := user.ProfileImageObjectID
	if thumbnail {
		objectID = user.ProfileImageThumbnailObjectID
	}
	if objectID == nil {
		return model.StorageObject{}, ErrNotFound
	}
	object, err := scanStorageObject(s.db.QueryRow(ctx, `
		SELECT so.id, so.project_id, so.number, so.owner_user_id, so.backend, so.bucket, so.object_key,
		       so.filename, so.content_type, so.byte_size, so.sha256, so.created_by_id,
		       so.created_at, so.updated_at, so.deleted_at
		FROM storage_objects so
		JOIN users u ON u.id = so.owner_user_id
		WHERE so.id = $1
		  AND so.owner_user_id = $2
		  AND so.project_id IS NULL
		  AND so.deleted_at IS NULL
		  AND u.deleted_at IS NULL
	`, *objectID, userID))
	if err != nil {
		if isNoRows(err) {
			return model.StorageObject{}, ErrNotFound
		}
		return model.StorageObject{}, err
	}
	return object, nil
}

func userForProfileImageUpdate(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (model.User, error) {
	user, err := scanUser(tx.QueryRow(ctx, `
		SELECT id, username, COALESCE(email, ''), name, is_admin, created_at, profile_image_object_id, profile_image_thumbnail_object_id
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, userID))
	if err != nil {
		if isNoRows(err) {
			return model.User{}, ErrNotFound
		}
		return model.User{}, err
	}
	return user, nil
}

func userStorageObjectForUpdate(ctx context.Context, tx pgx.Tx, userID, objectID uuid.UUID) (model.StorageObject, error) {
	object, err := scanStorageObject(tx.QueryRow(ctx, `
		SELECT so.id, so.project_id, so.number, so.owner_user_id, so.backend, so.bucket, so.object_key,
		       so.filename, so.content_type, so.byte_size, so.sha256, so.created_by_id,
		       so.created_at, so.updated_at, so.deleted_at
		FROM storage_objects so
		WHERE so.id = $1
		  AND so.owner_user_id = $2
		  AND so.project_id IS NULL
		  AND so.deleted_at IS NULL
		FOR UPDATE
	`, objectID, userID))
	if err != nil {
		if isNoRows(err) {
			return model.StorageObject{}, ErrNotFound
		}
		return model.StorageObject{}, err
	}
	return object, nil
}

func softDeleteUserStorageObjects(ctx context.Context, tx pgx.Tx, userID uuid.UUID, objectIDs []uuid.UUID) ([]model.StorageObject, error) {
	deleted := make([]model.StorageObject, 0, len(objectIDs))
	seen := map[uuid.UUID]bool{}
	for _, objectID := range objectIDs {
		if objectID == uuid.Nil || seen[objectID] {
			continue
		}
		seen[objectID] = true
		object, err := userStorageObjectForUpdate(ctx, tx, userID, objectID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		var updatedAt, deletedAt time.Time
		if err := tx.QueryRow(ctx, `
			UPDATE storage_objects
			SET deleted_at = now(),
			    updated_at = GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')
			WHERE id = $1 AND deleted_at IS NULL
			RETURNING updated_at, deleted_at
		`, objectID).Scan(&updatedAt, &deletedAt); err != nil {
			if isNoRows(err) {
				continue
			}
			return nil, err // defensive: selected object was live and locked above
		}
		object.UpdatedAt = updatedAt
		object.DeletedAt = &deletedAt
		deleted = append(deleted, object)
	}
	return deleted, nil
}
