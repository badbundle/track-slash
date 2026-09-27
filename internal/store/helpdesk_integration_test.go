package store_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func mustHelpDeskOutsider(t *testing.T, env *sprintsTestEnv, prefix string) model.User {
	t.Helper()
	username := prefix + strings.ToLower(uniqueProjectKey(t))
	user, err := env.store.CreateUserProfile(env.ctx, username, username+"@example.com", "Reporter "+prefix)
	if err != nil {
		t.Fatalf("CreateUserProfile %s: %v", prefix, err)
	}
	return user
}

func TestHelpDeskPermissionsAndReportedIssues(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	owner, err := env.store.GetUser(env.ctx, project.OwnerID)
	if err != nil {
		t.Fatalf("GetUser owner: %v", err)
	}
	member := mustPushProjectMember(t, env, "helpdesk-member")
	alice := mustHelpDeskOutsider(t, env, "helpdesk-alice")
	bob := mustHelpDeskOutsider(t, env, "helpdesk-bob")

	settings, err := env.store.UpdateProjectAccessMode(env.ctx, env.projectID, model.ProjectAccessHelpDesk)
	if err != nil || settings.AccessMode != model.ProjectAccessHelpDesk || settings.IsPublic || !settings.PublicIssueCreation {
		t.Fatalf("UpdateProjectAccessMode helpdesk = %+v, %v", settings, err)
	}

	for name, tc := range map[string]struct {
		user                            model.User
		read, create, reporter, members bool
	}{
		"owner":     {owner, true, true, false, true},
		"member":    {member, true, true, false, true},
		"reporter":  {alice, false, true, true, false},
		"anonymous": {model.User{}, false, false, false, false},
	} {
		permissions, err := env.store.ProjectPermissionsForUser(env.ctx, tc.user, env.projectID)
		if err != nil || permissions.CanRead != tc.read || permissions.CanCreateIssues != tc.create ||
			permissions.HelpDeskReporter != tc.reporter || permissions.CanReadMembersOnly != tc.members || permissions.CanWrite && !tc.read {
			t.Fatalf("%s help desk permissions = %+v, %v", name, permissions, err)
		}
	}

	// Outsiders may be the reporter of a help-desk issue.
	aliceIssue, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{ProjectID: env.projectID, Title: "Alice's report", ReporterID: &alice.ID})
	if err != nil {
		t.Fatalf("CreateIssue alice: %v", err)
	}
	bobIssue, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{ProjectID: env.projectID, Title: "Bob's report", ReporterID: &bob.ID})
	if err != nil {
		t.Fatalf("CreateIssue bob: %v", err)
	}
	child, err := env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{ParentIssueID: bobIssue.ID, Title: "Follow-up for Alice", ReporterID: &alice.ID})
	if err != nil {
		t.Fatalf("CreateIssue child: %v", err)
	}

	permissions, err := env.store.ProjectPermissionsForUser(env.ctx, alice, env.projectID)
	if err != nil {
		t.Fatalf("ProjectPermissionsForUser alice: %v", err)
	}
	if !permissions.CanFollowIssue(alice, aliceIssue) || permissions.CanFollowIssue(alice, bobIssue) || permissions.CanFollowIssue(bob, aliceIssue) {
		t.Fatalf("CanFollowIssue mismatch for %+v", permissions)
	}
	if unreported := (model.Issue{ID: uuid.New()}); permissions.CanFollowIssue(alice, unreported) {
		t.Fatal("CanFollowIssue followed an issue without a reporter")
	}
	memberPermissions, err := env.store.ProjectPermissionsForUser(env.ctx, member, env.projectID)
	if err != nil || memberPermissions.CanFollowIssue(member, aliceIssue) {
		t.Fatalf("member follows as reporter: %+v, %v", memberPermissions, err)
	}

	issues, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{
		ProjectID: env.projectID, ReporterID: &alice.ID, IncludeSubIssues: true, Limit: 10,
		Sort: store.ListIssuesSortCreated, Direction: store.ListIssuesSortDescending,
	})
	if err != nil || len(issues) != 2 || issues[0].ID != child.ID || issues[1].ID != aliceIssue.ID {
		t.Fatalf("reported issues = %+v, %v", issues, err)
	}

	// A help desk is never listed to a non-member, not even as a place to
	// file issues.
	for name, params := range map[string]store.ListProjectsParams{
		"visible":        {VisibleToUser: &alice.ID, Limit: 100},
		"issue creation": {IssueCreatableToUser: &alice.ID, Limit: 100},
	} {
		projects, _, err := env.store.ListProjects(env.ctx, params)
		if err != nil || projectInList(projects, env.projectID) {
			t.Fatalf("%s projects include help desk: %+v, %v", name, projects, err)
		}
	}

	// A blocked reporter loses everything.
	if _, err := env.store.BlockProjectUser(env.ctx, env.projectID, alice.ID, owner.ID); err != nil {
		t.Fatalf("BlockProjectUser: %v", err)
	}
	blocked, err := env.store.ProjectPermissionsForUser(env.ctx, alice, env.projectID)
	if err != nil || !blocked.IsBlocked || blocked.HelpDeskReporter || blocked.CanCreateIssues || blocked.CanFollowIssue(alice, aliceIssue) {
		t.Fatalf("blocked reporter permissions = %+v, %v", blocked, err)
	}
	if _, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{ProjectID: env.projectID, Title: "Blocked", ReporterID: &alice.ID}); err == nil {
		t.Fatal("CreateIssue accepted a blocked reporter")
	}
}

func TestPushNotificationsReachHelpDeskReporterForTheirIssueOnly(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if _, err := env.store.UpdateProjectAccessMode(env.ctx, env.projectID, model.ProjectAccessHelpDesk); err != nil {
		t.Fatalf("UpdateProjectAccessMode: %v", err)
	}
	reporter := mustHelpDeskOutsider(t, env, "pushhelpdesk")
	mustUpsertPushSubscription(t, env, reporter.ID, "https://push.example.test/"+uuid.NewString())
	if _, err := env.store.UpdatePushNotificationPreferences(env.ctx, reporter.ID, model.PushNotificationPreferences{
		Mentions: true, Comments: true, StatusChanges: true, DueDateChanges: true,
	}); err != nil {
		t.Fatalf("UpdatePushNotificationPreferences: %v", err)
	}
	issue, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{ProjectID: env.projectID, Title: "Help", ReporterID: &reporter.ID})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	mustMaterializePushEvents(t, env)
	mustCompletePushDeliveries(t, env, mustClaimPushDeliveries(t, env))

	prepared := func() map[store.PushNotificationCategory]bool {
		t.Helper()
		mustMaterializePushEvents(t, env)
		deliveries := mustClaimPushDeliveries(t, env)
		out := map[store.PushNotificationCategory]bool{}
		for _, delivery := range deliveries {
			if delivery.UserID != reporter.ID {
				continue
			}
			_, ok, err := env.store.PreparePushNotificationDelivery(env.ctx, delivery)
			if err != nil {
				t.Fatalf("PreparePushNotificationDelivery: %v", err)
			}
			out[delivery.Category] = ok
		}
		mustCompletePushDeliveries(t, env, deliveries)
		return out
	}

	ownerCtx := store.WithActor(env.ctx, project.OwnerID)
	if _, err := env.store.CreateComment(ownerCtx, store.CreateCommentParams{
		IssueID: issue.ID, AuthorID: project.OwnerID, Body: "We are looking into it", Visibility: model.CommentVisibilityShared,
	}); err != nil {
		t.Fatalf("CreateComment shared: %v", err)
	}
	if got := prepared(); !got[store.PushNotificationComments] {
		t.Fatalf("shared reply deliveries = %+v", got)
	}
	if _, err := env.store.CreateComment(ownerCtx, store.CreateCommentParams{IssueID: issue.ID, AuthorID: project.OwnerID, Body: "Internal note"}); err != nil {
		t.Fatalf("CreateComment default: %v", err)
	}
	if got := prepared(); got[store.PushNotificationComments] {
		t.Fatalf("members-only note delivered to reporter: %+v", got)
	}
	status := model.StatusInProgress
	if _, err := env.store.UpdateIssue(ownerCtx, issue.ID, store.UpdateIssueParams{Status: &status}); err != nil {
		t.Fatalf("UpdateIssue status: %v", err)
	}
	if got := prepared(); !got[store.PushNotificationStatusChanges] {
		t.Fatalf("status change deliveries = %+v", got)
	}
	due := model.DateFromTime(issue.CreatedAt.AddDate(0, 0, 7))
	if _, err := env.store.UpdateIssue(ownerCtx, issue.ID, store.UpdateIssueParams{DueDate: &due}); err != nil {
		t.Fatalf("UpdateIssue due date: %v", err)
	}
	if got := prepared(); got[store.PushNotificationDueDateChanges] {
		t.Fatalf("due date change delivered to reporter: %+v", got)
	}
}
