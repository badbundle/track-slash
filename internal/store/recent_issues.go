package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bradleymackey/track-slash/internal/model"
)

// RecentIssueViewsKept is how many recently viewed issues the store keeps per
// user. It is more than the sidebar shows, so issues that are later deleted or
// become unreadable still leave a full list behind them.
const RecentIssueViewsKept = 50

// RecordIssueView marks issueID as the user's most recently viewed issue and
// trims their history to RecentIssueViewsKept. Callers record a view only after
// checking the user can read the issue.
func (s *Store) RecordIssueView(ctx context.Context, userID, issueID uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO recent_issue_views (user_id, issue_id, viewed_at)
			SELECT u.id, i.id, clock_timestamp()
			FROM users u
			JOIN issues i ON i.id = $2
			WHERE u.id = $1 AND u.deleted_at IS NULL AND i.deleted_at IS NULL
			ON CONFLICT (user_id, issue_id) DO UPDATE SET viewed_at = EXCLUDED.viewed_at
		`, userID, issueID)
		if err != nil {
			// Defensive: both ids were checked by the insert's joins.
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		_, err = tx.Exec(ctx, `
			DELETE FROM recent_issue_views
			WHERE user_id = $1
			  AND issue_id NOT IN (
			      SELECT issue_id
			      FROM recent_issue_views
			      WHERE user_id = $1
			      ORDER BY viewed_at DESC, issue_id
			      LIMIT $2
			  )
		`, userID, RecentIssueViewsKept)
		// Defensive: the trim only fails on a database/runtime fault.
		return err
	})
}

// ListRecentIssues returns the issues the user viewed most recently, newest
// first. It leaves out issues the user can no longer read, using the same rules
// as ProjectPermissionsForUser: site admins and owners read everything, blocked
// users read nothing else, and everyone else reads public projects and the
// projects they are members of. Deleted issues, projects and owners are left
// out too.
func (s *Store) ListRecentIssues(ctx context.Context, user model.User, limit int) ([]model.Issue, error) {
	rows, err := s.db.Query(ctx, `
		SELECT i.id, i.project_id, owner.username, p.key, i.number, i.title, i.description, i.status, i.close_reason, i.priority,
		       i.assignee_id, i.reporter_id, i.sprint_id, i.parent_issue_id, i.due_date, i.created_at, i.updated_at
		FROM recent_issue_views v
		JOIN issues i ON i.id = v.issue_id
		JOIN projects p ON p.id = i.project_id
		JOIN users owner ON owner.id = p.owner_id
		WHERE v.user_id = $1
		  AND i.deleted_at IS NULL AND p.deleted_at IS NULL AND owner.deleted_at IS NULL
		  AND (
		      $2
		      OR p.owner_id = $1
		      OR (
		          NOT EXISTS (
		              SELECT 1 FROM project_user_blocks b
		              WHERE b.project_id = p.id AND b.user_id = $1
		          )
		          AND (
		              p.access_mode IN ('public', 'public_issues')
		              OR EXISTS (
		                  SELECT 1 FROM project_members pm
		                  WHERE pm.project_id = p.id AND pm.user_id = $1
		              )
		          )
		      )
		  )
		ORDER BY v.viewed_at DESC, v.issue_id
		LIMIT $3
	`, user.ID, user.IsAdmin, limit)
	if err != nil {
		// Defensive: query failures require a database/runtime fault.
		return nil, err
	}
	defer rows.Close()
	out := []model.Issue{}
	for rows.Next() {
		issue, err := scanIssue(rows)
		if err != nil {
			// Defensive: selected columns match scanIssue's fields.
			return nil, err
		}
		out = append(out, issue)
	}
	if err := rows.Err(); err != nil {
		// Defensive: post-scan failures require a database/runtime fault.
		return nil, err
	}
	return out, nil
}
