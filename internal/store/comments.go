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

type CreateCommentParams struct {
	IssueID  uuid.UUID
	AuthorID uuid.UUID
	Body     string
}

type UpdateCommentParams struct {
	ID       uuid.UUID
	AuthorID uuid.UUID
	Body     string
}

type commentScanner interface {
	Scan(dest ...any) error
}

func scanComment(row commentScanner) (model.Comment, error) {
	var out model.Comment
	err := row.Scan(&out.ID, &out.IssueID, &out.Number, &out.AuthorID, &out.Body, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return model.Comment{}, err
	}
	out.Ref = model.CommentRef(out.Number)
	if out.UpdatedAt.After(out.CreatedAt) {
		editedAt := out.UpdatedAt
		out.EditedAt = &editedAt
	}
	return out, nil
}

func (s *Store) CreateComment(ctx context.Context, p CreateCommentParams) (model.Comment, error) {
	var out model.Comment
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var number int
		if err := tx.QueryRow(ctx, `
			SELECT next_comment_number
			FROM issues
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, p.IssueID).Scan(&number); err != nil {
			if isNoRows(err) {
				return fmt.Errorf("issue not found: %w", ErrNotFound)
			}
			return err
		}
		var err error
		out, err = scanComment(tx.QueryRow(ctx, `
			INSERT INTO comments (issue_id, number, author_id, body)
			VALUES ($1, $2, $3, $4)
			RETURNING id, issue_id, number, author_id, body, created_at, updated_at
		`, p.IssueID, number, p.AuthorID, p.Body))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE issues SET next_comment_number = next_comment_number + 1, updated_at = now()
			WHERE id = $1
		`, p.IssueID)
		return err
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23503":
				return model.Comment{}, fmt.Errorf("issue or author not found: %w", ErrNotFound)
			case "23514":
				return model.Comment{}, fmt.Errorf("body must be 1..10000 chars: %w", ErrConflict)
			}
		}
		// Defensive: all expected constraint failures are mapped above.
		return model.Comment{}, err
	}
	return out, nil
}

func (s *Store) GetComment(ctx context.Context, id uuid.UUID) (model.Comment, error) {
	const q = `
		SELECT id, issue_id, number, author_id, body, created_at, updated_at
		FROM comments WHERE id = $1
	`
	out, err := scanComment(s.db.QueryRow(ctx, q, id))
	if err != nil {
		if isNoRows(err) {
			return model.Comment{}, ErrNotFound
		}
		// Defensive: only no-rows has a domain mapping here.
		return model.Comment{}, err
	}
	return out, nil
}

func (s *Store) GetCommentForIssueByNumber(ctx context.Context, issueID uuid.UUID, number int) (model.Comment, error) {
	const q = `
		SELECT id, issue_id, number, author_id, body, created_at, updated_at
		FROM comments
		WHERE issue_id = $1 AND number = $2
	`
	out, err := scanComment(s.db.QueryRow(ctx, q, issueID, number))
	if err != nil {
		if isNoRows(err) {
			return model.Comment{}, ErrNotFound
		}
		return model.Comment{}, err
	}
	return out, nil
}

type CommentsCursor struct {
	CreatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"i"`
}

type ListCommentsForIssueParams struct {
	IssueID uuid.UUID
	Cursor  *CommentsCursor
	Limit   int
}

func (s *Store) ListCommentsForIssue(ctx context.Context, p ListCommentsForIssueParams) ([]model.Comment, bool, error) {
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
		SELECT c.id, c.issue_id, c.number, c.author_id, c.body, c.created_at, c.updated_at
		FROM comments c
		WHERE c.issue_id = $1
	`
	if p.Cursor != nil {
		args = append(args, p.Cursor.CreatedAt, p.Cursor.ID)
		q += fmt.Sprintf(" AND (c.created_at, c.id) > ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(" ORDER BY c.created_at ASC, c.id ASC LIMIT $%d", len(args))

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		// Defensive: comment list has no expected constraint mapping.
		return nil, false, err
	}
	defer rows.Close()

	out := make([]model.Comment, 0, p.Limit)
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			// Defensive: selected columns match model fields.
			return nil, false, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		// Defensive: scan/query failures after setup are DB/runtime faults.
		return nil, false, err
	}
	hasMore := len(out) > p.Limit
	if hasMore {
		out = out[:p.Limit]
	}
	return out, hasMore, nil
}

func (s *Store) UpdateComment(ctx context.Context, p UpdateCommentParams) (model.Comment, error) {
	const q = `
		WITH existing AS (
			SELECT id, issue_id, number, author_id, body, created_at, updated_at
			FROM comments
			WHERE id = $1 AND author_id = $2
		), updated AS (
			UPDATE comments c
			SET body = $3,
				updated_at = GREATEST(clock_timestamp(), c.created_at + interval '1 microsecond')
			FROM existing e
			WHERE c.id = e.id AND c.body IS DISTINCT FROM $3
			RETURNING c.id, c.issue_id, c.number, c.author_id, c.body, c.created_at, c.updated_at
		)
		SELECT id, issue_id, number, author_id, body, created_at, updated_at FROM updated
		UNION ALL
		SELECT id, issue_id, number, author_id, body, created_at, updated_at
		FROM existing
		WHERE NOT EXISTS (SELECT 1 FROM updated)
	`
	out, err := scanComment(s.db.QueryRow(ctx, q, p.ID, p.AuthorID, p.Body))
	if err != nil {
		if isNoRows(err) {
			return model.Comment{}, ErrNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			return model.Comment{}, fmt.Errorf("body must be 1..10000 chars: %w", ErrConflict)
		}
		// Defensive: all expected update failures are mapped above.
		return model.Comment{}, err
	}
	return out, nil
}

func (s *Store) DeleteComment(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM comments WHERE id = $1`, id)
	if err != nil {
		// Defensive: delete has no expected FK/check mapping.
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
