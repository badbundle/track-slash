package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestSoftDeleteIssue(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	iss := mustCreateIssue(t, env, "delete me")

	if err := env.store.DeleteIssue(env.ctx, iss.ID); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	if _, err := env.store.GetIssue(env.ctx, iss.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetIssue deleted err = %v, want ErrNotFound", err)
	}
	if err := env.store.DeleteIssue(env.ctx, iss.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteIssue second err = %v, want ErrNotFound", err)
	}

	list, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{ProjectID: env.projectID, Limit: 10})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("ListIssues len = %d, want 0", len(list))
	}

	batch, err := env.store.ListIssuesByIDs(env.ctx, []uuid.UUID{iss.ID})
	if err != nil {
		t.Fatalf("ListIssuesByIDs: %v", err)
	}
	if len(batch) != 0 {
		t.Fatalf("ListIssuesByIDs len = %d, want 0", len(batch))
	}

	_, err = env.store.UpdateIssue(env.ctx, iss.ID, store.UpdateIssueParams{Description: ptr("nope")})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UpdateIssue deleted err = %v, want ErrNotFound", err)
	}
}

func TestRestoreIssue(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	parent := mustCreateIssue(t, env, "restore parent")
	child, err := env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "restore child",
	})
	if err != nil {
		t.Fatalf("CreateSubIssue: %v", err)
	}

	if err := env.store.DeleteIssue(env.ctx, parent.ID); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	deleted, err := env.store.GetDeletedIssueByOwnerKeyNumber(env.ctx, parent.OwnerUsername, parent.ProjectKey, parent.Number)
	if err != nil {
		t.Fatalf("GetDeletedIssueByOwnerKeyNumber: %v", err)
	}
	if deleted.ID != parent.ID {
		t.Fatalf("deleted issue ID = %s, want %s", deleted.ID, parent.ID)
	}

	restored, err := env.store.RestoreIssue(env.ctx, parent.ID)
	if err != nil {
		t.Fatalf("RestoreIssue: %v", err)
	}
	if restored.ID != parent.ID || restored.Identifier != parent.Identifier {
		t.Fatalf("restored issue = %+v, want %s", restored, parent.Identifier)
	}
	if _, err := env.store.GetIssue(env.ctx, child.ID); err != nil {
		t.Fatalf("GetIssue child after restore: %v", err)
	}
	if _, err := env.store.GetDeletedIssueByOwnerKeyNumber(env.ctx, parent.OwnerUsername, parent.ProjectKey, parent.Number); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetDeletedIssue restored err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.RestoreIssue(env.ctx, parent.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("RestoreIssue second err = %v, want ErrNotFound", err)
	}

	secondParent := mustCreateIssue(t, env, "restore child parent")
	secondChild, err := env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
		ParentIssueID: secondParent.ID,
		Title:         "restore child directly",
	})
	if err != nil {
		t.Fatalf("CreateSubIssue second child: %v", err)
	}
	if err := env.store.DeleteIssue(env.ctx, secondParent.ID); err != nil {
		t.Fatalf("DeleteIssue second parent: %v", err)
	}
	restoredChild, err := env.store.RestoreIssue(env.ctx, secondChild.ID)
	if err != nil {
		t.Fatalf("RestoreIssue child: %v", err)
	}
	if restoredChild.ID != secondChild.ID {
		t.Fatalf("restored child ID = %s, want %s", restoredChild.ID, secondChild.ID)
	}
	if _, err := env.store.GetIssue(env.ctx, secondParent.ID); err != nil {
		t.Fatalf("GetIssue parent after child restore: %v", err)
	}
}

func TestListDeletedIssues(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	live := mustCreateIssue(t, env, "live issue")
	deleted := mustCreateIssue(t, env, "deleted issue")
	parent := mustCreateIssue(t, env, "deleted parent")
	child, err := env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "deleted child",
	})
	if err != nil {
		t.Fatalf("CreateSubIssue child: %v", err)
	}
	otherProject, err := env.store.CreateProject(env.ctx, uniqueProjectKey(t), "other deleted project", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	otherDeleted, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID: otherProject.ID,
		Title:     "other deleted issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue other deleted: %v", err)
	}
	if err := env.store.DeleteIssue(env.ctx, deleted.ID); err != nil {
		t.Fatalf("DeleteIssue deleted: %v", err)
	}
	if err := env.store.DeleteIssue(env.ctx, parent.ID); err != nil {
		t.Fatalf("DeleteIssue parent: %v", err)
	}
	if err := env.store.DeleteIssue(env.ctx, otherDeleted.ID); err != nil {
		t.Fatalf("DeleteIssue other: %v", err)
	}

	got, hasMore, err := env.store.ListDeletedIssues(env.ctx, store.ListDeletedIssuesParams{
		ProjectID: env.projectID,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListDeletedIssues: %v", err)
	}
	if hasMore {
		t.Fatalf("ListDeletedIssues hasMore = true, want false")
	}
	wantIDs := []uuid.UUID{deleted.ID, parent.ID, child.ID}
	if len(got) != len(wantIDs) {
		t.Fatalf("ListDeletedIssues len = %d, want %d: %+v", len(got), len(wantIDs), got)
	}
	for i, wantID := range wantIDs {
		if got[i].ID != wantID {
			t.Fatalf("ListDeletedIssues[%d] = %s, want %s: %+v", i, got[i].ID, wantID, got)
		}
	}
	for _, iss := range got {
		if iss.ID == live.ID || iss.ID == otherDeleted.ID {
			t.Fatalf("ListDeletedIssues included excluded issue: %+v", iss)
		}
	}

	page, hasMore, err := env.store.ListDeletedIssues(env.ctx, store.ListDeletedIssuesParams{
		ProjectID: env.projectID,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("ListDeletedIssues page: %v", err)
	}
	if len(page) != 1 || page[0].ID != deleted.ID || !hasMore {
		t.Fatalf("ListDeletedIssues page len=%d first=%+v hasMore=%v, want first %s and hasMore", len(page), page, hasMore, deleted.ID)
	}
	next, hasMore, err := env.store.ListDeletedIssues(env.ctx, store.ListDeletedIssuesParams{
		ProjectID: env.projectID,
		Cursor:    &store.IssuesCursor{Number: page[0].Number},
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListDeletedIssues next: %v", err)
	}
	if hasMore || len(next) != 2 || next[0].ID != parent.ID || next[1].ID != child.ID {
		t.Fatalf("ListDeletedIssues next = %+v hasMore=%v, want parent/child only", next, hasMore)
	}
}

func TestSoftDeleteUser(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	u := mustCreateUser(t, env, "soft-user-"+uniqueDigits(time.Now().UnixNano(), 8)+"@example.com")
	if _, err := env.store.GrantProjectAccess(env.ctx, env.projectID, u.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	iss, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID:  env.projectID,
		Title:      "assigned",
		AssigneeID: &u.ID,
		ReporterID: &u.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	if err := env.store.DeleteUser(env.ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := env.store.GetUser(env.ctx, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetUser deleted err = %v, want ErrNotFound", err)
	}
	if err := env.store.DeleteUser(env.ctx, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteUser second err = %v, want ErrNotFound", err)
	}
	users, _, err := env.store.ListUsers(env.ctx, store.ListUsersParams{Limit: 100})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	for _, got := range users {
		if got.ID == u.ID {
			t.Fatalf("deleted user present in list")
		}
	}
	gotIssue, err := env.store.GetIssue(env.ctx, iss.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if gotIssue.AssigneeID == nil || *gotIssue.AssigneeID != u.ID || gotIssue.ReporterID == nil || *gotIssue.ReporterID != u.ID {
		t.Fatalf("assignee/reporter changed after user soft-delete: %+v", gotIssue)
	}
}

func TestSoftDeleteSprint(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	planned := mustCreateSprint(t, env, "planned", date(2026, 6, 1), date(2026, 6, 14))
	active := mustCreateSprint(t, env, "active", date(2026, 6, 15), date(2026, 6, 30))
	mustActivate(t, env, active.ID)
	completed := mustCreateSprint(t, env, "completed", date(2026, 7, 1), date(2026, 7, 14))

	if err := env.store.DeleteSprint(env.ctx, active.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("DeleteSprint active err = %v, want ErrConflict", err)
	}
	if _, err := env.store.CompleteSprint(env.ctx, active.ID); err != nil {
		t.Fatalf("CompleteSprint active: %v", err)
	}
	mustActivate(t, env, completed.ID)
	if _, err := env.store.CompleteSprint(env.ctx, completed.ID); err != nil {
		t.Fatalf("CompleteSprint completed: %v", err)
	}

	if err := env.store.DeleteSprint(env.ctx, completed.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("DeleteSprint completed err = %v, want ErrConflict", err)
	}
	if err := env.store.DeleteSprint(env.ctx, active.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("DeleteSprint completed former active err = %v, want ErrConflict", err)
	}
	if err := env.store.DeleteSprint(env.ctx, planned.ID); err != nil {
		t.Fatalf("DeleteSprint planned: %v", err)
	}
	if _, err := env.store.GetSprint(env.ctx, planned.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetSprint deleted err = %v, want ErrNotFound", err)
	}
	if err := env.store.DeleteSprint(env.ctx, planned.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteSprint second err = %v, want ErrNotFound", err)
	}

	_, err := env.store.UpdateSprint(env.ctx, planned.ID, store.UpdateSprintParams{Name: ptr("nope")})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UpdateSprint deleted err = %v, want ErrNotFound", err)
	}
}

func TestSoftDeleteProjectCascades(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	iss := mustCreateIssue(t, env, "project issue")
	sp := mustCreateSprint(t, env, "project sprint", date(2026, 7, 1), date(2026, 7, 14))

	if err := env.store.DeleteProject(env.ctx, env.projectID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if _, err := env.store.GetProject(env.ctx, env.projectID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetProject deleted err = %v, want ErrNotFound", err)
	}
	if err := env.store.DeleteProject(env.ctx, env.projectID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteProject second err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.GetIssue(env.ctx, iss.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetIssue cascade err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.GetSprint(env.ctx, sp.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetSprint cascade err = %v, want ErrNotFound", err)
	}

	var issueDeleted, sprintDeleted bool
	if err := env.pool.QueryRow(env.ctx, `SELECT deleted_at IS NOT NULL FROM issues WHERE id = $1`, iss.ID).Scan(&issueDeleted); err != nil {
		t.Fatalf("query issue deleted_at: %v", err)
	}
	if err := env.pool.QueryRow(env.ctx, `SELECT deleted_at IS NOT NULL FROM sprints WHERE id = $1`, sp.ID).Scan(&sprintDeleted); err != nil {
		t.Fatalf("query sprint deleted_at: %v", err)
	}
	if !issueDeleted || !sprintDeleted {
		t.Fatalf("cascade deleted flags issue=%v sprint=%v, want true true", issueDeleted, sprintDeleted)
	}

	_, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{ProjectID: env.projectID, Title: "nope"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("CreateIssue under deleted project err = %v, want ErrNotFound", err)
	}
	_, err = env.store.CreateSprint(env.ctx, store.CreateSprintParams{
		ProjectID: env.projectID,
		Name:      "nope",
		StartDate: datePtr(date(2026, 8, 1)),
		EndDate:   datePtr(date(2026, 8, 14)),
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("CreateSprint under deleted project err = %v, want ErrNotFound", err)
	}
}

func TestCompletedSprintCannotBeSoftDeleted(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "completed", date(2026, 9, 1), date(2026, 9, 14))
	mustActivate(t, env, sp.ID)
	if _, err := env.store.CompleteSprint(env.ctx, sp.ID); err != nil {
		t.Fatalf("CompleteSprint: %v", err)
	}
	if err := env.store.DeleteSprint(env.ctx, sp.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("DeleteSprint completed err = %v, want ErrConflict", err)
	}
	got, err := env.store.GetSprint(env.ctx, sp.ID)
	if err != nil {
		t.Fatalf("GetSprint completed after delete attempt: %v", err)
	}
	if got.Status != model.SprintStatusCompleted {
		t.Fatalf("status = %s, want completed", got.Status)
	}
}

func ptr[T any](v T) *T {
	return &v
}
