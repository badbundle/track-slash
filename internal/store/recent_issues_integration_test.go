package store_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func mustRecentViewer(t *testing.T, env *sprintsTestEnv, label string) model.User {
	t.Helper()
	user, err := env.store.CreateUser(env.ctx, label+"-"+uuid.NewString()+"@example.com", label)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return user
}

func mustRecordIssueView(t *testing.T, env *sprintsTestEnv, userID uuid.UUID, issue model.Issue) {
	t.Helper()
	if err := env.store.RecordIssueView(env.ctx, userID, issue.ID); err != nil {
		t.Fatalf("RecordIssueView %s: %v", issue.Identifier, err)
	}
}

func recentIdentifiers(t *testing.T, env *sprintsTestEnv, user model.User, limit int) []string {
	t.Helper()
	issues, err := env.store.ListRecentIssues(env.ctx, user, limit)
	if err != nil {
		t.Fatalf("ListRecentIssues: %v", err)
	}
	out := make([]string, 0, len(issues))
	for _, issue := range issues {
		out = append(out, issue.Identifier)
	}
	return out
}

func assertIdentifiers(t *testing.T, got []string, want ...string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("recent issues = %v, want %v", got, want)
	}
}

func TestRecordIssueViewOrdersNewestFirstWithoutDuplicates(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	viewer := mustRecentViewer(t, env, "recent-viewer")
	if _, err := env.store.SetProjectMemberRole(env.ctx, env.projectID, viewer.ID, model.ProjectMemberRoleReadonly); err != nil {
		t.Fatalf("SetProjectMemberRole: %v", err)
	}
	a := mustCreateIssue(t, env, "A")
	b := mustCreateIssue(t, env, "B")
	c := mustCreateIssue(t, env, "C")

	assertIdentifiers(t, recentIdentifiers(t, env, viewer, 10))
	for _, issue := range []model.Issue{a, b, c, a} {
		mustRecordIssueView(t, env, viewer.ID, issue)
	}
	assertIdentifiers(t, recentIdentifiers(t, env, viewer, 10), a.Identifier, c.Identifier, b.Identifier)
	assertIdentifiers(t, recentIdentifiers(t, env, viewer, 2), a.Identifier, c.Identifier)

	// Another user's history is their own.
	other := mustRecentViewer(t, env, "recent-other")
	if _, err := env.store.SetProjectMemberRole(env.ctx, env.projectID, other.ID, model.ProjectMemberRoleMember); err != nil {
		t.Fatalf("SetProjectMemberRole other: %v", err)
	}
	mustRecordIssueView(t, env, other.ID, b)
	assertIdentifiers(t, recentIdentifiers(t, env, other, 10), b.Identifier)
	assertIdentifiers(t, recentIdentifiers(t, env, viewer, 10), a.Identifier, c.Identifier, b.Identifier)
}

func TestRecordIssueViewTrimsHistory(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	viewer := mustRecentViewer(t, env, "recent-trim")
	if _, err := env.store.SetProjectMemberRole(env.ctx, env.projectID, viewer.ID, model.ProjectMemberRoleMember); err != nil {
		t.Fatalf("SetProjectMemberRole: %v", err)
	}
	var viewed []model.Issue
	for i := range store.RecentIssueViewsKept + 2 {
		issue := mustCreateIssue(t, env, fmt.Sprintf("Issue %d", i))
		mustRecordIssueView(t, env, viewer.ID, issue)
		viewed = append(viewed, issue)
	}

	var stored int
	if err := env.pool.QueryRow(env.ctx, `SELECT count(*) FROM recent_issue_views WHERE user_id = $1`, viewer.ID).Scan(&stored); err != nil {
		t.Fatalf("count recent views: %v", err)
	}
	if stored != store.RecentIssueViewsKept {
		t.Fatalf("stored recent views = %d, want %d", stored, store.RecentIssueViewsKept)
	}
	got := recentIdentifiers(t, env, viewer, store.RecentIssueViewsKept+10)
	if len(got) != store.RecentIssueViewsKept || got[0] != viewed[len(viewed)-1].Identifier || got[len(got)-1] != viewed[2].Identifier {
		t.Fatalf("trimmed history = %v, want the %d newest views", got, store.RecentIssueViewsKept)
	}
}

func TestRecordIssueViewRejectsMissingRows(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	viewer := mustRecentViewer(t, env, "recent-missing")
	issue := mustCreateIssue(t, env, "Deleted soon")
	if err := env.store.DeleteIssue(env.ctx, issue.ID); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	for name, ids := range map[string][2]uuid.UUID{
		"unknown issue": {viewer.ID, uuid.New()},
		"deleted issue": {viewer.ID, issue.ID},
		"unknown user":  {uuid.New(), mustCreateIssue(t, env, "Live").ID},
	} {
		if err := env.store.RecordIssueView(env.ctx, ids[0], ids[1]); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("%s: RecordIssueView err = %v, want ErrNotFound", name, err)
		}
	}
}

func TestListRecentIssuesLeavesOutIssuesTheUserCannotRead(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	owner := mustRecentViewer(t, env, "recent-owner")
	viewer := mustRecentViewer(t, env, "recent-reader")
	newProject := func(name string) model.Project {
		t.Helper()
		project, err := env.store.CreateProjectForUser(env.ctx, owner.ID, uniqueProjectKey(t), name, "")
		if err != nil {
			t.Fatalf("CreateProjectForUser %s: %v", name, err)
		}
		return project
	}
	issueIn := func(project model.Project, title string) model.Issue {
		t.Helper()
		issue, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{ProjectID: project.ID, Title: title})
		if err != nil {
			t.Fatalf("CreateIssue %s: %v", title, err)
		}
		return issue
	}

	member := newProject("member")
	revoked := newProject("revoked")
	public := newProject("public")
	blocked := newProject("blocked")
	private := newProject("private")
	deleted := newProject("deleted")
	for _, project := range []model.Project{member, revoked, deleted} {
		if _, err := env.store.SetProjectMemberRole(env.ctx, project.ID, viewer.ID, model.ProjectMemberRoleMember); err != nil {
			t.Fatalf("SetProjectMemberRole %s: %v", project.Name, err)
		}
	}
	for _, project := range []model.Project{public, blocked} {
		if _, err := env.store.UpdateProjectAccessSettings(env.ctx, project.ID, model.ProjectAccessSettings{IsPublic: true}); err != nil {
			t.Fatalf("UpdateProjectAccessSettings %s: %v", project.Name, err)
		}
	}

	memberIssue := issueIn(member, "Member issue")
	deletedIssue := issueIn(member, "Deleted issue")
	revokedIssue := issueIn(revoked, "Revoked issue")
	publicIssue := issueIn(public, "Public issue")
	blockedIssue := issueIn(blocked, "Blocked issue")
	privateIssue := issueIn(private, "Private issue")
	deletedProjectIssue := issueIn(deleted, "Deleted project issue")
	for _, issue := range []model.Issue{memberIssue, deletedIssue, revokedIssue, publicIssue, blockedIssue, privateIssue, deletedProjectIssue} {
		mustRecordIssueView(t, env, viewer.ID, issue)
	}

	if err := env.store.DeleteIssue(env.ctx, deletedIssue.ID); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	if err := env.store.RevokeProjectAccess(env.ctx, revoked.ID, viewer.ID); err != nil {
		t.Fatalf("RevokeProjectAccess: %v", err)
	}
	if _, err := env.store.BlockProjectUser(env.ctx, blocked.ID, viewer.ID, owner.ID); err != nil {
		t.Fatalf("BlockProjectUser: %v", err)
	}
	if err := env.store.DeleteProject(env.ctx, deleted.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	assertIdentifiers(t, recentIdentifiers(t, env, viewer, 10), publicIssue.Identifier, memberIssue.Identifier)

	// The owner and site admins read every live project, so the same views
	// recorded for them come back in full, less the deleted issue and project.
	admin, err := env.store.CreateOrUpdateAdminUser(env.ctx, "recent-admin-"+uuid.NewString()+"@example.com", "Admin")
	if err != nil {
		t.Fatalf("CreateOrUpdateAdminUser: %v", err)
	}
	for _, reader := range []model.User{owner, admin} {
		for _, issue := range []model.Issue{memberIssue, revokedIssue, blockedIssue, privateIssue} {
			mustRecordIssueView(t, env, reader.ID, issue)
		}
		assertIdentifiers(t, recentIdentifiers(t, env, reader, 10), privateIssue.Identifier, blockedIssue.Identifier, revokedIssue.Identifier, memberIssue.Identifier)
	}
}
