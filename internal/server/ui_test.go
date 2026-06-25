package server

import (
	"bytes"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

const uiCountBadgeClass = "inline-flex shrink-0 items-center rounded-md border border-slate-200 px-2 py-0.5 text-xs font-medium leading-4 text-slate-500 dark:border-slate-700 dark:text-slate-400"

func requireInlineCount(t *testing.T, body, heading string, count int) {
	t.Helper()
	headingIndex := strings.Index(body, ">"+heading+"</h")
	if headingIndex < 0 {
		t.Fatalf("missing heading %q: %s", heading, body)
	}
	segmentEnd := headingIndex + 350
	if segmentEnd > len(body) {
		segmentEnd = len(body)
	}
	segment := body[headingIndex:segmentEnd]
	want := `class="` + uiCountBadgeClass + `">` + strconv.Itoa(count) + `</span>`
	if !strings.Contains(segment, want) {
		t.Fatalf("heading %q missing inline count %d: %s", heading, count, body)
	}
}

func sectionClassForHeading(t *testing.T, body, heading string) string {
	t.Helper()
	headingIndex := strings.Index(body, ">"+heading+"</h")
	if headingIndex < 0 {
		t.Fatalf("missing heading %q: %s", heading, body)
	}
	sectionStart := strings.LastIndex(body[:headingIndex], "<section")
	if sectionStart < 0 {
		t.Fatalf("missing section before heading %q: %s", heading, body)
	}
	classStart := strings.Index(body[sectionStart:headingIndex], `class="`)
	if classStart < 0 {
		t.Fatalf("missing section class before heading %q: %s", heading, body)
	}
	classStart += sectionStart + len(`class="`)
	classEnd := strings.Index(body[classStart:headingIndex], `"`)
	if classEnd < 0 {
		t.Fatalf("unterminated section class before heading %q: %s", heading, body)
	}
	return body[classStart : classStart+classEnd]
}

func TestUIProjectIcon(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want string
	}{
		{name: "Roadmap", key: "TRACK", want: "R"},
		{name: " roadmap", key: "TRACK", want: "R"},
		{name: "", key: "TRACK", want: "T"},
		{name: "", key: "", want: "?"},
	}

	for _, tt := range tests {
		if got := uiProjectIcon(tt.name, tt.key); got != tt.want {
			t.Fatalf("uiProjectIcon(%q, %q) = %q, want %q", tt.name, tt.key, got, tt.want)
		}
	}
}

func TestUIStatusClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status model.Status
		want   string
	}{
		{status: model.StatusTodo, want: "border-slate-300 bg-slate-100 text-slate-700 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200"},
		{status: model.StatusInProgress, want: "border-blue-300 bg-blue-50 text-blue-800 dark:border-blue-500/40 dark:bg-blue-950/40 dark:text-blue-200"},
		{status: model.StatusDone, want: "border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-500/40 dark:bg-emerald-950/40 dark:text-emerald-200"},
		{status: model.StatusClosed, want: "border-zinc-300 bg-zinc-100 text-zinc-700 dark:border-zinc-600 dark:bg-zinc-900 dark:text-zinc-200"},
		{status: model.Status("custom"), want: "border-slate-300 bg-slate-50 text-slate-700 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200"},
	}

	for _, tt := range tests {
		if got := uiStatusClass(tt.status); got != tt.want {
			t.Fatalf("uiStatusClass(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestUIStatusRowClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status model.Status
		want   string
	}{
		{status: model.StatusTodo, want: "bg-slate-50/70 hover:bg-slate-100/80 dark:bg-slate-900/30 dark:hover:bg-slate-800/70"},
		{status: model.StatusInProgress, want: "bg-blue-50/45 hover:bg-blue-50 dark:bg-blue-950/15 dark:hover:bg-blue-950/30"},
		{status: model.StatusDone, want: "bg-emerald-50/45 hover:bg-emerald-50 dark:bg-emerald-950/15 dark:hover:bg-emerald-950/30"},
		{status: model.StatusClosed, want: "bg-zinc-50/70 hover:bg-zinc-100/80 dark:bg-zinc-900/35 dark:hover:bg-zinc-800/70"},
		{status: model.Status("custom"), want: "bg-white hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800/60"},
	}

	for _, tt := range tests {
		if got := uiStatusRowClass(tt.status); got != tt.want {
			t.Fatalf("uiStatusRowClass(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestUIStatusSurfaceClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status model.Status
		want   string
	}{
		{status: model.StatusTodo, want: "bg-slate-50/70 dark:bg-slate-900/30"},
		{status: model.StatusInProgress, want: "bg-blue-50/45 dark:bg-blue-950/15"},
		{status: model.StatusDone, want: "bg-emerald-50/45 dark:bg-emerald-950/15"},
		{status: model.StatusClosed, want: "bg-zinc-50/70 dark:bg-zinc-900/35"},
		{status: model.Status("custom"), want: "bg-white dark:bg-slate-900"},
	}

	for _, tt := range tests {
		if got := uiStatusSurfaceClass(tt.status); got != tt.want {
			t.Fatalf("uiStatusSurfaceClass(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestUICloseReasonLabelAndOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason model.IssueCloseReason
		want   string
	}{
		{reason: model.CloseReasonDuplicate, want: "Duplicate"},
		{reason: model.CloseReasonWontDo, want: "Won't Do"},
		{reason: model.CloseReasonInvalid, want: "Invalid"},
		{reason: model.IssueCloseReason("custom"), want: "custom"},
	}
	for _, tt := range tests {
		if got := uiCloseReasonLabel(tt.reason); got != tt.want {
			t.Fatalf("uiCloseReasonLabel(%q) = %q, want %q", tt.reason, got, tt.want)
		}
		reason := tt.reason
		if got := uiCloseReasonLabel(&reason); got != tt.want {
			t.Fatalf("uiCloseReasonLabel(&%q) = %q, want %q", tt.reason, got, tt.want)
		}
	}

	opts := uiCloseReasonOptions()
	if len(opts) != 3 ||
		opts[0].Reason != model.CloseReasonDuplicate ||
		opts[1].Reason != model.CloseReasonWontDo ||
		opts[2].Reason != model.CloseReasonInvalid {
		t.Fatalf("close reason options = %+v", opts)
	}
}

func TestUISubIssueProgress(t *testing.T) {
	t.Parallel()

	empty := uiSubIssueProgress(nil)
	if empty.Total != 0 || empty.DonePercent != "0%" || empty.InProgressPercent != "0%" || empty.TodoPercent != "0%" || empty.Label != "Sub-issue progress: no sub-issues" {
		t.Fatalf("empty progress = %+v", empty)
	}

	mixed := uiSubIssueProgress([]model.Issue{
		{Status: model.StatusDone},
		{Status: model.StatusDone},
		{Status: model.StatusClosed},
		{Status: model.StatusInProgress},
		{Status: model.StatusTodo},
		{Status: model.Status("custom")},
	})
	if mixed.Total != 6 || mixed.Done != 3 || mixed.InProgress != 1 || mixed.Todo != 2 {
		t.Fatalf("mixed counts = %+v", mixed)
	}
	if mixed.DonePercent != "50.00%" || mixed.InProgressPercent != "16.67%" || mixed.TodoPercent != "33.33%" {
		t.Fatalf("mixed percentages = %+v", mixed)
	}
	if mixed.Label != "Sub-issue progress: 3 done, 1 in progress, 2 to do" {
		t.Fatalf("mixed label = %q", mixed.Label)
	}
}

func TestUIIssueColumnStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status model.Status
		want   model.Status
	}{
		{status: model.StatusTodo, want: model.StatusTodo},
		{status: model.StatusInProgress, want: model.StatusInProgress},
		{status: model.StatusDone, want: model.StatusDone},
		{status: model.StatusClosed, want: model.StatusDone},
		{status: model.Status("custom"), want: model.Status("custom")},
	}
	for _, tt := range tests {
		if got := uiIssueColumnStatus(tt.status); got != tt.want {
			t.Fatalf("uiIssueColumnStatus(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestUIPriorityClassAndLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		priority  model.IssuePriority
		wantClass string
		wantLabel string
	}{
		{priority: model.PriorityP0, wantClass: "bg-red-600", wantLabel: "P0"},
		{priority: model.PriorityP1, wantClass: "bg-orange-500", wantLabel: "P1"},
		{priority: model.PriorityP2, wantClass: "bg-amber-500", wantLabel: "P2"},
		{priority: model.PriorityP3, wantClass: "bg-yellow-500", wantLabel: "P3"},
		{priority: model.PriorityP4, wantClass: "bg-gray-500", wantLabel: "P4"},
		{priority: "", wantClass: "bg-amber-500", wantLabel: "P2"},
		{priority: model.IssuePriority("PX"), wantClass: "bg-gray-500", wantLabel: "PX"},
	}

	for _, tt := range tests {
		if got := uiPriorityClass(tt.priority); got != tt.wantClass {
			t.Fatalf("uiPriorityClass(%q) = %q, want %q", tt.priority, got, tt.wantClass)
		}
		if got := uiPriorityLabel(tt.priority); got != tt.wantLabel {
			t.Fatalf("uiPriorityLabel(%q) = %q, want %q", tt.priority, got, tt.wantLabel)
		}
	}
}

func TestUIPriorityBadgeRendersFilledCircle(t *testing.T) {
	t.Parallel()

	for _, priority := range []model.IssuePriority{model.PriorityP0, model.PriorityP1, model.PriorityP2, model.PriorityP3, model.PriorityP4} {
		var buf bytes.Buffer
		if err := uiTemplates.ExecuteTemplate(&buf, "priority-badge", priority); err != nil {
			t.Fatalf("ExecuteTemplate %s: %v", priority, err)
		}
		body := buf.String()
		for _, want := range []string{
			`aria-label="Priority ` + string(priority) + `"`,
			"h-5 w-5",
			"rounded-full",
			"font-bold",
			"text-white",
			uiPriorityClass(priority),
			string(priority),
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("priority badge %s missing %q: %s", priority, want, body)
			}
		}
	}
}

func TestSafeUINextRootPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: "/"},
		{name: "root", raw: "/", want: "/"},
		{name: "removed work", raw: "/sprint", want: "/"},
		{name: "removed work panel with query", raw: "/sprint/panel?x=1", want: "/"},
		{name: "projects", raw: "/projects", want: "/projects"},
		{name: "projects panel", raw: "/projects/panel", want: "/projects/panel"},
		{name: "new project", raw: "/projects/new", want: "/projects/new"},
		{name: "new project panel with query", raw: "/projects/new/panel?x=1", want: "/projects/new/panel?x=1"},
		{name: "issue", raw: "/bradley/issues/TRACK-7", want: "/bradley/issues/TRACK-7"},
		{name: "issue panel with query", raw: "/bradley/issues/TRACK-7/panel?x=1", want: "/bradley/issues/TRACK-7/panel?x=1"},
		{name: "issue description edit with query", raw: "/bradley/issues/TRACK-7/description/edit?x=1", want: "/bradley/issues/TRACK-7/description/edit?x=1"},
		{name: "issue status edit", raw: "/bradley/issues/TRACK-7/status/edit", want: "/bradley/issues/TRACK-7/status/edit"},
		{name: "issue close reason edit", raw: "/bradley/issues/TRACK-7/close-reason/edit", want: "/bradley/issues/TRACK-7/close-reason/edit"},
		{name: "issue priority edit", raw: "/bradley/issues/TRACK-7/priority/edit", want: "/bradley/issues/TRACK-7/priority/edit"},
		{name: "issue sprint edit", raw: "/bradley/issues/TRACK-7/sprint/edit", want: "/bradley/issues/TRACK-7/sprint/edit"},
		{name: "issue restore", raw: "/bradley/issues/TRACK-7/restore", want: "/bradley/issues/TRACK-7/restore"},
		{name: "issue removed archive action", raw: "/bradley/issues/TRACK-7/archive", want: "/"},
		{name: "issue link add", raw: "/bradley/issues/TRACK-7/links/new?x=1", want: "/bradley/issues/TRACK-7/links/new?x=1"},
		{name: "issue sub-issue add", raw: "/bradley/issues/TRACK-7/sub-issues/new?x=1", want: "/bradley/issues/TRACK-7/sub-issues/new?x=1"},
		{name: "issue link edit", raw: "/bradley/issues/TRACK-7/links/link-2/edit", want: "/bradley/issues/TRACK-7/links/link-2/edit"},
		{name: "bad issue id", raw: "/bradley/issues/nope", want: "/"},
		{name: "bad issue child", raw: "/bradley/issues/TRACK-7/activity", want: "/"},
		{name: "bad issue nested panel", raw: "/bradley/issues/TRACK-7/panel/extra", want: "/"},
		{name: "bad issue status child", raw: "/bradley/issues/TRACK-7/status/panel", want: "/"},
		{name: "bad issue link ref", raw: "/bradley/issues/TRACK-7/links/nope/edit", want: "/"},
		{name: "bad issue link action", raw: "/bradley/issues/TRACK-7/links/link-2/delete", want: "/"},
		{name: "project", raw: "/bradley/projects/TRACK", want: "/bradley/projects/TRACK"},
		{name: "project about", raw: "/bradley/projects/TRACK/about", want: "/bradley/projects/TRACK/about"},
		{name: "project sprint", raw: "/bradley/projects/TRACK/sprint", want: "/bradley/projects/TRACK/sprint"},
		{name: "project planned", raw: "/bradley/projects/TRACK/planned", want: "/bradley/projects/TRACK/planned"},
		{name: "project all", raw: "/bradley/projects/TRACK/all", want: "/bradley/projects/TRACK/all"},
		{name: "project deleted", raw: "/bradley/projects/TRACK/deleted", want: "/bradley/projects/TRACK/deleted"},
		{name: "project about panel with query", raw: "/bradley/projects/TRACK/about/panel?x=1", want: "/bradley/projects/TRACK/about/panel?x=1"},
		{name: "project planned panel with query", raw: "/bradley/projects/TRACK/planned/panel?x=1", want: "/bradley/projects/TRACK/planned/panel?x=1"},
		{name: "project all panel with query", raw: "/bradley/projects/TRACK/all/panel?x=1", want: "/bradley/projects/TRACK/all/panel?x=1"},
		{name: "project all page with query", raw: "/bradley/projects/TRACK/all/page?cursor=abc", want: "/bradley/projects/TRACK/all/page?cursor=abc"},
		{name: "project backlog panel with query", raw: "/bradley/projects/TRACK/backlog/panel?x=1", want: "/bradley/projects/TRACK/backlog/panel?x=1"},
		{name: "project deleted panel with query", raw: "/bradley/projects/TRACK/deleted/panel?x=1", want: "/bradley/projects/TRACK/deleted/panel?x=1"},
		{name: "bad project key", raw: "/bradley/projects/bad!/sprint", want: "/"},
		{name: "bad project child", raw: "/bradley/projects/TRACK/issues", want: "/"},
		{name: "bad project panel", raw: "/bradley/projects/TRACK/sprint/card", want: "/"},
		{name: "api", raw: "/api/v1/projects", want: "/"},
		{name: "legacy app", raw: "/app/sprint", want: "/"},
		{name: "scheme relative", raw: "//evil.example/sprint", want: "/"},
		{name: "relative", raw: "sprint", want: "/"},
	}

	for _, tt := range tests {
		if got := safeUINext(tt.raw); got != tt.want {
			t.Fatalf("%s: safeUINext(%q) = %q, want %q", tt.name, tt.raw, got, tt.want)
		}
	}
}

func TestUIParseProjectAllQuery(t *testing.T) {
	t.Parallel()

	assigneeID := uuid.MustParse("23f14acb-6a57-4035-a046-33e93ffbd5bb")
	req := httptest.NewRequest("GET", "/all?status=todo&status=done&status=todo&priority=P0&priority=P0&sort=status&assignee_id="+assigneeID.String(), nil)
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

	for _, path := range []string{"/all?status=blocked", "/all?priority=P9", "/all?sort=number"} {
		req := httptest.NewRequest("GET", path, nil)
		if _, err := uiParseProjectAllQuery(req); err == nil {
			t.Fatalf("uiParseProjectAllQuery(%s) err = nil, want error", path)
		}
	}
}

func TestUIShellSidebarCollapseTargetsOnlyTopLevelSidebar(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "shell", uiShellData{
		User:        model.User{Name: "Demo User"},
		CurrentView: "projects",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		"#sidebar-toggle:checked ~ .app-shell > aside",
		`html[data-sidebar-collapsed] .app-shell > aside`,
		`track-slash.sidebar.collapsed`,
		`document.documentElement.toggleAttribute("data-sidebar-collapsed", collapsed)`,
		`sidebarToggle.addEventListener("change"`,
		`[data-member-menu] { bottom: 0.5rem; left: calc(100% + 0.5rem); right: auto; width: 12rem; }`,
		`overflow-visible border-r`,
		`data-member-summary`,
		`data-member-label`,
		`data-member-menu`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing sidebar behavior %q: %s", want, body)
		}
	}
	if strings.Contains(body, "#sidebar-toggle:checked ~ .app-shell aside { width") {
		t.Fatalf("sidebar collapse selector targets nested asides: %s", body)
	}
	menuStart := strings.Index(body, `<div data-member-menu`)
	if menuStart < 0 {
		t.Fatalf("shell missing member menu: %s", body)
	}
	menuEnd := strings.Index(body[menuStart:], `>`)
	if menuEnd < 0 {
		t.Fatalf("shell member menu has invalid markup: %s", body)
	}
	if strings.Contains(body[menuStart:menuStart+menuEnd], "wide-only") {
		t.Fatalf("member menu should remain visible when the sidebar is collapsed: %s", body)
	}
	for _, want := range []string{`[data-submit-shortcut='meta-enter']`, `event.metaKey`, `event.ctrlKey`, `form.requestSubmit()`} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing comment submit shortcut %q: %s", want, body)
		}
	}
	for _, want := range []string{`[data-autogrow-textarea]`, `resizeTextarea`, `textarea.scrollHeight`, `resizeTextareas(event.target)`, `resizeTextareas();`} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing autogrowing textarea behavior %q: %s", want, body)
		}
	}
	for _, want := range []string{`[data-search-input]`, `[data-search-option]`, `filterSearchOptions`, `option.dataset.value`, `input.form || input.closest("form")`} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing search component behavior %q: %s", want, body)
		}
	}
}

func TestUIPanelsUseConsistentPageWidth(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		path      string
		wantClass string
	}{
		{name: "work", path: "templates/work_panel.html", wantClass: `class="mx-auto max-w-6xl px-6 py-6"`},
		{name: "projects", path: "templates/projects_panel.html", wantClass: `class="mx-auto max-w-6xl px-6 py-6"`},
		{name: "new project", path: "templates/new_project_panel.html", wantClass: `class="mx-auto max-w-6xl px-6 py-6"`},
		{name: "project", path: "templates/project_panel.html", wantClass: `class="mx-auto max-w-6xl px-6 py-6"`},
		{name: "issue", path: "templates/issue_panel.html", wantClass: `class="mx-auto max-w-6xl px-6 py-6"`},
		{name: "settings", path: "templates/settings.html", wantClass: `class="mx-auto max-w-6xl px-6 py-6"`},
		{name: "tokens", path: "templates/tokens.html", wantClass: `class="mx-auto max-w-6xl px-6 py-6"`},
		{name: "empty shell", path: "templates/shell.html", wantClass: `class="mx-auto max-w-6xl px-6 py-8"`},
	} {
		src, err := uiTemplateFS.ReadFile(tt.path)
		if err != nil {
			t.Fatalf("%s: read template: %v", tt.name, err)
		}
		body := string(src)
		if !strings.Contains(body, tt.wantClass) {
			t.Fatalf("%s panel missing shared page width %q: %s", tt.name, tt.wantClass, body)
		}
		if strings.Contains(body, "max-w-5xl") {
			t.Fatalf("%s panel still uses narrower page width: %s", tt.name, body)
		}
	}
}

func TestUIIssuePanelRendersReadonlyDetail(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	linkedID := uuid.MustParse("ae77b9b8-9dcf-4a18-8b69-42b97bd4a4b5")
	userID := uuid.MustParse("23f14acb-6a57-4035-a046-33e93ffbd5bb")
	commentID := uuid.MustParse("d0c74b63-c75c-42b0-b899-6baf6948e3fd")
	linkID := uuid.MustParse("48c98f2e-bad8-4054-89d7-5a45a68af54f")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	dueDate, err := model.ParseDate("2099-06-24")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	assignee := model.User{ID: userID, Name: "Ada Lovelace", Email: "ada@example.com"}
	reporter := model.User{ID: userID, Name: "Ada Lovelace", Email: "ada@example.com"}
	sprint := model.Sprint{ID: uuid.MustParse("d7fc0dbf-845c-41b4-84ab-89f487cc4a08"), ProjectID: projectID, Name: "Planned One", Status: model.SprintStatusPlanned}
	var buf bytes.Buffer
	err = uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Identifier:    "TRACK-7",
			Title:         "Design issue detail",
			Description:   "Readonly description",
			Status:        model.StatusInProgress,
			AssigneeID:    &userID,
			ReporterID:    &userID,
			SprintID:      &sprint.ID,
			DueDate:       &dueDate,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project:       model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		Sprint:        &sprint,
		Assignee:      &assignee,
		Reporter:      &reporter,
		CanEditSprint: true,
		Comments: []uiIssueCommentItem{{
			Comment:     model.Comment{ID: commentID, IssueID: issueID, Number: 2, Ref: "comment-2", AuthorID: userID, Body: "Looks ready.", CreatedAt: when, UpdatedAt: when},
			AuthorName:  "Ada Lovelace",
			AuthorEmail: "ada@example.com",
			CanEdit:     true,
		}},
		Links: []uiIssueLinkItem{{
			Link:        model.IssueLink{ID: linkID, ProjectID: projectID, Number: 4, Ref: "link-4", SourceID: issueID, TargetID: linkedID, LinkType: model.LinkTypeBlocks, CreatedAt: when, UpdatedAt: when},
			LinkedIssue: model.Issue{ID: linkedID, ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-8", Title: "Linked work", Status: model.StatusDone},
			HasIssue:    true,
		}},
		BackHref:  "/bradley/projects/TRACK/backlog",
		BackHXGet: "/bradley/projects/TRACK/backlog/panel",
		BackLabel: "Backlog",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		"TRACK-7",
		"text-3xl",
		"Design issue detail",
		"Readonly description",
		"In progress",
		"Track Slash",
		"Ada Lovelace",
		"Planned One",
		"Due date",
		"Jun 24",
		`aria-label="Due Jun 24, 2099"`,
		`data-lucide="calendar"`,
		"Linked issues",
		"Blocks",
		"TRACK-8",
		"Linked work",
		"Comments",
		"Looks ready.",
		`href="/bradley/projects/TRACK/backlog"`,
		`hx-get="/bradley/projects/TRACK/backlog/panel"`,
		`href="/bradley/issues/TRACK-8"`,
		`hx-get="/bradley/issues/TRACK-8/panel"`,
		`aria-label="Issue actions"`,
		`data-lucide="more-horizontal"`,
		`cursor-pointer list-none`,
		`method="post" action="/bradley/issues/TRACK-7/delete"`,
		`hx-post="/bradley/issues/TRACK-7/delete"`,
		`hx-push-url="/bradley/projects/TRACK/backlog"`,
		`hx-confirm="Delete this issue? You can undo it from the next screen."`,
		`Delete issue`,
		`data-lucide="trash-2"`,
		`text-rose-600`,
		`dark:hover:bg-rose-950/40`,
		`aria-label="Edit description"`,
		`hx-get="/bradley/issues/TRACK-7/description/edit"`,
		`aria-label="Edit link"`,
		`hx-get="/bradley/issues/TRACK-7/links/link-4/edit"`,
		`aria-label="Edit comment"`,
		`hx-get="/bradley/issues/TRACK-7/comments/comment-2/edit"`,
		`aria-label="Change status"`,
		`aria-expanded="false"`,
		`hx-get="/bradley/issues/TRACK-7/status/edit"`,
		`aria-label="Edit assignee"`,
		`aria-label="Edit reporter"`,
		`aria-label="Edit due date"`,
		`hx-get="/bradley/issues/TRACK-7/due-date/edit"`,
		`aria-label="Edit sprint"`,
		`hx-get="/bradley/issues/TRACK-7/sprint/edit"`,
		`<span class="min-w-0 text-slate-900 dark:text-slate-100">Ada Lovelace</span>`,
		`<span class="min-w-0 truncate text-slate-900 dark:text-slate-100">Planned One</span>`,
		`aria-label="Add link"`,
		`hx-get="/bradley/issues/TRACK-7/links/new"`,
		`aria-label="Add sub-issue"`,
		`hx-get="/bradley/issues/TRACK-7/sub-issues/new"`,
		`aria-label="Post comment"`,
		`aria-haspopup="listbox"`,
		`data-lucide="chevron-down"`,
		`role="img" aria-label="Linked issue progress: 1 done, 0 in progress, 0 to do"`,
		`bg-emerald-500 dark:bg-emerald-400" style="width: 100.00%;"`,
		`placeholder="Add a comment"`,
		`method="post" action="/bradley/issues/TRACK-7/comments"`,
		`hx-post="/bradley/issues/TRACK-7/comments"`,
		`hx-target="#main"`,
		`hx-push-url="/bradley/issues/TRACK-7"`,
		`data-submit-shortcut="meta-enter"`,
		`data-autogrow-textarea`,
		`<textarea name="body" rows="1"`,
		`data-lucide="send-horizontal"`,
		`class="order-2 min-w-0 space-y-6 lg:order-1"`,
		`class="order-1 min-w-0 lg:order-2"`,
		`class="flex items-start gap-2"`,
		`class="min-w-0 flex-1 resize-none overflow-hidden rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-950`,
		`class="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-indigo-200 bg-indigo-50 text-indigo-700`,
		`class="space-y-3 px-4"`,
		`class="flex items-start gap-2"`,
		`class="grid h-4 w-4 shrink-0 place-items-center rounded-sm bg-slate-100 text-[7px] font-semibold leading-none text-slate-600 dark:bg-slate-800 dark:text-slate-300"`,
		`class="w-fit max-w-full rounded-xl border border-indigo-100 bg-indigo-50/70 px-3 py-2 dark:border-indigo-900/50 dark:bg-indigo-950/25"`,
		`class="mb-1 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 pl-1"`,
		`class="whitespace-pre-wrap break-words text-sm leading-6 text-slate-800 dark:text-slate-200"`,
		`inline-flex w-fit justify-self-start items-center whitespace-nowrap rounded-md border border-slate-300 bg-white px-1.5 py-0.5 font-mono text-[11px]`,
		`class="flex min-w-0 items-center gap-2 hover:text-indigo-700 dark:hover:text-indigo-200"`,
		`class="min-w-0 truncate text-slate-900 dark:text-slate-100">Linked work</span>`,
		`class="min-w-0 overflow-hidden rounded-md border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900 w-full"`,
		`class="text-xs font-semibold uppercase text-slate-500 dark:text-slate-400">Sub-issues</h2>`,
		`class="text-xs font-semibold uppercase text-slate-500 dark:text-slate-400">Linked issues</h2>`,
		`class="grid grid-cols-[4.75rem_1fr_auto] items-center gap-2 border-b border-slate-100 px-4 py-2.5 text-xs`,
		`h-5 w-5`,
		`h-3 w-3`,
		`border-blue-300 bg-blue-50 text-blue-800`,
		`bg-blue-50/45 dark:bg-blue-950/15`,
		`bg-emerald-50/45 hover:bg-emerald-50`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("issue panel missing %q: %s", want, body)
		}
	}
	if got := strings.Count(body, "bg-blue-50/45 dark:bg-blue-950/15"); got != 1 {
		t.Fatalf("issue panel should tint the title card only, got %d matches: %s", got, body)
	}
	if strings.Contains(body, "⌘ + Enter to send") {
		t.Fatalf("issue panel should not render command-enter helper text: %s", body)
	}
	requireInlineCount(t, body, "Sub-issues", 0)
	requireInlineCount(t, body, "Linked issues", 1)
	requireInlineCount(t, body, "Comments", 1)
	detailsStart := strings.Index(body, ">Details</h2>")
	if detailsStart < 0 {
		t.Fatalf("issue panel missing Details heading: %s", body)
	}
	commentsHeadingStart := strings.Index(body, ">Comments</h2>")
	commentsSectionStart := -1
	if commentsHeadingStart >= 0 {
		commentsSectionStart = strings.LastIndex(body[:commentsHeadingStart], "<section")
	}
	if commentsSectionStart < 0 || commentsHeadingStart > detailsStart {
		t.Fatalf("issue panel missing comments section before details: %s", body)
	}
	commentsBlock := body[commentsSectionStart:detailsStart]
	for _, notWant := range []string{
		`overflow-hidden rounded-lg border border-slate-200 bg-white`,
		`border-t border-slate-100 p-4`,
		`border-t border-dashed border-slate-200 px-4 py-4`,
		`rotate-45`,
	} {
		if strings.Contains(commentsBlock, notWant) {
			t.Fatalf("comments section should not render the outer card treatment %q: %s", notWant, body)
		}
	}
	detailsBlock := body[detailsStart:]
	statusIndex := strings.Index(detailsBlock, `aria-label="Change status"`)
	projectIndex := strings.Index(detailsBlock, ">Project</dt>")
	if statusIndex < 0 || projectIndex < 0 {
		t.Fatalf("issue panel missing status control or project details row: %s", body)
	}
	if statusIndex > projectIndex {
		t.Fatalf("status control should render before project detail: %s", body)
	}
	if strings.Contains(detailsBlock, ">Status</dt>") || strings.Contains(body, `aria-label="Edit status"`) {
		t.Fatalf("status control should not render a separate title or edit button: %s", body)
	}
	for _, notWant := range []string{`/archive`, `Archive issue`, `data-lucide="archive"`, `data-lucide="settings"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("issue panel included removed archive control %q: %s", notWant, body)
		}
	}
	if got := strings.Count(body, `class="mt-1 flex items-center justify-between gap-3"`); got != 4 {
		t.Fatalf("due date, assignee, reporter, and sprint rows should align edit buttons with values, got %d rows: %s", got, body)
	}
	if strings.Contains(detailsBlock, `class="flex items-start justify-between gap-3"`) {
		t.Fatalf("detail edit buttons should not align with row titles: %s", body)
	}
	for _, notWant := range []string{`method="post" action="/bradley/issues/TRACK-7/sub-issues"`, `aria-label="Create sub-issue"`, `aria-label="Cancel adding sub-issue"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("issue panel should not render the sub-issue composer by default %q: %s", notWant, body)
		}
	}
	if strings.Contains(body, `border-b border-slate-100 px-4 py-4 last:border-b-0`) {
		t.Fatalf("comments should render as bubbles instead of bordered rows: %s", body)
	}
	commentMetaStart := strings.Index(body, "Ada Lovelace")
	commentBodyStart := strings.Index(body, "Looks ready.")
	if commentMetaStart < 0 || commentBodyStart < 0 || commentMetaStart > commentBodyStart {
		t.Fatalf("issue panel should render comment metadata above the body: %s", body)
	}
	commentComposerStart := strings.Index(commentsBlock, `placeholder="Add a comment"`)
	if commentComposerStart < 0 || commentsSectionStart+commentComposerStart > commentMetaStart {
		t.Fatalf("comment composer should render above the comment list: %s", body)
	}
	commentBubbleStart := strings.Index(body, `class="w-fit max-w-full rounded-xl border border-indigo-100 bg-indigo-50/70 px-3 py-2`)
	if commentBubbleStart < 0 || commentBubbleStart < commentMetaStart || commentBubbleStart > commentBodyStart {
		t.Fatalf("comment body should render inside the bubble after metadata: %s", body)
	}
	commentAvatarStart := strings.Index(body, `class="grid h-4 w-4 shrink-0 place-items-center rounded-sm bg-slate-100 text-[7px] font-semibold leading-none text-slate-600 dark:bg-slate-800 dark:text-slate-300"`)
	if commentAvatarStart < 0 || commentAvatarStart > commentMetaStart {
		t.Fatalf("comment avatar should render with the metadata beside the author name: %s", body)
	}
	commentEditStart := strings.Index(body, `aria-label="Edit comment"`)
	if commentEditStart < 0 || commentEditStart < commentMetaStart || commentEditStart > commentBubbleStart {
		t.Fatalf("comment edit button should render with metadata above the bubble: %s", body)
	}
	if strings.Contains(body, "\n            Comment\n") {
		t.Fatalf("post comment button should be icon-only: %s", body)
	}
	if strings.Contains(body, "<textarea disabled") || strings.Contains(body, `aria-label="Post comment" class="grid h-9 w-9 shrink-0 cursor-not-allowed`) || strings.Contains(body, `aria-label="Post comment" class="grid h-7 w-7 shrink-0 cursor-not-allowed`) {
		t.Fatalf("comment composer should be enabled: %s", body)
	}
	titleHeaderEnd := strings.Index(body, "</header>")
	if titleHeaderEnd < 0 {
		t.Fatalf("issue panel missing title header: %s", body)
	}
	titleHeader := body[:titleHeaderEnd]
	for _, notWant := range []string{"Edit issue", "Change status", "Edit description", "Edit status", "In progress", "cursor-not-allowed"} {
		if strings.Contains(titleHeader, notWant) {
			t.Fatalf("title card still contains section action/status %q: %s", notWant, body)
		}
	}
	if strings.Contains(body, "title=") {
		t.Fatalf("issue panel controls should not render native title tooltips: %s", body)
	}
}

func TestUIDeletedIssuePanelRendersRestore(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "deleted-issue-panel", &uiDeletedIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Identifier:    "TRACK-7",
			Title:         "Deleted issue title",
			Description:   "Hidden deleted description",
			Status:        model.StatusDone,
			Priority:      model.PriorityP1,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project:   model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		BackHref:  "/bradley/projects/TRACK/deleted",
		BackHXGet: "/bradley/projects/TRACK/deleted/panel",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`href="/bradley/projects/TRACK/deleted"`,
		`hx-get="/bradley/projects/TRACK/deleted/panel"`,
		"Deleted issues",
		`rounded-lg border border-slate-300`,
		`mx-auto max-w-lg pt-10`,
		`Deleted issue`,
		"TRACK-7",
		"Deleted issue title",
		"This issue has been deleted",
		"Track Slash",
		"Done",
		`h-px max-w-xs bg-slate-200`,
		`method="post" action="/bradley/issues/TRACK-7/restore"`,
		`hx-post="/bradley/issues/TRACK-7/restore"`,
		`hx-target="#main"`,
		`hx-push-url="/bradley/issues/TRACK-7"`,
		`data-lucide="rotate-ccw"`,
		"Restore issue",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("deleted issue panel missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{"Hidden deleted description", "Comments", "Sub-issues", `aria-label="Issue actions"`, `Delete issue`, `data-lucide="trash-2"`, `rounded-t-[`, `rounded-b-md`, `mt-4 rounded-lg border border-slate-200 bg-slate-50 p-4`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("deleted issue panel leaked full issue UI %q: %s", notWant, body)
		}
	}
}

func TestUIIssuePanelRendersDueDateEditor(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	dueDate, err := model.ParseDate("2026-06-24")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	var buf bytes.Buffer
	err = uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Identifier:    "TRACK-7",
			Title:         "Due date editor",
			Status:        model.StatusTodo,
			DueDate:       &dueDate,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project:      model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		EditDueDate:  true,
		DueDateInput: "2026-06-24",
		DueDateError: "Use YYYY-MM-DD.",
		BackHref:     "/bradley/projects/TRACK/backlog",
		BackHXGet:    "/bradley/projects/TRACK/backlog/panel",
		BackLabel:    "Backlog",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`method="post" action="/bradley/issues/TRACK-7/due-date"`,
		`hx-post="/bradley/issues/TRACK-7/due-date"`,
		`type="date" name="due_date" value="2026-06-24"`,
		`aria-label="Save due date"`,
		`aria-label="Cancel editing due date"`,
		`hx-get="/bradley/issues/TRACK-7/panel"`,
		"Use YYYY-MM-DD.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("due date editor missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `hx-get="/bradley/issues/TRACK-7/due-date/edit"`) {
		t.Fatalf("due date editor rendered readonly edit button: %s", body)
	}
}

func TestUIIssuePanelRendersStatusDropdown(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Identifier:    "TRACK-7",
			Title:         "Design issue detail",
			Status:        model.StatusInProgress,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project:    model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		EditStatus: true,
		BackHref:   "/bradley/projects/TRACK/backlog",
		BackHXGet:  "/bradley/projects/TRACK/backlog/panel",
		BackLabel:  "Backlog",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`aria-label="Change status"`,
		`aria-expanded="true"`,
		`data-option-dropdown-toggle`,
		`data-option-dropdown-list`,
		`option-dropdown-enter`,
		`option-dropdown-settle`,
		`data-lucide="chevron-up"`,
		`hx-get="/bradley/issues/TRACK-7/panel"`,
		`method="post" action="/bradley/issues/TRACK-7/status"`,
		`hx-post="/bradley/issues/TRACK-7/status"`,
		`hx-target="#main"`,
		`hx-push-url="/bradley/issues/TRACK-7"`,
		`role="listbox" aria-label="Issue status"`,
		`name="status" value="todo"`,
		`name="status" value="in_progress"`,
		`name="status" value="done"`,
		`name="status" value="closed"`,
		`role="option" aria-selected="true"`,
		"To do",
		"In progress",
		"Done",
		"Closed",
		`data-lucide="check"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("status dropdown missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{
		`hx-get="/bradley/issues/TRACK-7/status/edit"`,
		`aria-label="Cancel status change"`,
		`cursor-default`,
		`disabled aria-label="Change status"`,
		`title="Cancel status change"`,
		`title="Change status"`,
	} {
		if strings.Contains(body, notWant) {
			t.Fatalf("status dropdown included %q: %s", notWant, body)
		}
	}
}

func TestUIIssuePanelRendersPriorityPicker(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Identifier:    "TRACK-7",
			Title:         "Design issue detail",
			Status:        model.StatusInProgress,
			Priority:      model.PriorityP1,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project:      model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		EditPriority: true,
		BackHref:     "/bradley/projects/TRACK/backlog",
		BackHXGet:    "/bradley/projects/TRACK/backlog/panel",
		BackLabel:    "Backlog",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`method="post" action="/bradley/issues/TRACK-7/priority"`,
		`hx-post="/bradley/issues/TRACK-7/priority"`,
		`hx-target="#main"`,
		`hx-push-url="/bradley/issues/TRACK-7"`,
		`@keyframes priority-picker-item-enter`,
		`@media (prefers-reduced-motion: no-preference)`,
		`[data-priority-picker] > button:nth-child(5) { animation-delay: 80ms; }`,
		`role="listbox" aria-label="Issue priority" data-priority-picker class="flex flex-wrap items-center gap-1"`,
		`name="priority" value="P0"`,
		`name="priority" value="P1"`,
		`name="priority" value="P2"`,
		`name="priority" value="P3"`,
		`name="priority" value="P4"`,
		`aria-label="Priority P1"`,
		`bg-red-600`,
		`bg-orange-500`,
		`bg-amber-500`,
		`bg-yellow-500`,
		`bg-gray-500`,
		`role="option" aria-selected="true"`,
		`type="button" hx-get="/bradley/issues/TRACK-7/panel" hx-target="#main" hx-push-url="/bradley/issues/TRACK-7" name="priority" value="P1"`,
		`rounded-full p-0.5 transition`,
		`opacity-100`,
		`opacity-40 hover:opacity-80`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("priority picker missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{
		`hx-get="/bradley/issues/TRACK-7/priority/edit"`,
		`aria-expanded="true"`,
		`data-lucide="chevron-up"`,
		`opacity-100 ring-2 ring-indigo-500`,
		`aria-label="Cancel priority change"`,
		`data-lucide="x"`,
		`title="Cancel priority change"`,
		`title="Change priority"`,
	} {
		if strings.Contains(body, notWant) {
			t.Fatalf("priority picker included %q: %s", notWant, body)
		}
	}
}

func TestUIIssuePanelRendersDescriptionEditForm(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Identifier:    "TRACK-7",
			Title:         "Design issue detail",
			Description:   "Editable description",
			Status:        model.StatusTodo,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project:         model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		EditDescription: true,
		BackHref:        "/bradley/projects/TRACK/backlog",
		BackHXGet:       "/bradley/projects/TRACK/backlog/panel",
		BackLabel:       "Backlog",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`method="post" action="/bradley/issues/TRACK-7/description"`,
		`hx-post="/bradley/issues/TRACK-7/description"`,
		`hx-target="#main"`,
		`hx-push-url="/bradley/issues/TRACK-7"`,
		`name="description"`,
		`placeholder="Description"`,
		`data-submit-shortcut="meta-enter"`,
		`aria-label="Save description"`,
		`data-lucide="check"`,
		`aria-label="Cancel editing description"`,
		`hx-get="/bradley/issues/TRACK-7/panel"`,
		`data-lucide="x"`,
		"Editable description",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("description edit form missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{
		`aria-label="Edit description"`,
		"No description.",
		"<textarea disabled",
		`title="Save description"`,
		`title="Cancel editing description"`,
	} {
		if strings.Contains(body, notWant) {
			t.Fatalf("description edit form included %q: %s", notWant, body)
		}
	}
}

func TestUIIssuePanelRendersSprintEditForm(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Identifier:    "TRACK-7",
			Title:         "Design issue detail",
			Status:        model.StatusInProgress,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project:       model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		EditSprint:    true,
		CanEditSprint: true,
		SprintInput:   "sprint-2",
		SprintError:   "Choose an active or planned sprint.",
		SprintOptions: []uiIssueSprintOption{
			{Value: "sprint-1", Label: "Active - Current Sprint - Jun 1-Jun 14"},
			{Value: "sprint-3", Label: "Planned - Next Sprint - Jun 15-Jun 28"},
		},
		BackHref:  "/bradley/projects/TRACK/sprint",
		BackHXGet: "/bradley/projects/TRACK/sprint/panel",
		BackLabel: "Sprint",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`method="post" action="/bradley/issues/TRACK-7/sprint"`,
		`hx-post="/bradley/issues/TRACK-7/sprint"`,
		`hx-target="#main"`,
		`hx-push-url="/bradley/issues/TRACK-7"`,
		`data-search`,
		`name="sprint" value="sprint-2" autocomplete="off"`,
		`data-search-input`,
		`placeholder="None"`,
		`data-lucide="search"`,
		`aria-label="Save sprint"`,
		`aria-label="Cancel editing sprint"`,
		`hx-get="/bradley/issues/TRACK-7/panel"`,
		`role="listbox" aria-label="Sprint suggestions"`,
		`data-search-option`,
		`data-value="sprint-1"`,
		`data-search-text="sprint-1 Active - Current Sprint - Jun 1-Jun 14"`,
		`Active - Current Sprint - Jun 1-Jun 14`,
		`data-value="sprint-3"`,
		`Planned - Next Sprint - Jun 15-Jun 28`,
		`Choose an active or planned sprint.`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("sprint edit form missing %q: %s", want, body)
		}
	}
	activeIndex := strings.Index(body, `data-value="sprint-1"`)
	plannedIndex := strings.Index(body, `data-value="sprint-3"`)
	if activeIndex < 0 || plannedIndex < 0 || activeIndex > plannedIndex {
		t.Fatalf("active sprint option should render before planned option: %s", body)
	}
	for _, notWant := range []string{
		`<datalist`,
		`list="issue-sprint-options"`,
		`hx-get="/bradley/issues/TRACK-7/sprint/edit"`,
		`value="sprint-4"`,
		`Completed Sprint`,
		`title="Save sprint"`,
		`title="Cancel editing sprint"`,
	} {
		if strings.Contains(body, notWant) {
			t.Fatalf("sprint edit form included %q: %s", notWant, body)
		}
	}
}

func TestUIIssuePanelDoneDisablesSprintEdit(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	sprintID := uuid.MustParse("d7fc0dbf-845c-41b4-84ab-89f487cc4a08")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Identifier:    "TRACK-7",
			Title:         "Done issue",
			Status:        model.StatusDone,
			SprintID:      &sprintID,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project:   model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		Sprint:    &model.Sprint{ID: sprintID, Name: "Completed Work"},
		BackHref:  "/bradley/projects/TRACK/sprint",
		BackHXGet: "/bradley/projects/TRACK/sprint/panel",
		BackLabel: "Sprint",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		"Completed Work",
		`aria-label="Edit sprint"`,
		"disabled",
		"cursor-not-allowed",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("done sprint row missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{
		`hx-get="/bradley/issues/TRACK-7/sprint/edit"`,
		`method="post" action="/bradley/issues/TRACK-7/sprint"`,
		`name="sprint"`,
	} {
		if strings.Contains(body, notWant) {
			t.Fatalf("done sprint row included %q: %s", notWant, body)
		}
	}
}

func TestUIIssuePanelClosedDisablesSprintEdit(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	sprintID := uuid.MustParse("d7fc0dbf-845c-41b4-84ab-89f487cc4a08")
	closeReason := model.CloseReasonWontDo
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Identifier:    "TRACK-7",
			Title:         "Closed issue",
			Status:        model.StatusClosed,
			CloseReason:   &closeReason,
			SprintID:      &sprintID,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project:   model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		Sprint:    &model.Sprint{ID: sprintID, Name: "Closed Work"},
		BackHref:  "/bradley/projects/TRACK/sprint",
		BackHXGet: "/bradley/projects/TRACK/sprint/panel",
		BackLabel: "Sprint",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		"Closed Work",
		`aria-label="Edit sprint"`,
		"disabled",
		"cursor-not-allowed",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("closed sprint row missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{
		`hx-get="/bradley/issues/TRACK-7/sprint/edit"`,
		`method="post" action="/bradley/issues/TRACK-7/sprint"`,
		`name="sprint"`,
	} {
		if strings.Contains(body, notWant) {
			t.Fatalf("closed sprint row included %q: %s", notWant, body)
		}
	}
}

func TestUIIssuePanelRendersSubIssueProgressBar(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	closeReason := model.CloseReasonWontDo
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Identifier:    "TRACK-7",
			Title:         "Parent issue",
			Status:        model.StatusTodo,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project: model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		SubIssues: []model.Issue{
			{Status: model.StatusDone},
			{Status: model.StatusClosed, CloseReason: &closeReason},
			{Status: model.StatusInProgress},
			{Status: model.StatusTodo},
		},
		BackHref:  "/bradley/projects/TRACK/backlog",
		BackHXGet: "/bradley/projects/TRACK/backlog/panel",
		BackLabel: "Backlog",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`role="img" aria-label="Sub-issue progress: 2 done, 1 in progress, 1 to do"`,
		`class="mt-1.5 flex h-1 w-full overflow-hidden rounded-full bg-slate-200 dark:bg-slate-800"`,
		`class="grid grid-cols-[6.5rem_auto_1fr_auto] items-center gap-2 border-b border-slate-100 px-4 py-2.5 text-xs`,
		`bg-emerald-500 dark:bg-emerald-400" style="width: 50.00%;"`,
		`bg-blue-400 dark:bg-blue-500" style="width: 25.00%;"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("sub-issue progress bar missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "max-w-xs") {
		t.Fatalf("sub-issue progress bar should fill the available width: %s", body)
	}
	requireInlineCount(t, body, "Sub-issues", 4)
	addIndex := strings.Index(body, `aria-label="Add sub-issue"`)
	progressIndex := strings.Index(body, `role="img" aria-label="Sub-issue progress: 2 done, 1 in progress, 1 to do"`)
	if addIndex < 0 || progressIndex < 0 || addIndex > progressIndex {
		t.Fatalf("sub-issue progress bar should render after the title row controls: %s", body)
	}
}

func TestUIIssuePanelRendersLinkedIssueProgressBar(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	doneID := uuid.MustParse("ae77b9b8-9dcf-4a18-8b69-42b97bd4a4b5")
	closedID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	progressID := uuid.MustParse("138095fe-77d7-4644-b127-d0b995757ff2")
	todoID := uuid.MustParse("2eeaf29c-ad20-4513-af41-edbb2c9abc2c")
	deletedID := uuid.MustParse("0e4c50a0-ae1a-46e9-a7b5-75989e4f3ec3")
	closeReason := model.CloseReasonInvalid
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Identifier:    "TRACK-7",
			Title:         "Issue with links",
			Status:        model.StatusTodo,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project: model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		Links: []uiIssueLinkItem{
			{
				Link:        model.IssueLink{ID: uuid.MustParse("48c98f2e-bad8-4054-89d7-5a45a68af54f"), ProjectID: projectID, Number: 1, Ref: "link-1", SourceID: issueID, TargetID: doneID, LinkType: model.LinkTypeRelatesTo, CreatedAt: when, UpdatedAt: when},
				LinkedIssue: model.Issue{ID: doneID, ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-8", Title: "Done link", Status: model.StatusDone},
				HasIssue:    true,
			},
			{
				Link:        model.IssueLink{ID: uuid.MustParse("4f6df8d9-f343-40f9-9c65-861d2967af90"), ProjectID: projectID, Number: 5, Ref: "link-5", SourceID: issueID, TargetID: closedID, LinkType: model.LinkTypeRelatesTo, CreatedAt: when, UpdatedAt: when},
				LinkedIssue: model.Issue{ID: closedID, ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-11", Title: "Closed link", Status: model.StatusClosed, CloseReason: &closeReason},
				HasIssue:    true,
			},
			{
				Link:        model.IssueLink{ID: uuid.MustParse("af63e70c-bf9d-4f80-999d-df145379ec6d"), ProjectID: projectID, Number: 2, Ref: "link-2", SourceID: issueID, TargetID: progressID, LinkType: model.LinkTypeBlocks, CreatedAt: when, UpdatedAt: when},
				LinkedIssue: model.Issue{ID: progressID, ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-9", Title: "Progress link", Status: model.StatusInProgress},
				HasIssue:    true,
			},
			{
				Link:        model.IssueLink{ID: uuid.MustParse("57bf290a-e723-42e6-8a1d-2c7ed8672646"), ProjectID: projectID, Number: 3, Ref: "link-3", SourceID: todoID, TargetID: issueID, LinkType: model.LinkTypeBlocks, CreatedAt: when, UpdatedAt: when},
				LinkedIssue: model.Issue{ID: todoID, ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-10", Title: "Todo link", Status: model.StatusTodo},
				HasIssue:    true,
			},
			{
				Link:     model.IssueLink{ID: uuid.MustParse("9b208bfe-63f0-461d-ad2b-4725106fd314"), ProjectID: projectID, Number: 4, Ref: "link-4", SourceID: issueID, TargetID: deletedID, LinkType: model.LinkTypeClones, CreatedAt: when, UpdatedAt: when},
				HasIssue: false,
			},
		},
		BackHref:  "/bradley/projects/TRACK/backlog",
		BackHXGet: "/bradley/projects/TRACK/backlog/panel",
		BackLabel: "Backlog",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`role="img" aria-label="Linked issue progress: 2 done, 1 in progress, 1 to do"`,
		`bg-emerald-500 dark:bg-emerald-400" style="width: 50.00%;"`,
		`bg-blue-400 dark:bg-blue-500" style="width: 25.00%;"`,
		"Deleted issue",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("linked issue progress missing %q: %s", want, body)
		}
	}
	requireInlineCount(t, body, "Linked issues", 5)
	addIndex := strings.Index(body, `aria-label="Add link"`)
	progressIndex := strings.Index(body, `role="img" aria-label="Linked issue progress: 2 done, 1 in progress, 1 to do"`)
	if addIndex < 0 || progressIndex < 0 || addIndex > progressIndex {
		t.Fatalf("linked issue progress bar should render after the title row controls: %s", body)
	}
}

func TestUIIssuePanelCollapsesEmptyRelationshipSections(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	linkedID := uuid.MustParse("ae77b9b8-9dcf-4a18-8b69-42b97bd4a4b5")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	basePanel := func() uiIssuePanelData {
		return uiIssuePanelData{
			Issue: model.Issue{
				ID:            issueID,
				ProjectID:     projectID,
				OwnerUsername: "bradley",
				ProjectKey:    "TRACK",
				Identifier:    "TRACK-7",
				Title:         "Parent issue",
				Status:        model.StatusTodo,
				CreatedAt:     when,
				UpdatedAt:     when,
			},
			Project:   model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
			BackHref:  "/bradley/projects/TRACK/backlog",
			BackHXGet: "/bradley/projects/TRACK/backlog/panel",
			BackLabel: "Backlog",
		}
	}
	render := func(t *testing.T, panel uiIssuePanelData) string {
		t.Helper()
		var buf bytes.Buffer
		if err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &panel); err != nil {
			t.Fatalf("ExecuteTemplate: %v", err)
		}
		return buf.String()
	}
	requireHeadingOrder := func(t *testing.T, body, first, second string) {
		t.Helper()
		firstIndex := strings.Index(body, ">"+first+"</h2>")
		secondIndex := strings.Index(body, ">"+second+"</h2>")
		if firstIndex < 0 || secondIndex < 0 || firstIndex > secondIndex {
			t.Fatalf("heading %q should render before %q: %s", first, second, body)
		}
	}

	emptyBody := render(t, basePanel())
	for _, notWant := range []string{"No sub-issues.", "No linked issues."} {
		if strings.Contains(emptyBody, notWant) {
			t.Fatalf("empty relationship section should not render %q: %s", notWant, emptyBody)
		}
	}
	if !strings.Contains(emptyBody, `class="flex flex-wrap gap-6"`) {
		t.Fatalf("relationship sections should share a wrapping row: %s", emptyBody)
	}
	if got := strings.Count(emptyBody, `w-full sm:w-1/3`); got != 2 {
		t.Fatalf("both empty relationship sections should render third-width, got %d: %s", got, emptyBody)
	}
	emptySubClass := sectionClassForHeading(t, emptyBody, "Sub-issues")
	emptyLinkClass := sectionClassForHeading(t, emptyBody, "Linked issues")
	for _, cls := range []string{emptySubClass, emptyLinkClass} {
		if !strings.Contains(cls, `w-full sm:w-1/3`) {
			t.Fatalf("empty relationship section should be third-width, got class %q: %s", cls, emptyBody)
		}
	}
	requireHeadingOrder(t, emptyBody, "Sub-issues", "Linked issues")

	populatedLinksPanel := basePanel()
	populatedLinksPanel.Links = []uiIssueLinkItem{{
		Link:        model.IssueLink{ID: uuid.MustParse("48c98f2e-bad8-4054-89d7-5a45a68af54f"), ProjectID: projectID, Number: 1, Ref: "link-1", SourceID: issueID, TargetID: linkedID, LinkType: model.LinkTypeRelatesTo, CreatedAt: when, UpdatedAt: when},
		LinkedIssue: model.Issue{ID: linkedID, ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-8", Title: "Linked work", Status: model.StatusTodo},
		HasIssue:    true,
	}}
	populatedLinksBody := render(t, populatedLinksPanel)
	populatedSubClass := sectionClassForHeading(t, populatedLinksBody, "Sub-issues")
	populatedLinkClass := sectionClassForHeading(t, populatedLinksBody, "Linked issues")
	if !strings.Contains(populatedSubClass, `w-full sm:w-1/3`) {
		t.Fatalf("empty sub-issues section should sit below populated links at third width, got %q: %s", populatedSubClass, populatedLinksBody)
	}
	if !strings.Contains(populatedLinkClass, "w-full") || strings.Contains(populatedLinkClass, "sm:w-[calc") {
		t.Fatalf("populated linked issues section should remain full width above the empty one, got %q: %s", populatedLinkClass, populatedLinksBody)
	}
	requireHeadingOrder(t, populatedLinksBody, "Linked issues", "Sub-issues")

	populatedSubIssuesPanel := basePanel()
	populatedSubIssuesPanel.SubIssues = []model.Issue{{
		ID:            uuid.MustParse("1e533f98-310a-4090-a8ff-7cc4c4a69df2"),
		ProjectID:     projectID,
		OwnerUsername: "bradley",
		ProjectKey:    "TRACK",
		Identifier:    "TRACK-8",
		Title:         "Existing child",
		Status:        model.StatusTodo,
		Priority:      model.PriorityP2,
		CreatedAt:     when,
		UpdatedAt:     when,
	}}
	populatedSubIssuesBody := render(t, populatedSubIssuesPanel)
	populatedSubIssuesClass := sectionClassForHeading(t, populatedSubIssuesBody, "Sub-issues")
	populatedEmptyLinkClass := sectionClassForHeading(t, populatedSubIssuesBody, "Linked issues")
	if !strings.Contains(populatedSubIssuesClass, "w-full") || strings.Contains(populatedSubIssuesClass, "sm:w-[calc") {
		t.Fatalf("populated sub-issues section should remain full width above the empty one, got %q: %s", populatedSubIssuesClass, populatedSubIssuesBody)
	}
	if !strings.Contains(populatedEmptyLinkClass, `w-full sm:w-1/3`) {
		t.Fatalf("empty linked issues section should sit below populated sub-issues at third width, got %q: %s", populatedEmptyLinkClass, populatedSubIssuesBody)
	}
	requireHeadingOrder(t, populatedSubIssuesBody, "Sub-issues", "Linked issues")
}

func TestUIIssuePanelRendersSubIssueComposerAtTop(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	childID := uuid.MustParse("1e533f98-310a-4090-a8ff-7cc4c4a69df2")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Identifier:    "TRACK-7",
			Title:         "Parent issue",
			Status:        model.StatusTodo,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project:       model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		AddSubIssue:   true,
		SubIssueTitle: "Draft child",
		SubIssueError: "Title required, max 200 chars.",
		SubIssues: []model.Issue{{
			ID:            childID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Identifier:    "TRACK-8",
			Title:         "Existing child",
			Status:        model.StatusTodo,
			Priority:      model.PriorityP2,
			CreatedAt:     when,
			UpdatedAt:     when,
		}},
		BackHref:  "/bradley/projects/TRACK/backlog",
		BackHXGet: "/bradley/projects/TRACK/backlog/panel",
		BackLabel: "Backlog",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`aria-label="Cancel adding sub-issue"`,
		`method="post" action="/bradley/issues/TRACK-7/sub-issues"`,
		`hx-post="/bradley/issues/TRACK-7/sub-issues"`,
		`name="title" value="Draft child" autofocus placeholder="Title"`,
		`aria-label="Create sub-issue"`,
		`data-lucide="check"`,
		"Title required, max 200 chars.",
		"Existing child",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("sub-issue composer missing %q: %s", want, body)
		}
	}
	formIndex := strings.Index(body, `name="title" value="Draft child"`)
	childIndex := strings.Index(body, "Existing child")
	if formIndex < 0 || childIndex < 0 || formIndex > childIndex {
		t.Fatalf("sub-issue composer should render before the list rows: %s", body)
	}
	for _, notWant := range []string{
		`aria-label="Add sub-issue"`,
		`hx-get="/bradley/issues/TRACK-7/sub-issues/new"`,
	} {
		if strings.Contains(body, notWant) {
			t.Fatalf("sub-issue composer included closed-state control %q: %s", notWant, body)
		}
	}
}

func TestUIIssuePanelRendersAddLinkForm(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Identifier:    "TRACK-7",
			Title:         "Design issue detail",
			Status:        model.StatusTodo,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project:      model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		AddLink:      true,
		LinkTarget:   "TRACK-8",
		LinkRelation: "blocked_by",
		LinkError:    "Linked issue required.",
		BackHref:     "/bradley/projects/TRACK/backlog",
		BackHXGet:    "/bradley/projects/TRACK/backlog/panel",
		BackLabel:    "Backlog",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`method="post" action="/bradley/issues/TRACK-7/links"`,
		`hx-post="/bradley/issues/TRACK-7/links"`,
		`hx-target="#main"`,
		`hx-push-url="/bradley/issues/TRACK-7"`,
		`name="relation" aria-label="Link relationship"`,
		`value="relates_to"`,
		`value="blocks"`,
		`value="blocked_by" selected`,
		`value="duplicates"`,
		`value="duplicated_by"`,
		`value="clones"`,
		`value="cloned_by"`,
		`name="target_issue" value="TRACK-8" placeholder="TRACK-12"`,
		`aria-label="Save link"`,
		`aria-label="Cancel adding link"`,
		"Linked issue required.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("add link form missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{
		`hx-get="/bradley/issues/TRACK-7/links/new"`,
		"No linked issues.",
		`title="Save link"`,
		`title="Cancel adding link"`,
	} {
		if strings.Contains(body, notWant) {
			t.Fatalf("add link form included %q: %s", notWant, body)
		}
	}
}

func TestUIIssuePanelRendersLinkEditForm(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	linkedID := uuid.MustParse("ae77b9b8-9dcf-4a18-8b69-42b97bd4a4b5")
	linkID := uuid.MustParse("48c98f2e-bad8-4054-89d7-5a45a68af54f")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Identifier:    "TRACK-7",
			Title:         "Design issue detail",
			Status:        model.StatusTodo,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project: model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		Links: []uiIssueLinkItem{{
			Link:        model.IssueLink{ID: linkID, ProjectID: projectID, Number: 3, Ref: "link-3", SourceID: linkedID, TargetID: issueID, LinkType: model.LinkTypeClones, CreatedAt: when, UpdatedAt: when},
			LinkedIssue: model.Issue{ID: linkedID, ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-8", Title: "Linked work", Status: model.StatusInProgress},
			HasIssue:    true,
		}},
		EditLinkID:   linkID,
		LinkTarget:   "TRACK-8",
		LinkRelation: "cloned_by",
		LinkError:    "Link already exists or cannot be updated.",
		BackHref:     "/bradley/projects/TRACK/backlog",
		BackHXGet:    "/bradley/projects/TRACK/backlog/panel",
		BackLabel:    "Backlog",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`method="post" action="/bradley/issues/TRACK-7/links/link-3"`,
		`hx-post="/bradley/issues/TRACK-7/links/link-3"`,
		`hx-target="#main"`,
		`hx-push-url="/bradley/issues/TRACK-7"`,
		`name="relation" aria-label="Link relationship"`,
		`value="cloned_by" selected`,
		`name="target_issue" value="TRACK-8" placeholder="TRACK-12"`,
		`aria-label="Save link"`,
		`aria-label="Cancel editing link"`,
		`aria-label="Remove link"`,
		`hx-post="/bradley/issues/TRACK-7/links/link-3/delete"`,
		`data-lucide="trash-2"`,
		"Link already exists or cannot be updated.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("edit link form missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{
		`hx-get="/bradley/issues/TRACK-7/links/link-3/edit"`,
		`title="Remove link"`,
		`title="Cancel editing link"`,
	} {
		if strings.Contains(body, notWant) {
			t.Fatalf("edit link form included %q: %s", notWant, body)
		}
	}
}

func TestUIIssueBackLink(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	sprintID := uuid.MustParse("d7fc0dbf-845c-41b4-84ab-89f487cc4a08")
	parentID := uuid.MustParse("2eeaf29c-ad20-4513-af41-edbb2c9abc2c")
	project := model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"}
	baseIssue := model.Issue{ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-7", SprintID: &sprintID}

	tests := []struct {
		name      string
		issue     model.Issue
		sprint    *model.Sprint
		parent    *model.Issue
		wantHref  string
		wantHXGet string
		wantLabel string
	}{
		{
			name:      "active sprint",
			issue:     baseIssue,
			sprint:    &model.Sprint{ID: sprintID, ProjectID: projectID, Status: model.SprintStatusActive},
			wantHref:  "/bradley/projects/TRACK/sprint",
			wantHXGet: "/bradley/projects/TRACK/sprint/panel",
			wantLabel: "Sprint",
		},
		{
			name:      "planned sprint",
			issue:     baseIssue,
			sprint:    &model.Sprint{ID: sprintID, ProjectID: projectID, Status: model.SprintStatusPlanned},
			wantHref:  "/bradley/projects/TRACK/planned",
			wantHXGet: "/bradley/projects/TRACK/planned/panel",
			wantLabel: "Planned",
		},
		{
			name:      "backlog issue",
			issue:     model.Issue{ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-8"},
			wantHref:  "/bradley/projects/TRACK/all",
			wantHXGet: "/bradley/projects/TRACK/all/panel",
			wantLabel: "All",
		},
		{
			name:      "completed sprint",
			issue:     baseIssue,
			sprint:    &model.Sprint{ID: sprintID, ProjectID: projectID, Status: model.SprintStatusCompleted},
			wantHref:  "/bradley/projects/TRACK/sprint",
			wantHXGet: "/bradley/projects/TRACK/sprint/panel",
			wantLabel: "Sprint",
		},
		{
			name:      "missing sprint",
			issue:     baseIssue,
			wantHref:  "/bradley/projects/TRACK/sprint",
			wantHXGet: "/bradley/projects/TRACK/sprint/panel",
			wantLabel: "Sprint",
		},
		{
			name:      "parent issue",
			issue:     model.Issue{ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-9", ParentIssueID: &parentID},
			parent:    &model.Issue{ID: parentID, ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-1"},
			wantHref:  "/bradley/issues/TRACK-1",
			wantHXGet: "/bradley/issues/TRACK-1/panel",
			wantLabel: "Parent issue",
		},
	}

	for _, tt := range tests {
		href, hxGet, label := uiIssueBackLink(project, tt.issue, tt.parent, tt.sprint)
		if href != tt.wantHref || hxGet != tt.wantHXGet || label != tt.wantLabel {
			t.Fatalf("%s: got (%q, %q, %q), want (%q, %q, %q)", tt.name, href, hxGet, label, tt.wantHref, tt.wantHXGet, tt.wantLabel)
		}
	}
}

func TestUIIssueLinkLabel(t *testing.T) {
	t.Parallel()

	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	otherID := uuid.MustParse("ae77b9b8-9dcf-4a18-8b69-42b97bd4a4b5")

	tests := []struct {
		name string
		link model.IssueLink
		want string
	}{
		{name: "blocks outgoing", link: model.IssueLink{SourceID: issueID, TargetID: otherID, LinkType: model.LinkTypeBlocks}, want: "Blocks"},
		{name: "blocks incoming", link: model.IssueLink{SourceID: otherID, TargetID: issueID, LinkType: model.LinkTypeBlocks}, want: "Blocked by"},
		{name: "duplicates outgoing", link: model.IssueLink{SourceID: issueID, TargetID: otherID, LinkType: model.LinkTypeDuplicates}, want: "Duplicates"},
		{name: "duplicates incoming", link: model.IssueLink{SourceID: otherID, TargetID: issueID, LinkType: model.LinkTypeDuplicates}, want: "Duplicated by"},
		{name: "relates outgoing", link: model.IssueLink{SourceID: issueID, TargetID: otherID, LinkType: model.LinkTypeRelatesTo}, want: "Relates to"},
		{name: "relates incoming", link: model.IssueLink{SourceID: otherID, TargetID: issueID, LinkType: model.LinkTypeRelatesTo}, want: "Relates to"},
		{name: "clones outgoing", link: model.IssueLink{SourceID: issueID, TargetID: otherID, LinkType: model.LinkTypeClones}, want: "Clones"},
		{name: "clones incoming", link: model.IssueLink{SourceID: otherID, TargetID: issueID, LinkType: model.LinkTypeClones}, want: "Cloned by"},
		{name: "unknown", link: model.IssueLink{SourceID: issueID, TargetID: otherID, LinkType: model.LinkType("custom")}, want: "custom"},
	}

	for _, tt := range tests {
		if got := uiIssueLinkLabel(tt.link, issueID); got != tt.want {
			t.Fatalf("%s: uiIssueLinkLabel = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestUIIssueLinkRelationParams(t *testing.T) {
	t.Parallel()

	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	otherID := uuid.MustParse("ae77b9b8-9dcf-4a18-8b69-42b97bd4a4b5")

	tests := []struct {
		name       string
		relation   string
		wantSource uuid.UUID
		wantTarget uuid.UUID
		wantType   model.LinkType
	}{
		{name: "blocks", relation: "blocks", wantSource: issueID, wantTarget: otherID, wantType: model.LinkTypeBlocks},
		{name: "blocked by", relation: "blocked_by", wantSource: otherID, wantTarget: issueID, wantType: model.LinkTypeBlocks},
		{name: "duplicates", relation: "duplicates", wantSource: issueID, wantTarget: otherID, wantType: model.LinkTypeDuplicates},
		{name: "duplicated by", relation: "duplicated_by", wantSource: otherID, wantTarget: issueID, wantType: model.LinkTypeDuplicates},
		{name: "relates", relation: "relates_to", wantSource: issueID, wantTarget: otherID, wantType: model.LinkTypeRelatesTo},
		{name: "clones", relation: "clones", wantSource: issueID, wantTarget: otherID, wantType: model.LinkTypeClones},
		{name: "cloned by", relation: "cloned_by", wantSource: otherID, wantTarget: issueID, wantType: model.LinkTypeClones},
	}

	for _, tt := range tests {
		sourceID, targetID, linkType, ok := uiIssueLinkRelationParams(issueID, otherID, tt.relation)
		if !ok || sourceID != tt.wantSource || targetID != tt.wantTarget || linkType != tt.wantType {
			t.Fatalf("%s: got (%s, %s, %s, %v), want (%s, %s, %s, true)", tt.name, sourceID, targetID, linkType, ok, tt.wantSource, tt.wantTarget, tt.wantType)
		}
	}
	if _, _, _, ok := uiIssueLinkRelationParams(issueID, otherID, "blocks_by_magic"); ok {
		t.Fatalf("invalid relation unexpectedly accepted")
	}

	if got := uiIssueLinkRelation(model.IssueLink{SourceID: otherID, TargetID: issueID, LinkType: model.LinkTypeBlocks}, issueID); got != "blocked_by" {
		t.Fatalf("incoming blocks relation = %q, want blocked_by", got)
	}
	if got := uiIssueLinkRelation(model.IssueLink{SourceID: otherID, TargetID: issueID, LinkType: model.LinkTypeDuplicates}, issueID); got != "duplicated_by" {
		t.Fatalf("incoming duplicates relation = %q, want duplicated_by", got)
	}
	if got := uiIssueLinkRelation(model.IssueLink{SourceID: otherID, TargetID: issueID, LinkType: model.LinkTypeClones}, issueID); got != "cloned_by" {
		t.Fatalf("incoming clones relation = %q, want cloned_by", got)
	}
	if got := uiIssueLinkRelation(model.IssueLink{SourceID: otherID, TargetID: issueID, LinkType: model.LinkTypeRelatesTo}, issueID); got != "relates_to" {
		t.Fatalf("incoming relates relation = %q, want relates_to", got)
	}
	if got := uiIssueLinkRelation(model.IssueLink{SourceID: issueID, TargetID: otherID, LinkType: model.LinkTypeBlocks}, issueID); got != "blocks" {
		t.Fatalf("outgoing blocks relation = %q, want blocks", got)
	}
}

func TestUIIssueLinkRef(t *testing.T) {
	t.Parallel()

	if got := uiIssueLinkRef(model.IssueLink{Ref: "link-7", Number: 3}); got != "link-7" {
		t.Fatalf("explicit ref = %q, want link-7", got)
	}
	if got := uiIssueLinkRef(&model.IssueLink{Number: 3}); got != "link-3" {
		t.Fatalf("number fallback = %q, want link-3", got)
	}
	if got := uiIssueLinkRef((*model.IssueLink)(nil)); got != "link-0" {
		t.Fatalf("nil fallback = %q, want link-0", got)
	}
}

func TestUIProjectPanelRendersCohesiveHeaderAndAboutDetails(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	selectedID := uuid.MustParse("23f14acb-6a57-4035-a046-33e93ffbd5bb")
	project := model.Project{
		ID:            projectID,
		OwnerUsername: "bradley",
		Key:           "TRACK",
		Name:          "Track Slash",
		Description:   "Fast issue tracking.",
		CreatedAt:     time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 6, 2, 10, 45, 0, 0, time.UTC),
	}
	selected := []uuid.UUID{selectedID}
	assignees := []model.ProjectAssignee{
		{ID: selectedID, Username: "ada", Name: "Ada Lovelace"},
	}
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "project-panel", &uiProjectPanelData{
		Project:              project,
		View:                 "about",
		ProjectTabs:          uiProjectTabs(project, "about", selected),
		AssigneeFilters:      uiProjectAssigneeFilters(project, "about", assignees, selected),
		AssigneeFilterActive: true,
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	projectHeaderStart := strings.Index(body, `<header class="rounded-lg`)
	if projectHeaderStart < 0 {
		t.Fatalf("project panel missing title card: %s", body)
	}
	headerEnd := strings.Index(body[projectHeaderStart:], "</header>")
	if headerEnd < 0 {
		t.Fatalf("project panel missing title card header: %s", body)
	}
	headerEnd += projectHeaderStart
	header := body[projectHeaderStart:headerEnd]
	backLink := strings.Index(body, `href="/projects"`)
	if backLink < 0 {
		t.Fatalf("project panel missing back link to projects: %s", body)
	}
	if backLink > projectHeaderStart {
		t.Fatalf("back link rendered inside or below title card: %s", body)
	}
	tabNav := strings.Index(body, `aria-label="Project views"`)
	if tabNav < 0 {
		t.Fatalf("project panel missing project view tabs: %s", body)
	}
	if tabNav < projectHeaderStart || tabNav > headerEnd {
		t.Fatalf("project view tabs should render inside title card: %s", body)
	}
	if strings.Contains(header, "Fast issue tracking.") {
		t.Fatalf("project description should not render inside title card: %s", body)
	}
	for _, want := range []string{"TRACK", "font-mono text-sm font-semibold uppercase", "Track Slash", "About", "Sprint", "Planned", "All", `data-lucide="person-standing"`, `data-lucide="info"`, `aria-current="page"`, `aria-label="Project actions"`, `data-lucide="more-horizontal"`, `href="/bradley/projects/TRACK/deleted"`, `hx-get="/bradley/projects/TRACK/deleted/panel"`, `data-lucide="trash-2"`, "Deleted issues"} {
		if !strings.Contains(header, want) {
			t.Fatalf("project title card missing markup %q: %s", want, body)
		}
	}
	for _, want := range []string{"Description", "Fast issue tracking.", "Details", "Owner", "@bradley", "Created", "Jun 1, 2026 09:30", "Updated", "Jun 2, 2026 10:45"} {
		if !strings.Contains(body, want) {
			t.Fatalf("project about view missing markup %q: %s", want, body)
		}
	}
	for _, notWant := range []string{`aria-label="Assignee filter"`, `assignee_id=`} {
		if strings.Contains(header, notWant) {
			t.Fatalf("project title card preserved about filter state %q: %s", notWant, body)
		}
		if strings.Contains(body, notWant) {
			t.Fatalf("project about view rendered assignee filter state %q: %s", notWant, body)
		}
	}
	aboutIdx := strings.Index(body, `href="/bradley/projects/TRACK/about"`)
	sprintsIdx := strings.Index(body, `href="/bradley/projects/TRACK/sprint"`)
	plannedIdx := strings.Index(body, `href="/bradley/projects/TRACK/planned"`)
	allIdx := strings.Index(body, `href="/bradley/projects/TRACK/all"`)
	if aboutIdx < 0 || sprintsIdx < 0 || plannedIdx < 0 || allIdx < 0 || sprintsIdx > plannedIdx || plannedIdx > allIdx || allIdx > aboutIdx {
		t.Fatalf("project tabs not ordered sprints, planned, all, about: sprints=%d planned=%d all=%d about=%d body=%s", sprintsIdx, plannedIdx, allIdx, aboutIdx, body)
	}
	if strings.Contains(body, "Back to projects") {
		t.Fatalf("project back link uses verbose label: %s", body)
	}
	tabEnd := strings.Index(body[tabNav:], "</nav>")
	if tabEnd < 0 {
		t.Fatalf("project tabs missing nav close: %s", body)
	}
	tabMarkup := body[tabNav : tabNav+tabEnd]
	if strings.Contains(tabMarkup, "Deleted") || strings.Contains(tabMarkup, `/deleted`) {
		t.Fatalf("deleted rendered as project tab: %s", body)
	}
	for _, want := range []string{"Projects", `hx-get="/projects/panel"`, "About", "Sprint", "Planned", "All", `data-lucide="person-standing"`, `data-lucide="calendar-range"`, `data-lucide="list-filter"`, "border-b-4", `aria-current="page"`, `href="/bradley/projects/TRACK/about"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("project panel missing tab markup %q: %s", want, body)
		}
	}
}

func TestUIProjectPanelRendersAssigneeFilterAndSprintGoal(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	selectedID := uuid.MustParse("23f14acb-6a57-4035-a046-33e93ffbd5bb")
	otherID := uuid.MustParse("ae77b9b8-9dcf-4a18-8b69-42b97bd4a4b5")
	project := model.Project{
		ID:            projectID,
		OwnerUsername: "bradley",
		Key:           "TRACK",
		Name:          "Track Slash",
	}
	selected := []uuid.UUID{selectedID}
	assignees := []model.ProjectAssignee{
		{ID: selectedID, Username: "ada", Name: "Ada Lovelace"},
		{ID: otherID, Username: "grace", Name: "Grace Hopper"},
	}
	columns := uiIssueColumns()
	columns[0].Issues = append(columns[0].Issues, uiIssueItem{
		Issue:   model.Issue{ID: uuid.MustParse("adbf2723-a44d-4b43-a3d0-e12276fa59c0"), ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-10", Title: "Todo count issue", Status: model.StatusTodo},
		Project: project,
	})
	columns[1].Issues = append(columns[1].Issues, uiIssueItem{
		Issue:   model.Issue{ID: uuid.MustParse("af63e70c-bf9d-4f80-999d-df145379ec6d"), ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-11", Title: "Progress count issue", Status: model.StatusInProgress},
		Project: project,
	})

	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "project-panel", &uiProjectPanelData{
		Project:              project,
		View:                 "sprint",
		ProjectTabs:          uiProjectTabs(project, "sprint", selected),
		AssigneeFilters:      uiProjectAssigneeFilters(project, "sprint", assignees, selected),
		AssigneeFilterActive: true,
		ClearAssigneeHref:    uiProjectViewPath(project, "sprint"),
		ClearAssigneeHXGet:   uiProjectPanelPath(project, "sprint"),
		ClearAssigneeHXPush:  uiProjectViewPath(project, "sprint"),
		ActiveSprint: &model.Sprint{
			ID:        uuid.MustParse("d7fc0dbf-845c-41b4-84ab-89f487cc4a08"),
			ProjectID: projectID,
			Name:      "Current Sprint",
			Goal:      "Ship filtering\nPolish sprint goals",
			StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			EndDate:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
		},
		SprintColumns: columns,
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`aria-label="Assignee filter"`,
		`aria-pressed="true"`,
		`aria-pressed="false"`,
		`aria-label="Toggle Ada Lovelace"`,
		`aria-label="Toggle Grace Hopper"`,
		"AL",
		"GH",
		"Ship filtering\nPolish sprint goals",
		"Todo count issue",
		"Progress count issue",
		"whitespace-pre-wrap",
		`href="/bradley/projects/TRACK/sprint?assignee_id=23f14acb-6a57-4035-a046-33e93ffbd5bb"`,
		`hx-get="/bradley/projects/TRACK/planned/panel"`,
		`hx-get="/bradley/projects/TRACK/all/panel"`,
		`href="/bradley/projects/TRACK/sprint"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("project panel missing %q: %s", want, body)
		}
	}
	filterIdx := strings.Index(body, `aria-label="Assignee filter"`)
	tabIdx := strings.Index(body, `aria-label="Project views"`)
	if filterIdx < 0 || tabIdx < 0 || filterIdx < tabIdx {
		t.Fatalf("assignee filter should render below project tabs: filter=%d tabs=%d body=%s", filterIdx, tabIdx, body)
	}
	for _, notWant := range []string{`href="/bradley/projects/TRACK/about?assignee_id=`, `hx-get="/bradley/projects/TRACK/about/panel?assignee_id=`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("about tab should not preserve assignee filter %q: %s", notWant, body)
		}
	}
	for _, notWant := range []string{`href="/bradley/projects/TRACK/planned?assignee_id=`, `href="/bradley/projects/TRACK/all?assignee_id=`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("project work tabs should not preserve sprint assignee filter %q: %s", notWant, body)
		}
	}
	requireInlineCount(t, body, "Sprint", 2)
	requireInlineCount(t, body, "To do", 1)
	requireInlineCount(t, body, "In progress", 1)
	requireInlineCount(t, body, "Done", 0)
}

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
	err = uiTemplates.ExecuteTemplate(&buf, "project-panel", &uiProjectPanelData{
		Project:              project,
		View:                 "all",
		ProjectTabs:          uiProjectTabs(project, "all", nil),
		AssigneeFilters:      uiProjectAllAssigneeFilters(project, []model.ProjectAssignee{{ID: assigneeID, Username: "ada", Name: "Ada Lovelace"}}, allQuery),
		AssigneeFilterActive: true,
		ClearAssigneeHref:    uiProjectAllViewPath(project, clearAssigneeQuery),
		ClearAssigneeHXGet:   uiProjectAllPanelPath(project, clearAssigneeQuery),
		ClearAssigneeHXPush:  uiProjectAllViewPath(project, clearAssigneeQuery),
		AllIssues:            allIssues,
		AllIssuePage: uiProjectAllIssuePageData{
			Issues:    allIssues,
			NextHXGet: uiProjectAllPagePath(project, nextQuery),
		},
		AllStatusFilters:   uiProjectAllStatusFilters(project, allQuery),
		AllPriorityFilters: uiProjectAllPriorityFilters(project, allQuery),
		AllSortOptions:     uiProjectAllSortOptions(project, allQuery),
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
		"Any",
		"Anyone",
		"Done",
		"To do",
		"Updated",
		"Created",
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
		{name: "work issue card list", template: "issue-card-list", data: []uiIssueItem{{Issue: issue, Project: project}}},
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
		if strings.Contains(body, "rounded-md border border-slate-200 bg-slate-50 px-2.5 py-1 text-xs text-slate-600") {
			t.Fatalf("%s still renders neutral row status: %s", tt.name, body)
		}
	}
}

func TestUIDueBadgeClassOverdueOnlyForOpenPastIssues(t *testing.T) {
	t.Parallel()

	past, err := model.ParseDate("2026-06-19")
	if err != nil {
		t.Fatalf("ParseDate past: %v", err)
	}
	future, err := model.ParseDate("2026-06-21")
	if err != nil {
		t.Fatalf("ParseDate future: %v", err)
	}
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)

	if !uiIssueOverdue(model.Issue{Status: model.StatusTodo, DueDate: &past}, now) {
		t.Fatal("open past issue should be overdue")
	}
	if uiIssueOverdue(model.Issue{Status: model.StatusDone, DueDate: &past}, now) {
		t.Fatal("done past issue should not be overdue")
	}
	if uiIssueOverdue(model.Issue{Status: model.StatusClosed, DueDate: &past}, now) {
		t.Fatal("closed past issue should not be overdue")
	}
	if uiIssueOverdue(model.Issue{Status: model.StatusTodo, DueDate: &future}, now) {
		t.Fatal("future issue should not be overdue")
	}
	if uiIssueOverdue(model.Issue{Status: model.StatusTodo}, now) {
		t.Fatal("issue without due date should not be overdue")
	}

	today, err := model.ParseDate("2026-06-20")
	if err != nil {
		t.Fatalf("ParseDate today: %v", err)
	}
	sixDays, err := model.ParseDate("2026-06-26")
	if err != nil {
		t.Fatalf("ParseDate six days: %v", err)
	}
	sevenDays, err := model.ParseDate("2026-06-27")
	if err != nil {
		t.Fatalf("ParseDate seven days: %v", err)
	}
	for _, issue := range []model.Issue{
		{Status: model.StatusTodo, DueDate: &today},
		{Status: model.StatusTodo, DueDate: &sixDays},
	} {
		if !uiIssueDueSoon(issue, now) {
			t.Fatalf("issue should be due soon: %+v", issue)
		}
	}
	if uiIssueDueSoon(model.Issue{Status: model.StatusTodo, DueDate: &sevenDays}, now) {
		t.Fatal("seven days out should not be due soon")
	}
	if days, ok := uiIssueDueDays(model.Issue{Status: model.StatusTodo, DueDate: &sixDays}, now); !ok || days != 6 {
		t.Fatalf("days = %d, ok = %v, want 6 true", days, ok)
	}
	if days, ok := uiIssueDueDays(model.Issue{Status: model.StatusDone, DueDate: &today}, now); ok || days != 0 {
		t.Fatalf("done days = %d, ok = %v, want 0 false", days, ok)
	}
	if days, ok := uiIssueDueDays(model.Issue{Status: model.StatusClosed, DueDate: &today}, now); ok || days != 0 {
		t.Fatalf("closed days = %d, ok = %v, want 0 false", days, ok)
	}
}

func TestUIDueDateFormatHelpers(t *testing.T) {
	t.Parallel()

	if uiDueDateValue(nil) != "" || uiDueDateShort(nil) != "" || uiDueDateFull(nil) != "" {
		t.Fatal("nil due date helpers should return empty strings")
	}
	dueDate, err := model.ParseDate("2026-06-24")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	if got := uiDueDateValue(&dueDate); got != "2026-06-24" {
		t.Fatalf("value = %q", got)
	}
	if got := uiDueDateShort(&dueDate); got != "Jun 24" {
		t.Fatalf("short = %q", got)
	}
	if got := uiDueDateFull(&dueDate); got != "Jun 24, 2026" {
		t.Fatalf("full = %q", got)
	}
	overdueDate, err := model.ParseDate("2020-01-01")
	if err != nil {
		t.Fatalf("ParseDate overdue: %v", err)
	}
	if got := uiDueBadgeClass(model.Issue{Status: model.StatusTodo, DueDate: &overdueDate}); !strings.Contains(got, "border-rose-200") {
		t.Fatalf("overdue class = %q", got)
	}
	today, err := model.ParseDate(time.Now().Format(model.DateLayout))
	if err != nil {
		t.Fatalf("ParseDate today: %v", err)
	}
	if got := uiDueBadgeClass(model.Issue{Status: model.StatusTodo, DueDate: &today}); !strings.Contains(got, "border-amber-200") {
		t.Fatalf("today class = %q", got)
	}
	if got := uiDueBadgeIcon(model.Issue{Status: model.StatusTodo, DueDate: &today}); got != "clock" {
		t.Fatalf("today icon = %q", got)
	}
	if got := uiDueBadgeLabel(model.Issue{Status: model.StatusTodo, DueDate: &today}); got != "Today" {
		t.Fatalf("today label = %q", got)
	}
	tomorrow := model.DateFromTime(time.Now().AddDate(0, 0, 1))
	if got := uiDueBadgeLabel(model.Issue{Status: model.StatusTodo, DueDate: &tomorrow}); got != "1 day" {
		t.Fatalf("tomorrow label = %q", got)
	}
	sixDays := model.DateFromTime(time.Now().AddDate(0, 0, 6))
	if got := uiDueBadgeLabel(model.Issue{Status: model.StatusTodo, DueDate: &sixDays}); got != "6 days" {
		t.Fatalf("six-day label = %q", got)
	}
	if got := uiDueBadgeClass(model.Issue{}); !strings.Contains(got, "border-slate-200") {
		t.Fatalf("neutral class = %q", got)
	}
	if got := uiDueBadgeIcon(model.Issue{}); got != "calendar" {
		t.Fatalf("neutral icon = %q", got)
	}
	if got := uiDueBadgeLabel(model.Issue{}); got != "" {
		t.Fatalf("nil label = %q", got)
	}
}

func TestUITabBarComponentRendersReusableTabs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "tab-bar", uiTabBarData{
		Label: "Example views",
		Items: []uiTabItem{
			{Label: "One", Icon: "circle", Href: "/one", HXGet: "/one/panel", HXTarget: "#main", HXPushURL: "/one", Active: true},
			{Label: "Two", Icon: "square", Href: "/two", HXGet: "/two/panel", HXTarget: "#main", HXPushURL: "/two"},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{`aria-label="Example views"`, "flex flex-wrap", "border-b-4", `data-lucide="circle"`, `href="/one"`, `hx-get="/one/panel"`, `aria-current="page"`, `href="/two"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("tab bar missing markup %q: %s", want, body)
		}
	}
	if strings.Contains(body, "overflow-x-auto") {
		t.Fatalf("tab bar should not force horizontal scrolling: %s", body)
	}
}
