package server

import (
	"bytes"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/google/uuid"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestUIParseProjectAllQuery(t *testing.T) {
	t.Parallel()

	assigneeID := uuid.MustParse("23f14acb-6a57-4035-a046-33e93ffbd5bb")
	req := httptest.NewRequest("GET", "/all?status=todo&status=done&status=todo&priority=P0&priority=P0&sort=status&direction=desc&assignee_id="+assigneeID.String(), nil)
	got, err := uiParseProjectAllQuery(req)
	if err != nil {
		t.Fatalf("uiParseProjectAllQuery: %v", err)
	}
	if len(got.Statuses) != 2 || got.Statuses[0] != model.StatusTodo || got.Statuses[1] != model.StatusDone {
		t.Fatalf("statuses = %+v, want todo/done", got.Statuses)
	}
	if len(got.Priorities) != 1 || got.Priorities[0] != model.PriorityP0 {
		t.Fatalf("priorities = %+v, want P0", got.Priorities)
	}
	if got.Sort != store.ListIssuesSortStatus {
		t.Fatalf("sort = %q, want status", got.Sort)
	}
	if got.Direction != store.ListIssuesSortDescending {
		t.Fatalf("direction = %q, want desc", got.Direction)
	}
	if len(got.AssigneeIDs) != 1 || got.AssigneeIDs[0] != assigneeID {
		t.Fatalf("assignees = %+v, want %s", got.AssigneeIDs, assigneeID)
	}

	req = httptest.NewRequest("GET", "/all", nil)
	got, err = uiParseProjectAllQuery(req)
	if err != nil {
		t.Fatalf("uiParseProjectAllQuery default: %v", err)
	}
	if got.Sort != store.ListIssuesSortUpdated {
		t.Fatalf("default sort = %q, want updated", got.Sort)
	}
	if got.Direction != store.ListIssuesSortDescending {
		t.Fatalf("default direction = %q, want desc", got.Direction)
	}

	for _, path := range []string{"/all?status=blocked", "/all?priority=P9", "/all?sort=number", "/all?direction=sideways"} {
		req := httptest.NewRequest("GET", path, nil)
		if _, err := uiParseProjectAllQuery(req); err == nil {
			t.Fatalf("uiParseProjectAllQuery(%s) err = nil, want error", path)
		}
	}
}

func TestUIParseIssueListQuery(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/me?status=todo&status=done&status=todo&priority=P0&priority=P0&sort=due&direction=asc", nil)
	got, err := uiParseIssueListQuery(req)
	if err != nil {
		t.Fatalf("uiParseIssueListQuery: %v", err)
	}
	if len(got.Statuses) != 2 || got.Statuses[0] != model.StatusTodo || got.Statuses[1] != model.StatusDone {
		t.Fatalf("statuses = %+v, want todo/done", got.Statuses)
	}
	if len(got.Priorities) != 1 || got.Priorities[0] != model.PriorityP0 {
		t.Fatalf("priorities = %+v, want P0", got.Priorities)
	}
	if got.Sort != store.ListIssuesSortDueDate {
		t.Fatalf("sort = %q, want due", got.Sort)
	}
	if got.Direction != store.ListIssuesSortAscending {
		t.Fatalf("direction = %q, want asc", got.Direction)
	}
	if got.AssigneeIDs != nil {
		t.Fatalf("assignees = %+v, want nil", got.AssigneeIDs)
	}

	req = httptest.NewRequest("GET", "/me", nil)
	got, err = uiParseIssueListQuery(req)
	if err != nil {
		t.Fatalf("uiParseIssueListQuery default: %v", err)
	}
	if got.Sort != store.ListIssuesSortUpdated {
		t.Fatalf("default sort = %q, want updated", got.Sort)
	}
	if got.Direction != store.ListIssuesSortDescending {
		t.Fatalf("default direction = %q, want desc", got.Direction)
	}

	for _, path := range []string{"/me?status=blocked", "/me?priority=P9", "/me?sort=number", "/me?direction=sideways"} {
		req := httptest.NewRequest("GET", path, nil)
		if _, err := uiParseIssueListQuery(req); err == nil {
			t.Fatalf("uiParseIssueListQuery(%s) err = nil, want error", path)
		}
	}
}

func TestUISortIssueItems(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	earlyDue, err := model.ParseDate("2026-06-18")
	if err != nil {
		t.Fatalf("ParseDate early: %v", err)
	}
	midDue, err := model.ParseDate("2026-06-20")
	if err != nil {
		t.Fatalf("ParseDate mid: %v", err)
	}
	lateDue, err := model.ParseDate("2026-06-22")
	if err != nil {
		t.Fatalf("ParseDate late: %v", err)
	}
	items := []uiIssueItem{
		sortTestIssue("todo p3 older", "ALPHA", 2, model.StatusTodo, model.PriorityP3, base, base.Add(2*time.Hour)),
		sortTestIssue("done p0 newest", "BETA", 1, model.StatusDone, model.PriorityP0, base.Add(2*time.Hour), base.Add(4*time.Hour)),
		sortTestIssue("progress p1", "ALPHA", 1, model.StatusInProgress, model.PriorityP1, base.Add(time.Hour), base.Add(3*time.Hour)),
		sortTestIssue("closed p4", "GAMMA", 1, model.StatusClosed, model.PriorityP4, base.Add(3*time.Hour), base.Add(time.Hour)),
	}
	items[0].Issue.DueDate = &midDue
	items[2].Issue.DueDate = &earlyDue
	items[3].Issue.DueDate = &lateDue

	cases := []struct {
		name      string
		sort      store.ListIssuesSort
		direction store.ListIssuesSortDirection
		want      []string
	}{
		{name: "updated", sort: store.ListIssuesSortUpdated, want: []string{"done p0 newest", "progress p1", "todo p3 older", "closed p4"}},
		{name: "updated asc", sort: store.ListIssuesSortUpdated, direction: store.ListIssuesSortAscending, want: []string{"closed p4", "todo p3 older", "progress p1", "done p0 newest"}},
		{name: "created", sort: store.ListIssuesSortCreated, want: []string{"closed p4", "done p0 newest", "progress p1", "todo p3 older"}},
		{name: "status", sort: store.ListIssuesSortStatus, want: []string{"todo p3 older", "progress p1", "done p0 newest", "closed p4"}},
		{name: "priority", sort: store.ListIssuesSortPriority, want: []string{"done p0 newest", "progress p1", "todo p3 older", "closed p4"}},
		{name: "priority desc", sort: store.ListIssuesSortPriority, direction: store.ListIssuesSortDescending, want: []string{"closed p4", "todo p3 older", "progress p1", "done p0 newest"}},
		{name: "due", sort: store.ListIssuesSortDueDate, want: []string{"progress p1", "todo p3 older", "closed p4", "done p0 newest"}},
		{name: "due desc", sort: store.ListIssuesSortDueDate, direction: store.ListIssuesSortDescending, want: []string{"closed p4", "todo p3 older", "progress p1", "done p0 newest"}},
	}
	for _, tt := range cases {
		got := append([]uiIssueItem(nil), items...)
		uiSortIssueItems(got, tt.sort, tt.direction)
		if titles := issueItemTitles(got); strings.Join(titles, "|") != strings.Join(tt.want, "|") {
			t.Fatalf("%s titles = %+v, want %+v", tt.name, titles, tt.want)
		}
	}

	tied := []uiIssueItem{
		sortTestIssue("beta one", "BETA", 1, model.StatusTodo, model.PriorityP2, base, base),
		sortTestIssue("alpha two", "ALPHA", 2, model.StatusTodo, model.PriorityP2, base, base),
		sortTestIssue("alpha one", "ALPHA", 1, model.StatusTodo, model.PriorityP2, base, base),
	}
	uiSortIssueItems(tied, store.ListIssuesSortUpdated, "")
	if titles := issueItemTitles(tied); strings.Join(titles, "|") != "alpha one|alpha two|beta one" {
		t.Fatalf("tie titles = %+v, want alpha one/alpha two/beta one", titles)
	}
}

func TestUIIssueRowsUseCompactIssueKeyAndColoredStatus(t *testing.T) {
	t.Parallel()

	dueDate, err := model.ParseDate("2099-06-24")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	issue := model.Issue{
		ID:         uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b"),
		ProjectID:  uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16"),
		Identifier: "TRACK-7",
		Title:      "Row issue",
		Status:     model.StatusDone,
		Priority:   model.PriorityP0,
		DueDate:    &dueDate,
		Tags: []model.IssueTag{
			uiTestIssueTag(uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16"), 1, "Card Tag", model.TagColorViolet),
		},
	}
	project := model.Project{ID: issue.ProjectID, Key: "TRACK", Name: "Track Slash"}

	tests := []struct {
		name     string
		template string
		data     any
		hasBadge bool
	}{
		{name: "project issue list", template: "issue-list", data: []model.Issue{issue}, hasBadge: true},
		{name: "project inset issue list", template: "issue-list-inset", data: []model.Issue{issue}, hasBadge: true},
		{name: "work issue row list", template: "issue-row-list", data: []uiIssueItem{{Issue: issue, Project: project}}, hasBadge: true},
		{name: "work issue card list", template: "issue-card-list", data: []uiIssueItem{{Issue: issue, Project: project, Assignee: &model.ProjectAssignee{ID: uuid.MustParse("23f14acb-6a57-4035-a046-33e93ffbd5bb"), Username: "ada", Name: "Ada Lovelace"}}}},
	}

	for _, tt := range tests {
		var buf bytes.Buffer
		if err := uiTemplates.ExecuteTemplate(&buf, tt.template, tt.data); err != nil {
			t.Fatalf("%s ExecuteTemplate: %v", tt.name, err)
		}
		body := buf.String()
		for _, want := range []string{
			"TRACK-7",
			"inline-flex w-fit justify-self-start",
			"bg-emerald-50/45 hover:bg-emerald-50",
			`aria-label="Priority P0"`,
			"bg-red-600",
			`aria-label="Due Jun 24, 2099"`,
			`data-lucide="calendar"`,
			"Jun 24",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing markup %q: %s", tt.name, want, body)
			}
		}
		if tt.hasBadge {
			for _, want := range []string{"Done", "border-emerald-300 bg-emerald-50 text-emerald-800"} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing status badge markup %q: %s", tt.name, want, body)
				}
			}
		}
		if tt.template == "issue-card-list" {
			for _, want := range []string{`aria-label="Assigned to Ada Lovelace"`, `title="Ada Lovelace"`, ">AL</span>", "#Card Tag", "border-violet-200 bg-violet-50 text-violet-700"} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing assignee avatar markup %q: %s", tt.name, want, body)
				}
			}
		}
		if strings.Contains(body, "rounded-md border border-slate-200 bg-slate-50 px-2.5 py-1 text-xs text-slate-600") {
			t.Fatalf("%s still renders neutral row status: %s", tt.name, body)
		}
	}
}
