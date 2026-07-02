package store_test

import (
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestProjectChangelogRecordsActorChangesAndPagination(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	issue := mustCreateIssue(t, env, "original")

	renamed := "renamed"
	if _, err := env.store.UpdateIssue(store.WithActor(env.ctx, project.OwnerID), issue.ID, store.UpdateIssueParams{Title: &renamed}); err != nil {
		t.Fatalf("UpdateIssue rename: %v", err)
	}
	if _, err := env.store.UpdateIssue(store.WithActor(env.ctx, project.OwnerID), issue.ID, store.UpdateIssueParams{Title: &renamed}); err != nil {
		t.Fatalf("UpdateIssue no-op: %v", err)
	}

	entries, hasMore, err := env.store.ListProjectChangelog(env.ctx, store.ListProjectChangelogParams{
		ProjectID: env.projectID,
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("ListProjectChangelog: %v", err)
	}
	if hasMore {
		t.Fatalf("hasMore = true, want false")
	}
	var updates, creates int
	var updateEntry model.ProjectChangelogEntry
	for _, entry := range entries {
		if strings.HasPrefix(entry.Summary, "Updated issue ") {
			updates++
			updateEntry = entry
			if entry.Actor == nil || entry.Actor.ID != project.OwnerID {
				t.Fatalf("update actor = %+v, want owner %s", entry.Actor, project.OwnerID)
			}
			if entry.TargetRef != issue.Identifier || entry.TargetTitle != renamed {
				t.Fatalf("update target = %s/%s, want %s/%s", entry.TargetRef, entry.TargetTitle, issue.Identifier, renamed)
			}
			if len(entry.Details.Changes) != 1 || entry.Details.Changes[0] != (model.ProjectChangelogChange{Field: "title", Label: "Title", From: "original", To: renamed}) {
				t.Fatalf("update changes = %+v", entry.Details.Changes)
			}
		}
		if strings.HasPrefix(entry.Summary, "Created issue ") {
			creates++
			if entry.Actor != nil || entry.ActorID != nil {
				t.Fatalf("create actor = %+v/%v, want nil", entry.Actor, entry.ActorID)
			}
		}
	}
	if updates != 1 {
		t.Fatalf("update entries = %d, want 1; entries=%+v", updates, entries)
	}
	gotProjectID, err := env.store.ProjectIDForProjectChangelog(env.ctx, updateEntry.ID)
	if err != nil {
		t.Fatalf("ProjectIDForProjectChangelog: %v", err)
	}
	if gotProjectID != env.projectID {
		t.Fatalf("ProjectIDForProjectChangelog = %s, want %s", gotProjectID, env.projectID)
	}
	if creates != 1 {
		t.Fatalf("create entries = %d, want 1; entries=%+v", creates, entries)
	}

	first, more, err := env.store.ListProjectChangelog(env.ctx, store.ListProjectChangelogParams{
		ProjectID: env.projectID,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("ListProjectChangelog page1: %v", err)
	}
	if len(first) != 1 || !more {
		t.Fatalf("page1 len/more = %d/%v, want 1/true", len(first), more)
	}
	second, _, err := env.store.ListProjectChangelog(env.ctx, store.ListProjectChangelogParams{
		ProjectID: env.projectID,
		Limit:     1,
		Cursor: &store.ProjectChangelogCursor{
			CreatedAt: first[0].CreatedAt,
			ID:        first[0].ID,
		},
	})
	if err != nil {
		t.Fatalf("ListProjectChangelog page2: %v", err)
	}
	if len(second) != 1 || second[0].ID == first[0].ID {
		t.Fatalf("page2 = %+v after page1 %+v", second, first)
	}
}
