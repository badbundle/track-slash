package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/bradleymackey/track-slash/internal/testutil"
)

type linksTestEnv struct {
	ctx       context.Context
	store     *store.Store
	pool      *pgxpool.Pool
	projectID uuid.UUID
}

func newLinksEnv(t *testing.T) *linksTestEnv {
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
	proj, err := s.CreateProjectForUser(ctx, owner.ID, uniqueProjectKey(t), "links-test", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser: %v", err)
	}
	return &linksTestEnv{ctx: ctx, store: s, pool: pool, projectID: proj.ID}
}

func (e *linksTestEnv) mustIssue(t *testing.T, title string) model.Issue {
	t.Helper()
	iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     title,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	return iss
}

func (e *linksTestEnv) mustIssueInProject(t *testing.T, projectID uuid.UUID, title string) model.Issue {
	t.Helper()
	iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: projectID,
		Title:     title,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	return iss
}

func TestCreateIssueLinkBlocks(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	a := env.mustIssue(t, "A")
	b := env.mustIssue(t, "B")

	link, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: b.ID, LinkType: model.LinkTypeBlocks,
	})
	if err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}
	if link.SourceID != a.ID || link.TargetID != b.ID || link.LinkType != model.LinkTypeBlocks {
		t.Fatalf("link = %+v", link)
	}
	if link.ProjectID != env.projectID {
		t.Fatalf("ProjectID = %s, want %s", link.ProjectID, env.projectID)
	}

	got, err := env.store.GetIssueLink(env.ctx, link.ID)
	if err != nil {
		t.Fatalf("GetIssueLink: %v", err)
	}
	if got.ID != link.ID {
		t.Fatalf("Get round-trip mismatch")
	}
}

func TestCreateIssueLinkDuplicatesClosesSource(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	a := env.mustIssue(t, "dup-src")
	b := env.mustIssue(t, "canonical")

	if _, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: b.ID, LinkType: model.LinkTypeDuplicates,
	}); err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}

	src, err := env.store.GetIssue(env.ctx, a.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if src.Status != model.StatusDone {
		t.Fatalf("source status = %s, want done", src.Status)
	}

	tgt, err := env.store.GetIssue(env.ctx, b.ID)
	if err != nil {
		t.Fatalf("GetIssue target: %v", err)
	}
	if tgt.Status != model.StatusTodo {
		t.Fatalf("target status = %s, want todo (untouched)", tgt.Status)
	}
}

func TestCreateIssueLinkDuplicatesSourceAlreadyDone(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	a := env.mustIssue(t, "already-done")
	b := env.mustIssue(t, "target")
	st := model.StatusDone
	if _, err := env.store.UpdateIssue(env.ctx, a.ID, store.UpdateIssueParams{Status: &st}); err != nil {
		t.Fatalf("set done: %v", err)
	}

	if _, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: b.ID, LinkType: model.LinkTypeDuplicates,
	}); err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}

	src, err := env.store.GetIssue(env.ctx, a.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if src.Status != model.StatusDone {
		t.Fatalf("source status = %s, want done", src.Status)
	}
}

func TestCreateIssueLinkAllTypes(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	for _, lt := range []model.LinkType{
		model.LinkTypeBlocks,
		model.LinkTypeRelatesTo,
		model.LinkTypeClones,
	} {
		a := env.mustIssue(t, "a-"+string(lt))
		b := env.mustIssue(t, "b-"+string(lt))
		if _, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
			SourceID: a.ID, TargetID: b.ID, LinkType: lt,
		}); err != nil {
			t.Fatalf("CreateIssueLink %s: %v", lt, err)
		}
	}
}

func TestCreateIssueLinkSelfRejected(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	a := env.mustIssue(t, "lonely")
	_, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: a.ID, LinkType: model.LinkTypeBlocks,
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestCreateIssueLinkDuplicateRowRejected(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	a := env.mustIssue(t, "A")
	b := env.mustIssue(t, "B")
	if _, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: b.ID, LinkType: model.LinkTypeBlocks,
	}); err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: b.ID, LinkType: model.LinkTypeBlocks,
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second: err = %v, want ErrConflict", err)
	}
}

func TestCreateIssueLinkCrossProjectRejected(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	other, err := env.store.CreateProject(env.ctx, uniqueProjectKey(t), "other-proj", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	a := env.mustIssue(t, "A")
	b := env.mustIssueInProject(t, other.ID, "B")

	_, err = env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: b.ID, LinkType: model.LinkTypeBlocks,
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestCreateIssueLinkSourceNotFound(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	b := env.mustIssue(t, "B")
	_, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: uuid.New(), TargetID: b.ID, LinkType: model.LinkTypeBlocks,
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCreateIssueLinkTargetNotFound(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	a := env.mustIssue(t, "A")
	_, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: uuid.New(), LinkType: model.LinkTypeBlocks,
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestGetIssueLinkNotFound(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	_, err := env.store.GetIssueLink(env.ctx, uuid.New())
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUpdateIssueLinkRewiresExistingLink(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	a := env.mustIssue(t, "A")
	b := env.mustIssue(t, "B")
	c := env.mustIssue(t, "C")
	link, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: b.ID, LinkType: model.LinkTypeBlocks,
	})
	if err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}

	updated, err := env.store.UpdateIssueLink(env.ctx, link.ID, store.UpdateIssueLinkParams{
		SourceID: b.ID,
		TargetID: c.ID,
		LinkType: model.LinkTypeClones,
	})
	if err != nil {
		t.Fatalf("UpdateIssueLink: %v", err)
	}
	if updated.ID != link.ID || updated.Number != link.Number || updated.Ref != link.Ref {
		t.Fatalf("identity changed: before=%+v after=%+v", link, updated)
	}
	if updated.SourceID != b.ID || updated.TargetID != c.ID || updated.LinkType != model.LinkTypeClones {
		t.Fatalf("updated = %+v, want B clones C", updated)
	}
}

func TestUpdateIssueLinkDuplicatesClosesSource(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	a := env.mustIssue(t, "A")
	b := env.mustIssue(t, "B")
	link, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: b.ID, LinkType: model.LinkTypeBlocks,
	})
	if err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}

	if _, err := env.store.UpdateIssueLink(env.ctx, link.ID, store.UpdateIssueLinkParams{
		SourceID: a.ID,
		TargetID: b.ID,
		LinkType: model.LinkTypeDuplicates,
	}); err != nil {
		t.Fatalf("UpdateIssueLink: %v", err)
	}
	src, err := env.store.GetIssue(env.ctx, a.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if src.Status != model.StatusDone {
		t.Fatalf("source status = %s, want done", src.Status)
	}
}

func TestUpdateIssueLinkDuplicatesSourceAlreadyDone(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	a := env.mustIssue(t, "A")
	b := env.mustIssue(t, "B")
	st := model.StatusDone
	if _, err := env.store.UpdateIssue(env.ctx, a.ID, store.UpdateIssueParams{Status: &st}); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	link, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: b.ID, LinkType: model.LinkTypeBlocks,
	})
	if err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}

	if _, err := env.store.UpdateIssueLink(env.ctx, link.ID, store.UpdateIssueLinkParams{
		SourceID: a.ID,
		TargetID: b.ID,
		LinkType: model.LinkTypeDuplicates,
	}); err != nil {
		t.Fatalf("UpdateIssueLink: %v", err)
	}
	src, err := env.store.GetIssue(env.ctx, a.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if src.Status != model.StatusDone {
		t.Fatalf("source status = %s, want done", src.Status)
	}
}

func TestUpdateIssueLinkRejectsInvalidChanges(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	a := env.mustIssue(t, "A")
	b := env.mustIssue(t, "B")
	c := env.mustIssue(t, "C")
	link, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: b.ID, LinkType: model.LinkTypeBlocks,
	})
	if err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}
	duplicate, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: c.ID, LinkType: model.LinkTypeBlocks,
	})
	if err != nil {
		t.Fatalf("CreateIssueLink duplicate candidate: %v", err)
	}
	other, err := env.store.CreateProject(env.ctx, uniqueProjectKey(t), "other-proj", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	otherIssue := env.mustIssueInProject(t, other.ID, "other")

	for _, tt := range []struct {
		name string
		id   uuid.UUID
		p    store.UpdateIssueLinkParams
		want error
	}{
		{
			name: "missing link",
			id:   uuid.New(),
			p:    store.UpdateIssueLinkParams{SourceID: a.ID, TargetID: b.ID, LinkType: model.LinkTypeBlocks},
			want: store.ErrNotFound,
		},
		{
			name: "missing source",
			id:   link.ID,
			p:    store.UpdateIssueLinkParams{SourceID: uuid.New(), TargetID: b.ID, LinkType: model.LinkTypeBlocks},
			want: store.ErrConflict,
		},
		{
			name: "missing target",
			id:   link.ID,
			p:    store.UpdateIssueLinkParams{SourceID: a.ID, TargetID: uuid.New(), LinkType: model.LinkTypeBlocks},
			want: store.ErrConflict,
		},
		{
			name: "cross project source",
			id:   link.ID,
			p:    store.UpdateIssueLinkParams{SourceID: otherIssue.ID, TargetID: b.ID, LinkType: model.LinkTypeBlocks},
			want: store.ErrConflict,
		},
		{
			name: "cross project target",
			id:   link.ID,
			p:    store.UpdateIssueLinkParams{SourceID: a.ID, TargetID: otherIssue.ID, LinkType: model.LinkTypeBlocks},
			want: store.ErrConflict,
		},
		{
			name: "self",
			id:   link.ID,
			p:    store.UpdateIssueLinkParams{SourceID: a.ID, TargetID: a.ID, LinkType: model.LinkTypeBlocks},
			want: store.ErrConflict,
		},
		{
			name: "duplicate row",
			id:   duplicate.ID,
			p:    store.UpdateIssueLinkParams{SourceID: a.ID, TargetID: b.ID, LinkType: model.LinkTypeBlocks},
			want: store.ErrConflict,
		},
	} {
		_, err := env.store.UpdateIssueLink(env.ctx, tt.id, tt.p)
		if !errors.Is(err, tt.want) {
			t.Fatalf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
}

func TestListIssueLinksForIssueReturnsBothDirections(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	a := env.mustIssue(t, "A")
	b := env.mustIssue(t, "B")
	c := env.mustIssue(t, "C")

	out, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: b.ID, LinkType: model.LinkTypeBlocks,
	})
	if err != nil {
		t.Fatalf("outgoing: %v", err)
	}
	in, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: c.ID, TargetID: a.ID, LinkType: model.LinkTypeRelatesTo,
	})
	if err != nil {
		t.Fatalf("incoming: %v", err)
	}

	links, _, err := env.store.ListIssueLinksForIssue(env.ctx, store.ListIssueLinksForIssueParams{IssueID: a.ID, Limit: 100})
	if err != nil {
		t.Fatalf("ListIssueLinksForIssue: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("len = %d, want 2", len(links))
	}
	ids := map[uuid.UUID]bool{}
	for _, l := range links {
		ids[l.ID] = true
	}
	if !ids[out.ID] || !ids[in.ID] {
		t.Fatalf("missing link in result: %+v", ids)
	}
}

func TestListIssueLinksForIssueEmpty(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	a := env.mustIssue(t, "lone")
	links, more, err := env.store.ListIssueLinksForIssue(env.ctx, store.ListIssueLinksForIssueParams{IssueID: a.ID, Limit: 100})
	if err != nil {
		t.Fatalf("ListIssueLinksForIssue: %v", err)
	}
	if more {
		t.Fatalf("hasMore=true on empty list")
	}
	if len(links) != 0 {
		t.Fatalf("len = %d, want 0", len(links))
	}
}

func TestDeleteIssueLink(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	a := env.mustIssue(t, "A")
	b := env.mustIssue(t, "B")
	link, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: b.ID, LinkType: model.LinkTypeBlocks,
	})
	if err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}

	if err := env.store.DeleteIssueLink(env.ctx, link.ID); err != nil {
		t.Fatalf("DeleteIssueLink: %v", err)
	}
	if err := env.store.DeleteIssueLink(env.ctx, link.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete: err = %v, want ErrNotFound", err)
	}
}

func TestIssueLinkCascadeOnIssueDelete(t *testing.T) {
	t.Parallel()
	env := newLinksEnv(t)
	a := env.mustIssue(t, "A")
	b := env.mustIssue(t, "B")
	link, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: a.ID, TargetID: b.ID, LinkType: model.LinkTypeBlocks,
	})
	if err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}

	// No public DeleteIssue exists yet; verify the FK cascade via raw SQL.
	if _, err := env.pool.Exec(env.ctx, `DELETE FROM issues WHERE id = $1`, a.ID); err != nil {
		t.Fatalf("DELETE issue: %v", err)
	}

	if _, err := env.store.GetIssueLink(env.ctx, link.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound (cascade)", err)
	}
}
