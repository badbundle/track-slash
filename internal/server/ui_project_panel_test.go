package server

import (
	"bytes"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/google/uuid"
	"html/template"
	"strings"
	"testing"
	"time"
)

func TestUIProjectBreadcrumbIncludesCurrentView(t *testing.T) {
	t.Parallel()

	project := model.Project{OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"}
	for _, tt := range []struct {
		view  string
		label string
	}{
		{view: "sprint", label: "Sprint"},
		{view: "planned", label: "Planned"},
		{view: "all", label: "All"},
		{view: "context", label: "Context"},
		{view: "about", label: "About"},
		{view: "members", label: "Members"},
		{view: "changelog", label: "Changelog"},
	} {
		t.Run(tt.view, func(t *testing.T) {
			items := uiProjectBreadcrumb(project, tt.view).Items
			if len(items) != 3 {
				t.Fatalf("breadcrumb item count = %d, want 3: %#v", len(items), items)
			}
			if items[1].Label != project.Name || items[1].Href != "/bradley/projects/TRACK/all" || items[1].HXGet != "/bradley/projects/TRACK/all/panel" || items[1].Current {
				t.Fatalf("project breadcrumb = %#v, want linked project name", items[1])
			}
			if items[2].Label != tt.label || !items[2].Current {
				t.Fatalf("view breadcrumb = %#v, want current %q", items[2], tt.label)
			}
		})
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
	tags := []model.IssueTag{
		uiTestIssueTag(projectID, 1, "Customer Beta", model.TagColorGreen),
	}
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "project-panel", &uiProjectPanelData{
		CanWrite:             true,
		Project:              project,
		View:                 "about",
		ProjectTabs:          uiProjectTabs(project, "about", selected),
		AssigneeFilters:      uiProjectAssigneeFilters(project, "about", assignees, selected),
		AssigneeFilterActive: true,
		ProjectStats: model.ProjectStats{
			ProjectID: projectID,
			AllTime: model.ProjectIssueStatusCounts{
				Total:      9,
				Todo:       3,
				InProgress: 2,
				Done:       3,
				Closed:     1,
			},
			Last7Days: model.ProjectIssueStatusCounts{
				Total:      4,
				Todo:       1,
				InProgress: 1,
				Done:       1,
				Closed:     1,
			},
			TopAssignees: []model.ProjectAssigneeIssueStats{{
				UserID:   selectedID,
				Username: "ada",
				Name:     "Ada Lovelace",
				Counts: model.ProjectIssueStatusCounts{
					Total:      5,
					Todo:       2,
					InProgress: 1,
					Done:       1,
					Closed:     1,
				},
			}},
		},
		Tags: tags,
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
	for _, want := range []string{`aria-label="Breadcrumb"`, `href="/projects"`, `hx-get="/projects/panel"`, `>Projects</a>`, `href="/bradley/projects/TRACK/all"`, `hx-get="/bradley/projects/TRACK/all/panel"`, `>Track Slash</a>`, `aria-current="page"`, `>About</span>`} {
		if !strings.Contains(body, want) {
			t.Fatalf("project panel missing breadcrumb markup %q: %s", want, body)
		}
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
	for _, want := range []string{"TRACK", "font-mono text-sm font-semibold uppercase", "Track Slash", "About", "Sprint", "Planned", "All", "Changelog", `data-lucide="person-standing"`, `data-lucide="history"`, `data-lucide="info"`, `aria-current="page"`, `aria-label="New issue"`, `href="/bradley/projects/TRACK/issues/new"`, `hx-get="/bradley/projects/TRACK/issues/new/panel"`, `aria-label="Project actions"`, `data-lucide="more-horizontal"`, "lg:mt-0 lg:border-t-0 lg:pt-0", `href="/bradley/projects/TRACK/deleted"`, `hx-get="/bradley/projects/TRACK/deleted/panel"`, `data-lucide="trash-2"`, "Deleted issues"} {
		if !strings.Contains(header, want) {
			t.Fatalf("project title card missing markup %q: %s", want, body)
		}
	}
	for _, want := range []string{"Description", "Fast issue tracking.", "Issue stats", "All time", "Last 7 days", "Top assignees", "Ada Lovelace", "@ada", "AL", "Details", "Owner", "@bradley", "Tags", "#Customer Beta", `aria-label="Manage tags"`, `hx-get="/bradley/projects/TRACK/tags"`, "Created", "Jun 1, 2026 09:30", "Updated", "Jun 2, 2026 10:45"} {
		if !strings.Contains(body, want) {
			t.Fatalf("project about view missing markup %q: %s", want, body)
		}
	}
	if strings.Contains(body, "No assigned issues.") {
		t.Fatalf("project about populated stats rendered empty assignee state: %s", body)
	}
	if strings.Contains(header, ">Manage tags</span>") {
		t.Fatalf("project title card should not render manage tags in overflow menu: %s", body)
	}
	requireMarkupOrder(t, body, ">Details</h2>", ">Tags</dt>")
	if strings.Contains(body, ">Context</dt>") || strings.Contains(body, `aria-label="Manage context"`) {
		t.Fatalf("project about should not render context in details: %s", body)
	}
	for _, notWant := range []string{`aria-label="Assignee filter"`, `assignee_id=`} {
		if strings.Contains(header, notWant) {
			t.Fatalf("project title card preserved about filter state %q: %s", notWant, body)
		}
		if strings.Contains(body, notWant) {
			t.Fatalf("project about view rendered assignee filter state %q: %s", notWant, body)
		}
	}
	if strings.Contains(body, "Back to projects") {
		t.Fatalf("project breadcrumb uses verbose label: %s", body)
	}
	tabEnd := strings.Index(body[tabNav:], "</nav>")
	if tabEnd < 0 {
		t.Fatalf("project tabs missing nav close: %s", body)
	}
	tabMarkup := body[tabNav : tabNav+tabEnd]
	if strings.Contains(tabMarkup, "Deleted") || strings.Contains(tabMarkup, `/deleted`) {
		t.Fatalf("deleted rendered as project tab: %s", body)
	}
	aboutIdx := strings.Index(tabMarkup, `href="/bradley/projects/TRACK/about"`)
	sprintsIdx := strings.Index(tabMarkup, `href="/bradley/projects/TRACK/sprint"`)
	plannedIdx := strings.Index(tabMarkup, `href="/bradley/projects/TRACK/planned"`)
	allIdx := strings.Index(tabMarkup, `href="/bradley/projects/TRACK/all"`)
	contextIdx := strings.Index(tabMarkup, `href="/bradley/projects/TRACK/context"`)
	if aboutIdx < 0 || sprintsIdx < 0 || plannedIdx < 0 || allIdx < 0 || contextIdx < 0 || sprintsIdx > plannedIdx || plannedIdx > allIdx || allIdx > contextIdx || contextIdx > aboutIdx {
		t.Fatalf("project tabs not ordered sprint, planned, all, context, about: sprint=%d planned=%d all=%d context=%d about=%d body=%s", sprintsIdx, plannedIdx, allIdx, contextIdx, aboutIdx, body)
	}
	if strings.Contains(tabMarkup, `href="/bradley/projects/TRACK/changelog"`) {
		t.Fatalf("changelog should not render in project tabs: %s", body)
	}
	for _, path := range []string{"context", "about"} {
		href := `href="/bradley/projects/TRACK/` + path + `"`
		if got := strings.Count(header, href); got != 2 {
			t.Fatalf("project %s view should render once as a desktop tab and once in the mobile overflow menu; got %d: %s", path, got, body)
		}
	}
	if got := strings.Count(header, `href="/bradley/projects/TRACK/changelog"`); got != 1 {
		t.Fatalf("changelog should render once in project overflow; got %d: %s", got, body)
	}
	for _, want := range []string{"flex flex-nowrap", "gap-4 sm:gap-6", "hidden lg:inline-flex", "lg:hidden"} {
		if !strings.Contains(header, want) {
			t.Fatalf("project header missing responsive tab overflow markup %q: %s", want, body)
		}
	}
	for _, want := range []string{"About", "Sprint", "Planned", "All", `data-lucide="person-standing"`, `data-lucide="calendar-range"`, `data-lucide="list-filter"`, "border-b-4", `aria-current="page"`, `href="/bradley/projects/TRACK/about"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("project panel missing tab markup %q: %s", want, body)
		}
	}
}

func TestUIProjectPanelRendersChangelog(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	issueID := uuid.New()
	actorID := uuid.New()
	project := model.Project{
		ID:            projectID,
		OwnerUsername: "bradley",
		Key:           "TRACK",
		Name:          "Track Slash",
		CreatedAt:     time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 6, 2, 10, 45, 0, 0, time.UTC),
	}
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "project-panel", uiProjectPanelData{
		CanWrite:    true,
		Project:     project,
		View:        "changelog",
		ProjectTabs: uiProjectTabs(project, "changelog", nil),
		ChangelogPage: uiProjectChangelogPageData{
			Project: project,
			Entries: []model.ProjectChangelogEntry{{
				ID:          uuid.New(),
				ProjectID:   projectID,
				ActorID:     &actorID,
				Actor:       &model.ProjectChangelogActor{ID: actorID, Username: "ada", Name: "Ada Lovelace"},
				Entity:      "issue",
				Op:          "update",
				EntityID:    issueID,
				IssueID:     &issueID,
				TargetRef:   "TRACK-7",
				TargetTitle: "Better changelog",
				Summary:     "Updated issue TRACK-7",
				Details: model.ProjectChangelogDetails{Changes: []model.ProjectChangelogChange{{
					Field: "status",
					Label: "Status",
					From:  "To do",
					To:    "Done",
				}}},
				CreatedAt: time.Date(2026, 6, 3, 11, 0, 0, 0, time.UTC),
			}},
			HasMore:    true,
			NextCursor: "cursor123",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		`data-project-changelog`,
		`data-project-id="` + projectID.String() + `"`,
		`data-refresh-url="/bradley/projects/TRACK/changelog/panel"`,
		`href="/bradley/projects/TRACK/changelog"`,
		`aria-current="page"`,
		`Updated issue TRACK-7`,
		`href="/bradley/issues/TRACK-7"`,
		`hx-get="/bradley/issues/TRACK-7/panel"`,
		`Better changelog`,
		`Ada Lovelace`,
		`Status`,
		`To do`,
		`Done`,
		`hx-get="/bradley/projects/TRACK/changelog/page?cursor=cursor123"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("changelog view missing %q: %s", want, body)
		}
	}
	tabStart := strings.Index(body, `aria-label="Project views"`)
	if tabStart < 0 {
		t.Fatalf("project tabs missing: %s", body)
	}
	tabEnd := strings.Index(body[tabStart:], "</nav>")
	if tabEnd < 0 {
		t.Fatalf("project tabs missing: %s", body)
	}
	tabMarkup := body[tabStart : tabStart+tabEnd]
	if strings.Contains(tabMarkup, "Deleted") || strings.Contains(tabMarkup, `/deleted`) {
		t.Fatalf("deleted rendered as project tab: %s", body)
	}
}

func TestUINewIssuePanelRendersAllCreateFields(t *testing.T) {
	t.Parallel()

	project := model.Project{ID: uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16"), OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"}
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "new-issue-panel", &uiNewIssuePanelData{
		Project:        project,
		HasProject:     true,
		ProjectID:      project.ID.String(),
		ProjectOptions: []model.Project{project},
		Error:          "Use YYYY-MM-DD.",
		Title:          "Draft issue",
		Description:    "Draft body",
		Priority:       string(model.PriorityP1),
		DueDate:        "tomorrow",
		AssigneeInput:  "@ada",
		ReporterInput:  "@grace",
		MemberOptions: []model.User{
			{Username: "ada", Email: "ada@example.com", Name: "Ada Lovelace"},
			{Username: "grace", Email: "grace@example.com", Name: "Grace Hopper"},
		},
		BackHref:  "/bradley/projects/TRACK/all",
		BackHXGet: "/bradley/projects/TRACK/all/panel",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		"New issue",
		"Create issue",
		"Use YYYY-MM-DD.",
		`data-new-issue-panel`,
		`id="new-issue-form" method="post" action="/issues"`,
		`hx-post="/issues"`,
		`hx-push-url="false"`,
		`id="new-issue-project-form" method="get" action="/issues/new/panel"`,
		`hx-get="/issues/new/panel"`,
		`hx-include="#issue-title,#issue-description,input[name='priority']:checked,#issue-due-date,#issue-assignee,#issue-reporter"`,
		`data-search`,
		`data-project-search`,
		`data-search-collapsible`,
		`data-search-clear-target="project_id"`,
		`id="issue-project" name="project" value="TRACK - Track Slash"`,
		`data-search-input`,
		`hx-trigger="input changed delay:300ms"`,
		`hx-target="#new-issue-project-options"`,
		`hx-swap="outerHTML"`,
		`hx-push-url="false"`,
		`hx-include="#new-issue-project-form"`,
		`type="hidden" name="project_id" value="8cc21ed4-2d69-4d43-9f0c-402736e4aa16"`,
		`id="new-issue-project-options"`,
		`data-search-options hidden role="listbox" aria-label="Project suggestions"`,
		`data-search-option`,
		`data-target-name="project_id"`,
		`data-target-value="8cc21ed4-2d69-4d43-9f0c-402736e4aa16"`,
		`data-search-text="TRACK Track Slash bradley"`,
		`hx-get="/issues/new/projects"`,
		`TRACK - Track Slash`,
		`id="issue-title" name="title" value="Draft issue"`,
		`id="issue-description" name="description"`,
		"Draft body",
		`id="issue-priority-label">Priority</span>`,
		`@keyframes priority-picker-item-enter`,
		`role="listbox" aria-labelledby="issue-priority-label" data-priority-picker`,
		`type="radio" name="priority" value="P0"`,
		`type="radio" name="priority" value="P1" checked`,
		`type="radio" name="priority" value="P2"`,
		`aria-label="Priority P1"`,
		`opacity-40`,
		`peer-checked:opacity-100`,
		`data-checkbox-reveal`,
		`id="issue-due-date-toggle" type="checkbox" data-checkbox-reveal-toggle aria-controls="issue-due-date-field" aria-expanded="true" checked`,
		`id="issue-due-date-field" data-checkbox-reveal-panel`,
		`type="date" name="due_date" value="tomorrow"`,
		`id="issue-assignee" name="assignee" value="@ada"`,
		`id="issue-reporter" name="reporter" value="@grace"`,
		`list="new-issue-members"`,
		`<datalist id="new-issue-members">`,
		`<option value="@ada">Ada Lovelace - ada@example.com</option>`,
		`aria-label="Cancel creating issue"`,
		`href="/bradley/projects/TRACK/all"`,
		`hx-get="/bradley/projects/TRACK/all/panel"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("new issue panel missing %q: %s", want, body)
		}
	}
}

func TestUINewIssueProjectFilter(t *testing.T) {
	t.Parallel()

	projects := []model.Project{
		{OwnerUsername: "bradley", Key: "CORE", Name: "Core Workflow"},
		{OwnerUsername: "bradley", Key: "OPS", Name: "Operations Desk"},
		{OwnerUsername: "ada", Key: "APP", Name: "Mobile Companion"},
	}
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty", want: []string{"CORE", "OPS", "APP"}},
		{name: "key", raw: "ops", want: []string{"OPS"}},
		{name: "name", raw: "mobile", want: []string{"APP"}},
		{name: "owner", raw: "ada", want: []string{"APP"}},
		{name: "multi token", raw: "core workflow", want: []string{"CORE"}},
		{name: "missing", raw: "zzz", want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uiFilterNewIssueProjects(projects, tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d: %+v", len(got), len(tt.want), got)
			}
			for i, want := range tt.want {
				if got[i].Key != want {
					t.Fatalf("got[%d] = %s, want %s", i, got[i].Key, want)
				}
			}
		})
	}
}

func TestUIProjectAboutStatsEmptyTopAssignees(t *testing.T) {
	t.Parallel()

	project := model.Project{
		ID:            uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16"),
		OwnerUsername: "bradley",
		Key:           "TRACK",
		Name:          "Track Slash",
	}
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "project-panel", &uiProjectPanelData{
		CanWrite:     true,
		Project:      project,
		View:         "about",
		ProjectTabs:  uiProjectTabs(project, "about", nil),
		ProjectStats: model.ProjectStats{ProjectID: project.ID},
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{"Issue stats", "Top assignees", "No assigned issues.", `<td class="px-4 py-2 text-right tabular-nums text-slate-700 dark:text-slate-300">0</td>`} {
		if !strings.Contains(body, want) {
			t.Fatalf("empty project about stats missing %q: %s", want, body)
		}
	}
}

func TestUIProjectContextSurfacesRenderCompactAboutAndManagerRows(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	contextID := uuid.MustParse("845bc7de-5238-4df2-a024-799f9dbeb5fe")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	project := model.Project{
		ID:            projectID,
		OwnerUsername: "bradley",
		Key:           "TRACK",
		Name:          "Track Slash",
	}
	contextItem := uiProjectContextItem{
		Context: model.ProjectContextSummary{
			ID:               contextID,
			ProjectID:        projectID,
			Number:           1,
			Ref:              "context-1",
			Scope:            model.ProjectContextScopeProject,
			Title:            "Architecture notes",
			Kind:             model.ProjectContextKindText,
			ContentType:      "text/plain; charset=utf-8",
			LinkedIssueCount: 1,
			CreatedAt:        when,
			UpdatedAt:        when,
		},
	}
	linkedIssue := model.Issue{
		ID:            issueID,
		ProjectID:     projectID,
		OwnerUsername: "bradley",
		ProjectKey:    "TRACK",
		Identifier:    "TRACK-8",
		Title:         "Linked work",
		Status:        model.StatusTodo,
		CreatedAt:     when,
		UpdatedAt:     when,
	}
	renderProject := func(panel *uiProjectPanelData) string {
		t.Helper()
		var buf bytes.Buffer
		if err := uiTemplates.ExecuteTemplate(&buf, "project-panel", panel); err != nil {
			t.Fatalf("ExecuteTemplate: %v", err)
		}
		return buf.String()
	}
	renderManager := func(panel *uiContextManagerData) string {
		t.Helper()
		var buf bytes.Buffer
		if err := uiTemplates.ExecuteTemplate(&buf, "context-manager-panel", panel); err != nil {
			t.Fatalf("ExecuteTemplate: %v", err)
		}
		return buf.String()
	}

	body := renderProject(&uiProjectPanelData{
		CanWrite:     true,
		Project:      project,
		View:         "about",
		ProjectTabs:  uiProjectTabs(project, "about", nil),
		ContextItems: []uiProjectContextItem{contextItem},
	})
	if strings.Contains(body, ">Context</dt>") || strings.Contains(body, `aria-label="Manage context"`) || strings.Contains(body, "Architecture notes") {
		t.Fatalf("project about should not render project context: %s", body)
	}

	managerItem := uiContextManagerItem{
		ID:               contextID,
		Ref:              "context-1",
		Number:           1,
		Scope:            model.ProjectContextScopeProject,
		Title:            "Architecture notes",
		ContentType:      "text/plain; charset=utf-8",
		LinkedIssueCount: 1,
		LinkedIssues:     []model.Issue{linkedIssue},
		UpdatedAt:        when,
	}
	manager := &uiContextManagerData{
		CanWrite:  true,
		Mode:      "project",
		Project:   project,
		BackHref:  "/bradley/projects/TRACK/about",
		BackHXGet: "/bradley/projects/TRACK/about/panel",
		BackLabel: "About",
		Items:     []uiContextManagerItem{managerItem},
	}
	body = renderManager(manager)
	for _, want := range []string{"Context", "Architecture notes", "linked issues", `aria-label="Link issue"`, `hx-get="/bradley/projects/TRACK/context/context-1/issues/new"`, `hx-push-url="/bradley/projects/TRACK/context/context-1/issues/new"`, `aria-label="Edit context"`, `hx-push-url="/bradley/projects/TRACK/context/context-1/edit"`, `aria-label="Delete context"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("project context manager row missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `font-mono`) {
		t.Fatalf("project context manager row should not render context refs as badges: %s", body)
	}
	for _, notWant := range []string{`placeholder="TRACK-12"`, "Linked work", `aria-label="Unlink issue"`, "Use the body."} {
		if strings.Contains(body, notWant) {
			t.Fatalf("project context manager row should stay compact, found %q: %s", notWant, body)
		}
	}

	activeContext := model.ProjectContext{ID: contextID, ProjectID: projectID, Number: 1, Ref: "context-1", Scope: model.ProjectContextScopeProject, Title: "Architecture notes", Kind: model.ProjectContextKindText, ContentType: "text/plain; charset=utf-8", Body: "Use the body.", UpdatedAt: when}
	linkManager := *manager
	linkManager.Action = "link"
	linkManager.ActiveContextID = contextID
	linkManager.ActiveContext = activeContext
	linkManager.LinkIssueInput = "TRACK-9"
	linkManager.LinkIssueError = "Issue already linked."
	body = renderManager(&linkManager)
	for _, want := range []string{`name="issue" value="TRACK-9" placeholder="TRACK-12" autofocus`, "Issue already linked.", "Linked issues", "TRACK-8", "Linked work", `aria-label="Unlink issue"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("project context issue link manager missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `role="dialog" aria-modal="true"`) {
		t.Fatalf("project context issue link manager should not render modal: %s", body)
	}

	editManager := *manager
	editManager.Action = "edit"
	editManager.ActiveContextID = contextID
	editManager.ActiveContext = activeContext
	editManager.ContextEditTitle = "Architecture notes"
	editManager.ContextEditBody = "Use the body."
	body = renderManager(&editManager)
	for _, want := range []string{`action="/bradley/projects/TRACK/context/context-1"`, `value="Architecture notes"`, "Use the body.", `aria-label="Save context"`, `aria-label="Cancel editing context"`, `hx-replace-url="/bradley/projects/TRACK/context"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("project context edit manager missing %q: %s", want, body)
		}
	}
}

func TestUIProjectContextRendersIntegratedMarkdownPages(t *testing.T) {
	t.Parallel()
	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	contextID := uuid.MustParse("845bc7de-5238-4df2-a024-799f9dbeb5fe")
	position := int64(1)
	project := model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"}
	contextItem := model.ProjectContext{
		ID: contextID, ProjectID: projectID, Number: 1, Ref: "context-1", Scope: model.ProjectContextScopeProject,
		Position: &position, Title: "Architecture notes", Kind: model.ProjectContextKindText,
		ContentType: "text/markdown; charset=utf-8", Body: "# Architecture\n\nUse transactions.", UpdatedAt: time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC),
	}
	sourceFilename := "architecture.md"
	contextItem.SourceFilename = &sourceFilename
	manager := &uiContextManagerData{
		CanWrite: true,
		Mode:     "project", Project: project, Action: "view", ActiveContextID: contextID, HasActiveContext: true, ActiveContext: contextItem,
		ActiveHTML: template.HTML("<h1>Architecture</h1><p>Use transactions.</p>"),
		Items:      []uiContextManagerItem{{ID: contextID, Ref: "context-1", Number: 1, Scope: model.ProjectContextScopeProject, Position: &position, Title: "Architecture notes", ContentType: contextItem.ContentType, LinkedIssueCount: 2}},
	}
	var buf bytes.Buffer
	if err := uiTemplates.ExecuteTemplate(&buf, "project-panel", &uiProjectPanelData{
		CanWrite: true,
		Project:  project, View: "context", ProjectTabs: uiProjectTabs(project, "context", nil), ContextManager: manager,
	}); err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		`aria-current="page"`, `href="/bradley/projects/TRACK/context/context-1"`, "Architecture notes",
		"<h1>Architecture</h1>", "Use transactions.", `aria-label="Move context page up"`,
		`aria-label="Move context page down"`, `aria-label="Manage linked issues"`, `aria-label="Edit context page"`,
		`aria-label="Delete context page"`, `href="/bradley/projects/TRACK/changelog"`,
		"architecture.md",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("integrated context panel missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `href="/bradley/projects/TRACK/changelog" hx-get="/bradley/projects/TRACK/changelog/panel" hx-target="#main" hx-push-url="/bradley/projects/TRACK/changelog"  class="hidden`) {
		t.Fatalf("changelog rendered as a tab: %s", body)
	}
	if strings.Contains(body, "text/markdown; charset=utf-8") {
		t.Fatalf("integrated context panel should not display MIME metadata: %s", body)
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
	tags := []model.IssueTag{
		uiTestIssueTag(projectID, 1, "Sprint Visible", model.TagColorGreen),
	}
	columns := uiIssueColumns()
	columns[0].Issues = append(columns[0].Issues, uiIssueItem{
		Issue:   model.Issue{ID: uuid.MustParse("adbf2723-a44d-4b43-a3d0-e12276fa59c0"), ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-10", Title: "Todo count issue", Status: model.StatusTodo, Tags: tags},
		Project: project,
	})
	columns[1].Issues = append(columns[1].Issues, uiIssueItem{
		Issue:   model.Issue{ID: uuid.MustParse("af63e70c-bf9d-4f80-999d-df145379ec6d"), ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-11", Title: "Progress count issue", Status: model.StatusInProgress},
		Project: project,
	})
	sprintQuery := uiIssueListQuery{AssigneeIDs: selected}
	assigneeFilters := uiProjectSprintAssigneeFilters(project, assignees, sprintQuery)
	clearAssigneeHref := uiProjectSprintViewPath(project, uiIssueListQuery{})
	clearAssigneeHXGet := uiProjectSprintPanelPath(project, uiIssueListQuery{})
	activeSprint := model.Sprint{
		ID:        uuid.MustParse("d7fc0dbf-845c-41b4-84ab-89f487cc4a08"),
		ProjectID: projectID,
		Ref:       "sprint-1",
		Name:      "Current Sprint",
		Goal:      "Ship filtering\nPolish sprint goals",
		StartDate: datePtr(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
		EndDate:   datePtr(time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)),
	}

	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "project-panel", &uiProjectPanelData{
		CanWrite:             true,
		Project:              project,
		View:                 "sprint",
		ProjectTabs:          uiProjectTabs(project, "sprint", selected),
		AssigneeFilters:      assigneeFilters,
		AssigneeFilterActive: true,
		ClearAssigneeHref:    clearAssigneeHref,
		ClearAssigneeHXGet:   clearAssigneeHXGet,
		ClearAssigneeHXPush:  clearAssigneeHref,
		SprintControls:       uiProjectSprintIssueControls(project, sprintQuery, nil, assigneeFilters, true, clearAssigneeHref, clearAssigneeHXGet, clearAssigneeHref),
		ActiveSprint:         &activeSprint,
		ActiveSprintDescription: uiSprintDescriptionData{
			Project:         project,
			Sprint:          activeSprint,
			DescriptionHTML: renderSprintDescriptionMarkdown(project, activeSprint, nil),
		},
		SprintColumns: columns,
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`aria-label="Issue controls"`,
		`aria-pressed="true"`,
		`aria-pressed="false"`,
		`aria-label="Toggle Ada Lovelace"`,
		`aria-label="Toggle Grace Hopper"`,
		"Status",
		"Priority",
		"Assignee",
		"Sort",
		"Direction",
		"Due date",
		"Desc",
		`data-lucide="arrow-down"`,
		"AL",
		"GH",
		"Ship filtering",
		"Todo count issue",
		"#Sprint Visible",
		"border-green-200 bg-green-50 text-green-700",
		"Progress count issue",
		"-mt-3 max-h-20 overflow-hidden",
		"See more",
		`href="/bradley/projects/TRACK/sprint?assignee_id=23f14acb-6a57-4035-a046-33e93ffbd5bb"`,
		`hx-get="/bradley/projects/TRACK/planned/panel"`,
		`hx-get="/bradley/projects/TRACK/all/panel"`,
		`href="/bradley/projects/TRACK/sprint"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("project panel missing %q: %s", want, body)
		}
	}
	filterIdx := strings.Index(body, `aria-label="Issue controls"`)
	tabIdx := strings.Index(body, `aria-label="Project views"`)
	sprintTitleIdx := strings.Index(body, "Current Sprint")
	boardIdx := strings.Index(body, `class="grid gap-4 lg:grid-cols-3"`)
	if filterIdx < 0 || tabIdx < 0 || filterIdx < tabIdx {
		t.Fatalf("issue controls should render below project tabs: filter=%d tabs=%d body=%s", filterIdx, tabIdx, body)
	}
	if sprintTitleIdx < 0 || boardIdx < 0 || filterIdx < sprintTitleIdx || filterIdx > boardIdx {
		t.Fatalf("issue controls should render below sprint title and above board: sprint=%d filter=%d board=%d body=%s", sprintTitleIdx, filterIdx, boardIdx, body)
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
