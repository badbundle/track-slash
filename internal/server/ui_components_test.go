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

func TestUIIssueSummaryRowReflowsForNarrowScreens(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-summary-row", model.Issue{
		Identifier: "TRACK-12",
		Title:      "Responsive issue row",
		Status:     model.StatusDone,
		Priority:   model.PriorityP1,
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`data-issue-summary-row`,
		`grid min-w-0 gap-2 sm:grid-cols-[7rem_auto_minmax(0,1fr)_auto] sm:items-center sm:gap-3`,
		`sm:contents`,
		`break-words`,
		`sm:truncate`,
		`flex-wrap`,
		`sm:flex-nowrap`,
		`TRACK-12`,
		`Responsive issue row`,
		`aria-label="Priority P1"`,
		`Done`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("responsive issue summary row missing %q: %s", want, body)
		}
	}
}

func TestUIIssueListsUseSharedResponsiveSummaryRow(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		path string
		call string
	}{
		{name: "personal work", path: "templates/work_panel.html", call: `{{template "issue-summary-row" .Issue}}`},
		{name: "project issue lists", path: "templates/project_issue_lists.html", call: `{{template "issue-summary-row" .}}`},
		{name: "planned sprint", path: "templates/project_panel_planned.html", call: `{{template "issue-summary-row" .}}`},
		{name: "sub-issues", path: "templates/issue_panel_relationships.html", call: `{{template "issue-summary-row" .}}`},
	} {
		src, err := uiTemplateFS.ReadFile(tt.path)
		if err != nil {
			t.Fatalf("%s: read template: %v", tt.name, err)
		}
		body := string(src)
		if !strings.Contains(body, tt.call) {
			t.Fatalf("%s missing shared responsive issue row call %q: %s", tt.name, tt.call, body)
		}
		if strings.Contains(body, `issue-summary-row-cells`) {
			t.Fatalf("%s still uses the fixed-grid issue row cells: %s", tt.name, body)
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
		CanWrite:    true,
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
		CanWrite:  true,
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

func TestUIShellRendersResponsiveAccessibleSidebar(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "shell", uiShellData{
		User:        model.User{Name: "Demo User", Username: "demo"},
		CurrentView: "projects",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`data-mobile-app-bar class="flex h-14`,
		`data-mobile-sidebar-toggle`,
		`aria-controls="app-sidebar"`,
		`aria-expanded="false"`,
		`data-mobile-sidebar-backdrop`,
		`hidden aria-hidden="true" tabindex="-1"`,
		`id="app-sidebar" data-mobile-sidebar`,
		`data-mobile-sidebar aria-hidden="true" inert`,
		`w-72 max-w-[calc(100vw-2rem)]`,
		`[data-mobile-sidebar] { visibility: hidden; transform: translateX(-100%); }`,
		`data-mobile-sidebar-close`,
		`data-mobile-sidebar-open`,
		`syncMobileSidebar`,
		`openMobileSidebar`,
		`closeMobileSidebar`,
		`let mobileSidebarOpen = false`,
		`mobileSidebar.setAttribute("aria-hidden", visible ? "false" : "true")`,
		`mobileSidebar.inert = !visible`,
		`mobileAppBar.inert = open`,
		`mainContent.inert = open`,
		`mobileSidebarToggle.setAttribute("aria-expanded", open ? "true" : "false")`,
		`mobileSidebarBackdrop.hidden = !open`,
		`mobileSidebarClose.focus()`,
		`mobileSidebarReturnFocus.focus()`,
		`mobileSidebarToggle.addEventListener("click", openMobileSidebar)`,
		`mobileSidebarClose.addEventListener("click", () => closeMobileSidebar())`,
		`mobileSidebarBackdrop.addEventListener("click", () => closeMobileSidebar())`,
		`mobileSidebar.addEventListener("click"`,
		`event.target.closest("a[href]")`,
		`event.key !== "Escape" || !mobileSidebarOpen`,
		`mobileSidebarBreakpoint.addEventListener("change", handleMobileSidebarBreakpoint)`,
		`focusFirstDesktopSidebarControl`,
		`focusWasInSidebar`,
		`focusWasInAppBar`,
		`(min-width: 768px)`,
		`@supports (height: 100dvh)`,
		`height: 100vh`,
		`height: 100dvh`,
		`.modal-panel { max-height: calc(100vh - 3rem); max-height: calc(100dvh - 3rem); }`,
		`overflow-x-hidden overflow-y-auto`,
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
		`>@demo<`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing sidebar behavior %q: %s", want, body)
		}
	}
	mediaStart := strings.Index(body, `@media (min-width: 768px)`)
	desktopCollapseStart := strings.Index(body, `html[data-sidebar-collapsed] .app-shell > aside`)
	if mediaStart < 0 || desktopCollapseStart < mediaStart {
		t.Fatalf("desktop sidebar collapse CSS must be scoped to the md breakpoint: %s", body)
	}
	mobileStateStart := strings.Index(body, `const mobileSidebar =`)
	mobileStateEnd := strings.Index(body, `const setNavLoading =`)
	if mobileStateStart < 0 || mobileStateEnd <= mobileStateStart {
		t.Fatalf("shell missing isolated mobile sidebar state: %s", body)
	}
	if strings.Contains(body[mobileStateStart:mobileStateEnd], `localStorage`) {
		t.Fatalf("mobile drawer state must not share desktop collapse persistence: %s", body[mobileStateStart:mobileStateEnd])
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
	for _, roleLabel := range []string{">Member<", ">Admin<"} {
		if strings.Contains(body, roleLabel) {
			t.Fatalf("member menu should show @username instead of role label %q: %s", roleLabel, body)
		}
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
	for _, want := range []string{`data-disclosure-toggle`, `setDisclosureOpen`, `aria-controls`, `panel.hidden = !open`, `aria-expanded`, `data-disclosure-icon`, `chevron-up`, `chevron-down`} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing disclosure behavior %q: %s", want, body)
		}
	}
	for _, want := range []string{`attachmentMarkdownSnippet`, `data-attachment-copy-markdown`, `writeClipboardText`, `navigator.clipboard.writeText`, `data-copy-label`, `contentUrl}?inline=1`} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing attachment copy/preview behavior %q: %s", want, body)
		}
	}
	for _, want := range []string{
		`.markdown-body { min-width: 0; overflow-wrap: anywhere; }`,
		`.markdown-body h1 { font-size: 1.5rem; }`,
		`.markdown-body h2 { font-size: 1.25rem; }`,
		`.markdown-body h3 { font-size: 1.125rem; }`,
		`.markdown-body table { width: 100%; border-collapse: collapse; font-size: 0.875rem; }`,
		`.markdown-body table { display: block; max-width: 100%; overflow-x: auto; }`,
		`.markdown-body th, .markdown-body td { border: 1px solid rgb(203 213 225); padding: 0.375rem 0.5rem; text-align: left; vertical-align: top; }`,
		`@media (prefers-color-scheme: dark) {`,
		`.markdown-body { color: rgb(226 232 240); }`,
		`.markdown-body h1, .markdown-body h2, .markdown-body h3, .markdown-body h4, .markdown-body h5, .markdown-body h6 { color: rgb(241 245 249); }`,
		`.markdown-body th, .markdown-body td { border-color: rgb(51 65 85); }`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing markdown CSS %q: %s", want, body)
		}
	}
	if strings.Contains(body, `.dark .markdown-body`) {
		t.Fatalf("shell markdown dark-mode CSS should follow the system media query: %s", body)
	}
}

func TestUIPanelsUseConsistentResponsivePageFrame(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		path string
	}{
		{name: "work", path: "templates/work_panel.html"},
		{name: "projects", path: "templates/projects_panel.html"},
		{name: "new project", path: "templates/new_project_panel.html"},
		{name: "new issue", path: "templates/new_issue_panel.html"},
		{name: "project", path: "templates/project_panel.html"},
		{name: "deleted issues", path: "templates/project_deleted_panel.html"},
		{name: "deleted issue", path: "templates/deleted_issue_panel.html"},
		{name: "issue", path: "templates/issue_panel.html"},
		{name: "context manager", path: "templates/context_manager.html"},
		{name: "tag manager", path: "templates/tag_manager.html"},
		{name: "settings", path: "templates/settings.html"},
		{name: "tokens", path: "templates/tokens.html"},
		{name: "empty shell", path: "templates/shell_main.html"},
	} {
		src, err := uiTemplateFS.ReadFile(tt.path)
		if err != nil {
			t.Fatalf("%s: read template: %v", tt.name, err)
		}
		body := string(src)
		wantClass := `class="mx-auto max-w-6xl px-4 py-4 sm:px-6 sm:py-6"`
		if !strings.Contains(body, wantClass) {
			t.Fatalf("%s panel missing responsive page frame %q: %s", tt.name, wantClass, body)
		}
		if strings.Contains(body, "max-w-5xl") {
			t.Fatalf("%s panel still uses narrower page width: %s", tt.name, body)
		}
		if tt.name != "issue" && tt.name != "tag manager" && strings.Contains(body, `data-lucide="arrow-left"`) {
			t.Fatalf("%s panel should not render app-level back buttons: %s", tt.name, body)
		}
	}
}

func TestUIModalAndSpecialRowsStayContainedOnNarrowScreens(t *testing.T) {
	t.Parallel()

	var modal bytes.Buffer
	if err := uiTemplates.ExecuteTemplate(&modal, "modal-open", uiModalData{
		ID:         "responsive-modal",
		Title:      "Responsive modal",
		WidthClass: "max-w-lg",
	}); err != nil {
		t.Fatalf("render modal: %v", err)
	}
	for _, want := range []string{
		`role="dialog"`,
		`aria-modal="true"`,
		`modal-panel`,
		`overflow-y-auto`,
	} {
		if !strings.Contains(modal.String(), want) {
			t.Fatalf("responsive modal missing %q: %s", want, modal.String())
		}
	}

	modal.Reset()
	if err := uiTemplates.ExecuteTemplate(&modal, "modal-open", uiModalData{
		ID:               "client-modal",
		Title:            "Client modal",
		WidthClass:       "max-w-md",
		CancelLabel:      "Close client modal",
		ClientControlled: true,
	}); err != nil {
		t.Fatalf("render client modal: %v", err)
	}
	for _, want := range []string{
		`id="client-modal" data-client-modal`,
		`class="fixed inset-0 z-50 hidden`,
		`data-modal-close`,
		`aria-label="Close client modal"`,
	} {
		if !strings.Contains(modal.String(), want) {
			t.Fatalf("client modal missing %q: %s", want, modal.String())
		}
	}

	for _, tt := range []struct {
		name string
		path string
		want []string
	}{
		{
			name: "linked issue",
			path: "templates/issue_panel_relationships.html",
			want: []string{
				`grid-cols-[minmax(0,1fr)_auto]`,
				`sm:grid-cols-[4.75rem_minmax(0,1fr)_auto]`,
				`col-span-2 row-start-2`,
				`sm:row-start-1`,
				`flex-wrap`,
				`sm:flex-nowrap`,
			},
		},
		{
			name: "deleted issue",
			path: "templates/project_deleted_panel.html",
			want: []string{
				`class="grid gap-3`,
				`sm:grid-cols-[7rem_auto_minmax(0,1fr)_auto_auto]`,
				`sm:contents`,
			},
		},
	} {
		src, err := uiTemplateFS.ReadFile(tt.path)
		if err != nil {
			t.Fatalf("%s: read template: %v", tt.name, err)
		}
		body := string(src)
		for _, want := range tt.want {
			if !strings.Contains(body, want) {
				t.Fatalf("%s row missing responsive marker %q: %s", tt.name, want, body)
			}
		}
	}
}

func TestUIShellIncludesClientModalBehavior(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := uiTemplates.ExecuteTemplate(&buf, "shell", uiShellData{User: model.User{Username: "demo"}}); err != nil {
		t.Fatalf("render shell: %v", err)
	}
	for _, want := range []string{
		`const setClientModalOpen = (modal, open, trigger = null) => {`,
		`event.target.closest("[data-modal-open]")`,
		`event.target.closest("[data-modal-close]")`,
		`event.target.closest("[data-client-modal]")`,
		`document.querySelector("[data-client-modal]:not(.hidden)")`,
		`modal.querySelector("input[type='file']")?.focus()`,
		`returnFocus.focus()`,
	} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("shell missing client modal behavior %q", want)
		}
	}
}

func TestUIActionMenusStayInsideNarrowHeaders(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		path string
	}{
		{name: "project", path: "templates/project_panel.html"},
		{name: "issue", path: "templates/issue_panel_header.html"},
	} {
		src, err := uiTemplateFS.ReadFile(tt.path)
		if err != nil {
			t.Fatalf("%s: read template: %v", tt.name, err)
		}
		body := string(src)
		for _, want := range []string{
			`grid-cols-[minmax(0,1fr)_auto]`,
			`justify-self-end`,
			`absolute right-0 top-11 z-20 w-48`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s action menu missing narrow-screen alignment %q: %s", tt.name, want, body)
			}
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
			{Label: "Three", Icon: "triangle", Href: "/three", HXGet: "/three/panel", HXTarget: "#main", HXPushURL: "/three", MobileOverflow: true},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{`aria-label="Example views"`, "flex flex-nowrap", "gap-4 sm:gap-6", "border-b-4", `data-lucide="circle"`, `href="/one"`, `hx-get="/one/panel"`, `aria-current="page"`, `href="/two"`, `href="/three"`, `hidden lg:inline-flex`} {
		if !strings.Contains(body, want) {
			t.Fatalf("tab bar missing markup %q: %s", want, body)
		}
	}
	for _, unwanted := range []string{"flex-wrap", "overflow-x-auto"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("tab bar should stay on one line instead of using %q: %s", unwanted, body)
		}
	}
}
