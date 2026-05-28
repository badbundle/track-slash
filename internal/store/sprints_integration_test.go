package store_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bradleymackey/track-slash/internal/migrations"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

// sprintsTestEnv prepares a Store backed by a real Postgres and returns it
// alongside a freshly created project for test isolation.
type sprintsTestEnv struct {
	ctx       context.Context
	store     *store.Store
	projectID uuid.UUID
}

func newSprintsEnv(t *testing.T) *sprintsTestEnv {
	t.Helper()
	dbURL := testDatabaseURL()
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL / DATABASE_URL not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	sqlDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := migrations.Up(sqlDB); err != nil {
		t.Fatalf("migrations.Up: %v", err)
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	s := store.New(pool)
	proj, err := s.CreateProject(ctx, uniqueProjectKey(t), "sprints-test", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return &sprintsTestEnv{ctx: ctx, store: s, projectID: proj.ID}
}

func uniqueProjectKey(t *testing.T) string {
	t.Helper()
	// Project key must match ^[A-Z][A-Z0-9]{1,9}$. Use last 9 digits of UnixNano.
	n := time.Now().UnixNano()
	return "P" + uniqueDigits(n, 9)
}

func uniqueDigits(n int64, width int) string {
	out := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		out[i] = byte('0' + (n % 10))
		n /= 10
	}
	return string(out)
}

func mustCreateSprint(t *testing.T, env *sprintsTestEnv, name string, start, end time.Time) model.Sprint {
	t.Helper()
	sp, err := env.store.CreateSprint(env.ctx, store.CreateSprintParams{
		ProjectID: env.projectID,
		Name:      name,
		StartDate: start,
		EndDate:   end,
	})
	if err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}
	return sp
}

func mustActivate(t *testing.T, env *sprintsTestEnv, id uuid.UUID) {
	t.Helper()
	st := model.SprintStatusActive
	if _, err := env.store.UpdateSprint(env.ctx, id, store.UpdateSprintParams{Status: &st}); err != nil {
		t.Fatalf("activate sprint: %v", err)
	}
}

func mustCreateIssue(t *testing.T, env *sprintsTestEnv, title string) model.Issue {
	t.Helper()
	iss, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID: env.projectID,
		Title:     title,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	return iss
}

func assignIssueToSprint(t *testing.T, env *sprintsTestEnv, issueID, sprintID uuid.UUID) {
	t.Helper()
	if _, err := env.store.UpdateIssue(env.ctx, issueID, store.UpdateIssueParams{SprintID: &sprintID}); err != nil {
		t.Fatalf("assign issue to sprint: %v", err)
	}
}

func setIssueStatus(t *testing.T, env *sprintsTestEnv, issueID uuid.UUID, st model.Status) {
	t.Helper()
	if _, err := env.store.UpdateIssue(env.ctx, issueID, store.UpdateIssueParams{Status: &st}); err != nil {
		t.Fatalf("set issue status: %v", err)
	}
}

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestCreateAndGetSprint(t *testing.T) {
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "S1", date(2026, 6, 1), date(2026, 6, 14))

	if sp.Status != model.SprintStatusPlanned {
		t.Fatalf("Status = %s, want planned", sp.Status)
	}
	if sp.CompletedAt != nil {
		t.Fatalf("CompletedAt = %v, want nil", sp.CompletedAt)
	}

	got, err := env.store.GetSprint(env.ctx, sp.ID)
	if err != nil {
		t.Fatalf("GetSprint: %v", err)
	}
	if got.ID != sp.ID || got.Name != "S1" || got.ProjectID != env.projectID {
		t.Fatalf("Get mismatch: %+v", got)
	}
}

func TestCreateSprintBadDateRange(t *testing.T) {
	env := newSprintsEnv(t)
	_, err := env.store.CreateSprint(env.ctx, store.CreateSprintParams{
		ProjectID: env.projectID,
		Name:      "bad",
		StartDate: date(2026, 6, 14),
		EndDate:   date(2026, 6, 1),
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestListSprintsOrderingAndStatusFilter(t *testing.T) {
	env := newSprintsEnv(t)
	c := mustCreateSprint(t, env, "C", date(2026, 7, 1), date(2026, 7, 14))
	a := mustCreateSprint(t, env, "A", date(2026, 6, 1), date(2026, 6, 14))
	b := mustCreateSprint(t, env, "B", date(2026, 6, 15), date(2026, 6, 30))

	all, err := env.store.ListSprints(env.ctx, store.ListSprintsParams{ProjectID: env.projectID})
	if err != nil {
		t.Fatalf("ListSprints: %v", err)
	}
	wantOrder := []uuid.UUID{a.ID, b.ID, c.ID}
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3", len(all))
	}
	for i, id := range wantOrder {
		if all[i].ID != id {
			t.Fatalf("position %d: got %s, want %s", i, all[i].ID, id)
		}
	}

	mustActivate(t, env, a.ID)
	active, err := env.store.ListSprints(env.ctx, store.ListSprintsParams{
		ProjectID: env.projectID,
		Status:    model.SprintStatusActive,
	})
	if err != nil {
		t.Fatalf("ListSprints active: %v", err)
	}
	if len(active) != 1 || active[0].ID != a.ID {
		t.Fatalf("active list = %+v", active)
	}
}

func TestUpdateSprintActivationUnique(t *testing.T) {
	env := newSprintsEnv(t)
	a := mustCreateSprint(t, env, "A", date(2026, 6, 1), date(2026, 6, 14))
	b := mustCreateSprint(t, env, "B", date(2026, 6, 15), date(2026, 6, 30))

	mustActivate(t, env, a.ID)

	st := model.SprintStatusActive
	_, err := env.store.UpdateSprint(env.ctx, b.ID, store.UpdateSprintParams{Status: &st})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("activating second sprint: err = %v, want ErrConflict", err)
	}

	// Different project — activating A2 in a fresh project should succeed.
	other, err := env.store.CreateProject(env.ctx, uniqueProjectKey(t), "other", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	sp2, err := env.store.CreateSprint(env.ctx, store.CreateSprintParams{
		ProjectID: other.ID, Name: "X",
		StartDate: date(2026, 6, 1), EndDate: date(2026, 6, 14),
	})
	if err != nil {
		t.Fatalf("CreateSprint other: %v", err)
	}
	if _, err := env.store.UpdateSprint(env.ctx, sp2.ID, store.UpdateSprintParams{Status: &st}); err != nil {
		t.Fatalf("activate in other project: %v", err)
	}
}

func TestUpdateSprintRejectsCompletedTransition(t *testing.T) {
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "S", date(2026, 6, 1), date(2026, 6, 14))
	mustActivate(t, env, sp.ID)

	st := model.SprintStatusCompleted
	_, err := env.store.UpdateSprint(env.ctx, sp.ID, store.UpdateSprintParams{Status: &st})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestCompleteSprintRollsForwardToNextPlanned(t *testing.T) {
	env := newSprintsEnv(t)
	active := mustCreateSprint(t, env, "active", date(2026, 6, 1), date(2026, 6, 14))
	next := mustCreateSprint(t, env, "next", date(2026, 6, 15), date(2026, 6, 30))
	mustCreateSprint(t, env, "later", date(2026, 7, 1), date(2026, 7, 14))
	mustActivate(t, env, active.ID)

	i1 := mustCreateIssue(t, env, "todo-1")
	i2 := mustCreateIssue(t, env, "todo-2")
	i3 := mustCreateIssue(t, env, "in-prog")
	i4 := mustCreateIssue(t, env, "done")
	for _, id := range []uuid.UUID{i1.ID, i2.ID, i3.ID, i4.ID} {
		assignIssueToSprint(t, env, id, active.ID)
	}
	setIssueStatus(t, env, i3.ID, model.StatusInProgress)
	setIssueStatus(t, env, i4.ID, model.StatusDone)

	out, err := env.store.CompleteSprint(env.ctx, active.ID)
	if err != nil {
		t.Fatalf("CompleteSprint: %v", err)
	}
	if out.Status != model.SprintStatusCompleted {
		t.Fatalf("Status = %s, want completed", out.Status)
	}
	if out.CompletedAt == nil {
		t.Fatal("CompletedAt is nil")
	}

	for _, id := range []uuid.UUID{i1.ID, i2.ID, i3.ID} {
		iss, err := env.store.GetIssue(env.ctx, id)
		if err != nil {
			t.Fatalf("GetIssue %s: %v", id, err)
		}
		if iss.SprintID == nil || *iss.SprintID != next.ID {
			t.Fatalf("issue %s sprint = %v, want %s", id, iss.SprintID, next.ID)
		}
	}

	done, err := env.store.GetIssue(env.ctx, i4.ID)
	if err != nil {
		t.Fatalf("GetIssue done: %v", err)
	}
	if done.SprintID == nil || *done.SprintID != active.ID {
		t.Fatalf("done issue sprint = %v, want %s (stays)", done.SprintID, active.ID)
	}
}

func TestCompleteSprintFallsBackToBacklog(t *testing.T) {
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "only", date(2026, 6, 1), date(2026, 6, 14))
	mustActivate(t, env, sp.ID)
	iss := mustCreateIssue(t, env, "stuck")
	assignIssueToSprint(t, env, iss.ID, sp.ID)

	if _, err := env.store.CompleteSprint(env.ctx, sp.ID); err != nil {
		t.Fatalf("CompleteSprint: %v", err)
	}

	got, err := env.store.GetIssue(env.ctx, iss.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.SprintID != nil {
		t.Fatalf("SprintID = %v, want nil (backlog)", got.SprintID)
	}
}

func TestCompleteSprintRejectsNonActive(t *testing.T) {
	env := newSprintsEnv(t)
	planned := mustCreateSprint(t, env, "planned", date(2026, 6, 1), date(2026, 6, 14))
	if _, err := env.store.CompleteSprint(env.ctx, planned.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("planned: err = %v, want ErrConflict", err)
	}

	active := mustCreateSprint(t, env, "active", date(2026, 7, 1), date(2026, 7, 14))
	mustActivate(t, env, active.ID)
	if _, err := env.store.CompleteSprint(env.ctx, active.ID); err != nil {
		t.Fatalf("CompleteSprint active: %v", err)
	}
	// Re-completing already completed sprint.
	if _, err := env.store.CompleteSprint(env.ctx, active.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("completed: err = %v, want ErrConflict", err)
	}
}

func TestCompleteSprintConcurrent(t *testing.T) {
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "race", date(2026, 6, 1), date(2026, 6, 14))
	mustActivate(t, env, sp.ID)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := env.store.CompleteSprint(env.ctx, sp.ID)
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	successes, conflicts := 0, 0
	for _, e := range errs {
		switch {
		case e == nil:
			successes++
		case errors.Is(e, store.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected err: %v", e)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes = %d, conflicts = %d, want 1/1", successes, conflicts)
	}
}

func TestUpdateIssueSetSprintCrossProjectRejected(t *testing.T) {
	env := newSprintsEnv(t)

	otherProj, err := env.store.CreateProject(env.ctx, uniqueProjectKey(t), "other-cross", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	otherSprint, err := env.store.CreateSprint(env.ctx, store.CreateSprintParams{
		ProjectID: otherProj.ID, Name: "x",
		StartDate: date(2026, 6, 1), EndDate: date(2026, 6, 14),
	})
	if err != nil {
		t.Fatalf("CreateSprint other: %v", err)
	}

	iss := mustCreateIssue(t, env, "task")
	_, err = env.store.UpdateIssue(env.ctx, iss.ID, store.UpdateIssueParams{SprintID: &otherSprint.ID})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestListIssuesBacklogFilter(t *testing.T) {
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "S", date(2026, 6, 1), date(2026, 6, 14))
	mustActivate(t, env, sp.ID)

	backlogIss := mustCreateIssue(t, env, "backlog-1")
	inSprint := mustCreateIssue(t, env, "in-sprint-1")
	assignIssueToSprint(t, env, inSprint.ID, sp.ID)

	backlog, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{
		ProjectID: env.projectID, Backlog: true,
	})
	if err != nil {
		t.Fatalf("ListIssues backlog: %v", err)
	}
	if len(backlog) != 1 || backlog[0].ID != backlogIss.ID {
		t.Fatalf("backlog = %+v, want only %s", backlog, backlogIss.ID)
	}

	bySprint, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{
		ProjectID: env.projectID, SprintID: &sp.ID,
	})
	if err != nil {
		t.Fatalf("ListIssues by sprint: %v", err)
	}
	if len(bySprint) != 1 || bySprint[0].ID != inSprint.ID {
		t.Fatalf("by sprint = %+v, want only %s", bySprint, inSprint.ID)
	}
}
