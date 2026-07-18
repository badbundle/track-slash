package store

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func normalizePreviewTermsVersion(raw string) (string, error) {
	version := strings.TrimSpace(raw)
	if version == "" {
		return "", nil
	}
	if len(version) > 64 {
		return "", errors.New("preview terms version must be at most 64 chars")
	}
	return version, nil
}

func recordPreviewTermsAcceptance(ctx context.Context, tx pgx.Tx, userID uuid.UUID, version string) error {
	if version == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO preview_terms_acceptances (user_id, terms_version)
		VALUES ($1, $2)
	`, userID, version)
	return err
}
