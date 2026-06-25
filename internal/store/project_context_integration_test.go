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
		Cursor:    &store.ProjectContextsCursor{Number: page1[0].Number},
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("ListProjectContexts page2: %v", err)
	}
	if len(page2) != 1 || more || page2[0].Ref != second.Ref {
		t.Fatalf("page2 = %+v more=%v", page2, more)
	}

	issueA := mustCreateIssue(t, env, "A")
	issueB := mustCreateIssue(t, env, "B")
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
	if summaries[0].LinkedIssueCount != 2 {
		t.Fatalf("linked count = %d, want 2", summaries[0].LinkedIssueCount)
	}

	issueContexts, _, err := env.store.ListContextsForIssue(env.ctx, store.ListContextsForIssueParams{
		IssueID: issueA.ID,
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ListContextsForIssue: %v", err)
	}
	if len(issueContexts) != 1 || issueContexts[0].Body != first.Body {
		t.Fatalf("issue contexts = %+v, want first body", issueContexts)
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
	if issueContexts[0].Body != updatedBody {
		t.Fatalf("linked context body = %q, want %q", issueContexts[0].Body, updatedBody)
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
	if len(issueContexts) != 0 {
		t.Fatalf("issue contexts after explicit unlink = %+v, want empty", issueContexts)
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
	if len(issueContexts) != 0 {
		t.Fatalf("issue contexts after context delete = %+v, want empty", issueContexts)
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
	if _, err := env.store.CreateProjectContext(env.ctx, store.CreateProjectContextParams{
		ProjectID:   env.projectID,
		Title:       "Title",
		Body:        "",
		CreatedByID: project.OwnerID,
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("empty body err = %v, want ErrConflict", err)
	}
}
