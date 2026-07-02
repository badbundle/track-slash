package server

import (
	"bytes"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestUIIssuePanelRendersTagModal(t *testing.T) {
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
		Status:        model.StatusTodo,
	}
	tags := []model.IssueTag{
		uiTestIssueTag(projectID, 1, "Customer Beta", model.TagColorGreen),
		uiTestIssueTag(projectID, 2, "Q3 Launch", model.TagColorViolet),
	}

	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue:             issue,
		Project:           project,
		EditTags:          true,
		TagModalAttached:  tags[:1],
		TagModalAvailable: tags[1:],
		TagInput:          "Missing",
		TagError:          "Tag not found.",
		BackHref:          "/bradley/projects/TRACK/all",
		BackHXGet:         "/bradley/projects/TRACK/all/panel",
		BackLabel:         "All issues",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		`role="dialog" aria-modal="true" aria-labelledby="issue-tags-title"`,
		`id="issue-tags-title"`,
		`font-mono text-[11px] font-semibold uppercase`,
		`Manage tags`,
		`Search project tags for TRACK-12.`,
		`hx-get="/bradley/issues/TRACK-12/panel"`,
		`Close tag manager`,
		`Attached tags`,
		`#Customer Beta`,
		`method="post" action="/bradley/issues/TRACK-12/tags/tag-1/delete"`,
		`hx-post="/bradley/issues/TRACK-12/tags/tag-1/delete"`,
		`aria-label="Remove #Customer Beta"`,
		`Attach tag`,
		`data-search data-search-collapsible`,
		`data-search-input`,
		`value="Missing"`,
		`Tag not found.`,
		`role="listbox" aria-label="Tag suggestions"`,
		`data-search-option data-value="Q3 Launch"`,
		`data-search-text="#Q3 Launch Q3 Launch"`,
		`#Q3 Launch`,
		`method="post" action="/bradley/issues/TRACK-12/tags"`,
		`hx-post="/bradley/issues/TRACK-12/tags"`,
		`hx-push-url="false"`,
		`href="/bradley/projects/TRACK/tags"`,
		`hx-get="/bradley/projects/TRACK/tags"`,
		`Manage project tags`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("issue tag modal missing %q: %s", want, body)
		}
	}
	for _, unwanted := range []string{`>tag-1<`, `>tag-2<`, `value="tag-2"`} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("issue tag modal rendered ref %q: %s", unwanted, body)
		}
	}

	buf.Reset()
	err = uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue:     issue,
		Project:   project,
		EditTags:  true,
		BackHref:  "/bradley/projects/TRACK/all",
		BackHXGet: "/bradley/projects/TRACK/all/panel",
		BackLabel: "All issues",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate empty modal: %v", err)
	}
	body = buf.String()
	for _, want := range []string{"No tags attached.", "No available tags."} {
		if !strings.Contains(body, want) {
			t.Fatalf("empty issue tag modal missing %q: %s", want, body)
		}
	}
}

func TestUIContextManagerPanelRendersIssueStates(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	contextID := uuid.MustParse("845bc7de-5238-4df2-a024-799f9dbeb5fe")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	project := model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"}
	issue := model.Issue{ID: issueID, ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-7", Title: "Parent issue", Status: model.StatusTodo}
	base := func() uiContextManagerData {
		return uiContextManagerData{
			Mode:           "issue",
			Project:        project,
			Issue:          issue,
			HasIssue:       true,
			BackHref:       "/bradley/issues/TRACK-7",
			BackHXGet:      "/bradley/issues/TRACK-7/panel",
			BackLabel:      "Issue",
			ContextOptions: []uiProjectContextOption{{Value: "Agent notes", Label: "text/plain; charset=utf-8"}},
		}
	}
	renderManager := func(panel uiContextManagerData) string {
		t.Helper()
		var buf bytes.Buffer
		if err := uiTemplates.ExecuteTemplate(&buf, "context-manager-panel", &panel); err != nil {
			t.Fatalf("ExecuteTemplate: %v", err)
		}
		return buf.String()
	}

	emptyBody := renderManager(base())
	for _, want := range []string{"Context", "No context.", `aria-label="Add context"`, `aria-label="Attach context"`, `hx-push-url="/bradley/issues/TRACK-7/context/new"`, `hx-push-url="/bradley/issues/TRACK-7/context/link"`} {
		if !strings.Contains(emptyBody, want) {
			t.Fatalf("empty issue context manager missing %q: %s", want, emptyBody)
		}
	}
	if strings.Contains(emptyBody, `role="dialog" aria-modal="true"`) {
		t.Fatalf("context manager should not render as a modal: %s", emptyBody)
	}

	createPanel := base()
	createPanel.Action = "create"
	createBody := renderManager(createPanel)
	for _, want := range []string{"New context", "Upload text", `placeholder="Context"`, `aria-label="Create context"`, `aria-label="Upload context"`, `name="file"`} {
		if !strings.Contains(createBody, want) {
			t.Fatalf("issue context create state missing %q: %s", want, createBody)
		}
	}
	if strings.Contains(createBody, `placeholder="Search context by title"`) {
		t.Fatalf("issue context create state should not render attach form: %s", createBody)
	}

	attachPanel := base()
	attachPanel.Action = "attach"
	attachPanel.ContextInput = "Agent notes"
	attachPanel.ContextError = "Context already linked."
	attachBody := renderManager(attachPanel)
	for _, want := range []string{`placeholder="Search context by title"`, `value="Agent notes"`, "Context already linked.", `aria-label="Attach context"`} {
		if !strings.Contains(attachBody, want) {
			t.Fatalf("issue context attach state missing %q: %s", want, attachBody)
		}
	}
	if strings.Contains(attachBody, "context-1") {
		t.Fatalf("issue context attach state should not expose context refs: %s", attachBody)
	}
	if strings.Contains(attachBody, `aria-label="Create issue context"`) || strings.Contains(attachBody, `aria-label="Upload issue context"`) {
		t.Fatalf("issue context attach state rendered create-only controls: %s", attachBody)
	}

	populatedPanel := base()
	populatedPanel.Items = []uiContextManagerItem{{
		ID:             contextID,
		Ref:            "context-1",
		Number:         1,
		Scope:          model.ProjectContextScopeIssue,
		Title:          "Agent notes",
		ContentType:    "text/plain; charset=utf-8",
		SourceFilename: nil,
		UpdatedAt:      when,
	}}
	populatedBody := renderManager(populatedPanel)
	for _, want := range []string{"Agent notes", "Issue-only", `aria-label="View context"`, `aria-label="Edit context"`, `aria-label="Remove context"`, `hx-push-url="/bradley/issues/TRACK-7/context/context-1"`, `hx-push-url="/bradley/issues/TRACK-7/context/context-1/edit"`} {
		if !strings.Contains(populatedBody, want) {
			t.Fatalf("populated issue context manager missing %q: %s", want, populatedBody)
		}
	}
	if strings.Contains(populatedBody, `font-mono`) {
		t.Fatalf("populated issue context manager should not render context refs as badges: %s", populatedBody)
	}
	if strings.Contains(populatedBody, "Use the compact path.") {
		t.Fatalf("populated issue context manager should not show body preview: %s", populatedBody)
	}

	viewPanel := populatedPanel
	viewPanel.Action = "view"
	viewPanel.ActiveContextID = contextID
	viewPanel.ActiveContext = model.ProjectContext{
		ID:          contextID,
		ProjectID:   projectID,
		Number:      1,
		Ref:         "context-1",
		Scope:       model.ProjectContextScopeIssue,
		Title:       "Agent notes",
		Kind:        model.ProjectContextKindText,
		ContentType: "text/plain; charset=utf-8",
		Body:        "Use the compact path.",
		UpdatedAt:   when,
	}
	viewBody := renderManager(viewPanel)
	if !strings.Contains(viewBody, "Use the compact path.") {
		t.Fatalf("issue context view state missing body: %s", viewBody)
	}

	editPanel := populatedPanel
	editPanel.Action = "edit"
	editPanel.ActiveContextID = contextID
	editPanel.ActiveContext = viewPanel.ActiveContext
	editPanel.ContextEditTitle = "Agent notes"
	editPanel.ContextEditBody = "Use the compact path."
	editBody := renderManager(editPanel)
	for _, want := range []string{`action="/bradley/issues/TRACK-7/context/context-1"`, `value="Agent notes"`, "Use the compact path.", `aria-label="Save context"`, `aria-label="Cancel editing context"`, `hx-replace-url="/bradley/issues/TRACK-7/context"`} {
		if !strings.Contains(editBody, want) {
			t.Fatalf("issue context edit state missing %q: %s", want, editBody)
		}
	}
}

func TestUIIssuePanelRendersContextModal(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	linkedID := uuid.MustParse("845bc7de-5238-4df2-a024-799f9dbeb5fe")
	availableID := uuid.MustParse("c71792f3-4e4a-4ab2-9cde-e7921ff8a431")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	project := model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"}
	issue := model.Issue{ID: issueID, ProjectID: projectID, OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-7", Title: "Parent issue", Status: model.StatusTodo, CreatedAt: when, UpdatedAt: when}
	base := func() *uiIssuePanelData {
		return &uiIssuePanelData{
			Issue:       issue,
			Project:     project,
			EditContext: true,
			ContextModalItems: []uiContextManagerItem{{
				ID:          linkedID,
				Ref:         "context-1",
				Number:      1,
				Scope:       model.ProjectContextScopeIssue,
				Title:       "Agent notes",
				ContentType: "text/plain; charset=utf-8",
				UpdatedAt:   when,
			}},
			ContextAvailable: []uiContextManagerItem{{
				ID:               availableID,
				Ref:              "context-2",
				Number:           2,
				Scope:            model.ProjectContextScopeProject,
				Title:            "Architecture notes",
				ContentType:      "text/plain; charset=utf-8",
				LinkedIssueCount: 1,
				UpdatedAt:        when,
			}},
			BackHref:  "/bradley/projects/TRACK/all",
			BackHXGet: "/bradley/projects/TRACK/all/panel",
			BackLabel: "All issues",
		}
	}
	render := func(panel *uiIssuePanelData) string {
		t.Helper()
		var buf bytes.Buffer
		if err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", panel); err != nil {
			t.Fatalf("ExecuteTemplate: %v", err)
		}
		return buf.String()
	}

	body := render(base())
	for _, want := range []string{
		`role="dialog" aria-modal="true" aria-labelledby="issue-context-title"`,
		`id="issue-context-title"`,
		`Manage context`,
		`Attach project context`,
		`Search context by title`,
		`data-search-option data-value="Architecture notes"`,
		`Agent notes`,
		`Issue-only`,
		`aria-label="View context"`,
		`aria-label="Edit context"`,
		`aria-label="Remove context"`,
		`hx-push-url="false"`,
		`Add issue context`,
		`Manage project context`,
		`href="/bradley/projects/TRACK/context"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("issue context modal missing %q: %s", want, body)
		}
	}
	contextDetail := body[strings.Index(body, ">Context</dt>"):]
	contextDetail = contextDetail[:min(len(contextDetail), 1100)]
	if !strings.Contains(contextDetail, `hx-get="/bradley/issues/TRACK-7/context"`) || !strings.Contains(contextDetail, `hx-push-url="false"`) {
		t.Fatalf("issue context detail should open modal without URL push: %s", body)
	}
	for _, notWant := range []string{`>Context</h1>`, `hx-push-url="/bradley/issues/TRACK-7/context"`, `role="dialog" aria-modal="true" aria-labelledby="issue-tags-title"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("issue context modal rendered stale markup %q: %s", notWant, body)
		}
	}

	createPanel := base()
	createPanel.ContextAction = "create"
	createPanel.ContextTitle = "Draft note"
	createPanel.ContextBody = "Use this path."
	createBody := render(createPanel)
	for _, want := range []string{"New issue context", "Draft note", "Use this path.", "Upload text", `name="file"`, `aria-label="Create context"`, `aria-label="Upload context"`} {
		if !strings.Contains(createBody, want) {
			t.Fatalf("issue context create modal missing %q: %s", want, createBody)
		}
	}
	if strings.Contains(createBody, `data-search-option data-value="Architecture notes"`) {
		t.Fatalf("issue context create modal should swap out attach search: %s", createBody)
	}

	editPanel := base()
	editPanel.ContextAction = "edit"
	editPanel.ActiveContextID = linkedID
	editPanel.ActiveContext = model.ProjectContext{ID: linkedID, ProjectID: projectID, Number: 1, Ref: "context-1", Scope: model.ProjectContextScopeIssue, Title: "Agent notes", Kind: model.ProjectContextKindText, ContentType: "text/plain; charset=utf-8", Body: "Use the compact path.", UpdatedAt: when}
	editPanel.ContextEditTitle = "Agent notes"
	editPanel.ContextEditBody = "Use the compact path."
	editBody := render(editPanel)
	for _, want := range []string{`action="/bradley/issues/TRACK-7/context/context-1"`, `value="Agent notes"`, "Use the compact path.", `aria-label="Save context"`, `aria-label="Cancel editing context"`} {
		if !strings.Contains(editBody, want) {
			t.Fatalf("issue context edit modal missing %q: %s", want, editBody)
		}
	}
}
