package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bradleymackey/track-slash/internal/model"
)

type CreateIssueTagParams struct {
	ProjectID uuid.UUID
	Name      string
	Color     model.IssueTagColor
}

type UpdateIssueTagParams struct {
	ID    uuid.UUID
	Name  *string
	Color *model.IssueTagColor
}

type IssueTagsCursor struct {
	Number int `json:"n"`
}

type ListIssueTagsParams struct {
	ProjectID uuid.UUID
	Cursor    *IssueTagsCursor
	Limit     int
}

type ListTagsForIssueParams struct {
	IssueID uuid.UUID
	Cursor  *IssueTagsCursor
	Limit   int
}

type CreateIssueTagLinkParams struct {
	IssueID uuid.UUID
	TagID   uuid.UUID
}

type issueTagScanner interface {
	Scan(dest ...any) error
}

func scanIssueTag(row issueTagScanner) (model.IssueTag, error) {
	var out model.IssueTag
	if err := row.Scan(&out.ID, &out.ProjectID, &out.Number, &out.Name, &out.Color, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return model.IssueTag{}, err
	}
	out.Ref = model.IssueTagRef(out.Number)
	out.DisplayName = model.IssueTagDisplayName(out.Name)
	return out, nil
}

func scanIssueTagLink(row issueTagScanner) (model.IssueTagLink, error) {
	var out model.IssueTagLink
	err := row.Scan(&out.ID, &out.ProjectID, &out.IssueID, &out.TagID, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func normalizeIssueTagNameForStore(raw string) (string, error) {
	name, err := model.NormalizeIssueTagName(raw)
	if err != nil {
		return "", fmt.Errorf("%s: %w", err.Error(), ErrConflict)
	}
	return name, nil
}

func issueTagColorForStore(color model.IssueTagColor) (model.IssueTagColor, error) {
	color = model.IssueTagColorOrDefault(color)
	if !color.Valid() {
		return "", fmt.Errorf("invalid tag color: %w", ErrConflict)
	}
	return color, nil
}

func (s *Store) CreateIssueTag(ctx context.Context, p CreateIssueTagParams) (model.IssueTag, error) {
	name, err := normalizeIssueTagNameForStore(p.Name)
	if err != nil {
		return model.IssueTag{}, err
	}
	color, err := issueTagColorForStore(p.Color)
	if err != nil {
		return model.IssueTag{}, err
	}

	var out model.IssueTag
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var number int
		if err := tx.QueryRow(ctx, `
			SELECT next_tag_number
			FROM projects
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, p.ProjectID).Scan(&number); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: DB outage past no-rows branch
		}

		out, err = scanIssueTag(tx.QueryRow(ctx, `
			INSERT INTO issue_tags (project_id, number, name, color)
			VALUES ($1, $2, $3, $4)
			RETURNING id, project_id, number, name, color, created_at, updated_at
		`, p.ProjectID, number, name, string(color)))
		if err != nil {
			if mapped := mapIssueTagWriteError(err); mapped != nil {
				return mapped
			}
			return err // defensive: non-pg or unmapped pg error
		}

		if _, err := tx.Exec(ctx, `
			UPDATE projects
			SET next_tag_number = next_tag_number + 1,
			    updated_at = now()
			WHERE id = $1
		`, p.ProjectID); err != nil {
			return err // defensive: project row was locked above
		}
		return nil
	})
	if err != nil {
		return model.IssueTag{}, err
	}
	return out, nil
}

func (s *Store) UpdateIssueTag(ctx context.Context, p UpdateIssueTagParams) (model.IssueTag, error) {
	sets := []string{}
	args := []any{}
	i := 1
	if p.Name != nil {
		name, err := normalizeIssueTagNameForStore(*p.Name)
		if err != nil {
			return model.IssueTag{}, err
		}
		sets = append(sets, fmt.Sprintf("name = $%d", i))
		args = append(args, name)
		i++
	}
	if p.Color != nil {
		color, err := issueTagColorForStore(*p.Color)
		if err != nil {
			return model.IssueTag{}, err
		}
		sets = append(sets, fmt.Sprintf("color = $%d", i))
		args = append(args, string(color))
		i++
	}
	if len(sets) == 0 {
		return s.GetIssueTag(ctx, p.ID)
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, p.ID)
	q := fmt.Sprintf(`
		UPDATE issue_tags SET %s WHERE id = $%d
		RETURNING id, project_id, number, name, color, created_at, updated_at
	`, strings.Join(sets, ", "), i)

	out, err := scanIssueTag(s.db.QueryRow(ctx, q, args...))
	if err != nil {
		if isNoRows(err) {
			return model.IssueTag{}, ErrNotFound
		}
		if mapped := mapIssueTagWriteError(err); mapped != nil {
			return model.IssueTag{}, mapped
		}
		return model.IssueTag{}, err
	}
	return out, nil
}

func (s *Store) DeleteIssueTag(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM issue_tags WHERE id = $1`, id)
	if err != nil {
		return err // defensive: delete cascades have no expected domain mapping
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetIssueTag(ctx context.Context, id uuid.UUID) (model.IssueTag, error) {
	const q = `
		SELECT t.id, t.project_id, t.number, t.name, t.color, t.created_at, t.updated_at
		FROM issue_tags t
		JOIN projects p ON p.id = t.project_id
		WHERE t.id = $1 AND p.deleted_at IS NULL
	`
	out, err := scanIssueTag(s.db.QueryRow(ctx, q, id))
	if err != nil {
		if isNoRows(err) {
			return model.IssueTag{}, ErrNotFound
		}
		return model.IssueTag{}, err
	}
	return out, nil
}

func (s *Store) GetIssueTagByProjectNumber(ctx context.Context, projectID uuid.UUID, number int) (model.IssueTag, error) {
	const q = `
		SELECT t.id, t.project_id, t.number, t.name, t.color, t.created_at, t.updated_at
		FROM issue_tags t
		JOIN projects p ON p.id = t.project_id
		WHERE t.project_id = $1 AND t.number = $2 AND p.deleted_at IS NULL
	`
	out, err := scanIssueTag(s.db.QueryRow(ctx, q, projectID, number))
	if err != nil {
		if isNoRows(err) {
			return model.IssueTag{}, ErrNotFound
		}
		return model.IssueTag{}, err
	}
	return out, nil
}

func (s *Store) GetIssueTagByProjectName(ctx context.Context, projectID uuid.UUID, rawName string) (model.IssueTag, error) {
	name, err := normalizeIssueTagNameForStore(rawName)
	if err != nil {
		return model.IssueTag{}, err
	}
	const q = `
		SELECT t.id, t.project_id, t.number, t.name, t.color, t.created_at, t.updated_at
		FROM issue_tags t
		JOIN projects p ON p.id = t.project_id
		WHERE t.project_id = $1 AND t.name = $2 AND p.deleted_at IS NULL
	`
	out, err := scanIssueTag(s.db.QueryRow(ctx, q, projectID, name))
	if err != nil {
		if isNoRows(err) {
			return model.IssueTag{}, ErrNotFound
		}
		return model.IssueTag{}, err
	}
	return out, nil
}

func (s *Store) ListIssueTags(ctx context.Context, p ListIssueTagsParams) ([]model.IssueTag, bool, error) {
	if _, err := s.GetProject(ctx, p.ProjectID); err != nil {
		return nil, false, err
	}
	args := []any{p.ProjectID}
	q := `
		SELECT t.id, t.project_id, t.number, t.name, t.color, t.created_at, t.updated_at
		FROM issue_tags t
		JOIN projects p ON p.id = t.project_id
		WHERE t.project_id = $1 AND p.deleted_at IS NULL
	`
	if p.Cursor != nil {
		args = append(args, p.Cursor.Number)
		q += fmt.Sprintf(" AND t.number > $%d", len(args))
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(" ORDER BY t.number ASC LIMIT $%d", len(args))
	return scanIssueTagRows(ctx, s, q, args, p.Limit)
}

func (s *Store) ListTagsForIssue(ctx context.Context, p ListTagsForIssueParams) ([]model.IssueTag, bool, error) {
	if _, err := s.ProjectIDForIssue(ctx, p.IssueID); err != nil {
		return nil, false, err
	}
	args := []any{p.IssueID}
	q := `
		SELECT t.id, t.project_id, t.number, t.name, t.color, t.created_at, t.updated_at
		FROM issue_tag_links l
		JOIN issue_tags t ON t.id = l.tag_id
		JOIN projects p ON p.id = t.project_id
		WHERE l.issue_id = $1 AND p.deleted_at IS NULL
	`
	if p.Cursor != nil {
		args = append(args, p.Cursor.Number)
		q += fmt.Sprintf(" AND t.number > $%d", len(args))
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(" ORDER BY t.number ASC LIMIT $%d", len(args))
	return scanIssueTagRows(ctx, s, q, args, p.Limit)
}

func scanIssueTagRows(ctx context.Context, s *Store, q string, args []any, limit int) ([]model.IssueTag, bool, error) {
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := make([]model.IssueTag, 0, limit)
	for rows.Next() {
		tag, err := scanIssueTag(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

func (s *Store) CreateIssueTagLink(ctx context.Context, p CreateIssueTagLinkParams) (model.IssueTagLink, error) {
	var out model.IssueTagLink
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var issueProject uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT project_id
			FROM issues
			WHERE id = $1 AND deleted_at IS NULL
		`, p.IssueID).Scan(&issueProject); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: DB outage past no-rows branch
		}

		var tagProject uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT project_id
			FROM issue_tags
			WHERE id = $1
		`, p.TagID).Scan(&tagProject); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: DB outage past no-rows branch
		}
		if issueProject != tagProject {
			return fmt.Errorf("issue and tag belong to different projects: %w", ErrConflict)
		}

		var err error
		out, err = scanIssueTagLink(tx.QueryRow(ctx, `
			INSERT INTO issue_tag_links (project_id, issue_id, tag_id)
			VALUES ($1, $2, $3)
			RETURNING id, project_id, issue_id, tag_id, created_at, updated_at
		`, issueProject, p.IssueID, p.TagID))
		if err != nil {
			if mapped := mapIssueTagLinkWriteError(err); mapped != nil {
				return mapped
			}
			return err // defensive: non-pg or unmapped pg error
		}
		return nil
	})
	if err != nil {
		return model.IssueTagLink{}, err
	}
	return out, nil
}

func (s *Store) DeleteIssueTagLink(ctx context.Context, issueID, tagID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM issue_tag_links WHERE issue_id = $1 AND tag_id = $2`, issueID, tagID)
	if err != nil {
		return err // defensive: delete has no expected domain mapping
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) issueTagsForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID][]model.IssueTag, error) {
	out := make(map[uuid.UUID][]model.IssueTag, len(issueIDs))
	if len(issueIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT l.issue_id, t.id, t.project_id, t.number, t.name, t.color, t.created_at, t.updated_at
		FROM issue_tag_links l
		JOIN issue_tags t ON t.id = l.tag_id
		WHERE l.issue_id = ANY($1)
		ORDER BY t.number ASC
	`, issueIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var issueID uuid.UUID
		var tag model.IssueTag
		if err := rows.Scan(&issueID, &tag.ID, &tag.ProjectID, &tag.Number, &tag.Name, &tag.Color, &tag.CreatedAt, &tag.UpdatedAt); err != nil {
			return nil, err
		}
		tag.Ref = model.IssueTagRef(tag.Number)
		tag.DisplayName = model.IssueTagDisplayName(tag.Name)
		out[issueID] = append(out[issueID], tag)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) hydrateIssueTags(ctx context.Context, issues []model.Issue) ([]model.Issue, error) {
	if len(issues) == 0 {
		return issues, nil
	}
	ids := make([]uuid.UUID, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}
	tagsByIssue, err := s.issueTagsForIssues(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range issues {
		issues[i].Tags = tagsByIssue[issues[i].ID]
		if issues[i].Tags == nil {
			issues[i].Tags = []model.IssueTag{}
		}
	}
	return issues, nil
}

func (s *Store) hydrateIssueTagsOne(ctx context.Context, issue model.Issue) (model.Issue, error) {
	issues, err := s.hydrateIssueTags(ctx, []model.Issue{issue})
	if err != nil {
		return model.Issue{}, err
	}
	return issues[0], nil
}

func normalizeIssueTagFilters(raws []string) ([]string, error) {
	out := make([]string, 0, len(raws))
	seen := map[string]struct{}{}
	for _, raw := range raws {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		name, err := model.NormalizeIssueTagName(raw)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out, nil
}

func mapIssueTagWriteError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	switch pgErr.Code {
	case "23505":
		return fmt.Errorf("tag already exists: %w", ErrConflict)
	case "23503":
		return fmt.Errorf("project not found: %w", ErrNotFound)
	case "23514", "22P02":
		return fmt.Errorf("invalid tag: %w", ErrConflict)
	}
	return nil
}

func mapIssueTagLinkWriteError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	switch pgErr.Code {
	case "23505":
		return fmt.Errorf("tag already attached: %w", ErrConflict)
	case "23503":
		return fmt.Errorf("invalid issue or tag reference: %w", ErrConflict)
	}
	return nil
}
