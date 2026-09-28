package store

import (
	"context"

	"github.com/google/uuid"
)

// RealtimeTopicIsPrivate reports whether a realtime topic is about a private
// issue: the issue itself, or a comment, link, context link, tag link,
// issue-scoped context or changelog entry that belongs to or names one. Other
// topics never are.
func (s *Store) RealtimeTopicIsPrivate(ctx context.Context, kind string, id uuid.UUID) (bool, error) {
	var q string
	switch kind {
	case "issue":
		q = `SELECT EXISTS (SELECT 1 FROM issues WHERE id = $1 AND private)`
	case "comment":
		q = `SELECT EXISTS (SELECT 1 FROM comments c JOIN issues i ON i.id = c.issue_id WHERE c.id = $1 AND i.private)`
	case "issue_link":
		q = `SELECT EXISTS (SELECT 1 FROM issue_links l JOIN issues i ON i.id IN (l.source_id, l.target_id) WHERE l.id = $1 AND i.private)`
	case "issue_context_link":
		q = `SELECT EXISTS (SELECT 1 FROM issue_context_links l JOIN issues i ON i.id = l.issue_id WHERE l.id = $1 AND i.private)`
	case "issue_tag_link":
		q = `SELECT EXISTS (SELECT 1 FROM issue_tag_links l JOIN issues i ON i.id = l.issue_id WHERE l.id = $1 AND i.private)`
	case "project_context":
		q = `SELECT EXISTS (
			SELECT 1 FROM project_context pc
			JOIN issue_context_links l ON l.context_id = pc.id
			JOIN issues i ON i.id = l.issue_id
			WHERE pc.id = $1 AND pc.scope = 'issue' AND i.private
		)`
	case "project_changelog":
		q = `SELECT EXISTS (
			SELECT 1 FROM project_changelog_entries e JOIN issues i ON i.id = e.issue_id OR i.id = ANY(e.related_issue_ids)
			WHERE e.id = $1 AND i.private
		)`
	default:
		return false, nil
	}
	var private bool
	if err := s.db.QueryRow(ctx, q, id).Scan(&private); err != nil {
		return false, err // defensive: EXISTS always returns a row
	}
	return private, nil
}
