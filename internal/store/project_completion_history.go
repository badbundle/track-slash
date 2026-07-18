package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/google/uuid"
)

const projectCompletionHistoryWeeks = 12

type ProjectCompletionHistoryParams struct {
	ProjectID uuid.UUID
	Now       time.Time
}

type completionHistoryIssue struct {
	ParentID  *uuid.UUID
	Status    model.Status
	CreatedAt time.Time
	Active    bool
}

type completionHistoryEvent struct {
	IssueID   uuid.UUID
	Op        string
	Details   []byte
	CreatedAt time.Time
}

func (s *Store) GetProjectCompletionHistory(ctx context.Context, p ProjectCompletionHistoryParams) (model.ProjectCompletionHistory, error) {
	if _, err := s.GetProject(ctx, p.ProjectID); err != nil {
		return model.ProjectCompletionHistory{}, err
	}
	now := p.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	currentWeekStart := completionHistoryWeekStart(now)
	start := currentWeekStart.AddDate(0, 0, -7*(projectCompletionHistoryWeeks-1))
	history := model.ProjectCompletionHistory{
		ProjectID: p.ProjectID,
		Start:     start,
		End:       now,
		Points:    make([]model.ProjectCompletionHistoryPoint, projectCompletionHistoryWeeks),
	}
	for i := range history.Points {
		periodStart := start.AddDate(0, 0, 7*i)
		asOf := periodStart.AddDate(0, 0, 7).Add(-time.Nanosecond)
		if asOf.After(now) {
			asOf = now
		}
		history.Points[i] = model.ProjectCompletionHistoryPoint{PeriodStart: periodStart, AsOf: asOf}
	}

	issues, children, err := s.completionHistoryIssues(ctx, p.ProjectID, now)
	if err != nil {
		return model.ProjectCompletionHistory{}, err
	}
	events, err := s.completionHistoryEvents(ctx, p.ProjectID, start, now)
	if err != nil {
		return model.ProjectCompletionHistory{}, err
	}
	eventIndex := 0
	for i := len(history.Points) - 1; i >= 0; i-- {
		point := &history.Points[i]
		for eventIndex < len(events) && events[eventIndex].CreatedAt.After(point.AsOf) {
			if err := reverseCompletionHistoryEvent(issues, children, events[eventIndex]); err != nil {
				return model.ProjectCompletionHistory{}, err
			}
			eventIndex++
		}
		for _, issue := range issues {
			if issue.CreatedAt.After(point.AsOf) || !issue.Active {
				continue
			}
			point.Total++
			if issue.Status.CountsAsDone() {
				point.Completed++
			}
		}
		if point.Total > 0 {
			point.Rate = float64(point.Completed) / float64(point.Total) * 100
		}
	}
	return history, nil
}

func completionHistoryWeekStart(t time.Time) time.Time {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	daysSinceMonday := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -daysSinceMonday)
}

func (s *Store) completionHistoryIssues(ctx context.Context, projectID uuid.UUID, now time.Time) (map[uuid.UUID]*completionHistoryIssue, map[uuid.UUID][]uuid.UUID, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, parent_issue_id, status, created_at, deleted_at
		FROM issues
		WHERE project_id = $1
	`, projectID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	issues := map[uuid.UUID]*completionHistoryIssue{}
	children := map[uuid.UUID][]uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		var issue completionHistoryIssue
		var deletedAt *time.Time
		if err := rows.Scan(&id, &issue.ParentID, &issue.Status, &issue.CreatedAt, &deletedAt); err != nil {
			return nil, nil, err
		}
		issue.CreatedAt = issue.CreatedAt.UTC()
		issue.Active = deletedAt == nil || deletedAt.After(now)
		issues[id] = &issue
		if issue.ParentID != nil {
			children[*issue.ParentID] = append(children[*issue.ParentID], id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return issues, children, nil
}

func (s *Store) completionHistoryEvents(ctx context.Context, projectID uuid.UUID, start, now time.Time) ([]completionHistoryEvent, error) {
	rows, err := s.db.Query(ctx, `
		SELECT entity_id, op, details, created_at
		FROM project_changelog_entries
		WHERE project_id = $1
		  AND entity = 'issue'
		  AND op IN ('update', 'delete', 'restore')
		  AND created_at > $2
		  AND created_at <= $3
		ORDER BY created_at DESC, id DESC
	`, projectID, start, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []completionHistoryEvent
	for rows.Next() {
		var event completionHistoryEvent
		if err := rows.Scan(&event.IssueID, &event.Op, &event.Details, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.CreatedAt = event.CreatedAt.UTC()
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func reverseCompletionHistoryEvent(issues map[uuid.UUID]*completionHistoryIssue, children map[uuid.UUID][]uuid.UUID, event completionHistoryEvent) error {
	issue := issues[event.IssueID]
	if issue == nil {
		return nil
	}
	switch event.Op {
	case "update":
		status, ok, err := completionHistoryPreviousStatus(event.Details)
		if err != nil {
			return err
		}
		if ok {
			issue.Status = status
		}
	case "delete":
		setCompletionHistoryActive(issues, append([]uuid.UUID{event.IssueID}, children[event.IssueID]...), true)
	case "restore":
		ids := append([]uuid.UUID{event.IssueID}, children[event.IssueID]...)
		if issue.ParentID != nil {
			ids = append(ids, *issue.ParentID)
		}
		setCompletionHistoryActive(issues, ids, false)
	}
	return nil
}

func setCompletionHistoryActive(issues map[uuid.UUID]*completionHistoryIssue, ids []uuid.UUID, active bool) {
	for _, id := range ids {
		if issue := issues[id]; issue != nil {
			issue.Active = active
		}
	}
}

func completionHistoryPreviousStatus(raw []byte) (model.Status, bool, error) {
	var details model.ProjectChangelogDetails
	if err := json.Unmarshal(raw, &details); err != nil {
		return "", false, err
	}
	for _, change := range details.Changes {
		if change.Field != "status" {
			continue
		}
		status, ok := completionHistoryStatus(change.From)
		if !ok {
			return "", false, fmt.Errorf("unknown historical issue status %q", change.From)
		}
		return status, true, nil
	}
	return "", false, nil
}

func completionHistoryStatus(label string) (model.Status, bool) {
	switch label {
	case "To do":
		return model.StatusTodo, true
	case "In progress":
		return model.StatusInProgress, true
	case "Done":
		return model.StatusDone, true
	case "Closed":
		return model.StatusClosed, true
	default:
		return "", false
	}
}
