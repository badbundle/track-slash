package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bradleymackey/track-slash/internal/model"
)

type CreateIssueLinkParams struct {
	SourceID uuid.UUID
	TargetID uuid.UUID
	LinkType model.LinkType
}

// CreateIssueLink inserts a typed link between two issues in the same project.
// A 'duplicates' link atomically closes the source issue (status=done) so the
// canonical JIRA "this is a dup of X" flow is one round trip.
func (s *Store) CreateIssueLink(ctx context.Context, p CreateIssueLinkParams) (model.IssueLink, error) {
	var out model.IssueLink
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			sourceProject uuid.UUID
			sourceStatus  model.Status
		)
		err := tx.QueryRow(ctx, `
			SELECT project_id, status FROM issues WHERE id = $1 FOR UPDATE
		`, p.SourceID).Scan(&sourceProject, &sourceStatus)
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: DB outage past the no-rows branch
		}

		var targetProject uuid.UUID
		err = tx.QueryRow(ctx, `SELECT project_id FROM issues WHERE id = $1`, p.TargetID).Scan(&targetProject)
		if err != nil {
			if isNoRows(err) {
				return fmt.Errorf("target issue not found: %w", ErrConflict)
			}
			return err // defensive: DB outage past the no-rows branch
		}
		if sourceProject != targetProject {
			return fmt.Errorf("issues belong to different projects: %w", ErrConflict)
		}

		err = tx.QueryRow(ctx, `
			INSERT INTO issue_links (project_id, source_id, target_id, link_type)
			VALUES ($1, $2, $3, $4)
			RETURNING id, project_id, source_id, target_id, link_type, created_at, updated_at
		`, sourceProject, p.SourceID, p.TargetID, string(p.LinkType)).Scan(
			&out.ID, &out.ProjectID, &out.SourceID, &out.TargetID, &out.LinkType,
			&out.CreatedAt, &out.UpdatedAt,
		)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				switch pgErr.Code {
				case "23505":
					return fmt.Errorf("link already exists: %w", ErrConflict)
				case "23514":
					return fmt.Errorf("cannot link issue to itself: %w", ErrConflict)
				case "23503":
					// Defensive: source/target/project FKs all verified above; only
					// reachable on a concurrent issue/project delete between the
					// FOR UPDATE select and the insert.
					return fmt.Errorf("invalid issue reference: %w", ErrConflict)
				}
			}
			return err // defensive: non-pg or unmapped pg error
		}

		if p.LinkType == model.LinkTypeDuplicates && sourceStatus != model.StatusDone {
			if _, err := tx.Exec(ctx, `
				UPDATE issues SET status = 'done', updated_at = now() WHERE id = $1
			`, p.SourceID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return model.IssueLink{}, err
	}
	return out, nil
}

func (s *Store) GetIssueLink(ctx context.Context, id uuid.UUID) (model.IssueLink, error) {
	const q = `
		SELECT id, project_id, source_id, target_id, link_type, created_at, updated_at
		FROM issue_links WHERE id = $1
	`
	var out model.IssueLink
	err := s.db.QueryRow(ctx, q, id).Scan(
		&out.ID, &out.ProjectID, &out.SourceID, &out.TargetID, &out.LinkType,
		&out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		if isNoRows(err) {
			return model.IssueLink{}, ErrNotFound
		}
		return model.IssueLink{}, err
	}
	return out, nil
}

// ListIssueLinksForIssue returns all links touching the given issue id, both
// outgoing (source_id = id) and incoming (target_id = id). The HTTP layer
// derives direction-aware display names.
func (s *Store) ListIssueLinksForIssue(ctx context.Context, issueID uuid.UUID) ([]model.IssueLink, error) {
	const q = `
		SELECT id, project_id, source_id, target_id, link_type, created_at, updated_at
		FROM issue_links
		WHERE source_id = $1 OR target_id = $1
		ORDER BY created_at ASC
	`
	rows, err := s.db.Query(ctx, q, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.IssueLink{}
	for rows.Next() {
		var l model.IssueLink
		if err := rows.Scan(
			&l.ID, &l.ProjectID, &l.SourceID, &l.TargetID, &l.LinkType,
			&l.CreatedAt, &l.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) DeleteIssueLink(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM issue_links WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
