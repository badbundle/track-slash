package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
)

type ProjectStatsParams struct {
	ProjectID uuid.UUID
	Now       time.Time
}

func (s *Store) GetProjectStats(ctx context.Context, p ProjectStatsParams) (model.ProjectStats, error) {
	if _, err := s.GetProject(ctx, p.ProjectID); err != nil {
		return model.ProjectStats{}, err
	}
	now := p.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	cutoff := now.Add(-7 * 24 * time.Hour)

	stats := model.ProjectStats{ProjectID: p.ProjectID}
	const countsQ = `
		SELECT
			COUNT(*)::INT,
			(COUNT(*) FILTER (WHERE i.status = 'todo'))::INT,
			(COUNT(*) FILTER (WHERE i.status = 'in_progress'))::INT,
			(COUNT(*) FILTER (WHERE i.status = 'done'))::INT,
			(COUNT(*) FILTER (WHERE i.status = 'closed'))::INT,
			(COUNT(*) FILTER (WHERE i.created_at >= $2))::INT,
			(COUNT(*) FILTER (WHERE i.created_at >= $2 AND i.status = 'todo'))::INT,
			(COUNT(*) FILTER (WHERE i.created_at >= $2 AND i.status = 'in_progress'))::INT,
			(COUNT(*) FILTER (WHERE i.created_at >= $2 AND i.status = 'done'))::INT,
			(COUNT(*) FILTER (WHERE i.created_at >= $2 AND i.status = 'closed'))::INT
		FROM issues i
		WHERE i.project_id = $1 AND i.deleted_at IS NULL
	`
	err := s.db.QueryRow(ctx, countsQ, p.ProjectID, cutoff).Scan(
		&stats.AllTime.Total,
		&stats.AllTime.Todo,
		&stats.AllTime.InProgress,
		&stats.AllTime.Done,
		&stats.AllTime.Closed,
		&stats.Last7Days.Total,
		&stats.Last7Days.Todo,
		&stats.Last7Days.InProgress,
		&stats.Last7Days.Done,
		&stats.Last7Days.Closed,
	)
	if err != nil {
		return model.ProjectStats{}, err
	}

	const topAssigneesQ = `
		SELECT
			u.id,
			u.username,
			u.name,
			u.profile_image_thumbnail_object_id,
			COUNT(*)::INT,
			(COUNT(*) FILTER (WHERE i.status = 'todo'))::INT,
			(COUNT(*) FILTER (WHERE i.status = 'in_progress'))::INT,
			(COUNT(*) FILTER (WHERE i.status = 'done'))::INT,
			(COUNT(*) FILTER (WHERE i.status = 'closed'))::INT
		FROM issues i
		JOIN users u ON u.id = i.assignee_id
		WHERE i.project_id = $1
		  AND i.deleted_at IS NULL
		  AND u.deleted_at IS NULL
		GROUP BY u.id, u.username, u.name, u.profile_image_thumbnail_object_id
		ORDER BY COUNT(*) DESC, lower(u.name) ASC, lower(u.username) ASC, u.id ASC
		LIMIT 5
	`
	rows, err := s.db.Query(ctx, topAssigneesQ, p.ProjectID)
	if err != nil {
		return model.ProjectStats{}, err
	}
	defer rows.Close()

	stats.TopAssignees = []model.ProjectAssigneeIssueStats{}
	for rows.Next() {
		var assignee model.ProjectAssigneeIssueStats
		var thumbnailID uuid.NullUUID
		if err := rows.Scan(
			&assignee.UserID,
			&assignee.Username,
			&assignee.Name,
			&thumbnailID,
			&assignee.Counts.Total,
			&assignee.Counts.Todo,
			&assignee.Counts.InProgress,
			&assignee.Counts.Done,
			&assignee.Counts.Closed,
		); err != nil {
			return model.ProjectStats{}, err
		}
		if thumbnailID.Valid {
			id := thumbnailID.UUID
			assignee.ProfileImageThumbnailObjectID = &id
		}
		stats.TopAssignees = append(stats.TopAssignees, assignee)
	}
	if err := rows.Err(); err != nil {
		return model.ProjectStats{}, err
	}
	return stats, nil
}
