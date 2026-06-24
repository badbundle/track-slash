package store

import (
	"context"
	"errors"
	"fmt"
	"time"

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

type UpdateIssueLinkParams struct {
	SourceID uuid.UUID
	TargetID uuid.UUID
	LinkType model.LinkType
}

type issueLinkScanner interface {
	Scan(dest ...any) error
}

func scanIssueLink(row issueLinkScanner) (model.IssueLink, error) {
	var out model.IssueLink
	err := row.Scan(&out.ID, &out.ProjectID, &out.Number, &out.SourceID, &out.TargetID, &out.LinkType, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return model.IssueLink{}, err
	}
	out.Ref = model.IssueLinkRef(out.Number)
	return out, nil
}

// CreateIssueLink inserts a typed link between two issues in the same project.
// A 'duplicates' link atomically closes the source issue (status=closed) so the
// canonical JIRA "this is a dup of X" flow is one round trip.
func (s *Store) CreateIssueLink(ctx context.Context, p CreateIssueLinkParams) (model.IssueLink, error) {
	var out model.IssueLink
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			sourceProject uuid.UUID
			sourceStatus  model.Status
			number        int
		)
		err := tx.QueryRow(ctx, `
			SELECT i.project_id, i.status, pr.next_issue_link_number
			FROM issues i
			JOIN projects pr ON pr.id = i.project_id
			WHERE i.id = $1 AND i.deleted_at IS NULL AND pr.deleted_at IS NULL
			FOR UPDATE OF i, pr
		`, p.SourceID).Scan(&sourceProject, &sourceStatus, &number)
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: DB outage past the no-rows branch
		}

		var targetProject uuid.UUID
		err = tx.QueryRow(ctx, `SELECT project_id FROM issues WHERE id = $1 AND deleted_at IS NULL`, p.TargetID).Scan(&targetProject)
		if err != nil {
			if isNoRows(err) {
				return fmt.Errorf("target issue not found: %w", ErrConflict)
			}
			return err // defensive: DB outage past the no-rows branch
		}
		if sourceProject != targetProject {
			return fmt.Errorf("issues belong to different projects: %w", ErrConflict)
		}

		out, err = scanIssueLink(tx.QueryRow(ctx, `
			INSERT INTO issue_links (project_id, number, source_id, target_id, link_type)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id, project_id, number, source_id, target_id, link_type, created_at, updated_at
		`, sourceProject, number, p.SourceID, p.TargetID, string(p.LinkType)))
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

		if _, err := tx.Exec(ctx, `
			UPDATE projects SET next_issue_link_number = next_issue_link_number + 1, updated_at = now()
			WHERE id = $1
		`, sourceProject); err != nil {
			return err
		}

		if p.LinkType == model.LinkTypeDuplicates && !sourceStatus.CountsAsDone() {
			if _, err := tx.Exec(ctx, `
				UPDATE issues SET status = 'closed', updated_at = now() WHERE id = $1
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

// UpdateIssueLink rewires an existing link within its project. If the edited
// relationship becomes 'duplicates', the new source issue is closed just like
// the create path.
func (s *Store) UpdateIssueLink(ctx context.Context, id uuid.UUID, p UpdateIssueLinkParams) (model.IssueLink, error) {
	var out model.IssueLink
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var linkProject uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT il.project_id
			FROM issue_links il
			JOIN projects pr ON pr.id = il.project_id
			WHERE il.id = $1 AND pr.deleted_at IS NULL
			FOR UPDATE OF il
		`, id).Scan(&linkProject); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: DB outage past the no-rows branch
		}

		var (
			sourceProject uuid.UUID
			sourceStatus  model.Status
		)
		if err := tx.QueryRow(ctx, `
			SELECT i.project_id, i.status
			FROM issues i
			JOIN projects pr ON pr.id = i.project_id
			WHERE i.id = $1 AND i.deleted_at IS NULL AND pr.deleted_at IS NULL
			FOR UPDATE OF i
		`, p.SourceID).Scan(&sourceProject, &sourceStatus); err != nil {
			if isNoRows(err) {
				return fmt.Errorf("source issue not found: %w", ErrConflict)
			}
			return err // defensive: DB outage past the no-rows branch
		}

		var targetProject uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT i.project_id
			FROM issues i
			JOIN projects pr ON pr.id = i.project_id
			WHERE i.id = $1 AND i.deleted_at IS NULL AND pr.deleted_at IS NULL
		`, p.TargetID).Scan(&targetProject); err != nil {
			if isNoRows(err) {
				return fmt.Errorf("target issue not found: %w", ErrConflict)
			}
			return err // defensive: DB outage past the no-rows branch
		}
		if sourceProject != linkProject || targetProject != linkProject {
			return fmt.Errorf("issues belong to different projects: %w", ErrConflict)
		}

		var err error
		out, err = scanIssueLink(tx.QueryRow(ctx, `
			UPDATE issue_links
			SET source_id = $2, target_id = $3, link_type = $4, updated_at = now()
			WHERE id = $1
			RETURNING id, project_id, number, source_id, target_id, link_type, created_at, updated_at
		`, id, p.SourceID, p.TargetID, string(p.LinkType)))
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
					// validation selects and the update.
					return fmt.Errorf("invalid issue reference: %w", ErrConflict)
				}
			}
			return err // defensive: non-pg or unmapped pg error
		}

		if p.LinkType == model.LinkTypeDuplicates && !sourceStatus.CountsAsDone() {
			if _, err := tx.Exec(ctx, `
				UPDATE issues SET status = 'closed', updated_at = now() WHERE id = $1
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
		SELECT id, project_id, number, source_id, target_id, link_type, created_at, updated_at
		FROM issue_links WHERE id = $1
	`
	out, err := scanIssueLink(s.db.QueryRow(ctx, q, id))
	if err != nil {
		if isNoRows(err) {
			return model.IssueLink{}, ErrNotFound
		}
		return model.IssueLink{}, err
	}
	return out, nil
}

func (s *Store) GetIssueLinkByProjectNumber(ctx context.Context, projectID uuid.UUID, number int) (model.IssueLink, error) {
	const q = `
		SELECT id, project_id, number, source_id, target_id, link_type, created_at, updated_at
		FROM issue_links
		WHERE project_id = $1 AND number = $2
	`
	out, err := scanIssueLink(s.db.QueryRow(ctx, q, projectID, number))
	if err != nil {
		if isNoRows(err) {
			return model.IssueLink{}, ErrNotFound
		}
		return model.IssueLink{}, err
	}
	return out, nil
}

// IssueLinksCursor keys off (created_at, id) — created_at alone can tie under
// rapid bulk inserts so id is the deterministic tiebreaker.
type IssueLinksCursor struct {
	CreatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"i"`
}

type ListIssueLinksForIssueParams struct {
	IssueID uuid.UUID
	Cursor  *IssueLinksCursor
	Limit   int
}

// ListIssueLinksForIssue returns links touching the given issue id, both
// outgoing (source_id = id) and incoming (target_id = id). The HTTP layer
// derives direction-aware display names.
func (s *Store) ListIssueLinksForIssue(ctx context.Context, p ListIssueLinksForIssueParams) ([]model.IssueLink, bool, error) {
	var issueID uuid.UUID
	if err := s.db.QueryRow(ctx, `SELECT id FROM issues WHERE id = $1 AND deleted_at IS NULL`, p.IssueID).Scan(&issueID); err != nil {
		if isNoRows(err) {
			return nil, false, ErrNotFound
		}
		// Defensive: only no-rows has a domain mapping here.
		return nil, false, err
	}

	args := []any{p.IssueID}
	q := `
		SELECT id, project_id, number, source_id, target_id, link_type, created_at, updated_at
		FROM issue_links
		WHERE (source_id = $1 OR target_id = $1)
	`
	if p.Cursor != nil {
		args = append(args, p.Cursor.CreatedAt, p.Cursor.ID)
		q += fmt.Sprintf(" AND (created_at, id) > ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(" ORDER BY created_at ASC, id ASC LIMIT $%d", len(args))

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := make([]model.IssueLink, 0, p.Limit)
	for rows.Next() {
		l, err := scanIssueLink(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, l)
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
