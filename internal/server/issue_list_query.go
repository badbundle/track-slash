package server

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

type issueListQueryOptions struct {
	DefaultSort      store.ListIssuesSort
	IncludeAssignees bool
	AllowNumberSort  bool
}

type issueListQuery struct {
	Statuses    []model.Status
	Priorities  []model.IssuePriority
	TagNames    []string
	AssigneeIDs []uuid.UUID
	Sort        store.ListIssuesSort
	Direction   store.ListIssuesSortDirection
}

func parseIssueListQueryValues(values url.Values, opts issueListQueryOptions) (issueListQuery, error) {
	statuses, err := parseIssueStatusFilters(values["status"])
	if err != nil {
		return issueListQuery{}, err
	}
	priorities, err := parseIssuePriorityFilters(values["priority"])
	if err != nil {
		return issueListQuery{}, err
	}
	tags, err := parseIssueTagNames(values["tag"])
	if err != nil {
		return issueListQuery{}, err
	}
	sortBy, err := parseIssueListSort(values.Get("sort"), opts.DefaultSort, opts.AllowNumberSort)
	if err != nil {
		return issueListQuery{}, err
	}
	direction, err := parseIssueListSortDirection(values.Get("direction"), sortBy)
	if err != nil {
		return issueListQuery{}, err
	}
	var assigneeIDs []uuid.UUID
	if opts.IncludeAssignees {
		assigneeIDs, err = parseIssueAssigneeIDs(values["assignee_id"])
		if err != nil {
			return issueListQuery{}, err
		}
	}
	return issueListQuery{
		Statuses:    statuses,
		Priorities:  priorities,
		TagNames:    tags,
		AssigneeIDs: assigneeIDs,
		Sort:        sortBy,
		Direction:   direction,
	}, nil
}

func parseIssueStatusFilters(raws []string) ([]model.Status, error) {
	statuses := make([]model.Status, 0, len(raws))
	seen := make(map[model.Status]struct{}, len(raws))
	for _, raw := range raws {
		status := model.Status(strings.TrimSpace(raw))
		if status == "" {
			continue
		}
		if !status.Valid() {
			return nil, fmt.Errorf("invalid status")
		}
		if _, ok := seen[status]; ok {
			continue
		}
		seen[status] = struct{}{}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func parseIssuePriorityFilters(raws []string) ([]model.IssuePriority, error) {
	priorities := make([]model.IssuePriority, 0, len(raws))
	seen := make(map[model.IssuePriority]struct{}, len(raws))
	for _, raw := range raws {
		priority := model.IssuePriority(strings.TrimSpace(raw))
		if priority == "" {
			continue
		}
		if !priority.Valid() {
			return nil, fmt.Errorf("invalid priority")
		}
		if _, ok := seen[priority]; ok {
			continue
		}
		seen[priority] = struct{}{}
		priorities = append(priorities, priority)
	}
	return priorities, nil
}

func parseIssueTagNames(raws []string) ([]string, error) {
	tags := make([]string, 0, len(raws))
	seen := make(map[string]struct{}, len(raws))
	for _, raw := range raws {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		name, err := model.NormalizeIssueTagName(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid tag: %w", err)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		tags = append(tags, name)
	}
	return tags, nil
}

func parseIssueAssigneeIDs(raws []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(raws))
	seen := make(map[uuid.UUID]struct{}, len(raws))
	for _, raw := range raws {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("invalid assignee_id")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

func parseIssueListSort(raw string, defaultSort store.ListIssuesSort, allowNumber bool) (store.ListIssuesSort, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultSort, nil
	}
	if allowNumber && raw == "number" {
		return store.ListIssuesSortNumber, nil
	}
	sortBy := store.ListIssuesSort(raw)
	switch sortBy {
	case store.ListIssuesSortCreated, store.ListIssuesSortUpdated, store.ListIssuesSortStatus, store.ListIssuesSortPriority, store.ListIssuesSortDueDate:
		return sortBy, nil
	default:
		return "", fmt.Errorf("invalid sort")
	}
}

func parseIssueListSortDirection(raw string, sortBy store.ListIssuesSort) (store.ListIssuesSortDirection, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultIssueListSortDirection(sortBy), nil
	}
	direction := store.ListIssuesSortDirection(raw)
	switch direction {
	case store.ListIssuesSortAscending, store.ListIssuesSortDescending:
		return direction, nil
	default:
		return "", fmt.Errorf("invalid direction")
	}
}

func defaultIssueListSortDirection(sortBy store.ListIssuesSort) store.ListIssuesSortDirection {
	switch sortBy {
	case store.ListIssuesSortCreated, store.ListIssuesSortUpdated:
		return store.ListIssuesSortDescending
	default:
		return store.ListIssuesSortAscending
	}
}

func issueListCursor(issue model.Issue, sortBy store.ListIssuesSort) store.IssuesCursor {
	cursor := store.IssuesCursor{Number: issue.Number}
	switch sortBy {
	case store.ListIssuesSortCreated:
		cursor.CreatedAt = issue.CreatedAt
	case store.ListIssuesSortUpdated:
		cursor.UpdatedAt = issue.UpdatedAt
	case store.ListIssuesSortStatus:
		cursor.Status = issue.Status
	case store.ListIssuesSortPriority:
		cursor.Priority = issue.Priority
	case store.ListIssuesSortDueDate:
		cursor.DueDate = issue.DueDate
	}
	return cursor
}
