package store_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestSubIssuesCreateListFieldsAndFiltering(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	parent := mustCreateIssue(t, env, "parent")
	assignee := mustCreateUser(t, env, "sub-assignee-"+uniqueDigits(timeNow(t), 8)+"@example.com")
	if _, err := env.store.GrantProjectAccess(env.ctx, env.projectID, assignee.ID); err != nil {
		t.Fatalf("GrantProjectAccess assignee: %v", err)
	}

	child, err := env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "child one",
		Description:   "child body",
		AssigneeID:    &assignee.ID,
		ReporterID:    &assignee.ID,
	})
	if err != nil {
		t.Fatalf("CreateSubIssue: %v", err)
	}
	if child.ProjectID != parent.ProjectID {
		t.Fatalf("child project = %s, want %s", child.ProjectID, parent.ProjectID)
	}
	if child.ParentIssueID == nil || *child.ParentIssueID != parent.ID {
		t.Fatalf("ParentIssueID = %v, want %s", child.ParentIssueID, parent.ID)
	}
	if child.SprintID != nil {
		t.Fatalf("sub-issue sprint = %v, want nil", child.SprintID)
	}
	if child.Number != parent.Number+1 {
		t.Fatalf("child number = %d, want %d", child.Number, parent.Number+1)
	}
	if child.AssigneeID == nil || *child.AssigneeID != assignee.ID || child.ReporterID == nil || *child.ReporterID != assignee.ID {
		t.Fatalf("child people fields not preserved: %+v", child)
	}

	childTwo, err := env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "child two",
	})
	if err != nil {
		t.Fatalf("CreateSubIssue second: %v", err)
	}

	page1, more, err := env.store.ListSubIssuesForIssue(env.ctx, store.ListSubIssuesForIssueParams{
		ParentIssueID: parent.ID,
		Limit:         1,
	})
	if err != nil {
		t.Fatalf("ListSubIssuesForIssue page1: %v", err)
	}
	if !more || len(page1) != 1 || page1[0].ID != child.ID {
		t.Fatalf("page1 = %+v more=%v", page1, more)
	}
	page2, more, err := env.store.ListSubIssuesForIssue(env.ctx, store.ListSubIssuesForIssueParams{
		ParentIssueID: parent.ID,
		Cursor:        &store.IssuesCursor{Number: page1[0].Number},
		Limit:         1,
	})
	if err != nil {
		t.Fatalf("ListSubIssuesForIssue page2: %v", err)
	}
	if more || len(page2) != 1 || page2[0].ID != childTwo.ID {
		t.Fatalf("page2 = %+v more=%v", page2, more)
	}

	topLevel, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{ProjectID: env.projectID, Limit: 10})
	if err != nil {
		t.Fatalf("ListIssues top-level: %v", err)
	}
	if len(topLevel) != 1 || topLevel[0].ID != parent.ID {
		t.Fatalf("top-level issues = %+v, want only parent %s", topLevel, parent.ID)
	}
	all, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{
		ProjectID:        env.projectID,
		Limit:            10,
		IncludeSubIssues: true,
	})
	if err != nil {
		t.Fatalf("ListIssues include sub-issues: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("all issues len = %d, want 3: %+v", len(all), all)
	}
	backlog, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{
		ProjectID: env.projectID,
		Backlog:   true,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListIssues backlog: %v", err)
	}
	if len(backlog) != 1 || backlog[0].ID != parent.ID {
		t.Fatalf("backlog issues = %+v, want only parent %s", backlog, parent.ID)
	}

	status := model.StatusInProgress
	description := "updated child"
	updated, err := env.store.UpdateIssue(env.ctx, child.ID, store.UpdateIssueParams{
		Status:      &status,
		Description: &description,
	})
	if err != nil {
		t.Fatalf("UpdateIssue child: %v", err)
	}
	if updated.Status != status || updated.Description != description || updated.ParentIssueID == nil {
		t.Fatalf("updated child mismatch: %+v", updated)
	}

	comment := mustCreateComment(t, env, child.ID, assignee.ID, "child comment")
	comments, _, err := env.store.ListCommentsForIssue(env.ctx, store.ListCommentsForIssueParams{IssueID: child.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListCommentsForIssue child: %v", err)
	}
	if len(comments) != 1 || comments[0].ID != comment.ID {
		t.Fatalf("child comments = %+v, want %s", comments, comment.ID)
	}

	link, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: child.ID,
		TargetID: parent.ID,
		LinkType: model.LinkTypeRelatesTo,
	})
	if err != nil {
		t.Fatalf("CreateIssueLink child: %v", err)
	}
	links, _, err := env.store.ListIssueLinksForIssue(env.ctx, store.ListIssueLinksForIssueParams{IssueID: child.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListIssueLinksForIssue child: %v", err)
	}
	if len(links) != 1 || links[0].ID != link.ID {
		t.Fatalf("child links = %+v, want %s", links, link.ID)
	}
}

func TestSubIssueValidationAndSprintRules(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	parent := mustCreateIssue(t, env, "parent")
	child, err := env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "child",
	})
	if err != nil {
		t.Fatalf("CreateSubIssue: %v", err)
	}

	_, err = env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
		ParentIssueID: uuid.New(),
		Title:         "missing parent",
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing parent err = %v, want ErrNotFound", err)
	}

	_, _, err = env.store.ListSubIssuesForIssue(env.ctx, store.ListSubIssuesForIssueParams{
		ParentIssueID: uuid.New(),
		Limit:         10,
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("list missing parent err = %v, want ErrNotFound", err)
	}

	_, err = env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "invalid assignee",
		AssigneeID:    ptrUUID(uuid.New()),
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("invalid assignee err = %v, want ErrNotFound", err)
	}

	member := mustCreateUser(t, env, "sub-member-"+uniqueDigits(timeNow(t), 8)+"@example.com")
	if _, err := env.store.GrantProjectAccess(env.ctx, env.projectID, member.ID); err != nil {
		t.Fatalf("GrantProjectAccess member: %v", err)
	}
	withPeople, err := env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "member people",
		AssigneeID:    &member.ID,
		ReporterID:    &member.ID,
	})
	if err != nil {
		t.Fatalf("CreateSubIssue member people: %v", err)
	}
	if withPeople.AssigneeID == nil || *withPeople.AssigneeID != member.ID || withPeople.ReporterID == nil || *withPeople.ReporterID != member.ID {
		t.Fatalf("sub-issue people = assignee %v reporter %v, want %s", withPeople.AssigneeID, withPeople.ReporterID, member.ID)
	}

	_, err = env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
		ParentIssueID: child.ID,
		Title:         "nested child",
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("nested child err = %v, want ErrConflict", err)
	}

	_, _, err = env.store.ListSubIssuesForIssue(env.ctx, store.ListSubIssuesForIssueParams{
		ParentIssueID: child.ID,
		Limit:         10,
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("list nested err = %v, want ErrConflict", err)
	}

	sp := mustCreateSprint(t, env, "S", date(2026, 6, 1), date(2026, 6, 14))
	_, err = env.store.UpdateIssue(env.ctx, child.ID, store.UpdateIssueParams{SprintID: &sp.ID})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("assign child to sprint err = %v, want ErrConflict", err)
	}
}

func TestListSubIssueProgress(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)

	empty, err := env.store.ListSubIssueProgress(env.ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty progress = %+v, %v", empty, err)
	}

	parent := mustCreateIssue(t, env, "progress parent")
	withoutChildren := mustCreateIssue(t, env, "empty parent")
	children := make([]model.Issue, 4)
	for i := range children {
		children[i], err = env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
			ParentIssueID: parent.ID,
			Title:         fmt.Sprintf("progress child %d", i+1),
		})
		if err != nil {
			t.Fatalf("CreateSubIssue %d: %v", i+1, err)
		}
	}
	done := model.StatusDone
	if _, err := env.store.UpdateIssue(env.ctx, children[1].ID, store.UpdateIssueParams{Status: &done}); err != nil {
		t.Fatalf("complete child: %v", err)
	}
	closed := model.StatusClosed
	reason := model.CloseReasonDuplicate
	if _, err := env.store.UpdateIssue(env.ctx, children[2].ID, store.UpdateIssueParams{Status: &closed, CloseReason: &reason}); err != nil {
		t.Fatalf("close child: %v", err)
	}
	if err := env.store.DeleteIssue(env.ctx, children[3].ID); err != nil {
		t.Fatalf("delete child: %v", err)
	}

	progress, err := env.store.ListSubIssueProgress(env.ctx, []uuid.UUID{parent.ID, withoutChildren.ID})
	if err != nil {
		t.Fatalf("ListSubIssueProgress: %v", err)
	}
	if got := progress[parent.ID]; got != (store.SubIssueProgress{Total: 3, Completed: 2}) {
		t.Fatalf("parent progress = %+v, want 2/3", got)
	}
	if _, ok := progress[withoutChildren.ID]; ok {
		t.Fatalf("empty parent unexpectedly returned progress: %+v", progress)
	}
}

func ptrUUID(id uuid.UUID) *uuid.UUID {
	return &id
}

func TestDeleteIssueSoftDeletesSubIssuesOnlyWhenParentDeleted(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	parent := mustCreateIssue(t, env, "parent")
	child, err := env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "child",
	})
	if err != nil {
		t.Fatalf("CreateSubIssue: %v", err)
	}

	if err := env.store.DeleteIssue(env.ctx, child.ID); err != nil {
		t.Fatalf("DeleteIssue child: %v", err)
	}
	if _, err := env.store.GetIssue(env.ctx, parent.ID); err != nil {
		t.Fatalf("parent should remain after deleting child: %v", err)
	}

	child, err = env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "child two",
	})
	if err != nil {
		t.Fatalf("CreateSubIssue second: %v", err)
	}
	if err := env.store.DeleteIssue(env.ctx, parent.ID); err != nil {
		t.Fatalf("DeleteIssue parent: %v", err)
	}
	if _, err := env.store.GetIssue(env.ctx, child.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("child after parent delete err = %v, want ErrNotFound", err)
	}
}
