package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestGetProjectStatsCountsAndTopAssignees(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-2 * 24 * time.Hour)
	old := now.Add(-8 * 24 * time.Hour)

	alice := mustCreateStatsUser(t, env, "stats-alice", "Alice Example")
	bob := mustCreateStatsUser(t, env, "stats-bob", "Bob Example")
	aaron := mustCreateStatsUser(t, env, "stats-aaron", "Aaron Assignee")
	carol := mustCreateStatsUser(t, env, "stats-carol", "Carol Example")
	eve := mustCreateStatsUser(t, env, "stats-eve", "Eve Example")
	frank := mustCreateStatsUser(t, env, "stats-frank", "Frank Example")
	deletedUser := mustCreateStatsUser(t, env, "stats-deleted", "Deleted Example")

	createStatsIssue(t, env, "alice recent todo", model.StatusTodo, &alice.ID, recent)
	createStatsIssue(t, env, "alice recent progress", model.StatusInProgress, &alice.ID, recent)
	createStatsIssue(t, env, "alice old done", model.StatusDone, &alice.ID, old)
	createStatsIssue(t, env, "alice old closed", model.StatusClosed, &alice.ID, old)
	parent := createStatsIssue(t, env, "parent with child", model.StatusTodo, nil, old)
	child, err := env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "alice recent child",
		AssigneeID:    &alice.ID,
	})
	if err != nil {
		t.Fatalf("CreateSubIssue: %v", err)
	}
	setStatsIssueCreatedAt(t, env, child.ID, recent)

	createStatsIssue(t, env, "bob old todo", model.StatusTodo, &bob.ID, old)
	createStatsIssue(t, env, "bob recent done", model.StatusDone, &bob.ID, recent)
	createStatsIssue(t, env, "bob recent closed", model.StatusClosed, &bob.ID, recent)
	createStatsIssue(t, env, "aaron recent progress", model.StatusInProgress, &aaron.ID, recent)
	createStatsIssue(t, env, "aaron old done", model.StatusDone, &aaron.ID, old)
	createStatsIssue(t, env, "carol old todo", model.StatusTodo, &carol.ID, old)
	createStatsIssue(t, env, "carol recent progress", model.StatusInProgress, &carol.ID, recent)
	createStatsIssue(t, env, "eve recent todo", model.StatusTodo, &eve.ID, recent)
	createStatsIssue(t, env, "frank old todo", model.StatusTodo, &frank.ID, old)
	createStatsIssue(t, env, "unassigned recent done", model.StatusDone, nil, recent)
	createStatsIssue(t, env, "deleted user recent closed", model.StatusClosed, &deletedUser.ID, recent)
	if err := env.store.DeleteUser(env.ctx, deletedUser.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	deletedIssue := createStatsIssue(t, env, "deleted closed issue", model.StatusClosed, &bob.ID, recent)
	if err := env.store.DeleteIssue(env.ctx, deletedIssue.ID); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}

	otherProject, err := env.store.CreateProject(env.ctx, uniqueProjectKey(t), "other stats project", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	otherIssue, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID: otherProject.ID,
		Title:     "other project done",
	})
	if err != nil {
		t.Fatalf("CreateIssue other: %v", err)
	}
	done := model.StatusDone
	if _, err := env.store.UpdateIssue(env.ctx, otherIssue.ID, store.UpdateIssueParams{Status: &done}); err != nil {
		t.Fatalf("UpdateIssue other: %v", err)
	}
	setStatsIssueCreatedAt(t, env, otherIssue.ID, recent)

	stats, err := env.store.GetProjectStats(env.ctx, store.ProjectStatsParams{
		ProjectID: env.projectID,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("GetProjectStats: %v", err)
	}

	requireStatsCounts(t, stats.AllTime, model.ProjectIssueStatusCounts{
		Total:      17,
		Todo:       7,
		InProgress: 3,
		Done:       4,
		Closed:     3,
	})
	requireStatsCounts(t, stats.Last7Days, model.ProjectIssueStatusCounts{
		Total:      10,
		Todo:       3,
		InProgress: 3,
		Done:       2,
		Closed:     2,
	})
	if len(stats.TopAssignees) != 5 {
		t.Fatalf("top assignees len = %d, want 5: %+v", len(stats.TopAssignees), stats.TopAssignees)
	}
	wantIDs := []uuid.UUID{alice.ID, bob.ID, aaron.ID, carol.ID, eve.ID}
	for i, wantID := range wantIDs {
		if stats.TopAssignees[i].UserID != wantID {
			t.Fatalf("top assignee %d = %s, want %s; all = %+v", i, stats.TopAssignees[i].UserID, wantID, stats.TopAssignees)
		}
	}
	requireStatsCounts(t, stats.TopAssignees[0].Counts, model.ProjectIssueStatusCounts{
		Total:      5,
		Todo:       2,
		InProgress: 1,
		Done:       1,
		Closed:     1,
	})
	requireStatsCounts(t, stats.TopAssignees[1].Counts, model.ProjectIssueStatusCounts{
		Total:  3,
		Todo:   1,
		Done:   1,
		Closed: 1,
	})
	for _, assignee := range stats.TopAssignees {
		if assignee.UserID == deletedUser.ID || assignee.UserID == frank.ID {
			t.Fatalf("top assignees included excluded user: %+v", stats.TopAssignees)
		}
	}

	if _, err := env.store.GetProjectStats(env.ctx, store.ProjectStatsParams{ProjectID: uuid.New(), Now: now}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing project err = %v, want ErrNotFound", err)
	}
}

func TestGetProjectStatsDefaultWindowAndNoAssignees(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	if _, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID: env.projectID,
		Title:     "fresh unassigned issue",
	}); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	stats, err := env.store.GetProjectStats(env.ctx, store.ProjectStatsParams{ProjectID: env.projectID})
	if err != nil {
		t.Fatalf("GetProjectStats: %v", err)
	}
	requireStatsCounts(t, stats.AllTime, model.ProjectIssueStatusCounts{Total: 1, Todo: 1})
	requireStatsCounts(t, stats.Last7Days, model.ProjectIssueStatusCounts{Total: 1, Todo: 1})
	if len(stats.TopAssignees) != 0 {
		t.Fatalf("top assignees = %+v, want empty", stats.TopAssignees)
	}
}

func mustCreateStatsUser(t *testing.T, env *sprintsTestEnv, usernamePrefix, name string) model.User {
	t.Helper()
	username := usernamePrefix + "-" + uuid.NewString()[:8]
	user, err := env.store.CreateUserProfile(env.ctx, username, username+"@example.com", name)
	if err != nil {
		t.Fatalf("CreateUserProfile %s: %v", usernamePrefix, err)
	}
	return user
}

func createStatsIssue(t *testing.T, env *sprintsTestEnv, title string, status model.Status, assigneeID *uuid.UUID, createdAt time.Time) model.Issue {
	t.Helper()
	issue, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID:  env.projectID,
		Title:      title,
		AssigneeID: assigneeID,
	})
	if err != nil {
		t.Fatalf("CreateIssue %q: %v", title, err)
	}
	if status != model.StatusTodo {
		params := store.UpdateIssueParams{Status: &status}
		if status == model.StatusClosed {
			reason := model.CloseReasonWontDo
			params.CloseReason = &reason
		}
		issue, err = env.store.UpdateIssue(env.ctx, issue.ID, params)
		if err != nil {
			t.Fatalf("UpdateIssue %q: %v", title, err)
		}
	}
	setStatsIssueCreatedAt(t, env, issue.ID, createdAt)
	return issue
}

func setStatsIssueCreatedAt(t *testing.T, env *sprintsTestEnv, issueID uuid.UUID, createdAt time.Time) {
	t.Helper()
	if _, err := env.pool.Exec(env.ctx, `
		UPDATE issues SET created_at = $1, updated_at = $1 WHERE id = $2
	`, createdAt, issueID); err != nil {
		t.Fatalf("set issue created_at: %v", err)
	}
}

func requireStatsCounts(t *testing.T, got, want model.ProjectIssueStatusCounts) {
	t.Helper()
	if got != want {
		t.Fatalf("counts = %+v, want %+v", got, want)
	}
}
