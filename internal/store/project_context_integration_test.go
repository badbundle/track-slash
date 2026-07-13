package store_test

import (
	"errors"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func mustCreateProjectContext(t *testing.T, env *sprintsTestEnv, title, body string) model.ProjectContext {
	t.Helper()
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	contextItem, err := env.store.CreateProjectContext(env.ctx, store.CreateProjectContextParams{
		ProjectID:   env.projectID,
		Title:       title,
		Kind:        model.ProjectContextKindText,
		ContentType: "text/plain; charset=utf-8",
		Body:        body,
		CreatedByID: project.OwnerID,
	})
	if err != nil {
		t.Fatalf("CreateProjectContext: %v", err)
	}
	return contextItem
}

func TestProjectContextCRUDAndSharedIssueLinks(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	filename := "guide.md"
	first := mustCreateProjectContext(t, env, "Architecture", "Use store transactions.")
	second, err := env.store.CreateProjectContext(env.ctx, store.CreateProjectContextParams{
		ProjectID:      env.projectID,
		Title:          "Markdown",
		Kind:           model.ProjectContextKindText,
		ContentType:    "text/markdown; charset=utf-8",
		Body:           "# Notes",
		SourceFilename: &filename,
		CreatedByID:    project.OwnerID,
	})
	if err != nil {
		t.Fatalf("CreateProjectContext second: %v", err)
	}
	if first.Ref != "context-1" || second.Ref != "context-2" {
		t.Fatalf("refs = %q %q, want context-1/context-2", first.Ref, second.Ref)
	}
	if first.Position == nil || *first.Position != 1 || second.Position == nil || *second.Position != 2 {
		t.Fatalf("positions = %v %v, want 1/2", first.Position, second.Position)
	}
	if first.Scope != model.ProjectContextScopeProject || second.Scope != model.ProjectContextScopeProject {
		t.Fatalf("project context scopes = %q %q, want project/project", first.Scope, second.Scope)
	}
	if second.SourceFilename == nil || *second.SourceFilename != filename {
		t.Fatalf("source filename = %v, want %q", second.SourceFilename, filename)
	}
	byNumber, err := env.store.GetProjectContextByProjectNumber(env.ctx, env.projectID, first.Number)
	if err != nil {
		t.Fatalf("GetProjectContextByProjectNumber: %v", err)
	}
	if byNumber.ID != first.ID || byNumber.Ref != first.Ref {
		t.Fatalf("by number = %+v, want first context", byNumber)
	}
	if _, err := env.store.GetProjectContextByProjectNumber(env.ctx, env.projectID, 9999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetProjectContextByProjectNumber missing err = %v, want ErrNotFound", err)
	}

	page1, more, err := env.store.ListProjectContexts(env.ctx, store.ListProjectContextsParams{
		ProjectID: env.projectID,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("ListProjectContexts page1: %v", err)
	}
	if len(page1) != 1 || !more || page1[0].Ref != first.Ref || page1[0].LinkedIssueCount != 0 {
		t.Fatalf("page1 = %+v more=%v", page1, more)
	}
	page2, more, err := env.store.ListProjectContexts(env.ctx, store.ListProjectContextsParams{
		ProjectID: env.projectID,
		Cursor:    &store.ProjectContextsCursor{Position: *page1[0].Position},
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("ListProjectContexts page2: %v", err)
	}
	if len(page2) != 1 || more || page2[0].Ref != second.Ref {
		t.Fatalf("page2 = %+v more=%v", page2, more)
	}
	moveToFirst := int64(1)
	moved, err := env.store.UpdateProjectContext(env.ctx, store.UpdateProjectContextParams{
		ID: second.ID, Position: &moveToFirst, UpdatedByID: project.OwnerID,
	})
	if err != nil {
		t.Fatalf("move context: %v", err)
	}
	if moved.Position == nil || *moved.Position != 1 {
		t.Fatalf("moved position = %v, want 1", moved.Position)
	}
	ordered, _, err := env.store.ListProjectContexts(env.ctx, store.ListProjectContextsParams{ProjectID: env.projectID, Limit: 10})
	if err != nil || len(ordered) != 2 || ordered[0].ID != second.ID || ordered[1].ID != first.ID {
		t.Fatalf("reordered contexts = %+v err=%v", ordered, err)
	}

	issueA := mustCreateIssue(t, env, "A")
	issueB := mustCreateIssue(t, env, "B")
	issueScoped, err := env.store.CreateIssueContext(env.ctx, store.CreateIssueContextParams{
		IssueID:     issueA.ID,
		Title:       "Issue only",
		Kind:        model.ProjectContextKindText,
		ContentType: "text/plain; charset=utf-8",
		Body:        "Only relevant here.",
		CreatedByID: project.OwnerID,
	})
	if err != nil {
		t.Fatalf("CreateIssueContext: %v", err)
	}
	if issueScoped.Ref != "context-3" || issueScoped.Scope != model.ProjectContextScopeIssue || issueScoped.ProjectID != env.projectID {
		t.Fatalf("issue-scoped context = %+v, want context-3 issue scope in project", issueScoped)
	}
	if _, err := env.store.CreateIssueContextLink(env.ctx, issueB.ID, issueScoped.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("link issue-scoped context err = %v, want ErrConflict", err)
	}

	linkA, err := env.store.CreateIssueContextLink(env.ctx, issueA.ID, first.ID)
	if err != nil {
		t.Fatalf("CreateIssueContextLink A: %v", err)
	}
	if _, err := env.store.CreateIssueContextLink(env.ctx, issueB.ID, first.ID); err != nil {
		t.Fatalf("CreateIssueContextLink B: %v", err)
	}
	if _, err := env.store.CreateIssueContextLink(env.ctx, issueA.ID, first.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate link err = %v, want ErrConflict", err)
	}
	if got, err := env.store.ProjectIDForIssueContextLink(env.ctx, linkA.ID); err != nil || got != env.projectID {
		t.Fatalf("ProjectIDForIssueContextLink = %s, %v; want %s", got, err, env.projectID)
	}
	if got, err := env.store.ProjectIDForProjectContext(env.ctx, first.ID); err != nil || got != env.projectID {
		t.Fatalf("ProjectIDForProjectContext = %s, %v; want %s", got, err, env.projectID)
	}

	summaries, _, err := env.store.ListProjectContexts(env.ctx, store.ListProjectContextsParams{
		ProjectID: env.projectID,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListProjectContexts after links: %v", err)
	}
	if len(summaries) != 2 || summaries[1].LinkedIssueCount != 2 {
		t.Fatalf("project context summaries = %+v, want two project contexts and first linked twice", summaries)
	}

	issueContexts, _, err := env.store.ListContextsForIssue(env.ctx, store.ListContextsForIssueParams{
		IssueID: issueA.ID,
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ListContextsForIssue: %v", err)
	}
	if len(issueContexts) != 2 || issueContexts[0].Body != first.Body || issueContexts[1].Body != issueScoped.Body {
		t.Fatalf("issue contexts = %+v, want shared then issue-scoped body", issueContexts)
	}

	updatedBody := "Use store transactions and project context."
	updatedTitle := "Architecture guide"
	updated, err := env.store.UpdateProjectContext(env.ctx, store.UpdateProjectContextParams{
		ID:          first.ID,
		Title:       &updatedTitle,
		Body:        &updatedBody,
		UpdatedByID: project.OwnerID,
	})
	if err != nil {
		t.Fatalf("UpdateProjectContext: %v", err)
	}
	if updated.Title != updatedTitle || updated.Body != updatedBody || !updated.UpdatedAt.After(updated.CreatedAt) {
		t.Fatalf("updated context = %+v", updated)
	}
	issueContexts, _, err = env.store.ListContextsForIssue(env.ctx, store.ListContextsForIssueParams{
		IssueID: issueA.ID,
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ListContextsForIssue after update: %v", err)
	}
	if issueContexts[0].Body != updatedBody || issueContexts[1].Body != issueScoped.Body {
		t.Fatalf("linked context bodies = %+v, want updated shared and unchanged issue-scoped", issueContexts)
	}

	issues, _, err := env.store.ListIssuesForContext(env.ctx, store.ListIssuesForContextParams{
		ContextID: first.ID,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListIssuesForContext: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("linked issues len = %d, want 2", len(issues))
	}

	if err := env.store.DeleteIssueContextLink(env.ctx, issueA.ID, first.ID); err != nil {
		t.Fatalf("DeleteIssueContextLink: %v", err)
	}
	if err := env.store.DeleteIssueContextLink(env.ctx, issueA.ID, first.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteIssueContextLink missing err = %v, want ErrNotFound", err)
	}
	issueContexts, _, err = env.store.ListContextsForIssue(env.ctx, store.ListContextsForIssueParams{
		IssueID: issueA.ID,
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ListContextsForIssue after unlink: %v", err)
	}
	if len(issueContexts) != 1 || issueContexts[0].ID != issueScoped.ID {
		t.Fatalf("issue contexts after explicit unlink = %+v, want issue-scoped only", issueContexts)
	}
	if _, err := env.store.CreateIssueContextLink(env.ctx, issueA.ID, first.ID); err != nil {
		t.Fatalf("CreateIssueContextLink relink A: %v", err)
	}

	if err := env.store.DeleteProjectContext(env.ctx, first.ID); err != nil {
		t.Fatalf("DeleteProjectContext: %v", err)
	}
	if _, err := env.store.GetProjectContext(env.ctx, first.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetProjectContext deleted err = %v, want ErrNotFound", err)
	}
	issueContexts, _, err = env.store.ListContextsForIssue(env.ctx, store.ListContextsForIssueParams{
		IssueID: issueA.ID,
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ListContextsForIssue after delete: %v", err)
	}
	if len(issueContexts) != 1 || issueContexts[0].ID != issueScoped.ID {
		t.Fatalf("issue contexts after context delete = %+v, want issue-scoped only", issueContexts)
	}
	if err := env.store.DeleteProjectContext(env.ctx, issueScoped.ID); err != nil {
		t.Fatalf("DeleteProjectContext issue-scoped: %v", err)
	}
	issueContexts, _, err = env.store.ListContextsForIssue(env.ctx, store.ListContextsForIssueParams{
		IssueID: issueA.ID,
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ListContextsForIssue after issue-scoped delete: %v", err)
	}
	if len(issueContexts) != 0 {
		t.Fatalf("issue contexts after issue-scoped delete = %+v, want empty", issueContexts)
	}
}

func TestProjectContextConflictAndValidation(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	otherProject, err := env.store.CreateProjectForUser(env.ctx, project.OwnerID, uniqueProjectKey(t), "other", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser other: %v", err)
	}
	contextOther, err := env.store.CreateProjectContext(env.ctx, store.CreateProjectContextParams{
		ProjectID:   otherProject.ID,
		Title:       "Other",
		Kind:        model.ProjectContextKindText,
		ContentType: "text/plain; charset=utf-8",
		Body:        "Other project",
		CreatedByID: project.OwnerID,
	})
	if err != nil {
		t.Fatalf("CreateProjectContext other: %v", err)
	}
	issue := mustCreateIssue(t, env, "A")

	if _, err := env.store.CreateIssueContextLink(env.ctx, issue.ID, contextOther.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("cross-project link err = %v, want ErrConflict", err)
	}

	if _, err := env.store.CreateProjectContext(env.ctx, store.CreateProjectContextParams{
		ProjectID:   env.projectID,
		Title:       "",
		Body:        "body",
		CreatedByID: project.OwnerID,
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("empty title err = %v, want ErrConflict", err)
	}
	if _, err := env.store.CreateIssueContext(env.ctx, store.CreateIssueContextParams{
		IssueID:     issue.ID,
		Title:       "Title",
		Body:        "",
		CreatedByID: project.OwnerID,
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("empty issue context body err = %v, want ErrConflict", err)
	}
}

func TestBulkIssueContextLinksAreAtomicAndIdempotent(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	firstContext := mustCreateProjectContext(t, env, "Architecture", "Shared architecture notes.")
	secondContext := mustCreateProjectContext(t, env, "Runbook", "Shared operational notes.")
	issueA := mustCreateIssue(t, env, "A")
	issueB := mustCreateIssue(t, env, "B")

	if _, err := env.store.CreateIssueContextLink(env.ctx, issueA.ID, firstContext.ID); err != nil {
		t.Fatalf("CreateIssueContextLink existing: %v", err)
	}
	links := []store.IssueContextLinkPair{
		{IssueNumber: issueA.Number, ContextNumber: firstContext.Number},
		{IssueNumber: issueA.Number, ContextNumber: secondContext.Number},
		{IssueNumber: issueB.Number, ContextNumber: firstContext.Number},
		{IssueNumber: issueB.Number, ContextNumber: firstContext.Number},
	}
	result, err := env.store.CreateIssueContextLinks(store.WithActor(env.ctx, project.OwnerID), store.CreateIssueContextLinksParams{
		ProjectID: env.projectID,
		Links:     links,
	})
	if err != nil {
		t.Fatalf("CreateIssueContextLinks: %v", err)
	}
	if result != (store.CreateIssueContextLinksResult{Requested: 4, Created: 2, Unchanged: 2}) {
		t.Fatalf("bulk result = %+v", result)
	}

	contextsA, _, err := env.store.ListContextsForIssue(env.ctx, store.ListContextsForIssueParams{IssueID: issueA.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListContextsForIssue A: %v", err)
	}
	contextsB, _, err := env.store.ListContextsForIssue(env.ctx, store.ListContextsForIssueParams{IssueID: issueB.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListContextsForIssue B: %v", err)
	}
	if len(contextsA) != 2 || contextsA[0].ID != firstContext.ID || contextsA[1].ID != secondContext.ID {
		t.Fatalf("issue A contexts = %+v", contextsA)
	}
	if len(contextsB) != 1 || contextsB[0].ID != firstContext.ID {
		t.Fatalf("issue B contexts = %+v", contextsB)
	}

	entries, _, err := env.store.ListProjectChangelog(env.ctx, store.ListProjectChangelogParams{ProjectID: env.projectID, Limit: 100})
	if err != nil {
		t.Fatalf("ListProjectChangelog: %v", err)
	}
	bulkEntries := 0
	for _, entry := range entries {
		if entry.Entity != "issue_context_link" || entry.ActorID == nil || *entry.ActorID != project.OwnerID {
			continue
		}
		bulkEntries++
		if entry.IssueID == nil || (entry.Summary != "Attached context Runbook to "+issueA.Identifier && entry.Summary != "Attached context Architecture to "+issueB.Identifier) {
			t.Fatalf("bulk changelog entry = %+v", entry)
		}
	}
	if bulkEntries != 2 {
		t.Fatalf("attributed bulk changelog entries = %d, want 2", bulkEntries)
	}

	result, err = env.store.CreateIssueContextLinks(store.WithActor(env.ctx, project.OwnerID), store.CreateIssueContextLinksParams{
		ProjectID: env.projectID,
		Links:     links,
	})
	if err != nil {
		t.Fatalf("CreateIssueContextLinks repeat: %v", err)
	}
	if result != (store.CreateIssueContextLinksResult{Requested: 4, Created: 0, Unchanged: 4}) {
		t.Fatalf("repeat bulk result = %+v", result)
	}
	entries, _, err = env.store.ListProjectChangelog(env.ctx, store.ListProjectChangelogParams{ProjectID: env.projectID, Limit: 100})
	if err != nil {
		t.Fatalf("ListProjectChangelog repeat: %v", err)
	}
	repeatedEntries := 0
	for _, entry := range entries {
		if entry.Entity == "issue_context_link" && entry.ActorID != nil && *entry.ActorID == project.OwnerID {
			repeatedEntries++
		}
	}
	if repeatedEntries != bulkEntries {
		t.Fatalf("bulk changelog entries after repeat = %d, want %d", repeatedEntries, bulkEntries)
	}

	empty, err := env.store.CreateIssueContextLinks(env.ctx, store.CreateIssueContextLinksParams{ProjectID: env.projectID})
	if err != nil || empty != (store.CreateIssueContextLinksResult{}) {
		t.Fatalf("empty bulk result = %+v err=%v", empty, err)
	}
}

func TestBulkIssueContextLinksRejectInvalidBatchAtomically(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	shared := mustCreateProjectContext(t, env, "Shared", "Shared context.")
	target := mustCreateIssue(t, env, "Target")
	deleted := mustCreateIssue(t, env, "Deleted")
	if err := env.store.DeleteIssue(env.ctx, deleted.ID); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	scopeOwner := mustCreateIssue(t, env, "Issue scope owner")
	issueScoped, err := env.store.CreateIssueContext(env.ctx, store.CreateIssueContextParams{
		IssueID:     scopeOwner.ID,
		Title:       "Issue only",
		Body:        "Only this issue.",
		CreatedByID: project.OwnerID,
	})
	if err != nil {
		t.Fatalf("CreateIssueContext: %v", err)
	}

	valid := store.IssueContextLinkPair{IssueNumber: target.Number, ContextNumber: shared.Number}
	for _, test := range []struct {
		name    string
		invalid store.IssueContextLinkPair
	}{
		{name: "missing issue", invalid: store.IssueContextLinkPair{IssueNumber: 999999, ContextNumber: shared.Number}},
		{name: "deleted issue", invalid: store.IssueContextLinkPair{IssueNumber: deleted.Number, ContextNumber: shared.Number}},
		{name: "missing context", invalid: store.IssueContextLinkPair{IssueNumber: target.Number, ContextNumber: 999999}},
		{name: "issue scoped context", invalid: store.IssueContextLinkPair{IssueNumber: target.Number, ContextNumber: issueScoped.Number}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := env.store.CreateIssueContextLinks(env.ctx, store.CreateIssueContextLinksParams{
				ProjectID: env.projectID,
				Links:     []store.IssueContextLinkPair{valid, test.invalid},
			})
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("CreateIssueContextLinks err = %v, want ErrNotFound", err)
			}
			contexts, _, err := env.store.ListContextsForIssue(env.ctx, store.ListContextsForIssueParams{IssueID: target.ID, Limit: 10})
			if err != nil {
				t.Fatalf("ListContextsForIssue: %v", err)
			}
			if len(contexts) != 0 {
				t.Fatalf("target contexts after rejected batch = %+v", contexts)
			}
		})
	}
}
