package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
)

var ErrInvalidBackgroundPreset = fmt.Errorf("background must be indigo, slate, emerald, sky or amber: %w", ErrConflict)

// GetUserBackground returns the user's background preset. A stored value that
// is no longer a preset reads as the default.
func (s *Store) GetUserBackground(ctx context.Context, userID uuid.UUID) (model.BackgroundPreset, error) {
	var background string
	err := s.db.QueryRow(ctx, `SELECT background FROM users WHERE id = $1 AND deleted_at IS NULL`, userID).Scan(&background)
	if err != nil {
		if isNoRows(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	return model.BackgroundPreset(background).OrDefault(), nil
}

func (s *Store) SetUserBackground(ctx context.Context, userID uuid.UUID, background model.BackgroundPreset) error {
	if !background.Valid() {
		return ErrInvalidBackgroundPreset
	}
	tag, err := s.db.Exec(ctx, `UPDATE users SET background = $2 WHERE id = $1 AND deleted_at IS NULL`, userID, string(background))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
