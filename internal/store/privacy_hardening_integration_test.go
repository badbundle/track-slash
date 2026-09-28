package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

// Members are found by name or username, never by email, and a query's LIKE
// wildcards match only themselves.
func TestProjectMemberSearchIgnoresEmailAndWildcards(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	key := strings.ToLower(uniqueProjectKey(t))
	member, err := env.store.CreateUserProfile(env.ctx, "searchable"+key, "hidden-"+key+"@secret.example", "Searchable Member")
	if err != nil {
		t.Fatalf("CreateUserProfile: %v", err)
	}
	if _, err := env.store.GrantProjectAccess(env.ctx, env.projectID, member.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	search := func(query string) []uuid.UUID {
		t.Helper()
		users, err := env.store.SearchProjectMembers(env.ctx, store.SearchProjectMembersParams{ProjectID: env.projectID, Query: query, Limit: 50})
		if err != nil {
			t.Fatalf("SearchProjectMembers(%q): %v", query, err)
		}
		ids := []uuid.UUID{}
		for _, u := range users {
			ids = append(ids, u.ID)
		}
		return ids
	}
	contains := func(ids []uuid.UUID, id uuid.UUID) bool {
		for _, candidate := range ids {
			if candidate == id {
				return true
			}
		}
		return false
	}
	if !contains(search("searchable"+key), member.ID) || !contains(search("Searchable Member"), member.ID) {
		t.Fatal("member not found by username or name")
	}
	for _, probe := range []string{"hidden-" + key, "@secret.example", "%", "_", "searchable%"} {
		if contains(search(probe), member.ID) {
			t.Fatalf("query %q matched the member", probe)
		}
	}
	// Candidate search escapes wildcards too.
	candidates, err := env.store.SearchAvailableProjectMembers(env.ctx, store.SearchAvailableProjectMembersParams{ProjectID: env.projectID, Query: "__", Limit: 10})
	if err != nil || len(candidates) != 0 {
		t.Fatalf("candidates for %q = %+v, %v", "__", candidates, err)
	}
}

// Who a project blocked is for its members.
func TestBlockChangelogIsForMembers(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	target, err := env.store.CreateUserProfile(env.ctx, "blocktarget"+strings.ToLower(uniqueProjectKey(t)), "", "Target")
	if err != nil {
		t.Fatalf("CreateUserProfile: %v", err)
	}
	if _, err := env.store.BlockProjectUser(env.ctx, env.projectID, target.ID, project.OwnerID); err != nil {
		t.Fatalf("BlockProjectUser: %v", err)
	}
	if err := env.store.UnblockProjectUser(env.ctx, env.projectID, target.ID); err != nil {
		t.Fatalf("UnblockProjectUser: %v", err)
	}
	count := func(include bool) int {
		t.Helper()
		entries, _, err := env.store.ListProjectChangelog(env.ctx, store.ListProjectChangelogParams{ProjectID: env.projectID, Limit: 100, IncludeMembersOnly: include})
		if err != nil {
			t.Fatalf("ListProjectChangelog: %v", err)
		}
		n := 0
		for _, entry := range entries {
			if entry.Entity == "project_block" {
				n++
			}
		}
		return n
	}
	if got := count(false); got != 0 {
		t.Fatalf("non-members see %d block entries", got)
	}
	if got := count(true); got != 2 {
		t.Fatalf("members see %d block entries, want 2", got)
	}
}

// A deleted issue, the changelog about it and the files only it holds are for
// members; so is the preview a deleted comment leaves behind.
func TestDeletedContentIsForMembers(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	ownerCtx := store.WithActor(env.ctx, project.OwnerID)
	kept := mustCreateIssue(t, env, "Kept")
	gone, err := env.store.CreateIssue(ownerCtx, store.CreateIssueParams{ProjectID: env.projectID, Title: "Pasted a secret here"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	object, err := env.store.CreateStorageObject(env.ctx, storageObjectParams(env.projectID, project.OwnerID, "projects/a/objects/"+uuid.NewString()))
	if err != nil {
		t.Fatalf("CreateStorageObject: %v", err)
	}
	if _, err := env.store.CreateIssueAttachment(env.ctx, store.CreateIssueAttachmentParams{IssueID: gone.ID, StorageObjectID: object.ID, CreatedByID: project.OwnerID}); err != nil {
		t.Fatalf("CreateIssueAttachment: %v", err)
	}
	comment, err := env.store.CreateComment(ownerCtx, store.CreateCommentParams{IssueID: kept.ID, AuthorID: project.OwnerID, Body: "a comment worth deleting"})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if err := env.store.DeleteComment(ownerCtx, store.DeleteCommentParams{ID: comment.ID, AuthorID: project.OwnerID}); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	if err := env.store.DeleteIssue(ownerCtx, gone.ID); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}

	// Files.
	if _, err := env.store.GetVisibleStorageObjectByProjectNumber(env.ctx, env.projectID, object.Number, true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("hidden object lookup = %v, want ErrNotFound", err)
	}
	if _, err := env.store.GetVisibleStorageObjectByProjectNumber(env.ctx, env.projectID, object.Number, false); err != nil {
		t.Fatalf("member object lookup: %v", err)
	}
	listed := func(hide bool) bool {
		t.Helper()
		objects, _, err := env.store.ListStorageObjects(env.ctx, store.ListStorageObjectsParams{ProjectID: env.projectID, Limit: 50, HideMembersOnlyIssueObjects: hide})
		if err != nil {
			t.Fatalf("ListStorageObjects: %v", err)
		}
		for _, candidate := range objects {
			if candidate.ID == object.ID {
				return true
			}
		}
		return false
	}
	if listed(true) || !listed(false) {
		t.Fatalf("object listed hidden=%v member=%v", listed(true), listed(false))
	}
	// Restoring the issue brings its file back for everyone.
	if _, err := env.store.RestoreIssue(ownerCtx, gone.ID); err != nil {
		t.Fatalf("RestoreIssue: %v", err)
	}
	if !listed(true) {
		t.Fatal("restored issue's file still hidden")
	}
	if err := env.store.DeleteIssue(ownerCtx, gone.ID); err != nil {
		t.Fatalf("DeleteIssue again: %v", err)
	}

	// Changelog.
	entries := func(include bool) []model.ProjectChangelogEntry {
		t.Helper()
		out, _, err := env.store.ListProjectChangelog(env.ctx, store.ListProjectChangelogParams{ProjectID: env.projectID, Limit: 100, IncludeMembersOnly: include})
		if err != nil {
			t.Fatalf("ListProjectChangelog: %v", err)
		}
		return out
	}
	for _, entry := range entries(false) {
		if entry.IssueID != nil && *entry.IssueID == gone.ID {
			t.Fatalf("non-members see the deleted issue's history: %+v", entry)
		}
		if entry.Entity == "comment" && entry.Op == "delete" {
			t.Fatalf("non-members see a deleted comment's preview: %+v", entry)
		}
	}
	sawGone, sawDeletedComment := false, false
	for _, entry := range entries(true) {
		sawGone = sawGone || (entry.IssueID != nil && *entry.IssueID == gone.ID)
		sawDeletedComment = sawDeletedComment || (entry.Entity == "comment" && entry.Op == "delete")
	}
	if !sawGone || !sawDeletedComment {
		t.Fatalf("members miss history: deleted issue %v, deleted comment %v", sawGone, sawDeletedComment)
	}
}

// Emails are not verified, so bootstrapping an admin by email must not
// promote whoever claimed that email first unless the operator says so.
func TestCreateOrUpdateAdminUserRefusesToPromoteByEmail(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	email := "claimed-" + uuid.NewString() + "@example.com"
	claimer, err := env.store.CreateUserProfile(env.ctx, "claimer"+strings.ToLower(uniqueProjectKey(t)), email, "Claimer")
	if err != nil {
		t.Fatalf("CreateUserProfile: %v", err)
	}
	if _, err := env.store.CreateOrUpdateAdminUser(env.ctx, email, "Operator"); !errors.Is(err, store.ErrEmailBelongsToNonAdmin) {
		t.Fatalf("CreateOrUpdateAdminUser over a non-admin = %v, want ErrEmailBelongsToNonAdmin", err)
	}
	if u, err := env.store.GetUser(env.ctx, claimer.ID); err != nil || u.IsAdmin {
		t.Fatalf("claimer after refusal = %+v, %v", u, err)
	}
	promoted, err := env.store.PromoteUserToAdminByEmail(env.ctx, email)
	if err != nil || promoted.ID != claimer.ID || !promoted.IsAdmin || promoted.Username != claimer.Username {
		t.Fatalf("PromoteUserToAdminByEmail = %+v, %v", promoted, err)
	}
	// An existing admin is refreshed as before.
	if again, err := env.store.CreateOrUpdateAdminUser(env.ctx, email, "Operator"); err != nil || again.ID != claimer.ID || !again.IsAdmin {
		t.Fatalf("CreateOrUpdateAdminUser over an admin = %+v, %v", again, err)
	}
	if _, err := env.store.PromoteUserToAdminByEmail(env.ctx, "nobody-"+uuid.NewString()+"@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("PromoteUserToAdminByEmail unknown = %v, want ErrNotFound", err)
	}
}

// A mention notifies only people already part of the work, and one change can
// mention at most a bounded number of them.
func TestPushMentionsReachOnlyParticipants(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if _, err := env.store.UpdateProjectAccessMode(env.ctx, env.projectID, model.ProjectAccessPublic); err != nil {
		t.Fatalf("UpdateProjectAccessMode: %v", err)
	}
	member := mustPushProjectMember(t, env, "mentionmember")
	stranger, err := env.store.CreateUserProfile(env.ctx, "mentionstranger"+strings.ToLower(uniqueProjectKey(t)), "", "Stranger")
	if err != nil {
		t.Fatalf("CreateUserProfile: %v", err)
	}
	for _, u := range []model.User{member, stranger} {
		mustUpsertPushSubscription(t, env, u.ID, "https://push.example.test/"+uuid.NewString())
	}
	issue := mustCreateIssue(t, env, "Mentions")
	mustMaterializePushEvents(t, env)
	mustCompletePushDeliveries(t, env, mustClaimPushDeliveries(t, env))

	if _, err := env.store.CreateComment(store.WithActor(env.ctx, project.OwnerID), store.CreateCommentParams{
		IssueID: issue.ID, AuthorID: project.OwnerID, Body: "@" + member.Username + " @" + stranger.Username + " take a look",
	}); err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	mustMaterializePushEvents(t, env)
	deliveries := mustClaimPushDeliveries(t, env)
	notified := map[uuid.UUID]bool{}
	for _, delivery := range deliveries {
		notified[delivery.UserID] = true
	}
	if !notified[member.ID] || notified[stranger.ID] {
		t.Fatalf("mention deliveries = %+v", notified)
	}
	mustCompletePushDeliveries(t, env, deliveries)
}

// A help-desk reporter hears about status changes in their own terms, and not
// at all when the member status moves without their status changing.
func TestPushStatusChangesSpeakTheReporterStatus(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if _, err := env.store.UpdateProjectAccessMode(env.ctx, env.projectID, model.ProjectAccessHelpDesk); err != nil {
		t.Fatalf("UpdateProjectAccessMode: %v", err)
	}
	reporter, err := env.store.CreateUserProfile(env.ctx, "statusreporter"+strings.ToLower(uniqueProjectKey(t)), "", "Reporter")
	if err != nil {
		t.Fatalf("CreateUserProfile: %v", err)
	}
	mustUpsertPushSubscription(t, env, reporter.ID, "https://push.example.test/"+uuid.NewString())
	if _, err := env.store.UpdatePushNotificationPreferences(env.ctx, reporter.ID, model.PushNotificationPreferences{StatusChanges: true}); err != nil {
		t.Fatalf("UpdatePushNotificationPreferences: %v", err)
	}
	issue, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{ProjectID: env.projectID, Title: "Help", ReporterID: &reporter.ID})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	mustMaterializePushEvents(t, env)
	mustCompletePushDeliveries(t, env, mustClaimPushDeliveries(t, env))

	move := func(status model.Status) (string, bool) {
		t.Helper()
		params := store.UpdateIssueParams{Status: &status}
		if status == model.StatusClosed {
			reason := model.CloseReasonWontDo
			params.CloseReason = &reason
		}
		if _, err := env.store.UpdateIssue(store.WithActor(env.ctx, project.OwnerID), issue.ID, params); err != nil {
			t.Fatalf("UpdateIssue %s: %v", status, err)
		}
		mustMaterializePushEvents(t, env)
		deliveries := mustClaimPushDeliveries(t, env)
		defer mustCompletePushDeliveries(t, env, deliveries)
		for _, delivery := range deliveries {
			if delivery.UserID != reporter.ID {
				continue
			}
			payload, ok, err := env.store.PreparePushNotificationDelivery(env.ctx, delivery)
			if err != nil {
				t.Fatalf("PreparePushNotificationDelivery: %v", err)
			}
			return payload.Body, ok
		}
		return "", false
	}
	if body, ok := move(model.StatusInProgress); !ok || !strings.HasPrefix(body, "Status changed to In progress") {
		t.Fatalf("todo to in progress = %q, %v", body, ok)
	}
	if body, ok := move(model.StatusDone); !ok || !strings.HasPrefix(body, "Status changed to Closed") {
		t.Fatalf("in progress to done = %q, %v", body, ok)
	}
	if body, ok := move(model.StatusClosed); ok {
		t.Fatalf("done to closed, both closed to the reporter, was delivered: %q", body)
	}
}
