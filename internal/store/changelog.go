package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bradleymackey/track-slash/internal/model"
)

type actorContextKey struct{}

func WithActor(ctx context.Context, actorID uuid.UUID) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actorID)
}

func actorFromContext(ctx context.Context) *uuid.UUID {
	id, ok := ctx.Value(actorContextKey{}).(uuid.UUID)
	if !ok || id == uuid.Nil {
		return nil
	}
	return &id
}

type ProjectChangelogCursor struct {
	CreatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"i"`
}

type ListProjectChangelogParams struct {
	ProjectID uuid.UUID
	Cursor    *ProjectChangelogCursor
	Limit     int
}

type appendProjectChangelogParams struct {
	ProjectID     uuid.UUID
	Entity        string
	Op            string
	EntityID      uuid.UUID
	IssueID       *uuid.UUID
	ParentIssueID *uuid.UUID
	TargetRef     string
	TargetTitle   string
	Summary       string
	Details       model.ProjectChangelogDetails
}

type changelogQueryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type changelogScanner interface {
	Scan(dest ...any) error
}

func scanProjectChangelogEntry(row changelogScanner) (model.ProjectChangelogEntry, error) {
	var out model.ProjectChangelogEntry
	var actorID uuid.NullUUID
	var actorUsername, actorName sql.NullString
	var actorThumbnailID uuid.NullUUID
	var details []byte
	err := row.Scan(
		&out.ID, &out.ProjectID, &actorID, &out.Entity, &out.Op, &out.EntityID, &out.IssueID, &out.ParentIssueID,
		&out.TargetRef, &out.TargetTitle, &out.Summary, &details, &out.Version, &out.CreatedAt,
		&actorUsername, &actorName, &actorThumbnailID,
	)
	if err != nil {
		return model.ProjectChangelogEntry{}, err
	}
	if actorID.Valid {
		id := actorID.UUID
		out.ActorID = &id
		out.Actor = &model.ProjectChangelogActor{
			ID:                            id,
			Username:                      actorUsername.String,
			Name:                          actorName.String,
			ProfileImageThumbnailObjectID: nil,
		}
		if actorThumbnailID.Valid {
			thumbnailID := actorThumbnailID.UUID
			out.Actor.ProfileImageThumbnailObjectID = &thumbnailID
		}
	}
	if len(details) > 0 {
		if err := json.Unmarshal(details, &out.Details); err != nil {
			return model.ProjectChangelogEntry{}, err
		}
	}
	return out, nil
}

func (s *Store) ListProjectChangelog(ctx context.Context, p ListProjectChangelogParams) ([]model.ProjectChangelogEntry, bool, error) {
	if _, err := s.GetProject(ctx, p.ProjectID); err != nil {
		return nil, false, err
	}
	args := []any{p.ProjectID}
	q := `
		SELECT e.id, e.project_id, e.actor_id, e.entity, e.op, e.entity_id, e.issue_id, e.parent_issue_id,
		       e.target_ref, e.target_title, e.summary, e.details, e.version, e.created_at,
		       u.username, u.name, u.profile_image_thumbnail_object_id
		FROM project_changelog_entries e
		LEFT JOIN users u ON u.id = e.actor_id
		WHERE e.project_id = $1
	`
	if p.Cursor != nil {
		args = append(args, p.Cursor.CreatedAt, p.Cursor.ID)
		q += fmt.Sprintf(" AND (e.created_at, e.id) < ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(" ORDER BY e.created_at DESC, e.id DESC LIMIT $%d", len(args))

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := make([]model.ProjectChangelogEntry, 0, p.Limit)
	for rows.Next() {
		entry, err := scanProjectChangelogEntry(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, entry)
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

func appendProjectChangelog(ctx context.Context, q changelogQueryer, p appendProjectChangelogParams) error {
	if p.Summary == "" || p.ProjectID == uuid.Nil || p.EntityID == uuid.Nil {
		return nil
	}
	details, err := json.Marshal(p.Details)
	if err != nil {
		return err
	}
	var id uuid.UUID
	return q.QueryRow(ctx, `
		INSERT INTO project_changelog_entries (
			project_id, actor_id, entity, op, entity_id, issue_id, parent_issue_id,
			target_ref, target_title, summary, details
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
	`, p.ProjectID, actorFromContext(ctx), p.Entity, p.Op, p.EntityID, p.IssueID, p.ParentIssueID, p.TargetRef, p.TargetTitle, p.Summary, details).Scan(&id)
}

func changelogChange(field, label, from, to string) model.ProjectChangelogChange {
	return model.ProjectChangelogChange{Field: field, Label: label, From: from, To: to}
}

func changelogAppendChange(changes []model.ProjectChangelogChange, field, label, from, to string) []model.ProjectChangelogChange {
	if from == to {
		return changes
	}
	return append(changes, changelogChange(field, label, from, to))
}

func changelogPreview(raw string) string {
	preview := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if utf8.RuneCountInString(preview) <= 160 {
		return preview
	}
	runes := []rune(preview)
	return string(runes[:157]) + "..."
}

func changelogIssueSummary(issue model.Issue, action string) string {
	return fmt.Sprintf("%s %s", action, issue.Identifier)
}

func changelogTarget(issue model.Issue) (string, string) {
	return issue.Identifier, issue.Title
}

func changelogStatusLabel(status model.Status) string {
	switch status {
	case model.StatusTodo:
		return "To do"
	case model.StatusInProgress:
		return "In progress"
	case model.StatusDone:
		return "Done"
	case model.StatusClosed:
		return "Closed"
	default:
		return string(status)
	}
}

func changelogCloseReasonLabel(reason *model.IssueCloseReason) string {
	if reason == nil {
		return ""
	}
	switch *reason {
	case model.CloseReasonDuplicate:
		return "Duplicate"
	case model.CloseReasonWontDo:
		return "Won't do"
	case model.CloseReasonInvalid:
		return "Invalid"
	default:
		return string(*reason)
	}
}

func changelogSprintStatusLabel(status model.SprintStatus) string {
	switch status {
	case model.SprintStatusPlanned:
		return "Planned"
	case model.SprintStatusActive:
		return "Active"
	case model.SprintStatusCompleted:
		return "Completed"
	default:
		return string(status)
	}
}

func changelogLinkTypeLabel(linkType model.LinkType) string {
	switch linkType {
	case model.LinkTypeBlocks:
		return "Blocks"
	case model.LinkTypeRelatesTo:
		return "Relates to"
	case model.LinkTypeDuplicates:
		return "Duplicates"
	default:
		return string(linkType)
	}
}

func changelogLinkTitle(source model.Issue, link model.IssueLink, target model.Issue) string {
	return fmt.Sprintf("%s %s %s", source.Identifier, strings.ToLower(changelogLinkTypeLabel(link.LinkType)), target.Identifier)
}

func changelogDateLabel(date *model.Date) string {
	if date == nil {
		return "None"
	}
	return date.String()
}

func changelogUUIDLabel(ctx context.Context, q changelogQueryer, id *uuid.UUID, query string) string {
	if id == nil {
		return "None"
	}
	var label string
	if err := q.QueryRow(ctx, query, *id).Scan(&label); err != nil || label == "" {
		return "Changed"
	}
	return label
}

func changelogUserLabel(ctx context.Context, q changelogQueryer, id *uuid.UUID) string {
	return changelogUUIDLabel(ctx, q, id, `SELECT '@' || username FROM users WHERE id = $1`)
}

func changelogSprintLabel(ctx context.Context, q changelogQueryer, id *uuid.UUID) string {
	return changelogUUIDLabel(ctx, q, id, `SELECT name FROM sprints WHERE id = $1`)
}

func changelogSprintRef(sprint model.Sprint) string {
	if sprint.Ref != "" {
		return sprint.Ref
	}
	return model.SprintRef(sprint.Number)
}

func changelogContextRef(contextItem model.ProjectContext) string {
	if contextItem.Ref != "" {
		return contextItem.Ref
	}
	return model.ProjectContextRef(contextItem.Number)
}

func changelogTagRef(tag model.IssueTag) string {
	if tag.Ref != "" {
		return tag.Ref
	}
	return model.IssueTagRef(tag.Number)
}

func changelogLinkRef(link model.IssueLink) string {
	if link.Ref != "" {
		return link.Ref
	}
	return model.IssueLinkRef(link.Number)
}
