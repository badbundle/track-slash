package server

import (
	"bytes"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/google/uuid"
	"strings"
	"testing"
)

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

func TestUIWorkPanelRendersTabsAndIssueControls(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	project := model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"}
	query := uiIssueListQuery{
		Statuses:   []model.Status{model.StatusDone},
		Priorities: []model.IssuePriority{model.PriorityP0},
		Sort:       store.ListIssuesSortPriority,
	}
	issue := model.Issue{
		ID:            uuid.MustParse("138095fe-77d7-4644-b127-d0b995757ff2"),
		ProjectID:     projectID,
		OwnerUsername: "bradley",
		ProjectKey:    "TRACK",
		Number:        12,
		Identifier:    "TRACK-12",
		Title:         "Done active issue",
		Status:        model.StatusDone,
		Priority:      model.PriorityP0,
	}

	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "work-panel", &uiWorkPanelData{
		View:           "active",
		Title:          "Me",
		Subtitle:       "Active sprint issues assigned to you across accessible projects.",
		IssueListLabel: "Active sprint issues",
		ProjectCount:   1,
		WorkTabs:       uiWorkTabs("active", query),
		IssueControls:  uiWorkIssueControls("active", query),
		Issues:         []uiIssueItem{{Issue: issue, Project: project}},
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	body := buf.String()
	if !strings.Contains(body, "<header>") || !strings.Contains(body, `<section class="pt-4 pb-6">`) {
		t.Fatalf("work panel should keep compact spacing between tabs and controls: %s", body)
	}
	if strings.Contains(body, `<header class="pb-5">`) || strings.Contains(body, `<header class="border-b border-slate-200 pb-5`) || strings.Contains(body, `<section class="py-6">`) {
		t.Fatalf("work panel should not render extra spacing above controls: %s", body)
	}
	for _, want := range []string{
		"Active Sprints",
		"All",
		`aria-label="Me views"`,
		`<details data-issue-list-controls data-close-on-outside class="mb-4`,
		`aria-label="Issue controls"`,
		`data-lucide="sliders-horizontal"`,
		`class="` + uiCountBadgeClass + `">2</span>`,
		"Status",
		"Priority",
		"Sort",
		"Direction",
		"Asc",
		`data-lucide="arrow-up"`,
		"Due date",
		"Done",
		`aria-label="Priority P0"`,
		`href="/me/all?priority=P0&amp;sort=priority&amp;status=done"`,
		`hx-get="/me/all/panel?priority=P0&amp;sort=priority&amp;status=done"`,
		`href="/me?priority=P0&amp;sort=priority&amp;status=done"`,
		`hx-get="/me/panel?priority=P0&amp;sort=priority&amp;status=done"`,
		`aria-current="page"`,
		`aria-pressed="true"`,
		"Active sprint issues",
		"Done active issue",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("work panel missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Assignee") || strings.Contains(body, "Anyone") {
		t.Fatalf("work panel rendered assignee controls: %s", body)
	}
}

func TestUIProjectIssueControlsRenderTagFiltersAndBadges(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("1f58d897-0e5d-4f72-bf4f-0f7be3d964f6")
	project := model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"}
	tags := []model.IssueTag{
		uiTestIssueTag(projectID, 1, "Customer Beta", model.TagColorGreen),
		uiTestIssueTag(projectID, 2, "Q3 Launch", model.TagColorViolet),
		uiTestIssueTag(projectID, 3, "Ops Review", model.TagColorAmber),
		uiTestIssueTag(projectID, 4, "Escalated", model.TagColorRed),
	}
	query := uiProjectAllQuery{TagNames: []string{"Customer Beta"}}
	issue := model.Issue{
		ID:            uuid.MustParse("1ecf456e-3a8e-4d8f-a685-6388c058abcf"),
		ProjectID:     projectID,
		OwnerUsername: "bradley",
		ProjectKey:    "TRACK",
		Number:        12,
		Identifier:    "TRACK-12",
		Title:         "Tagged issue",
		Status:        model.StatusTodo,
		Priority:      model.PriorityP2,
		Tags:          tags,
	}

	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "project-panel", &uiProjectPanelData{
		Project:     project,
		View:        "all",
		ProjectTabs: uiProjectTabs(project, "all", nil),
		AllIssuePage: uiProjectAllIssuePageData{
			Issues: []model.Issue{issue},
		},
		AllControls: uiProjectAllIssueControls(project, query, tags[:2], nil, false, "", "", ""),
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		"Tags",
		"#Customer Beta",
		"#Q3 Launch",
		"border-green-200 bg-green-50 text-green-700",
		"border-violet-200 bg-violet-50 text-violet-700",
		`data-lucide="check"`,
		"border-indigo-200 bg-indigo-50/70",
		`href="/bradley/projects/TRACK/all?tag=Customer&#43;Beta"`,
		`href="/bradley/projects/TRACK/all?tag=Customer&#43;Beta&amp;tag=Q3&#43;Launch"`,
		"Tagged issue",
		"#Ops Review",
		`>&#43;1</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("project tag controls/badges missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Assignee") {
		t.Fatalf("project tag controls unexpectedly rendered assignee row: %s", body)
	}
}

func TestUITagManagerUsesTagNamesNotRefs(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("a5a29c23-62e6-4afa-a1f4-2329d9589787")
	project := model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"}
	issue := model.Issue{
		ID:            uuid.MustParse("8df3db99-219a-4a89-9f5e-9727f033c4ea"),
		ProjectID:     projectID,
		OwnerUsername: "bradley",
		ProjectKey:    "TRACK",
		Number:        12,
		Identifier:    "TRACK-12",
		Title:         "Tagged issue",
	}
	tags := []model.IssueTag{
		uiTestIssueTag(projectID, 1, "Customer Beta", model.TagColorGreen),
		uiTestIssueTag(projectID, 2, "Q3 Launch", model.TagColorViolet),
	}

	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "tag-manager-panel", &uiTagManagerData{
		Mode:      "issue",
		Project:   project,
		Issue:     issue,
		HasIssue:  true,
		Tags:      tags[:1],
		Available: tags[1:],
		BackHref:  "/bradley/issues/TRACK-12",
		BackHXGet: "/bradley/issues/TRACK-12/panel",
		BackLabel: "Issue",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	body := buf.String()
	for _, want := range []string{"#Customer Beta", "#Q3 Launch", `value="Q3 Launch"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("tag manager missing %q: %s", want, body)
		}
	}
	for _, unwanted := range []string{`>tag-1<`, `>tag-2<`, `value="tag-2"`} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("tag manager rendered ref %q: %s", unwanted, body)
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
	for _, want := range []string{`[data-search-input]`, `[data-search-option]`, `filterSearchOptions`, `option.dataset.value`, `option.dataset.targetName`, `option.dataset.targetValue`, `data-search-collapsible`, `setSearchOptionsOpen`, `options.hidden = !open`, `search.dataset.searchClearTarget`, `data-project-search`, `!search.hasAttribute("data-project-search")`, `focusin`, `event.key === "Escape"`, `input.form || input.closest("form")`} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing search component behavior %q: %s", want, body)
		}
	}
	for _, want := range []string{`[data-checkbox-reveal]`, `syncCheckboxReveal`, `data-checkbox-reveal-toggle`, `data-checkbox-reveal-panel`, `panel.hidden = !open`, `control.disabled = !open`, `control.value = ""`, `aria-expanded`, `syncCheckboxReveals(event.target)`} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing checkbox reveal behavior %q: %s", want, body)
		}
	}
	for _, want := range []string{`data-close-on-outside`, `closeOpenDropdowns`, `details[data-close-on-outside][open]`, `details.removeAttribute("open")`, `data-option-dropdown-root`, `toggle.click()`} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing dropdown outside-click behavior %q: %s", want, body)
		}
	}
	for _, want := range []string{`data-issue-list-controls`, `rememberIssueListControls(event.target)`, `restoreIssueListControls(event.target)`, `controls.setAttribute("open", "")`} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing issue control reopen behavior %q: %s", want, body)
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
		{name: "empty shell", path: "templates/shell_main.html", wantClass: `class="mx-auto max-w-6xl px-6 py-8"`},
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
		if tt.name != "issue" && strings.Contains(body, `data-lucide="arrow-left"`) {
			t.Fatalf("%s panel should not render app-level back buttons: %s", tt.name, body)
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
