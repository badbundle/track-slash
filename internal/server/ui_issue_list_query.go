package server

import (
	"fmt"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/google/uuid"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

func uiParseIssueListQuery(r *http.Request) (uiIssueListQuery, error) {
	statuses, err := uiParseProjectIssueStatusFilters(r)
	if err != nil {
		return uiIssueListQuery{}, err
	}
	priorities, err := uiParseProjectIssuePriorityFilters(r)
	if err != nil {
		return uiIssueListQuery{}, err
	}
	tagNames, err := uiParseIssueTagFilters(r)
	if err != nil {
		return uiIssueListQuery{}, err
	}
	sort, err := uiParseProjectIssueSort(r)
	if err != nil {
		return uiIssueListQuery{}, err
	}
	direction, err := uiParseProjectIssueSortDirection(r, sort)
	if err != nil {
		return uiIssueListQuery{}, err
	}
	return uiIssueListQuery{
		Statuses:   statuses,
		Priorities: priorities,
		TagNames:   tagNames,
		Sort:       sort,
		Direction:  direction,
	}, nil
}

func uiParseProjectAllQuery(r *http.Request) (uiProjectAllQuery, error) {
	assigneeIDs, err := uiParseAssigneeFilter(r)
	if err != nil {
		return uiProjectAllQuery{}, err
	}
	query, err := uiParseIssueListQuery(r)
	if err != nil {
		return uiProjectAllQuery{}, err
	}
	query.AssigneeIDs = assigneeIDs
	return query, nil
}

func uiParseProjectIssueStatusFilters(r *http.Request) ([]model.Status, error) {
	raws := r.URL.Query()["status"]
	statuses := make([]model.Status, 0, len(raws))
	seen := make(map[model.Status]struct{}, len(raws))
	for _, raw := range raws {
		status := model.Status(strings.TrimSpace(raw))
		if status == "" {
			continue
		}
		if !status.Valid() {
			return nil, fmt.Errorf("invalid status: %w", errUIBadRequest)
		}
		if _, ok := seen[status]; ok {
			continue
		}
		seen[status] = struct{}{}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func uiParseProjectIssuePriorityFilters(r *http.Request) ([]model.IssuePriority, error) {
	raws := r.URL.Query()["priority"]
	priorities := make([]model.IssuePriority, 0, len(raws))
	seen := make(map[model.IssuePriority]struct{}, len(raws))
	for _, raw := range raws {
		priority := model.IssuePriority(strings.TrimSpace(raw))
		if priority == "" {
			continue
		}
		if !priority.Valid() {
			return nil, fmt.Errorf("invalid priority: %w", errUIBadRequest)
		}
		if _, ok := seen[priority]; ok {
			continue
		}
		seen[priority] = struct{}{}
		priorities = append(priorities, priority)
	}
	return priorities, nil
}

func uiParseIssueTagFilters(r *http.Request) ([]string, error) {
	raws := r.URL.Query()["tag"]
	tags := make([]string, 0, len(raws))
	seen := make(map[string]struct{}, len(raws))
	for _, raw := range raws {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		name, err := model.NormalizeIssueTagName(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid tag: %w", errUIBadRequest)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		tags = append(tags, name)
	}
	return tags, nil
}

func uiParseProjectIssueSort(r *http.Request) (store.ListIssuesSort, error) {
	sort := store.ListIssuesSort(strings.TrimSpace(r.URL.Query().Get("sort")))
	if sort == "" {
		return uiIssueListDefaultSort, nil
	}
	switch sort {
	case store.ListIssuesSortCreated, store.ListIssuesSortUpdated, store.ListIssuesSortStatus, store.ListIssuesSortPriority, store.ListIssuesSortDueDate:
		return sort, nil
	default:
		return "", fmt.Errorf("invalid sort: %w", errUIBadRequest)
	}
}

func uiParseProjectIssueSortDirection(r *http.Request, sort store.ListIssuesSort) (store.ListIssuesSortDirection, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("direction"))
	if raw == "" {
		return uiDefaultIssueSortDirection(sort), nil
	}
	direction := store.ListIssuesSortDirection(raw)
	switch direction {
	case store.ListIssuesSortAscending, store.ListIssuesSortDescending:
		return direction, nil
	default:
		return "", fmt.Errorf("invalid direction: %w", errUIBadRequest)
	}
}

func uiSortIssueItems(items []uiIssueItem, sortBy store.ListIssuesSort, direction store.ListIssuesSortDirection) {
	currentSort := sortBy
	if currentSort == "" {
		currentSort = uiIssueListDefaultSort
	}
	currentDirection := direction
	if currentDirection == "" {
		currentDirection = uiDefaultIssueSortDirection(currentSort)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return uiIssueItemLess(items[i], items[j], currentSort, currentDirection)
	})
}

func uiIssueItemLess(a, b uiIssueItem, sortBy store.ListIssuesSort, direction store.ListIssuesSortDirection) bool {
	switch sortBy {
	case store.ListIssuesSortCreated:
		if !a.Issue.CreatedAt.Equal(b.Issue.CreatedAt) {
			if direction == store.ListIssuesSortAscending {
				return a.Issue.CreatedAt.Before(b.Issue.CreatedAt)
			}
			return a.Issue.CreatedAt.After(b.Issue.CreatedAt)
		}
	case store.ListIssuesSortUpdated:
		if !a.Issue.UpdatedAt.Equal(b.Issue.UpdatedAt) {
			if direction == store.ListIssuesSortAscending {
				return a.Issue.UpdatedAt.Before(b.Issue.UpdatedAt)
			}
			return a.Issue.UpdatedAt.After(b.Issue.UpdatedAt)
		}
	case store.ListIssuesSortStatus:
		aRank := uiIssueStatusSortRank(a.Issue.Status)
		bRank := uiIssueStatusSortRank(b.Issue.Status)
		if aRank != bRank {
			if direction == store.ListIssuesSortDescending {
				return aRank > bRank
			}
			return aRank < bRank
		}
	case store.ListIssuesSortPriority:
		aRank := uiIssuePrioritySortRank(a.Issue.Priority)
		bRank := uiIssuePrioritySortRank(b.Issue.Priority)
		if aRank != bRank {
			if direction == store.ListIssuesSortDescending {
				return aRank > bRank
			}
			return aRank < bRank
		}
	case store.ListIssuesSortDueDate:
		if less, ok := uiIssueDueDateLess(a.Issue.DueDate, b.Issue.DueDate, direction); ok {
			return less
		}
	}
	return uiIssueItemTieLess(a, b)
}

func uiIssueDueDateLess(a, b *model.Date, direction store.ListIssuesSortDirection) (bool, bool) {
	switch {
	case a == nil && b == nil:
		return false, false
	case a == nil:
		return false, true
	case b == nil:
		return true, true
	}
	aTime := a.Time()
	bTime := b.Time()
	if aTime.Equal(bTime) {
		return false, false
	}
	if direction == store.ListIssuesSortAscending {
		return aTime.Before(bTime), true
	}
	return aTime.After(bTime), true
}

func uiIssueItemTieLess(a, b uiIssueItem) bool {
	aOwner := a.Project.OwnerUsername
	if aOwner == "" {
		aOwner = a.Issue.OwnerUsername
	}
	bOwner := b.Project.OwnerUsername
	if bOwner == "" {
		bOwner = b.Issue.OwnerUsername
	}
	if aOwner != bOwner {
		return aOwner < bOwner
	}
	aKey := a.Project.Key
	if aKey == "" {
		aKey = a.Issue.ProjectKey
	}
	bKey := b.Project.Key
	if bKey == "" {
		bKey = b.Issue.ProjectKey
	}
	if aKey != bKey {
		return aKey < bKey
	}
	if a.Issue.Number != b.Issue.Number {
		return a.Issue.Number < b.Issue.Number
	}
	return a.Issue.ID.String() < b.Issue.ID.String()
}

func uiIssueStatusSortRank(status model.Status) int {
	switch status {
	case model.StatusTodo:
		return 0
	case model.StatusInProgress:
		return 1
	case model.StatusDone:
		return 2
	case model.StatusClosed:
		return 3
	default:
		return 4
	}
}

func uiIssuePrioritySortRank(priority model.IssuePriority) int {
	switch priority {
	case model.PriorityP0:
		return 0
	case model.PriorityP1:
		return 1
	case model.PriorityP2:
		return 2
	case model.PriorityP3:
		return 3
	case model.PriorityP4:
		return 4
	default:
		return 5
	}
}

func uiToggleAssigneeIDs(ids []uuid.UUID, id uuid.UUID) ([]uuid.UUID, bool) {
	selected := false
	for _, current := range ids {
		if current == id {
			selected = true
			break
		}
	}
	if !selected {
		out := make([]uuid.UUID, 0, len(ids)+1)
		out = append(out, ids...)
		out = append(out, id)
		return out, false
	}
	out := make([]uuid.UUID, 0, len(ids)-1)
	for _, current := range ids {
		if current != id {
			out = append(out, current)
		}
	}
	return out, true
}

func uiToggleStatuses(statuses []model.Status, status model.Status) ([]model.Status, bool) {
	selected := false
	for _, current := range statuses {
		if current == status {
			selected = true
			break
		}
	}
	if !selected {
		out := make([]model.Status, 0, len(statuses)+1)
		out = append(out, statuses...)
		out = append(out, status)
		return out, false
	}
	out := make([]model.Status, 0, len(statuses)-1)
	for _, current := range statuses {
		if current != status {
			out = append(out, current)
		}
	}
	return out, true
}

func uiTogglePriorities(priorities []model.IssuePriority, priority model.IssuePriority) ([]model.IssuePriority, bool) {
	selected := false
	for _, current := range priorities {
		if current == priority {
			selected = true
			break
		}
	}
	if !selected {
		out := make([]model.IssuePriority, 0, len(priorities)+1)
		out = append(out, priorities...)
		out = append(out, priority)
		return out, false
	}
	out := make([]model.IssuePriority, 0, len(priorities)-1)
	for _, current := range priorities {
		if current != priority {
			out = append(out, current)
		}
	}
	return out, true
}

func uiToggleTagNames(names []string, name string) ([]string, bool) {
	selected := false
	for _, current := range names {
		if current == name {
			selected = true
			break
		}
	}
	if !selected {
		out := make([]string, 0, len(names)+1)
		out = append(out, names...)
		out = append(out, name)
		return out, false
	}
	out := make([]string, 0, len(names)-1)
	for _, current := range names {
		if current != name {
			out = append(out, current)
		}
	}
	return out, true
}

func uiAppendAssigneeQuery(path string, assigneeIDs []uuid.UUID) string {
	if len(assigneeIDs) == 0 {
		return path
	}
	values := url.Values{}
	for _, id := range assigneeIDs {
		values.Add("assignee_id", id.String())
	}
	return path + "?" + values.Encode()
}

func uiProjectAllIssueCursor(issue model.Issue, sort store.ListIssuesSort) store.IssuesCursor {
	cursor := store.IssuesCursor{Number: issue.Number}
	switch sort {
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

func uiWorkViewPath(view string, query uiIssueListQuery) string {
	path := "/me"
	if view == "all" {
		path = "/me/all"
	}
	return uiIssueListPath(path, query, false)
}

func uiWorkPanelPath(view string, query uiIssueListQuery) string {
	path := "/me/panel"
	if view == "all" {
		path = "/me/all/panel"
	}
	return uiIssueListPath(path, query, false)
}

func uiProjectAllViewPath(project model.Project, query uiProjectAllQuery) string {
	return uiProjectAllPath(uiProjectPath(project)+"/all", query)
}

func uiProjectSprintViewPath(project model.Project, query uiIssueListQuery) string {
	return uiIssueListPath(uiProjectPath(project)+"/sprint", query, true)
}

func uiProjectAllPanelPath(project model.Project, query uiProjectAllQuery) string {
	return uiProjectAllPath(uiProjectPath(project)+"/all/panel", query)
}

func uiProjectSprintPanelPath(project model.Project, query uiIssueListQuery) string {
	return uiIssueListPath(uiProjectPath(project)+"/sprint/panel", query, true)
}

func uiProjectAllPagePath(project model.Project, query uiProjectAllQuery) string {
	return uiProjectAllPath(uiProjectPath(project)+"/all/page", query)
}

func uiProjectAllPath(path string, query uiProjectAllQuery) string {
	return uiIssueListPath(path, query, true)
}

func uiIssueListPath(path string, query uiIssueListQuery, includeAssignees bool) string {
	values := url.Values{}
	for _, status := range query.Statuses {
		values.Add("status", string(status))
	}
	for _, priority := range query.Priorities {
		values.Add("priority", string(priority))
	}
	for _, tag := range query.TagNames {
		values.Add("tag", tag)
	}
	sortBy := uiIssueListSort(query)
	if sortBy != uiIssueListDefaultSort {
		values.Set("sort", string(sortBy))
	}
	direction := uiIssueListDirection(query)
	if direction != uiDefaultIssueSortDirection(sortBy) {
		values.Set("direction", string(direction))
	}
	if includeAssignees {
		for _, id := range query.AssigneeIDs {
			values.Add("assignee_id", id.String())
		}
	}
	if query.Cursor != "" {
		values.Set("cursor", query.Cursor)
	}
	if len(values) == 0 {
		return path
	}
	return path + "?" + values.Encode()
}
