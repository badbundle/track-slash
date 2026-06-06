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
		{name: "issue", raw: "/issues/8cc21ed4-2d69-4d43-9f0c-402736e4aa16", want: "/issues/8cc21ed4-2d69-4d43-9f0c-402736e4aa16"},
		{name: "issue panel with query", raw: "/issues/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/panel?x=1", want: "/issues/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/panel?x=1"},
		{name: "bad issue id", raw: "/issues/nope", want: "/"},
		{name: "bad issue child", raw: "/issues/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/activity", want: "/"},
		{name: "bad issue nested panel", raw: "/issues/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/panel/extra", want: "/"},
		{name: "project", raw: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16", want: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16"},
		{name: "project about", raw: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/about", want: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/about"},
		{name: "project sprint", raw: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/sprint", want: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/sprint"},
		{name: "project about panel with query", raw: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/about/panel?x=1", want: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/about/panel?x=1"},
		{name: "project backlog panel with query", raw: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/backlog/panel?x=1", want: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/backlog/panel?x=1"},
		{name: "bad project id", raw: "/projects/nope/sprint", want: "/"},
		{name: "bad project child", raw: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/issues", want: "/"},
		{name: "bad project panel", raw: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/sprint/card", want: "/"},
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
	if !strings.Contains(body, "#sidebar-toggle:checked ~ .app-shell > aside") {
		t.Fatalf("shell missing direct-child sidebar collapse selector: %s", body)
	}
	if strings.Contains(body, "#sidebar-toggle:checked ~ .app-shell aside { width") {
		t.Fatalf("sidebar collapse selector targets nested asides: %s", body)
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
			ID:          issueID,
			ProjectID:   projectID,
			Identifier:  "TRACK-7",
			Title:       "Design issue detail",
			Description: "Readonly description",
			Status:      model.StatusInProgress,
			AssigneeID:  &userID,
			ReporterID:  &userID,
			SprintID:    &sprint.ID,
			CreatedAt:   when,
			UpdatedAt:   when,
		},
		Project:  model.Project{ID: projectID, Key: "TRACK", Name: "Track Slash"},
		Sprint:   &sprint,
		Assignee: &assignee,
		Reporter: &reporter,
		Comments: []uiIssueCommentItem{{
			Comment:     model.Comment{ID: commentID, IssueID: issueID, AuthorID: userID, Body: "Looks ready.", CreatedAt: when, UpdatedAt: when},
			AuthorName:  "Ada Lovelace",
			AuthorEmail: "ada@example.com",
		}},
		Links: []uiIssueLinkItem{{
			Link:        model.IssueLink{ID: linkID, ProjectID: projectID, SourceID: issueID, TargetID: linkedID, LinkType: model.LinkTypeBlocks, CreatedAt: when, UpdatedAt: when},
			LinkedIssue: model.Issue{ID: linkedID, ProjectID: projectID, Identifier: "TRACK-8", Title: "Linked work", Status: model.StatusDone},
			HasIssue:    true,
		}},
		BackHref:  "/projects/" + projectID.String() + "/backlog",
		BackHXGet: "/projects/" + projectID.String() + "/backlog/panel",
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
		`href="/projects/` + projectID.String() + `/backlog"`,
		`hx-get="/projects/` + projectID.String() + `/backlog/panel"`,
		`href="/issues/` + linkedID.String() + `"`,
		`hx-get="/issues/` + linkedID.String() + `/panel"`,
		`aria-label="Issue settings"`,
		`aria-label="Edit description"`,
		`aria-label="Edit link"`,
		`aria-label="Edit comment"`,
		`aria-label="Change status"`,
		`aria-label="Edit assignee"`,
		`aria-label="Edit reporter"`,
		`aria-label="Edit sprint"`,
		`aria-label="Add link"`,
		`aria-label="Post comment"`,
		`aria-haspopup="listbox"`,
		`data-lucide="chevron-down"`,
		`placeholder="Add a comment"`,
		`method="post" action="/issues/` + issueID.String() + `/comments"`,
		`hx-post="/issues/` + issueID.String() + `/comments"`,
		`hx-target="#main"`,
		`hx-push-url="/issues/` + issueID.String() + `"`,
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
	for _, notWant := range []string{"Edit issue", "Change status", "Edit description", "Edit status", "In progress"} {
		if strings.Contains(titleHeader, notWant) {
			t.Fatalf("title card still contains section action/status %q: %s", notWant, body)
		}
	}
	if strings.Contains(body, "title=") {
		t.Fatalf("issue panel controls should not render native title tooltips: %s", body)
	}
}

func TestUIIssueBackLink(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	sprintID := uuid.MustParse("d7fc0dbf-845c-41b4-84ab-89f487cc4a08")
	parentID := uuid.MustParse("2eeaf29c-ad20-4513-af41-edbb2c9abc2c")
	baseIssue := model.Issue{ProjectID: projectID, SprintID: &sprintID}

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
			wantHref:  "/projects/" + projectID.String() + "/sprint",
			wantHXGet: "/projects/" + projectID.String() + "/sprint/panel",
			wantLabel: "Sprint",
		},
		{
			name:      "planned sprint",
			issue:     baseIssue,
			sprint:    &model.Sprint{ID: sprintID, ProjectID: projectID, Status: model.SprintStatusPlanned},
			wantHref:  "/projects/" + projectID.String() + "/backlog",
			wantHXGet: "/projects/" + projectID.String() + "/backlog/panel",
			wantLabel: "Backlog",
		},
		{
			name:      "backlog issue",
			issue:     model.Issue{ProjectID: projectID},
			wantHref:  "/projects/" + projectID.String() + "/backlog",
			wantHXGet: "/projects/" + projectID.String() + "/backlog/panel",
			wantLabel: "Backlog",
		},
		{
			name:      "completed sprint",
			issue:     baseIssue,
			sprint:    &model.Sprint{ID: sprintID, ProjectID: projectID, Status: model.SprintStatusCompleted},
			wantHref:  "/projects/" + projectID.String() + "/sprint",
			wantHXGet: "/projects/" + projectID.String() + "/sprint/panel",
			wantLabel: "Sprint",
		},
		{
			name:      "missing sprint",
			issue:     baseIssue,
			wantHref:  "/projects/" + projectID.String() + "/sprint",
			wantHXGet: "/projects/" + projectID.String() + "/sprint/panel",
			wantLabel: "Sprint",
		},
		{
			name:      "parent issue",
			issue:     model.Issue{ProjectID: projectID, ParentIssueID: &parentID},
			parent:    &model.Issue{ID: parentID, ProjectID: projectID},
			wantHref:  "/issues/" + parentID.String(),
			wantHXGet: "/issues/" + parentID.String() + "/panel",
			wantLabel: "Parent issue",
		},
	}

	for _, tt := range tests {
		href, hxGet, label := uiIssueBackLink(projectID, tt.issue, tt.parent, tt.sprint)
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

func TestUIProjectPanelRendersAboutTabBelowTitleCard(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "project-panel", &uiProjectPanelData{
		Project: model.Project{
			ID:          projectID,
			Key:         "TRACK",
			Name:        "Track Slash",
			Description: "Fast issue tracking.",
		},
		View:        "about",
		ProjectTabs: uiProjectTabs(projectID, "about"),
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
	aboutIdx := strings.Index(body, `href="/projects/`+projectID.String()+`/about"`)
	sprintsIdx := strings.Index(body, `href="/projects/`+projectID.String()+`/sprint"`)
	backlogIdx := strings.Index(body, `href="/projects/`+projectID.String()+`/backlog"`)
	if aboutIdx < 0 || sprintsIdx < 0 || backlogIdx < 0 || aboutIdx > sprintsIdx || sprintsIdx > backlogIdx {
		t.Fatalf("project tabs not ordered about, sprints, backlog: about=%d sprints=%d backlog=%d body=%s", aboutIdx, sprintsIdx, backlogIdx, body)
	}
	if strings.Contains(body, "Back to projects") {
		t.Fatalf("project back link uses verbose label: %s", body)
	}
	for _, want := range []string{"Projects", `hx-get="/projects/panel"`, "About", "Sprints", "Backlog", "border-b-4", `aria-current="page"`, `href="/projects/` + projectID.String() + `/about"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("project panel missing tab markup %q: %s", want, body)
		}
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
