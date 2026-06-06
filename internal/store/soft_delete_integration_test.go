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

func TestSoftDeleteUser(t *testing.T) {
	env := newSprintsEnv(t)
	u := mustCreateUser(t, env, "soft-user-"+uniqueDigits(time.Now().UnixNano(), 8)+"@example.com")
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
		StartDate: date(2026, 8, 1),
		EndDate:   date(2026, 8, 14),
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("CreateSprint under deleted project err = %v, want ErrNotFound", err)
	}
}

func TestCompletedSprintCannotBeSoftDeleted(t *testing.T) {
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
