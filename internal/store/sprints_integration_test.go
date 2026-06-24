package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/bradleymackey/track-slash/internal/testutil"
)

// sprintsTestEnv prepares a Store backed by a real Postgres and returns it
// alongside a freshly created project for test isolation.
type sprintsTestEnv struct {
	ctx       context.Context
	pool      *pgxpool.Pool
	store     *store.Store
	projectID uuid.UUID
}

func newSprintsEnv(t *testing.T) *sprintsTestEnv {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	db := testutil.NewMigratedDatabase(t)
	pool := db.Pool

	s := store.New(pool)
	owner, err := s.CreateOrUpdateAdminUser(ctx, "owner-"+uniqueProjectKey(t)+"@example.com", "Owner")
	if err != nil {
		t.Fatalf("CreateOrUpdateAdminUser: %v", err)
	}
	proj, err := s.CreateProjectForUser(ctx, owner.ID, uniqueProjectKey(t), "sprints-test", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser: %v", err)
	}
	return &sprintsTestEnv{ctx: ctx, pool: pool, store: s, projectID: proj.ID}
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
	t.Parallel()
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "S1", date(2026, 6, 1), date(2026, 6, 14))

	if sp.Status != model.SprintStatusPlanned {
		t.Fatalf("Status = %s, want planned", sp.Status)
	}
	if sp.CompletedAt != nil {
		t.Fatalf("CompletedAt = %v, want nil", sp.CompletedAt)
	}
	if sp.PlannedOrder == nil || *sp.PlannedOrder != 1 {
		t.Fatalf("PlannedOrder = %v, want 1", sp.PlannedOrder)
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
	t.Parallel()
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
	t.Parallel()
	env := newSprintsEnv(t)
	c := mustCreateSprint(t, env, "C", date(2026, 7, 1), date(2026, 7, 14))
	a := mustCreateSprint(t, env, "A", date(2026, 6, 1), date(2026, 6, 14))
	b := mustCreateSprint(t, env, "B", date(2026, 6, 15), date(2026, 6, 30))

	all, more, err := env.store.ListSprints(env.ctx, store.ListSprintsParams{ProjectID: env.projectID, Limit: 100})
	if err != nil {
		t.Fatalf("ListSprints: %v", err)
	}
	if more {
		t.Fatalf("hasMore=true unexpectedly")
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
	active, _, err := env.store.ListSprints(env.ctx, store.ListSprintsParams{
		ProjectID: env.projectID,
		Status:    model.SprintStatusActive,
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("ListSprints active: %v", err)
	}
	if len(active) != 1 || active[0].ID != a.ID {
		t.Fatalf("active list = %+v", active)
	}
	if active[0].PlannedOrder != nil {
		t.Fatalf("active planned_order = %v, want nil", active[0].PlannedOrder)
	}
}

func TestCreateSprintAppendsPlannedOrder(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	a := mustCreateSprint(t, env, "A", date(2026, 6, 1), date(2026, 6, 14))
	b := mustCreateSprint(t, env, "B", date(2026, 6, 15), date(2026, 6, 30))
	c := mustCreateSprint(t, env, "C", date(2026, 7, 1), date(2026, 7, 14))

	for i, sp := range []model.Sprint{a, b, c} {
		want := int64(i + 1)
		if sp.PlannedOrder == nil || *sp.PlannedOrder != want {
			t.Fatalf("%s planned_order = %v, want %d", sp.Name, sp.PlannedOrder, want)
		}
	}
}

func TestReorderPlannedSprints(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	a := mustCreateSprint(t, env, "A", date(2026, 6, 1), date(2026, 6, 14))
	b := mustCreateSprint(t, env, "B", date(2026, 6, 15), date(2026, 6, 30))
	c := mustCreateSprint(t, env, "C", date(2026, 7, 1), date(2026, 7, 14))

	got, err := env.store.ReorderPlannedSprints(env.ctx, store.ReorderPlannedSprintsParams{
		ProjectID: env.projectID,
		SprintIDs: []uuid.UUID{c.ID, a.ID, b.ID},
	})
	if err != nil {
		t.Fatalf("ReorderPlannedSprints: %v", err)
	}
	wantOrder := []uuid.UUID{c.ID, a.ID, b.ID}
	if len(got) != len(wantOrder) {
		t.Fatalf("len = %d, want %d", len(got), len(wantOrder))
	}
	for i, id := range wantOrder {
		if got[i].ID != id {
			t.Fatalf("position %d = %s, want %s", i, got[i].ID, id)
		}
		wantRank := int64(i + 1)
		if got[i].PlannedOrder == nil || *got[i].PlannedOrder != wantRank {
			t.Fatalf("rank %d = %v, want %d", i, got[i].PlannedOrder, wantRank)
		}
	}

	listed, _, err := env.store.ListSprints(env.ctx, store.ListSprintsParams{
		ProjectID: env.projectID,
		Status:    model.SprintStatusPlanned,
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("ListSprints planned: %v", err)
	}
	for i, id := range wantOrder {
		if listed[i].ID != id {
			t.Fatalf("listed position %d = %s, want %s", i, listed[i].ID, id)
		}
	}

	page1, more, err := env.store.ListSprints(env.ctx, store.ListSprintsParams{
		ProjectID: env.projectID,
		Status:    model.SprintStatusPlanned,
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("ListSprints planned page1: %v", err)
	}
	if !more || len(page1) != 2 || page1[0].ID != c.ID || page1[1].ID != a.ID {
		t.Fatalf("page1 = %+v more=%v", page1, more)
	}
	page2, more, err := env.store.ListSprints(env.ctx, store.ListSprintsParams{
		ProjectID: env.projectID,
		Status:    model.SprintStatusPlanned,
		Cursor: &store.SprintsCursor{
			PlannedOrder: *page1[1].PlannedOrder,
			ID:           page1[1].ID,
		},
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("ListSprints planned page2: %v", err)
	}
	if more || len(page2) != 1 || page2[0].ID != b.ID {
		t.Fatalf("page2 = %+v more=%v", page2, more)
	}
}

func TestReorderPlannedSprintsRejectsInvalidSets(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	a := mustCreateSprint(t, env, "A", date(2026, 6, 1), date(2026, 6, 14))
	b := mustCreateSprint(t, env, "B", date(2026, 6, 15), date(2026, 6, 30))
	active := mustCreateSprint(t, env, "active", date(2026, 7, 1), date(2026, 7, 14))
	mustActivate(t, env, active.ID)

	otherProject, err := env.store.CreateProject(env.ctx, uniqueProjectKey(t), "other", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	other, err := env.store.CreateSprint(env.ctx, store.CreateSprintParams{
		ProjectID: otherProject.ID,
		Name:      "other",
		StartDate: date(2026, 8, 1),
		EndDate:   date(2026, 8, 14),
	})
	if err != nil {
		t.Fatalf("CreateSprint other: %v", err)
	}

	cases := []struct {
		name string
		ids  []uuid.UUID
	}{
		{name: "missing", ids: []uuid.UUID{a.ID}},
		{name: "duplicate", ids: []uuid.UUID{a.ID, a.ID}},
		{name: "active", ids: []uuid.UUID{a.ID, active.ID}},
		{name: "other-project", ids: []uuid.UUID{a.ID, other.ID}},
		{name: "unknown", ids: []uuid.UUID{a.ID, uuid.New()}},
		{name: "extra", ids: []uuid.UUID{a.ID, b.ID, uuid.New()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := env.store.ReorderPlannedSprints(env.ctx, store.ReorderPlannedSprintsParams{
				ProjectID: env.projectID,
				SprintIDs: tc.ids,
			})
			if !errors.Is(err, store.ErrConflict) {
				t.Fatalf("err = %v, want ErrConflict", err)
			}
		})
	}
}

func TestReorderPlannedSprintsEmptyAndNotFound(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	emptyProject, err := env.store.CreateProject(env.ctx, uniqueProjectKey(t), "empty", "")
	if err != nil {
		t.Fatalf("CreateProject empty: %v", err)
	}
	got, err := env.store.ReorderPlannedSprints(env.ctx, store.ReorderPlannedSprintsParams{ProjectID: emptyProject.ID})
	if err != nil {
		t.Fatalf("ReorderPlannedSprints empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty len = %d, want 0", len(got))
	}
	if _, err := env.store.ReorderPlannedSprints(env.ctx, store.ReorderPlannedSprintsParams{ProjectID: uuid.New()}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("not found err = %v, want ErrNotFound", err)
	}
}

func TestUpdateSprintActivationUnique(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "S", date(2026, 6, 1), date(2026, 6, 14))
	mustActivate(t, env, sp.ID)

	st := model.SprintStatusCompleted
	_, err := env.store.UpdateSprint(env.ctx, sp.ID, store.UpdateSprintParams{Status: &st})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestCompleteSprintMovesUnfinishedToNextPlannedSprint(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	active := mustCreateSprint(t, env, "active", date(2026, 6, 1), date(2026, 6, 14))
	next := mustCreateSprint(t, env, "next", date(2026, 6, 15), date(2026, 6, 30))
	mustCreateSprint(t, env, "later", date(2026, 7, 1), date(2026, 7, 14))
	mustActivate(t, env, active.ID)

	i1 := mustCreateIssue(t, env, "todo-1")
	i2 := mustCreateIssue(t, env, "todo-2")
	i3 := mustCreateIssue(t, env, "in-prog")
	i4 := mustCreateIssue(t, env, "done")
	i5 := mustCreateIssue(t, env, "closed")
	for _, id := range []uuid.UUID{i1.ID, i2.ID, i3.ID, i4.ID, i5.ID} {
		assignIssueToSprint(t, env, id, active.ID)
	}
	setIssueStatus(t, env, i3.ID, model.StatusInProgress)
	setIssueStatus(t, env, i4.ID, model.StatusDone)
	setIssueStatus(t, env, i5.ID, model.StatusClosed)

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
			t.Fatalf("issue %s sprint = %v, want next sprint %s", id, iss.SprintID, next.ID)
		}
	}

	done, err := env.store.GetIssue(env.ctx, i4.ID)
	if err != nil {
		t.Fatalf("GetIssue done: %v", err)
	}
	if done.SprintID == nil || *done.SprintID != active.ID {
		t.Fatalf("done issue sprint = %v, want %s (stays)", done.SprintID, active.ID)
	}
	closed, err := env.store.GetIssue(env.ctx, i5.ID)
	if err != nil {
		t.Fatalf("GetIssue closed: %v", err)
	}
	if closed.SprintID == nil || *closed.SprintID != active.ID {
		t.Fatalf("closed issue sprint = %v, want %s (stays)", closed.SprintID, active.ID)
	}
	gotNext, err := env.store.GetSprint(env.ctx, next.ID)
	if err != nil {
		t.Fatalf("GetSprint next: %v", err)
	}
	if gotNext.Status != model.SprintStatusPlanned {
		t.Fatalf("next status = %s, want planned", gotNext.Status)
	}
	completed, _, err := env.store.ListSprints(env.ctx, store.ListSprintsParams{
		ProjectID: env.projectID,
		Status:    model.SprintStatusCompleted,
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("ListSprints completed: %v", err)
	}
	if len(completed) != 1 || completed[0].ID != active.ID {
		t.Fatalf("completed = %+v, want active sprint", completed)
	}
}

func TestCompleteSprintFallsBackToBacklog(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestGetSprintNotFound(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	_, err := env.store.GetSprint(env.ctx, uuid.New())
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCreateSprintProjectNotFound(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	_, err := env.store.CreateSprint(env.ctx, store.CreateSprintParams{
		ProjectID: uuid.New(),
		Name:      "ghost",
		StartDate: date(2026, 6, 1),
		EndDate:   date(2026, 6, 14),
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUpdateSprintNotFound(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	name := "nope"
	_, err := env.store.UpdateSprint(env.ctx, uuid.New(), store.UpdateSprintParams{Name: &name})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUpdateSprintNoFieldsReturnsCurrent(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "noop", date(2026, 6, 1), date(2026, 6, 14))
	got, err := env.store.UpdateSprint(env.ctx, sp.ID, store.UpdateSprintParams{})
	if err != nil {
		t.Fatalf("UpdateSprint: %v", err)
	}
	if got.ID != sp.ID || got.Name != "noop" || got.Status != model.SprintStatusPlanned {
		t.Fatalf("got = %+v", got)
	}
}

func TestUpdateSprintRejectsActiveToPlanned(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "S", date(2026, 6, 1), date(2026, 6, 14))
	mustActivate(t, env, sp.ID)
	st := model.SprintStatusPlanned
	_, err := env.store.UpdateSprint(env.ctx, sp.ID, store.UpdateSprintParams{Status: &st})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("active→planned: err = %v, want ErrConflict", err)
	}
}

func TestUpdateSprintRejectsAfterCompleted(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "S", date(2026, 6, 1), date(2026, 6, 14))
	mustActivate(t, env, sp.ID)
	if _, err := env.store.CompleteSprint(env.ctx, sp.ID); err != nil {
		t.Fatalf("CompleteSprint: %v", err)
	}
	for _, target := range []model.SprintStatus{model.SprintStatusActive, model.SprintStatusPlanned} {
		st := target
		_, err := env.store.UpdateSprint(env.ctx, sp.ID, store.UpdateSprintParams{Status: &st})
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("completed→%s: err = %v, want ErrConflict", target, err)
		}
	}
}

func TestUpdateSprintCompletedAllowsRenameOnly(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "old", date(2026, 6, 1), date(2026, 6, 14))
	mustActivate(t, env, sp.ID)
	if _, err := env.store.CompleteSprint(env.ctx, sp.ID); err != nil {
		t.Fatalf("CompleteSprint: %v", err)
	}

	newName := "renamed"
	got, err := env.store.UpdateSprint(env.ctx, sp.ID, store.UpdateSprintParams{Name: &newName})
	if err != nil {
		t.Fatalf("rename completed sprint: %v", err)
	}
	if got.Name != newName || got.Status != model.SprintStatusCompleted {
		t.Fatalf("got = %+v", got)
	}

	newGoal := "new goal"
	newStart := date(2026, 6, 2)
	newEnd := date(2026, 6, 15)
	target := model.SprintStatusCompleted
	cases := []store.UpdateSprintParams{
		{Goal: &newGoal},
		{StartDate: &newStart},
		{EndDate: &newEnd},
		{Status: &target},
		{Name: &newName, Goal: &newGoal},
	}
	for i, params := range cases {
		if _, err := env.store.UpdateSprint(env.ctx, sp.ID, params); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("case %d err = %v, want ErrConflict", i, err)
		}
	}
}

func TestUpdateSprintNameGoalDates(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "old", date(2026, 6, 1), date(2026, 6, 14))

	newName := "renamed"
	newGoal := "ship feature X"
	newStart := date(2026, 6, 8)
	newEnd := date(2026, 6, 22)
	got, err := env.store.UpdateSprint(env.ctx, sp.ID, store.UpdateSprintParams{
		Name:      &newName,
		Goal:      &newGoal,
		StartDate: &newStart,
		EndDate:   &newEnd,
	})
	if err != nil {
		t.Fatalf("UpdateSprint: %v", err)
	}
	if got.Name != newName || got.Goal != newGoal {
		t.Fatalf("name/goal not updated: %+v", got)
	}
	if !got.StartDate.Equal(newStart) || !got.EndDate.Equal(newEnd) {
		t.Fatalf("dates not updated: %s..%s", got.StartDate, got.EndDate)
	}
}

func TestUpdateSprintDateCheckViolation(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "S", date(2026, 6, 1), date(2026, 6, 14))
	newEnd := date(2026, 5, 1)
	_, err := env.store.UpdateSprint(env.ctx, sp.ID, store.UpdateSprintParams{EndDate: &newEnd})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict (CHECK)", err)
	}
}

func TestCompleteSprintNotFound(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	_, err := env.store.CompleteSprint(env.ctx, uuid.New())
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUpdateSprintStatusNoOp(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "S", date(2026, 6, 1), date(2026, 6, 14))
	st := model.SprintStatusPlanned
	got, err := env.store.UpdateSprint(env.ctx, sp.ID, store.UpdateSprintParams{Status: &st})
	if err != nil {
		t.Fatalf("planned→planned should be no-op: %v", err)
	}
	if got.Status != model.SprintStatusPlanned {
		t.Fatalf("Status = %s", got.Status)
	}
}

func TestListSprintsEmptyProject(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	out, more, err := env.store.ListSprints(env.ctx, store.ListSprintsParams{ProjectID: env.projectID, Limit: 100})
	if err != nil {
		t.Fatalf("ListSprints: %v", err)
	}
	if more {
		t.Fatalf("hasMore=true on empty list")
	}
	if len(out) != 0 {
		t.Fatalf("len = %d, want 0", len(out))
	}
}

func TestUpdateIssueClearAssignee(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	email := fmt.Sprintf("u%d@test.local", time.Now().UnixNano())
	user, err := env.store.CreateUser(env.ctx, email, "A")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	iss, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID:  env.projectID,
		Title:      "T",
		AssigneeID: &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	got, err := env.store.UpdateIssue(env.ctx, iss.ID, store.UpdateIssueParams{ClearAssignee: true})
	if err != nil {
		t.Fatalf("UpdateIssue clear: %v", err)
	}
	if got.AssigneeID != nil {
		t.Fatalf("AssigneeID = %v, want nil", got.AssigneeID)
	}
}

func TestUpdateIssuePeopleRequireProjectMembers(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	member, err := env.store.CreateUser(env.ctx, "issue-member-"+uniqueProjectKey(t)+"@example.com", "Issue Member")
	if err != nil {
		t.Fatalf("CreateUser member: %v", err)
	}
	nonMember, err := env.store.CreateUser(env.ctx, "issue-nonmember-"+uniqueProjectKey(t)+"@example.com", "Issue Nonmember")
	if err != nil {
		t.Fatalf("CreateUser nonmember: %v", err)
	}
	if _, err := env.store.GrantProjectAccess(env.ctx, env.projectID, member.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	iss, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID: env.projectID,
		Title:     "people",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	got, err := env.store.UpdateIssue(env.ctx, iss.ID, store.UpdateIssueParams{
		AssigneeID: &member.ID,
		ReporterID: &member.ID,
	})
	if err != nil {
		t.Fatalf("UpdateIssue set people: %v", err)
	}
	if got.AssigneeID == nil || *got.AssigneeID != member.ID || got.ReporterID == nil || *got.ReporterID != member.ID {
		t.Fatalf("people = assignee %v reporter %v, want %s", got.AssigneeID, got.ReporterID, member.ID)
	}

	got, err = env.store.UpdateIssue(env.ctx, iss.ID, store.UpdateIssueParams{
		ClearAssignee: true,
		ClearReporter: true,
	})
	if err != nil {
		t.Fatalf("UpdateIssue clear people: %v", err)
	}
	if got.AssigneeID != nil || got.ReporterID != nil {
		t.Fatalf("cleared people = assignee %v reporter %v, want nil", got.AssigneeID, got.ReporterID)
	}

	_, err = env.store.UpdateIssue(env.ctx, iss.ID, store.UpdateIssueParams{AssigneeID: &nonMember.ID})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("non-member assignee err = %v, want ErrNotFound", err)
	}
	missing := uuid.New()
	_, err = env.store.UpdateIssue(env.ctx, iss.ID, store.UpdateIssueParams{ReporterID: &missing})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing reporter err = %v, want ErrNotFound", err)
	}
}

func TestUpdateIssueClearSprint(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "S", date(2026, 6, 1), date(2026, 6, 14))
	iss := mustCreateIssue(t, env, "T")
	assignIssueToSprint(t, env, iss.ID, sp.ID)

	got, err := env.store.UpdateIssue(env.ctx, iss.ID, store.UpdateIssueParams{ClearSprint: true})
	if err != nil {
		t.Fatalf("UpdateIssue ClearSprint: %v", err)
	}
	if got.SprintID != nil {
		t.Fatalf("SprintID = %v, want nil", got.SprintID)
	}
}

func TestUpdateIssueDoneRejectsSprintEdit(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	current := mustCreateSprint(t, env, "current", date(2026, 6, 1), date(2026, 6, 14))
	next := mustCreateSprint(t, env, "next", date(2026, 6, 15), date(2026, 6, 30))
	iss := mustCreateIssue(t, env, "done")
	assignIssueToSprint(t, env, iss.ID, current.ID)
	setIssueStatus(t, env, iss.ID, model.StatusDone)

	if _, err := env.store.UpdateIssue(env.ctx, iss.ID, store.UpdateIssueParams{SprintID: &next.ID}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("set sprint err = %v, want ErrConflict", err)
	}
	if _, err := env.store.UpdateIssue(env.ctx, iss.ID, store.UpdateIssueParams{ClearSprint: true}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("clear sprint err = %v, want ErrConflict", err)
	}
	got, err := env.store.GetIssue(env.ctx, iss.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.SprintID == nil || *got.SprintID != current.ID {
		t.Fatalf("SprintID = %v, want %s", got.SprintID, current.ID)
	}

	closedIssue := mustCreateIssue(t, env, "closed")
	assignIssueToSprint(t, env, closedIssue.ID, current.ID)
	setIssueStatus(t, env, closedIssue.ID, model.StatusClosed)
	if _, err := env.store.UpdateIssue(env.ctx, closedIssue.ID, store.UpdateIssueParams{SprintID: &next.ID}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("set closed sprint err = %v, want ErrConflict", err)
	}
	if _, err := env.store.UpdateIssue(env.ctx, closedIssue.ID, store.UpdateIssueParams{ClearSprint: true}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("clear closed sprint err = %v, want ErrConflict", err)
	}
	got, err = env.store.GetIssue(env.ctx, closedIssue.ID)
	if err != nil {
		t.Fatalf("GetIssue closed: %v", err)
	}
	if got.SprintID == nil || *got.SprintID != current.ID {
		t.Fatalf("closed SprintID = %v, want %s", got.SprintID, current.ID)
	}

	becomingDone := mustCreateIssue(t, env, "done-with-move")
	done := model.StatusDone
	if _, err := env.store.UpdateIssue(env.ctx, becomingDone.ID, store.UpdateIssueParams{
		Status:   &done,
		SprintID: &next.ID,
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("status done plus sprint err = %v, want ErrConflict", err)
	}
	got, err = env.store.GetIssue(env.ctx, becomingDone.ID)
	if err != nil {
		t.Fatalf("GetIssue becoming done: %v", err)
	}
	if got.Status != model.StatusTodo || got.SprintID != nil {
		t.Fatalf("issue after rejected update = status %s sprint %v, want todo/no sprint", got.Status, got.SprintID)
	}

	becomingClosed := mustCreateIssue(t, env, "closed-with-move")
	closed := model.StatusClosed
	if _, err := env.store.UpdateIssue(env.ctx, becomingClosed.ID, store.UpdateIssueParams{
		Status:   &closed,
		SprintID: &next.ID,
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("status closed plus sprint err = %v, want ErrConflict", err)
	}
	got, err = env.store.GetIssue(env.ctx, becomingClosed.ID)
	if err != nil {
		t.Fatalf("GetIssue becoming closed: %v", err)
	}
	if got.Status != model.StatusTodo || got.SprintID != nil {
		t.Fatalf("closed issue after rejected update = status %s sprint %v, want todo/no sprint", got.Status, got.SprintID)
	}
}

func TestUpdateIssueEmptyParamsReturnsCurrent(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	iss := mustCreateIssue(t, env, "unchanged")
	got, err := env.store.UpdateIssue(env.ctx, iss.ID, store.UpdateIssueParams{})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if got.ID != iss.ID || got.Title != "unchanged" {
		t.Fatalf("got = %+v", got)
	}
}

func TestUpdateIssueNotFound(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	title := "new"
	_, err := env.store.UpdateIssue(env.ctx, uuid.New(), store.UpdateIssueParams{Title: &title})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUpdateIssueSprintNotFound(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	iss := mustCreateIssue(t, env, "T")
	bogus := uuid.New()
	_, err := env.store.UpdateIssue(env.ctx, iss.ID, store.UpdateIssueParams{SprintID: &bogus})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestUpdateIssueCompletedSprintRejected(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "done sprint", date(2026, 6, 1), date(2026, 6, 14))
	mustActivate(t, env, sp.ID)
	if _, err := env.store.CompleteSprint(env.ctx, sp.ID); err != nil {
		t.Fatalf("CompleteSprint: %v", err)
	}
	iss := mustCreateIssue(t, env, "T")
	_, err := env.store.UpdateIssue(env.ctx, iss.ID, store.UpdateIssueParams{SprintID: &sp.ID})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestListIssuesByIDsRoundtrip(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	i1 := mustCreateIssue(t, env, "one")
	i2 := mustCreateIssue(t, env, "two")

	empty, err := env.store.ListIssuesByIDs(env.ctx, nil)
	if err != nil {
		t.Fatalf("ListIssuesByIDs empty: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty len = %d", len(empty))
	}

	got, err := env.store.ListIssuesByIDs(env.ctx, []uuid.UUID{i1.ID, i2.ID, uuid.New()})
	if err != nil {
		t.Fatalf("ListIssuesByIDs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (missing id silently skipped)", len(got))
	}
	seen := map[uuid.UUID]bool{}
	for _, iss := range got {
		seen[iss.ID] = true
	}
	if !seen[i1.ID] || !seen[i2.ID] {
		t.Fatalf("missing one of expected ids: %+v", seen)
	}
}

func TestIssuePriorityRoundtrip(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	defaultIssue := mustCreateIssue(t, env, "default priority")
	if defaultIssue.Priority != model.PriorityP2 {
		t.Fatalf("default Priority = %q, want %q", defaultIssue.Priority, model.PriorityP2)
	}

	explicit, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID: env.projectID,
		Title:     "urgent issue",
		Priority:  model.PriorityP0,
	})
	if err != nil {
		t.Fatalf("CreateIssue explicit priority: %v", err)
	}
	if explicit.Priority != model.PriorityP0 {
		t.Fatalf("explicit Priority = %q, want %q", explicit.Priority, model.PriorityP0)
	}

	got, err := env.store.GetIssue(env.ctx, explicit.ID)
	if err != nil {
		t.Fatalf("GetIssue explicit priority: %v", err)
	}
	if got.Priority != model.PriorityP0 {
		t.Fatalf("GetIssue Priority = %q, want %q", got.Priority, model.PriorityP0)
	}

	p4 := model.PriorityP4
	updated, err := env.store.UpdateIssue(env.ctx, explicit.ID, store.UpdateIssueParams{Priority: &p4})
	if err != nil {
		t.Fatalf("UpdateIssue priority: %v", err)
	}
	if updated.Priority != model.PriorityP4 {
		t.Fatalf("updated Priority = %q, want %q", updated.Priority, model.PriorityP4)
	}

	listed, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{ProjectID: env.projectID, Limit: 10})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	listedPriorities := map[uuid.UUID]model.IssuePriority{}
	for _, iss := range listed {
		listedPriorities[iss.ID] = iss.Priority
	}
	if listedPriorities[defaultIssue.ID] != model.PriorityP2 || listedPriorities[explicit.ID] != model.PriorityP4 {
		t.Fatalf("listed priorities = %+v", listedPriorities)
	}

	byID, err := env.store.ListIssuesByIDs(env.ctx, []uuid.UUID{defaultIssue.ID, explicit.ID})
	if err != nil {
		t.Fatalf("ListIssuesByIDs: %v", err)
	}
	byIDPriorities := map[uuid.UUID]model.IssuePriority{}
	for _, iss := range byID {
		byIDPriorities[iss.ID] = iss.Priority
	}
	if byIDPriorities[defaultIssue.ID] != model.PriorityP2 || byIDPriorities[explicit.ID] != model.PriorityP4 {
		t.Fatalf("ListIssuesByIDs priorities = %+v", byIDPriorities)
	}

	child, err := env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
		ParentIssueID: defaultIssue.ID,
		Title:         "child priority",
		Priority:      model.PriorityP1,
	})
	if err != nil {
		t.Fatalf("CreateSubIssue priority: %v", err)
	}
	if child.Priority != model.PriorityP1 {
		t.Fatalf("child Priority = %q, want %q", child.Priority, model.PriorityP1)
	}
	children, _, err := env.store.ListSubIssuesForIssue(env.ctx, store.ListSubIssuesForIssueParams{
		ParentIssueID: defaultIssue.ID,
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("ListSubIssuesForIssue: %v", err)
	}
	if len(children) != 1 || children[0].Priority != model.PriorityP1 {
		t.Fatalf("children priorities = %+v", children)
	}
}

func TestIssueDueDateRoundtrip(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	due, err := model.ParseDate("2026-06-24")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	iss, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID: env.projectID,
		Title:     "due issue",
		DueDate:   &due,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if iss.DueDate == nil || iss.DueDate.String() != "2026-06-24" {
		t.Fatalf("CreateIssue DueDate = %v", iss.DueDate)
	}

	got, err := env.store.GetIssue(env.ctx, iss.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.DueDate == nil || got.DueDate.String() != "2026-06-24" {
		t.Fatalf("GetIssue DueDate = %v", got.DueDate)
	}

	listed, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{ProjectID: env.projectID, Limit: 10})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	listedDueDates := map[uuid.UUID]string{}
	for _, item := range listed {
		if item.DueDate != nil {
			listedDueDates[item.ID] = item.DueDate.String()
		}
	}
	if listedDueDates[iss.ID] != "2026-06-24" {
		t.Fatalf("listed due dates = %+v", listedDueDates)
	}

	byID, err := env.store.ListIssuesByIDs(env.ctx, []uuid.UUID{iss.ID})
	if err != nil {
		t.Fatalf("ListIssuesByIDs: %v", err)
	}
	if len(byID) != 1 || byID[0].DueDate == nil || byID[0].DueDate.String() != "2026-06-24" {
		t.Fatalf("ListIssuesByIDs = %+v", byID)
	}

	childDue, err := model.ParseDate("2026-06-25")
	if err != nil {
		t.Fatalf("ParseDate child: %v", err)
	}
	child, err := env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
		ParentIssueID: iss.ID,
		Title:         "due child",
		DueDate:       &childDue,
	})
	if err != nil {
		t.Fatalf("CreateSubIssue: %v", err)
	}
	children, _, err := env.store.ListSubIssuesForIssue(env.ctx, store.ListSubIssuesForIssueParams{
		ParentIssueID: iss.ID,
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("ListSubIssuesForIssue: %v", err)
	}
	if len(children) != 1 || children[0].ID != child.ID || children[0].DueDate == nil || children[0].DueDate.String() != "2026-06-25" {
		t.Fatalf("children = %+v", children)
	}

	updatedDue, err := model.ParseDate("2026-06-26")
	if err != nil {
		t.Fatalf("ParseDate updated: %v", err)
	}
	updated, err := env.store.UpdateIssue(env.ctx, iss.ID, store.UpdateIssueParams{DueDate: &updatedDue})
	if err != nil {
		t.Fatalf("UpdateIssue due date: %v", err)
	}
	if updated.DueDate == nil || updated.DueDate.String() != "2026-06-26" {
		t.Fatalf("updated DueDate = %v", updated.DueDate)
	}
	cleared, err := env.store.UpdateIssue(env.ctx, iss.ID, store.UpdateIssueParams{ClearDueDate: true})
	if err != nil {
		t.Fatalf("UpdateIssue clear due date: %v", err)
	}
	if cleared.DueDate != nil {
		t.Fatalf("cleared DueDate = %v, want nil", cleared.DueDate)
	}
}

func TestListIssuesStatusAndSprintCombined(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "S", date(2026, 6, 1), date(2026, 6, 14))

	a := mustCreateIssue(t, env, "todo-in-sprint")
	b := mustCreateIssue(t, env, "done-in-sprint")
	c := mustCreateIssue(t, env, "todo-backlog")

	assignIssueToSprint(t, env, a.ID, sp.ID)
	assignIssueToSprint(t, env, b.ID, sp.ID)
	setIssueStatus(t, env, b.ID, model.StatusDone)

	todoInSprint, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{
		ProjectID: env.projectID,
		Status:    model.StatusTodo,
		SprintID:  &sp.ID,
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(todoInSprint) != 1 || todoInSprint[0].ID != a.ID {
		t.Fatalf("todo+sprint = %+v, want only %s", todoInSprint, a.ID)
	}

	doneBacklog, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{
		ProjectID: env.projectID,
		Status:    model.StatusDone,
		Backlog:   true,
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("ListIssues backlog+done: %v", err)
	}
	if len(doneBacklog) != 0 {
		t.Fatalf("done+backlog len = %d, want 0", len(doneBacklog))
	}

	_ = c
}

func TestListIssuesBacklogFilter(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	sp := mustCreateSprint(t, env, "S", date(2026, 6, 1), date(2026, 6, 14))
	mustActivate(t, env, sp.ID)

	backlogIss := mustCreateIssue(t, env, "backlog-1")
	inSprint := mustCreateIssue(t, env, "in-sprint-1")
	assignIssueToSprint(t, env, inSprint.ID, sp.ID)

	backlog, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{
		ProjectID: env.projectID, Backlog: true, Limit: 100,
	})
	if err != nil {
		t.Fatalf("ListIssues backlog: %v", err)
	}
	if len(backlog) != 1 || backlog[0].ID != backlogIss.ID {
		t.Fatalf("backlog = %+v, want only %s", backlog, backlogIss.ID)
	}

	bySprint, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{
		ProjectID: env.projectID, SprintID: &sp.ID, Limit: 100,
	})
	if err != nil {
		t.Fatalf("ListIssues by sprint: %v", err)
	}
	if len(bySprint) != 1 || bySprint[0].ID != inSprint.ID {
		t.Fatalf("by sprint = %+v, want only %s", bySprint, inSprint.ID)
	}
}

func TestListIssuesAssigneeFilter(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	alice := mustCreateUser(t, env, "alice-"+uniqueDigits(time.Now().UnixNano(), 8)+"@example.com")
	bob := mustCreateUser(t, env, "bob-"+uniqueDigits(time.Now().UnixNano(), 8)+"@example.com")
	sp := mustCreateSprint(t, env, "S", date(2026, 6, 1), date(2026, 6, 14))

	assignedAlice, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID:  env.projectID,
		Title:      "alice sprint",
		AssigneeID: &alice.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue alice: %v", err)
	}
	assignedBob, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID:  env.projectID,
		Title:      "bob sprint",
		AssigneeID: &bob.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue bob: %v", err)
	}
	unassigned := mustCreateIssue(t, env, "unassigned sprint")
	if _, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID:  env.projectID,
		Title:      "alice backlog",
		AssigneeID: &alice.ID,
	}); err != nil {
		t.Fatalf("CreateIssue alice backlog: %v", err)
	}
	assignIssueToSprint(t, env, assignedAlice.ID, sp.ID)
	assignIssueToSprint(t, env, assignedBob.ID, sp.ID)
	assignIssueToSprint(t, env, unassigned.ID, sp.ID)
	done := model.StatusDone
	if _, err := env.store.UpdateIssue(env.ctx, assignedBob.ID, store.UpdateIssueParams{Status: &done}); err != nil {
		t.Fatalf("set bob done: %v", err)
	}

	got, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{
		ProjectID:   env.projectID,
		AssigneeIDs: []uuid.UUID{alice.ID, bob.ID},
		SprintID:    &sp.ID,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("ListIssues assignees+sprint: %v", err)
	}
	if len(got) != 2 || got[0].ID != assignedAlice.ID || got[1].ID != assignedBob.ID {
		t.Fatalf("assignee+sprint = %+v, want alice/bob sprint issues", got)
	}

	got, _, err = env.store.ListIssues(env.ctx, store.ListIssuesParams{
		ProjectID:   env.projectID,
		AssigneeIDs: []uuid.UUID{bob.ID},
		Status:      model.StatusDone,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("ListIssues assignee+status: %v", err)
	}
	if len(got) != 1 || got[0].ID != assignedBob.ID {
		t.Fatalf("assignee+status = %+v, want bob done", got)
	}
}

func TestListProjectAssigneesIncludesMembersAndAssignedUsers(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	member, err := env.store.CreateUserProfile(env.ctx, "member-"+strings.ToLower(uniqueProjectKey(t)), "member@example.com", "Member User")
	if err != nil {
		t.Fatalf("CreateUserProfile member: %v", err)
	}
	if _, err := env.store.GrantProjectAccess(env.ctx, env.projectID, member.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	assigned, err := env.store.CreateUserProfile(env.ctx, "assigned-"+strings.ToLower(uniqueProjectKey(t)), "assigned@example.com", "Assigned User")
	if err != nil {
		t.Fatalf("CreateUserProfile assigned: %v", err)
	}
	unrelated, err := env.store.CreateUserProfile(env.ctx, "unrelated-"+strings.ToLower(uniqueProjectKey(t)), "unrelated@example.com", "Unrelated User")
	if err != nil {
		t.Fatalf("CreateUserProfile unrelated: %v", err)
	}
	if _, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID:  env.projectID,
		Title:      "assigned issue",
		AssigneeID: &assigned.ID,
	}); err != nil {
		t.Fatalf("CreateIssue assigned: %v", err)
	}

	got, err := env.store.ListProjectAssignees(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("ListProjectAssignees: %v", err)
	}
	if !projectAssigneesContain(got, member.ID) || !projectAssigneesContain(got, assigned.ID) {
		t.Fatalf("project assignees missing member/assigned: %+v", got)
	}
	if projectAssigneesContain(got, unrelated.ID) {
		t.Fatalf("project assignees included unrelated user: %+v", got)
	}

	_, err = env.store.ListProjectAssignees(env.ctx, uuid.New())
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing project err = %v, want ErrNotFound", err)
	}
}

func projectAssigneesContain(in []model.ProjectAssignee, id uuid.UUID) bool {
	for _, assignee := range in {
		if assignee.ID == id {
			return true
		}
	}
	return false
}
