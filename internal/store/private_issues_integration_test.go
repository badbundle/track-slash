package store_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func issueInList(issues []model.Issue, id uuid.UUID) bool {
	for _, issue := range issues {
		if issue.ID == id {
			return true
		}
	}
	return false
}

// Every store listing that names, counts or charts issues leaves private ones
// out unless the caller includes them for a member.
func TestPrivateIssuesStayOutOfListsForNonMembers(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	ownerCtx := store.WithActor(env.ctx, project.OwnerID)
	if _, err := env.store.UpdateProjectAccessMode(env.ctx, env.projectID, model.ProjectAccessPublicIssues); err != nil {
		t.Fatalf("UpdateProjectAccessMode: %v", err)
	}
	reporter, err := env.store.CreateUserProfile(env.ctx, "privatereporter"+strings.ToLower(uniqueProjectKey(t)), "private-reporter-"+uuid.NewString()+"@example.com", "Reporter")
	if err != nil {
		t.Fatalf("CreateUserProfile reporter: %v", err)
	}
	stranger, err := env.store.CreateUserProfile(env.ctx, "privatestranger"+strings.ToLower(uniqueProjectKey(t)), "private-stranger-"+uuid.NewString()+"@example.com", "Stranger")
	if err != nil {
		t.Fatalf("CreateUserProfile stranger: %v", err)
	}

	public, err := env.store.CreateIssue(ownerCtx, store.CreateIssueParams{ProjectID: env.projectID, Title: "Public issue"})
	if err != nil {
		t.Fatalf("CreateIssue public: %v", err)
	}
	secret, err := env.store.CreateIssue(ownerCtx, store.CreateIssueParams{ProjectID: env.projectID, Title: "Private report", Private: true, ReporterID: &reporter.ID})
	if err != nil || !secret.Private {
		t.Fatalf("CreateIssue private = %+v, %v", secret, err)
	}
	privateChild, err := env.store.CreateSubIssue(ownerCtx, store.CreateSubIssueParams{ParentIssueID: public.ID, Title: "Private child", Private: true})
	if err != nil || !privateChild.Private {
		t.Fatalf("CreateSubIssue private = %+v, %v", privateChild, err)
	}
	publicChild, err := env.store.CreateSubIssue(ownerCtx, store.CreateSubIssueParams{ParentIssueID: public.ID, Title: "Public child"})
	if err != nil || publicChild.Private {
		t.Fatalf("CreateSubIssue public = %+v, %v", publicChild, err)
	}
	link, err := env.store.CreateIssueLink(ownerCtx, store.CreateIssueLinkParams{SourceID: public.ID, TargetID: secret.ID, LinkType: model.LinkTypeRelatesTo})
	if err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}
	contextItem, err := env.store.CreateProjectContext(ownerCtx, store.CreateProjectContextParams{
		ProjectID: env.projectID, Scope: model.ProjectContextScopeProject, Title: "Runbook",
		Kind: model.ProjectContextKindText, ContentType: "text/plain; charset=utf-8", Body: "steps", CreatedByID: project.OwnerID,
	})
	if err != nil {
		t.Fatalf("CreateProjectContext: %v", err)
	}
	contextLink, err := env.store.CreateIssueContextLink(ownerCtx, secret.ID, contextItem.ID)
	if err != nil {
		t.Fatalf("CreateIssueContextLink: %v", err)
	}
	tag, err := env.store.CreateIssueTag(ownerCtx, store.CreateIssueTagParams{ProjectID: env.projectID, Name: "security", Color: model.TagColorRed})
	if err != nil {
		t.Fatalf("CreateIssueTag: %v", err)
	}
	tagLink, err := env.store.CreateIssueTagLink(ownerCtx, store.CreateIssueTagLinkParams{IssueID: secret.ID, TagID: tag.ID})
	if err != nil {
		t.Fatalf("CreateIssueTagLink: %v", err)
	}
	comment, err := env.store.CreateComment(ownerCtx, store.CreateCommentParams{IssueID: secret.ID, AuthorID: project.OwnerID, Body: "Confirmed"})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	object, err := env.store.CreateStorageObject(env.ctx, storageObjectParams(env.projectID, project.OwnerID, "projects/a/objects/"+uuid.NewString()))
	if err != nil {
		t.Fatalf("CreateStorageObject: %v", err)
	}
	if _, err := env.store.CreateIssueAttachment(env.ctx, store.CreateIssueAttachmentParams{IssueID: secret.ID, StorageObjectID: object.ID, CreatedByID: project.OwnerID}); err != nil {
		t.Fatalf("CreateIssueAttachment: %v", err)
	}
	for _, status := range []model.Status{model.StatusInProgress, model.StatusDone} {
		for _, issue := range []model.Issue{public, secret} {
			if _, err := env.store.UpdateIssue(ownerCtx, issue.ID, store.UpdateIssueParams{Status: &status}); err != nil {
				t.Fatalf("UpdateIssue %s: %v", status, err)
			}
		}
	}

	for _, include := range []bool{false, true} {
		issues, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{ProjectID: env.projectID, Limit: 50, IncludeSubIssues: true, IncludePrivate: include})
		if err != nil {
			t.Fatalf("ListIssues: %v", err)
		}
		if !issueInList(issues, public.ID) || issueInList(issues, secret.ID) != include || issueInList(issues, privateChild.ID) != include {
			t.Fatalf("ListIssues include=%v = %+v", include, issues)
		}
		yes := true
		onlyPrivate, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{ProjectID: env.projectID, Limit: 50, IncludePrivate: include, Private: &yes})
		if err != nil || issueInList(onlyPrivate, public.ID) || issueInList(onlyPrivate, secret.ID) != include {
			t.Fatalf("ListIssues private filter include=%v = %+v, %v", include, onlyPrivate, err)
		}
		children, _, err := env.store.ListSubIssuesForIssue(env.ctx, store.ListSubIssuesForIssueParams{ParentIssueID: public.ID, Limit: 50, IncludePrivate: include})
		if err != nil || !issueInList(children, publicChild.ID) || issueInList(children, privateChild.ID) != include {
			t.Fatalf("ListSubIssuesForIssue include=%v = %+v, %v", include, children, err)
		}
		progress, err := env.store.ListSubIssueProgress(env.ctx, []uuid.UUID{public.ID}, include)
		wantChildren := map[bool]int{false: 1, true: 2}[include]
		if err != nil || progress[public.ID].Total != wantChildren {
			t.Fatalf("ListSubIssueProgress include=%v = %+v, %v", include, progress, err)
		}
		stats, err := env.store.GetProjectStats(env.ctx, store.ProjectStatsParams{ProjectID: env.projectID, IncludePrivate: include})
		wantTotal := map[bool]int{false: 2, true: 4}[include]
		if err != nil || stats.AllTime.Total != wantTotal {
			t.Fatalf("GetProjectStats include=%v = %+v, %v", include, stats.AllTime, err)
		}
		completed, _, err := env.store.ListRecentlyCompletedIssues(env.ctx, store.ListRecentlyCompletedIssuesParams{ProjectID: env.projectID, Since: time.Now().Add(-time.Hour), Limit: 50, IncludePrivate: include})
		sawSecret := false
		for _, done := range completed {
			sawSecret = sawSecret || done.ID == secret.ID
		}
		if err != nil || sawSecret != include {
			t.Fatalf("ListRecentlyCompletedIssues include=%v = %+v, %v", include, completed, err)
		}
		insights, err := env.store.GetProjectInsights(env.ctx, store.ProjectInsightsParams{ProjectID: env.projectID, IncludePrivate: include})
		sawSecret = false
		for _, point := range insights.CycleTime.Issues {
			sawSecret = sawSecret || point.IssueID == secret.ID
		}
		if err != nil || sawSecret != include || len(insights.CycleTime.Issues) != map[bool]int{false: 1, true: 2}[include] {
			t.Fatalf("GetProjectInsights include=%v cycle time = %+v, %v", include, insights.CycleTime, err)
		}
		links, _, err := env.store.ListIssueLinksForIssue(env.ctx, store.ListIssueLinksForIssueParams{IssueID: public.ID, Limit: 50, IncludePrivate: include})
		if err != nil || (len(links) == 1) != include {
			t.Fatalf("ListIssueLinksForIssue include=%v = %+v, %v", include, links, err)
		}
		if _, err := env.store.GetIssueLinkByProjectNumber(env.ctx, env.projectID, link.Number, include); (err == nil) != include || (!include && !errors.Is(err, store.ErrNotFound)) {
			t.Fatalf("GetIssueLinkByProjectNumber include=%v = %v", include, err)
		}
		contexts, _, err := env.store.ListProjectContexts(env.ctx, store.ListProjectContextsParams{ProjectID: env.projectID, Limit: 50, IncludePrivate: include})
		if err != nil || len(contexts) != 1 || contexts[0].LinkedIssueCount != map[bool]int{false: 0, true: 1}[include] {
			t.Fatalf("ListProjectContexts include=%v = %+v, %v", include, contexts, err)
		}
		contextIssues, _, err := env.store.ListIssuesForContext(env.ctx, store.ListIssuesForContextParams{ContextID: contextItem.ID, Limit: 50, IncludePrivate: include})
		if err != nil || issueInList(contextIssues, secret.ID) != include {
			t.Fatalf("ListIssuesForContext include=%v = %+v, %v", include, contextIssues, err)
		}
		if _, err := env.store.GetVisibleStorageObjectByProjectNumber(env.ctx, env.projectID, object.Number, !include); (err == nil) != include {
			t.Fatalf("GetVisibleStorageObjectByProjectNumber hide=%v = %v", !include, err)
		}
		entries, _, err := env.store.ListProjectChangelog(env.ctx, store.ListProjectChangelogParams{ProjectID: env.projectID, Limit: 200, IncludeMembersOnly: include})
		if err != nil {
			t.Fatalf("ListProjectChangelog: %v", err)
		}
		sawSecret, sawLink, sawPublic := false, false, false
		for _, entry := range entries {
			sawSecret = sawSecret || (entry.IssueID != nil && (*entry.IssueID == secret.ID || *entry.IssueID == privateChild.ID))
			sawLink = sawLink || entry.Entity == "issue_link"
			sawPublic = sawPublic || (entry.IssueID != nil && *entry.IssueID == public.ID)
		}
		if sawSecret != include || sawLink != include || !sawPublic {
			t.Fatalf("changelog include=%v: private %v, link naming it %v, public %v", include, sawSecret, sawLink, sawPublic)
		}
	}

	// Realtime topics about a private issue.
	for kind, tc := range map[string]struct {
		id   uuid.UUID
		want bool
	}{
		"issue":              {id: secret.ID, want: true},
		"comment":            {id: comment.ID, want: true},
		"issue_link":         {id: link.ID, want: true},
		"issue_context_link": {id: contextLink.ID, want: true},
		"issue_tag_link":     {id: tagLink.ID, want: true},
		"issue_tag":          {id: tag.ID},
		"project":            {id: env.projectID},
	} {
		if got, err := env.store.RealtimeTopicIsPrivate(env.ctx, kind, tc.id); err != nil || got != tc.want {
			t.Fatalf("RealtimeTopicIsPrivate %s = %v, %v, want %v", kind, got, err, tc.want)
		}
	}
	if got, err := env.store.RealtimeTopicIsPrivate(env.ctx, "issue", public.ID); err != nil || got {
		t.Fatalf("RealtimeTopicIsPrivate public issue = %v, %v", got, err)
	}
	entries, _, err := env.store.ListProjectChangelog(env.ctx, store.ListProjectChangelogParams{ProjectID: env.projectID, Limit: 200, IncludeMembersOnly: true})
	if err != nil {
		t.Fatalf("ListProjectChangelog: %v", err)
	}
	for _, entry := range entries {
		named := entry.Entity == "issue_link" || (entry.IssueID != nil && *entry.IssueID == secret.ID)
		if got, err := env.store.RealtimeTopicIsPrivate(env.ctx, "project_changelog", entry.ID); err != nil || (named && !got) {
			t.Fatalf("RealtimeTopicIsPrivate changelog %s %s = %v, %v", entry.Entity, entry.Op, got, err)
		}
	}

	// Recents keep a private issue only for its reporter and the members.
	for _, user := range []model.User{reporter, stranger} {
		if err := env.store.RecordIssueView(env.ctx, user.ID, secret.ID); err != nil {
			t.Fatalf("RecordIssueView: %v", err)
		}
	}
	for name, tc := range map[string]struct {
		user model.User
		want bool
	}{"reporter": {user: reporter, want: true}, "stranger": {user: stranger}} {
		recents, err := env.store.ListRecentIssues(env.ctx, tc.user, 10)
		if err != nil || issueInList(recents, secret.ID) != tc.want {
			t.Fatalf("ListRecentIssues %s = %+v, %v", name, recents, err)
		}
	}
	if _, err := env.store.SetProjectMemberRole(env.ctx, env.projectID, stranger.ID, model.ProjectMemberRoleReadonly); err != nil {
		t.Fatalf("SetProjectMemberRole: %v", err)
	}
	if recents, err := env.store.ListRecentIssues(env.ctx, stranger, 10); err != nil || !issueInList(recents, secret.ID) {
		t.Fatalf("ListRecentIssues read-only member = %+v, %v", recents, err)
	}
}

func TestPrivateIssueSprintHistory(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	ownerCtx := store.WithActor(env.ctx, project.OwnerID)
	sprint, err := env.store.CreateSprint(ownerCtx, store.CreateSprintParams{ProjectID: env.projectID, Name: "With a secret"})
	if err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}
	public := mustCreateIssue(t, env, "Public sprint issue")
	secret, err := env.store.CreateIssue(ownerCtx, store.CreateIssueParams{ProjectID: env.projectID, Title: "Private sprint issue", Private: true})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	for _, issue := range []model.Issue{public, secret} {
		if _, err := env.store.UpdateIssue(ownerCtx, issue.ID, store.UpdateIssueParams{SprintID: &sprint.ID}); err != nil {
			t.Fatalf("UpdateIssue sprint: %v", err)
		}
	}
	active := model.SprintStatusActive
	if _, err := env.store.UpdateSprint(ownerCtx, sprint.ID, store.UpdateSprintParams{Status: &active}); err != nil {
		t.Fatalf("activate sprint: %v", err)
	}
	if _, err := env.store.CompleteSprint(ownerCtx, sprint.ID); err != nil {
		t.Fatalf("CompleteSprint: %v", err)
	}
	for _, include := range []bool{false, true} {
		issues, _, err := env.store.ListSprintSnapshotIssues(env.ctx, store.ListSprintSnapshotIssuesParams{ProjectID: env.projectID, SprintID: sprint.ID, Limit: 50, IncludePrivate: include})
		if err != nil || !issueInList(issues, public.ID) || issueInList(issues, secret.ID) != include {
			t.Fatalf("ListSprintSnapshotIssues include=%v = %+v, %v", include, issues, err)
		}
		counts, err := env.store.CountSprintSnapshotIssuesByStatus(env.ctx, store.CountSprintSnapshotIssuesByStatusParams{ProjectID: env.projectID, SprintIDs: []uuid.UUID{sprint.ID}, IncludePrivate: include})
		if err != nil || counts[sprint.ID].Total != map[bool]int{false: 1, true: 2}[include] {
			t.Fatalf("CountSprintSnapshotIssuesByStatus include=%v = %+v, %v", include, counts, err)
		}
	}
}

// A sub-issue is never more visible than its parent.
func TestPrivateIssueSubIssuesFollowTheirParent(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	ownerCtx := store.WithActor(env.ctx, project.OwnerID)
	parent := mustCreateIssue(t, env, "Parent")
	live, err := env.store.CreateSubIssue(ownerCtx, store.CreateSubIssueParams{ParentIssueID: parent.ID, Title: "Live child"})
	if err != nil {
		t.Fatalf("CreateSubIssue live: %v", err)
	}
	deleted, err := env.store.CreateSubIssue(ownerCtx, store.CreateSubIssueParams{ParentIssueID: parent.ID, Title: "Deleted child"})
	if err != nil {
		t.Fatalf("CreateSubIssue deleted: %v", err)
	}
	if err := env.store.DeleteIssue(ownerCtx, deleted.ID); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}

	yes, no := true, false
	updated, err := env.store.UpdateIssue(ownerCtx, parent.ID, store.UpdateIssueParams{Private: &yes})
	if err != nil || !updated.Private {
		t.Fatalf("make parent private = %+v, %v", updated, err)
	}
	if child, err := env.store.GetIssue(env.ctx, live.ID); err != nil || !child.Private {
		t.Fatalf("live child after parent went private = %+v, %v", child, err)
	}
	if _, err := env.store.RestoreIssue(ownerCtx, deleted.ID); err != nil {
		t.Fatalf("RestoreIssue: %v", err)
	}
	if child, err := env.store.GetIssue(env.ctx, deleted.ID); err != nil || !child.Private {
		t.Fatalf("restored child = %+v, %v", child, err)
	}
	entries, _, err := env.store.ListProjectChangelog(env.ctx, store.ListProjectChangelogParams{ProjectID: env.projectID, Limit: 100, IncludeMembersOnly: true})
	if err != nil {
		t.Fatalf("ListProjectChangelog: %v", err)
	}
	cascaded := false
	for _, entry := range entries {
		if entry.IssueID != nil && *entry.IssueID == live.ID && len(entry.Details.Changes) == 1 {
			change := entry.Details.Changes[0]
			cascaded = cascaded || (change.Field == "private" && change.From == "No" && change.To == "Yes")
		}
	}
	if !cascaded {
		t.Fatalf("the child's changelog does not record it going private: %+v", entries)
	}

	if _, err := env.store.UpdateIssue(ownerCtx, live.ID, store.UpdateIssueParams{Private: &no}); !errors.Is(err, store.ErrSubIssueOfPrivateIssue) || !errors.Is(err, store.ErrConflict) {
		t.Fatalf("making a child of a private issue public = %v", err)
	}
	under, err := env.store.CreateSubIssue(ownerCtx, store.CreateSubIssueParams{ParentIssueID: parent.ID, Title: "Filed under a private parent"})
	if err != nil || !under.Private {
		t.Fatalf("CreateSubIssue under private parent = %+v, %v", under, err)
	}

	// Making the parent public again is a choice for each issue.
	if _, err := env.store.UpdateIssue(ownerCtx, parent.ID, store.UpdateIssueParams{Private: &no}); err != nil {
		t.Fatalf("make parent public: %v", err)
	}
	if child, err := env.store.GetIssue(env.ctx, live.ID); err != nil || !child.Private {
		t.Fatalf("child after parent went public = %+v, %v", child, err)
	}
	if child, err := env.store.UpdateIssue(ownerCtx, live.ID, store.UpdateIssueParams{Private: &no}); err != nil || child.Private {
		t.Fatalf("make child public under a public parent = %+v, %v", child, err)
	}
}

func TestPrivateIssuePermissions(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	owner, err := env.store.GetUser(env.ctx, project.OwnerID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if _, err := env.store.UpdateProjectAccessMode(env.ctx, env.projectID, model.ProjectAccessPublicIssues); err != nil {
		t.Fatalf("UpdateProjectAccessMode: %v", err)
	}
	newUser := func(prefix string) model.User {
		t.Helper()
		user, err := env.store.CreateUserProfile(env.ctx, prefix+strings.ToLower(uniqueProjectKey(t)), prefix+"-"+uuid.NewString()+"@example.com", prefix)
		if err != nil {
			t.Fatalf("CreateUserProfile %s: %v", prefix, err)
		}
		return user
	}
	reporter, outsider, readonly, blocked := newUser("permreporter"), newUser("permoutsider"), newUser("permreadonly"), newUser("permblocked")
	if _, err := env.store.SetProjectMemberRole(env.ctx, env.projectID, readonly.ID, model.ProjectMemberRoleReadonly); err != nil {
		t.Fatalf("SetProjectMemberRole: %v", err)
	}
	if _, err := env.store.BlockProjectUser(env.ctx, env.projectID, blocked.ID, owner.ID); err != nil {
		t.Fatalf("BlockProjectUser: %v", err)
	}
	secret, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{ProjectID: env.projectID, Title: "Private", Private: true, ReporterID: &reporter.ID})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	open := secret
	open.Private = false

	for name, tc := range map[string]struct {
		user                      model.User
		reads, hides, follows     bool
		readsOpen, writesOpenOnly bool
	}{
		"owner":     {user: owner, reads: true, readsOpen: true},
		"read-only": {user: readonly, reads: true, readsOpen: true},
		"reporter":  {user: reporter, hides: true, follows: true, readsOpen: true},
		"outsider":  {user: outsider, hides: true, readsOpen: true},
		"anonymous": {user: model.User{}, hides: true, readsOpen: true},
		"blocked":   {user: blocked},
	} {
		permissions, err := env.store.ProjectPermissionsForUser(env.ctx, tc.user, env.projectID)
		if err != nil {
			t.Fatalf("%s ProjectPermissionsForUser: %v", name, err)
		}
		if permissions.CanReadIssue(secret) != tc.reads || permissions.HidesIssue(secret) != tc.hides || permissions.CanReadIssue(open) != tc.readsOpen || permissions.HidesIssue(open) {
			t.Fatalf("%s reads private %v hides %v reads open %v: %+v", name, permissions.CanReadIssue(secret), permissions.HidesIssue(secret), permissions.CanReadIssue(open), permissions)
		}
		narrowed := permissions.ForIssue(tc.user, secret)
		if narrowed.CanRead != tc.reads || narrowed.CanFollowIssue(tc.user, secret) != tc.follows || narrowed.PrivateIssueReporter != tc.follows {
			t.Fatalf("%s ForIssue = %+v", name, narrowed)
		}
		if tc.hides && (narrowed.CanWrite || narrowed.CanCreateIssues || narrowed.CanReadMembersOnly || narrowed.CanManageMembers || narrowed.CanDelete) {
			t.Fatalf("%s keeps rights on a private issue it can't read: %+v", name, narrowed)
		}
		if unchanged := permissions.ForIssue(tc.user, open); unchanged != permissions {
			t.Fatalf("%s ForIssue changed an ordinary issue: %+v", name, unchanged)
		}
	}
}

// Push notifications about a private issue reach its members and its
// reporter, never a non-member who commented on it while it was public.
func TestPrivateIssuePushNotifications(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	ownerCtx := store.WithActor(env.ctx, project.OwnerID)
	if _, err := env.store.UpdateProjectAccessMode(env.ctx, env.projectID, model.ProjectAccessPublicIssues); err != nil {
		t.Fatalf("UpdateProjectAccessMode: %v", err)
	}
	member := mustPushProjectMember(t, env, "privatepushmember")
	reporter, err := env.store.CreateUserProfile(env.ctx, "privatepushreporter"+strings.ToLower(uniqueProjectKey(t)), "push-reporter-"+uuid.NewString()+"@example.com", "Reporter")
	if err != nil {
		t.Fatalf("CreateUserProfile reporter: %v", err)
	}
	commenter, err := env.store.CreateUserProfile(env.ctx, "privatepushcommenter"+strings.ToLower(uniqueProjectKey(t)), "push-commenter-"+uuid.NewString()+"@example.com", "Commenter")
	if err != nil {
		t.Fatalf("CreateUserProfile commenter: %v", err)
	}
	for _, user := range []model.User{member, reporter, commenter} {
		mustUpsertPushSubscription(t, env, user.ID, "https://push.example.test/"+uuid.NewString())
		if _, err := env.store.UpdatePushNotificationPreferences(env.ctx, user.ID, model.PushNotificationPreferences{Comments: true, StatusChanges: true}); err != nil {
			t.Fatalf("UpdatePushNotificationPreferences: %v", err)
		}
	}
	issue, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{ProjectID: env.projectID, Title: "Starts public", ReporterID: &reporter.ID, AssigneeID: &member.ID})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if _, err := env.store.CreateComment(store.WithActor(env.ctx, commenter.ID), store.CreateCommentParams{IssueID: issue.ID, AuthorID: commenter.ID, Body: "Me too"}); err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	mustMaterializePushEvents(t, env)
	mustCompletePushDeliveries(t, env, mustClaimPushDeliveries(t, env))

	yes := true
	if _, err := env.store.UpdateIssue(ownerCtx, issue.ID, store.UpdateIssueParams{Private: &yes}); err != nil {
		t.Fatalf("make private: %v", err)
	}
	mustMaterializePushEvents(t, env)
	mustCompletePushDeliveries(t, env, mustClaimPushDeliveries(t, env))
	status := model.StatusInProgress
	if _, err := env.store.UpdateIssue(ownerCtx, issue.ID, store.UpdateIssueParams{Status: &status}); err != nil {
		t.Fatalf("UpdateIssue status: %v", err)
	}
	mustMaterializePushEvents(t, env)
	deliveries := mustClaimPushDeliveries(t, env)
	defer mustCompletePushDeliveries(t, env, deliveries)
	delivered := map[uuid.UUID]string{}
	for _, delivery := range deliveries {
		payload, ok, err := env.store.PreparePushNotificationDelivery(env.ctx, delivery)
		if err != nil {
			t.Fatalf("PreparePushNotificationDelivery: %v", err)
		}
		if ok {
			delivered[delivery.UserID] = payload.Body
		}
	}
	if _, ok := delivered[commenter.ID]; ok {
		t.Fatalf("a non-member commenter heard about the private issue: %+v", delivered)
	}
	if body, ok := delivered[member.ID]; !ok || !strings.Contains(body, "In progress") {
		t.Fatalf("member delivery = %q, %v", body, ok)
	}
	if body, ok := delivered[reporter.ID]; !ok || !strings.HasPrefix(body, "Status changed to In progress") {
		t.Fatalf("reporter delivery = %q, %v", body, ok)
	}
}
