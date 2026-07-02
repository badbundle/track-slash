package server

import (
	"bytes"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestUIProjectPanelRendersPlannedAndAllViews(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	project := model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"}
	assigneeID := uuid.MustParse("23f14acb-6a57-4035-a046-33e93ffbd5bb")
	sprint := model.Sprint{
		ID:        uuid.MustParse("d7fc0dbf-845c-41b4-84ab-89f487cc4a08"),
		ProjectID: projectID,
		Name:      "First Planned Sprint",
		StartDate: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC),
	}

	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "project-panel", &uiProjectPanelData{
		Project:     project,
		View:        "planned",
		ProjectTabs: uiProjectTabs(project, "planned", nil),
		PlannedSprints: []uiPlannedSprint{{
			Sprint: sprint,
			Issues: []model.Issue{
				{ID: uuid.MustParse("adbf2723-a44d-4b43-a3d0-e12276fa59c0"), ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-10", Title: "First planned issue", Status: model.StatusTodo},
				{ID: uuid.MustParse("af63e70c-bf9d-4f80-999d-df145379ec6d"), ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-11", Title: "Second planned issue", Status: model.StatusDone},
			},
		}},
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{"First planned issue", "Second planned issue"} {
		if !strings.Contains(body, want) {
			t.Fatalf("project planned panel missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "All issues") || strings.Contains(body, "Backlog") {
		t.Fatalf("planned panel included all/backlog content: %s", body)
	}
	requireInlineCount(t, body, "Planned", 1)
	requireInlineCount(t, body, "First Planned Sprint", 2)

	buf.Reset()
	allIssues := []model.Issue{
		{ID: uuid.MustParse("138095fe-77d7-4644-b127-d0b995757ff2"), ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-12", Title: "First all issue", Status: model.StatusTodo, Priority: model.PriorityP0},
		{ID: uuid.MustParse("2eeaf29c-ad20-4513-af41-edbb2c9abc2c"), ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-13", Title: "Second all issue", Status: model.StatusInProgress, Priority: model.PriorityP1},
	}
	allQuery := uiProjectAllQuery{
		Statuses:    []model.Status{model.StatusDone, model.StatusTodo},
		Priorities:  []model.IssuePriority{model.PriorityP0},
		Sort:        store.ListIssuesSortPriority,
		AssigneeIDs: []uuid.UUID{assigneeID},
	}
	clearAssigneeQuery := allQuery
	clearAssigneeQuery.AssigneeIDs = nil
	nextQuery := allQuery
	nextQuery.Cursor = "next-cursor"
	assigneeFilters := uiProjectAllAssigneeFilters(project, []model.ProjectAssignee{{ID: assigneeID, Username: "ada", Name: "Ada Lovelace"}}, allQuery)
	allControls := uiProjectAllIssueControls(project, allQuery, nil, assigneeFilters, true, uiProjectAllViewPath(project, clearAssigneeQuery), uiProjectAllPanelPath(project, clearAssigneeQuery), uiProjectAllViewPath(project, clearAssigneeQuery))
	err = uiTemplates.ExecuteTemplate(&buf, "project-panel", &uiProjectPanelData{
		Project:              project,
		View:                 "all",
		ProjectTabs:          uiProjectTabs(project, "all", nil),
		AssigneeFilters:      assigneeFilters,
		AssigneeFilterActive: true,
		ClearAssigneeHref:    uiProjectAllViewPath(project, clearAssigneeQuery),
		ClearAssigneeHXGet:   uiProjectAllPanelPath(project, clearAssigneeQuery),
		ClearAssigneeHXPush:  uiProjectAllViewPath(project, clearAssigneeQuery),
		AllIssues:            allIssues,
		AllIssuePage: uiProjectAllIssuePageData{
			Issues:    allIssues,
			NextHXGet: uiProjectAllPagePath(project, nextQuery),
		},
		AllControls: allControls,
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate all: %v", err)
	}
	body = buf.String()
	for _, want := range []string{
		`aria-label="Issue controls"`,
		"Status",
		"Priority",
		"Assignee",
		"Sort",
		"Direction",
		"Any",
		"Anyone",
		"Done",
		"To do",
		"Updated",
		"Created",
		"Due date",
		"Asc",
		`data-lucide="arrow-up"`,
		`aria-label="Priority P0"`,
		`aria-pressed="true"`,
		`aria-current="page"`,
		`href="/bradley/projects/TRACK/all?assignee_id=23f14acb-6a57-4035-a046-33e93ffbd5bb&amp;priority=P0&amp;sort=priority&amp;status=done&amp;status=todo"`,
		"First all issue",
		"Second all issue",
		`hx-get="/bradley/projects/TRACK/all/page?assignee_id=23f14acb-6a57-4035-a046-33e93ffbd5bb&amp;cursor=next-cursor&amp;priority=P0&amp;sort=priority&amp;status=done&amp;status=todo"`,
		`hx-trigger="intersect once"`,
		`hx-swap="outerHTML"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("project all panel missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{`aria-label="Status filter"`, `aria-label="Assignee filter"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("project all panel kept separate filter box %q: %s", notWant, body)
		}
	}
	requireInlineCount(t, body, "All issues", 2)
}
