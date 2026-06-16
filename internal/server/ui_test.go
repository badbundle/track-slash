package server

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
)

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
		{status: model.StatusInProgress, want: "border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-500/40 dark:bg-amber-950/40 dark:text-amber-200"},
		{status: model.StatusDone, want: "border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-500/40 dark:bg-emerald-950/40 dark:text-emerald-200"},
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
		{status: model.StatusInProgress, want: "bg-amber-50/45 hover:bg-amber-50 dark:bg-amber-950/15 dark:hover:bg-amber-950/30"},
		{status: model.StatusDone, want: "bg-emerald-50/45 hover:bg-emerald-50 dark:bg-emerald-950/15 dark:hover:bg-emerald-950/30"},
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
		{status: model.StatusInProgress, want: "bg-amber-50/45 dark:bg-amber-950/15"},
		{status: model.StatusDone, want: "bg-emerald-50/45 dark:bg-emerald-950/15"},
		{status: model.Status("custom"), want: "bg-white dark:bg-slate-900"},
	}

	for _, tt := range tests {
		if got := uiStatusSurfaceClass(tt.status); got != tt.want {
			t.Fatalf("uiStatusSurfaceClass(%q) = %q, want %q", tt.status, got, tt.want)
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
		{name: "issue", raw: "/bradley/issues/TRACK-7", want: "/bradley/issues/TRACK-7"},
		{name: "issue panel with query", raw: "/bradley/issues/TRACK-7/panel?x=1", want: "/bradley/issues/TRACK-7/panel?x=1"},
		{name: "issue description edit with query", raw: "/bradley/issues/TRACK-7/description/edit?x=1", want: "/bradley/issues/TRACK-7/description/edit?x=1"},
		{name: "issue status edit", raw: "/bradley/issues/TRACK-7/status/edit", want: "/bradley/issues/TRACK-7/status/edit"},
		{name: "issue priority edit", raw: "/bradley/issues/TRACK-7/priority/edit", want: "/bradley/issues/TRACK-7/priority/edit"},
		{name: "issue link add", raw: "/bradley/issues/TRACK-7/links/new?x=1", want: "/bradley/issues/TRACK-7/links/new?x=1"},
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
		{name: "project about panel with query", raw: "/bradley/projects/TRACK/about/panel?x=1", want: "/bradley/projects/TRACK/about/panel?x=1"},
		{name: "project backlog panel with query", raw: "/bradley/projects/TRACK/backlog/panel?x=1", want: "/bradley/projects/TRACK/backlog/panel?x=1"},
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
	assignee := model.User{ID: userID, Name: "Ada Lovelace", Email: "ada@example.com"}
	reporter := model.User{ID: userID, Name: "Ada Lovelace", Email: "ada@example.com"}
	sprint := model.Sprint{ID: uuid.MustParse("d7fc0dbf-845c-41b4-84ab-89f487cc4a08"), ProjectID: projectID, Name: "Planned One", Status: model.SprintStatusPlanned}
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
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
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project:  model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		Sprint:   &sprint,
		Assignee: &assignee,
		Reporter: &reporter,
		Comments: []uiIssueCommentItem{{
			Comment:     model.Comment{ID: commentID, IssueID: issueID, AuthorID: userID, Body: "Looks ready.", CreatedAt: when, UpdatedAt: when},
			AuthorName:  "Ada Lovelace",
			AuthorEmail: "ada@example.com",
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
		`aria-label="Issue settings"`,
		`cursor-pointer list-none`,
		`method="post" action="/bradley/issues/TRACK-7/archive"`,
		`hx-post="/bradley/issues/TRACK-7/archive"`,
		`Archive issue`,
		`data-lucide="archive"`,
		`method="post" action="/bradley/issues/TRACK-7/delete"`,
		`hx-post="/bradley/issues/TRACK-7/delete"`,
		`hx-push-url="/bradley/projects/TRACK/backlog"`,
		`hx-confirm="Delete this issue? This cannot be undone from the UI."`,
		`Delete issue`,
		`data-lucide="trash-2"`,
		`text-rose-600`,
		`dark:hover:bg-rose-950/40`,
		`aria-label="Edit description"`,
		`hx-get="/bradley/issues/TRACK-7/description/edit"`,
		`aria-label="Edit link"`,
		`hx-get="/bradley/issues/TRACK-7/links/link-4/edit"`,
		`aria-label="Edit comment"`,
		`aria-label="Change status"`,
		`aria-expanded="false"`,
		`hx-get="/bradley/issues/TRACK-7/status/edit"`,
		`aria-label="Edit assignee"`,
		`aria-label="Edit reporter"`,
		`aria-label="Edit sprint"`,
		`aria-label="Add link"`,
		`hx-get="/bradley/issues/TRACK-7/links/new"`,
		`aria-label="Post comment"`,
		`aria-haspopup="listbox"`,
		`data-lucide="chevron-down"`,
		`placeholder="Add a comment"`,
		`method="post" action="/bradley/issues/TRACK-7/comments"`,
		`hx-post="/bradley/issues/TRACK-7/comments"`,
		`hx-target="#main"`,
		`hx-push-url="/bradley/issues/TRACK-7"`,
		`data-submit-shortcut="meta-enter"`,
		`class="flex items-start gap-2"`,
		`class="min-w-0 flex-1 resize-none rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-950`,
		`class="grid h-7 w-7 shrink-0 place-items-center rounded-md bg-indigo-600 text-white`,
		`class="flex items-start gap-3 border-b border-slate-100 px-4 py-4 last:border-b-0 dark:border-slate-800"`,
		`inline-flex w-fit justify-self-start items-center whitespace-nowrap rounded-md border border-slate-300 bg-white px-1.5 py-0.5 font-mono text-[11px]`,
		`class="flex min-w-0 items-center gap-2 hover:text-indigo-700 dark:hover:text-indigo-200"`,
		`class="min-w-0 truncate text-slate-900 dark:text-slate-100">Linked work</span>`,
		`h-6 w-6`,
		`h-3.5 w-3.5`,
		`border-amber-300 bg-amber-50 text-amber-800`,
		`bg-amber-50/45 dark:bg-amber-950/15`,
		`bg-emerald-50/45 hover:bg-emerald-50`,
		"disabled",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("issue panel missing %q: %s", want, body)
		}
	}
	if got := strings.Count(body, "bg-amber-50/45 dark:bg-amber-950/15"); got != 1 {
		t.Fatalf("issue panel should tint the title card only, got %d matches: %s", got, body)
	}
	detailsStart := strings.Index(body, ">Details</h2>")
	if detailsStart < 0 {
		t.Fatalf("issue panel missing Details heading: %s", body)
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
	commentMetaStart := strings.Index(body, "Ada Lovelace")
	commentBodyStart := strings.Index(body, "Looks ready.")
	if commentMetaStart < 0 || commentBodyStart < 0 || commentMetaStart > commentBodyStart {
		t.Fatalf("issue panel missing comment metadata/body ordering: %s", body)
	}
	commentEditStart := strings.Index(body, `aria-label="Edit comment"`)
	if commentEditStart < 0 || commentEditStart < commentBodyStart {
		t.Fatalf("comment edit button should render at the right edge after comment body content: %s", body)
	}
	if strings.Contains(body[commentMetaStart:commentBodyStart], `aria-label="Edit comment"`) {
		t.Fatalf("comment edit button should not render beside comment metadata: %s", body)
	}
	if strings.Contains(body, "\n            Comment\n") {
		t.Fatalf("post comment button should be icon-only: %s", body)
	}
	if strings.Contains(body, "<textarea disabled") || strings.Contains(body, `aria-label="Post comment" class="grid h-9 w-9`) || strings.Contains(body, `aria-label="Post comment" class="grid h-7 w-7 shrink-0 cursor-not-allowed`) {
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
		`data-lucide="chevron-up"`,
		`aria-label="Cancel status change"`,
		`hx-get="/bradley/issues/TRACK-7/panel"`,
		`method="post" action="/bradley/issues/TRACK-7/status"`,
		`hx-post="/bradley/issues/TRACK-7/status"`,
		`hx-target="#main"`,
		`hx-push-url="/bradley/issues/TRACK-7"`,
		`role="listbox" aria-label="Issue status"`,
		`name="status" value="todo"`,
		`name="status" value="in_progress"`,
		`name="status" value="done"`,
		`role="option" aria-selected="true"`,
		"To do",
		"In progress",
		"Done",
		`data-lucide="check"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("status dropdown missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{
		`hx-get="/bradley/issues/TRACK-7/status/edit"`,
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
		`aria-label="Cancel priority change"`,
		`hx-get="/bradley/issues/TRACK-7/panel"`,
		`method="post" action="/bradley/issues/TRACK-7/priority"`,
		`hx-post="/bradley/issues/TRACK-7/priority"`,
		`hx-target="#main"`,
		`hx-push-url="/bradley/issues/TRACK-7"`,
		`role="listbox" aria-label="Issue priority"`,
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
		`flex items-center gap-2`,
		`flex flex-wrap items-center gap-2`,
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
			wantHref:  "/bradley/projects/TRACK/backlog",
			wantHXGet: "/bradley/projects/TRACK/backlog/panel",
			wantLabel: "Backlog",
		},
		{
			name:      "backlog issue",
			issue:     model.Issue{ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-8"},
			wantHref:  "/bradley/projects/TRACK/backlog",
			wantHXGet: "/bradley/projects/TRACK/backlog/panel",
			wantLabel: "Backlog",
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

func TestUIProjectPanelRendersTabsBelowTitleCard(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	project := model.Project{
		ID:            projectID,
		OwnerUsername: "bradley",
		Key:           "TRACK",
		Name:          "Track Slash",
		Description:   "Fast issue tracking.",
	}
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "project-panel", &uiProjectPanelData{
		Project:     project,
		View:        "about",
		ProjectTabs: uiProjectTabs(project, "about", nil),
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	headerEnd := strings.Index(body, "</header>")
	if headerEnd < 0 {
		t.Fatalf("project panel missing title card header: %s", body)
	}
	titleCard := strings.Index(body, `<header class="rounded-lg`)
	if titleCard < 0 {
		t.Fatalf("project panel missing title card: %s", body)
	}
	backLink := strings.Index(body, `href="/projects"`)
	if backLink < 0 {
		t.Fatalf("project panel missing back link to projects: %s", body)
	}
	if backLink > titleCard {
		t.Fatalf("back link rendered inside or below title card: %s", body)
	}
	tabNav := strings.Index(body, `aria-label="Project views"`)
	if tabNav < 0 {
		t.Fatalf("project panel missing project view tabs: %s", body)
	}
	if tabNav < headerEnd {
		t.Fatalf("project view tabs rendered inside title card: %s", body)
	}
	header := body[:headerEnd]
	for _, notWant := range []string{"About", "Sprints", "Backlog", "Fast issue tracking.", `/about/panel`, `/sprint/panel`, `/backlog/panel`} {
		if strings.Contains(header, notWant) {
			t.Fatalf("title card still contains tab control %q: %s", notWant, body)
		}
	}
	for _, want := range []string{"TRACK", "font-mono text-sm font-semibold uppercase", "Fast issue tracking.", `data-lucide="info"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("project panel missing about/title markup %q: %s", want, body)
		}
	}
	aboutIdx := strings.Index(body, `href="/bradley/projects/TRACK/about"`)
	sprintsIdx := strings.Index(body, `href="/bradley/projects/TRACK/sprint"`)
	backlogIdx := strings.Index(body, `href="/bradley/projects/TRACK/backlog"`)
	if aboutIdx < 0 || sprintsIdx < 0 || backlogIdx < 0 || sprintsIdx > backlogIdx || backlogIdx > aboutIdx {
		t.Fatalf("project tabs not ordered sprints, backlog, about: sprints=%d backlog=%d about=%d body=%s", sprintsIdx, backlogIdx, aboutIdx, body)
	}
	if strings.Contains(body, "Back to projects") {
		t.Fatalf("project back link uses verbose label: %s", body)
	}
	for _, want := range []string{"Projects", `hx-get="/projects/panel"`, "About", "Sprints", "Backlog", "border-b-4", `aria-current="page"`, `href="/bradley/projects/TRACK/about"`} {
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
		SprintColumns: uiIssueColumns(),
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
		"whitespace-pre-wrap",
		`href="/bradley/projects/TRACK/sprint?assignee_id=23f14acb-6a57-4035-a046-33e93ffbd5bb"`,
		`hx-get="/bradley/projects/TRACK/backlog/panel?assignee_id=23f14acb-6a57-4035-a046-33e93ffbd5bb"`,
		`href="/bradley/projects/TRACK/sprint"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("project panel missing %q: %s", want, body)
		}
	}
	filterIdx := strings.Index(body, `aria-label="Assignee filter"`)
	tabIdx := strings.Index(body, `aria-label="Project views"`)
	if filterIdx < 0 || tabIdx < 0 || filterIdx > tabIdx {
		t.Fatalf("assignee filter should render above tabs: filter=%d tabs=%d body=%s", filterIdx, tabIdx, body)
	}
}

func TestUIIssueRowsUseCompactIssueKeyAndColoredStatus(t *testing.T) {
	t.Parallel()

	issue := model.Issue{
		ID:         uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b"),
		ProjectID:  uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16"),
		Identifier: "TRACK-7",
		Title:      "Row issue",
		Status:     model.StatusDone,
		Priority:   model.PriorityP0,
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
