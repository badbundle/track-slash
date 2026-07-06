package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/bradleymackey/track-slash/internal/testutil"
)

func TestStoreOwnerScopedPublicRefs(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	db := testutil.NewMigratedDatabase(t)
	s := store.New(db.Pool)
	key := storePublicRefKey(t)
	ownerA, err := s.CreateUserProfile(ctx, "owner-a-"+storePublicRefSuffix(t), "owner-a@example.com", "Owner A")
	if err != nil {
		t.Fatalf("CreateUserProfile ownerA: %v", err)
	}
	ownerB, err := s.CreateUserProfile(ctx, "owner-b-"+storePublicRefSuffix(t), "owner-b@example.com", "Owner B")
	if err != nil {
		t.Fatalf("CreateUserProfile ownerB: %v", err)
	}

	projectA, err := s.CreateProjectForUser(ctx, ownerA.ID, key, "A", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser A: %v", err)
	}
	projectB, err := s.CreateProjectForUser(ctx, ownerB.ID, key, "B", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser B same key: %v", err)
	}
	if projectA.OwnerID != ownerA.ID || projectA.OwnerUsername != ownerA.Username {
		t.Fatalf("projectA owner = %s/%q, want %s/%q", projectA.OwnerID, projectA.OwnerUsername, ownerA.ID, ownerA.Username)
	}
	if projectB.OwnerID != ownerB.ID || projectB.OwnerUsername != ownerB.Username {
		t.Fatalf("projectB owner = %s/%q, want %s/%q", projectB.OwnerID, projectB.OwnerUsername, ownerB.ID, ownerB.Username)
	}

	gotA, err := s.GetProjectByOwnerKey(ctx, ownerA.Username, key)
	if err != nil {
		t.Fatalf("GetProjectByOwnerKey A: %v", err)
	}
	gotB, err := s.GetProjectByOwnerKey(ctx, ownerB.Username, key)
	if err != nil {
		t.Fatalf("GetProjectByOwnerKey B: %v", err)
	}
	if gotA.ID != projectA.ID || gotB.ID != projectB.ID {
		t.Fatalf("owner/key lookup returned %+v and %+v", gotA, gotB)
	}
	if _, err := s.GetProjectByOwnerKey(ctx, ownerA.Username, "Z"+key[1:]); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wrong owner/key err = %v, want ErrNotFound", err)
	}

	sprint1, err := s.CreateSprint(ctx, store.CreateSprintParams{
		ProjectID: projectA.ID,
		Name:      "Sprint One",
		StartDate: datePtr(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
		EndDate:   datePtr(time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("CreateSprint 1: %v", err)
	}
	sprint2, err := s.CreateSprint(ctx, store.CreateSprintParams{
		ProjectID: projectA.ID,
		Name:      "Sprint Two",
		StartDate: datePtr(time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)),
		EndDate:   datePtr(time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("CreateSprint 2: %v", err)
	}
	if sprint1.Number != 1 || sprint1.Ref != "sprint-1" || sprint2.Number != 2 || sprint2.Ref != "sprint-2" {
		t.Fatalf("sprint refs = %+v %+v", sprint1, sprint2)
	}
	gotSprint, err := s.GetSprintByProjectNumber(ctx, projectA.ID, 2)
	if err != nil {
		t.Fatalf("GetSprintByProjectNumber: %v", err)
	}
	if gotSprint.ID != sprint2.ID {
		t.Fatalf("sprint lookup = %s, want %s", gotSprint.ID, sprint2.ID)
	}
	otherSprint, err := s.CreateSprint(ctx, store.CreateSprintParams{
		ProjectID: projectB.ID,
		Name:      "Other Sprint",
		StartDate: datePtr(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)),
		EndDate:   datePtr(time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("CreateSprint other: %v", err)
	}
	if otherSprint.Number != 1 || otherSprint.Ref != "sprint-1" {
		t.Fatalf("other project sprint ref = %+v", otherSprint)
	}

	issueA, err := s.CreateIssue(ctx, store.CreateIssueParams{ProjectID: projectA.ID, Title: "A-1"})
	if err != nil {
		t.Fatalf("CreateIssue A: %v", err)
	}
	issueB, err := s.CreateIssue(ctx, store.CreateIssueParams{ProjectID: projectB.ID, Title: "B-1"})
	if err != nil {
		t.Fatalf("CreateIssue B: %v", err)
	}
	if issueA.Identifier != key+"-1" || issueB.Identifier != key+"-1" {
		t.Fatalf("issue identifiers = %q %q", issueA.Identifier, issueB.Identifier)
	}
	gotIssueA, err := s.GetIssueByOwnerKeyNumber(ctx, ownerA.Username, key, 1)
	if err != nil {
		t.Fatalf("GetIssueByOwnerKeyNumber A: %v", err)
	}
	gotIssueB, err := s.GetIssueByOwnerKeyNumber(ctx, ownerB.Username, key, 1)
	if err != nil {
		t.Fatalf("GetIssueByOwnerKeyNumber B: %v", err)
	}
	if gotIssueA.ID != issueA.ID || gotIssueB.ID != issueB.ID {
		t.Fatalf("issue lookups = %+v %+v", gotIssueA, gotIssueB)
	}

	comment1, err := s.CreateComment(ctx, store.CreateCommentParams{
		IssueID:  issueA.ID,
		AuthorID: ownerA.ID,
		Body:     "one",
	})
	if err != nil {
		t.Fatalf("CreateComment 1: %v", err)
	}
	comment2, err := s.CreateComment(ctx, store.CreateCommentParams{
		IssueID:  issueA.ID,
		AuthorID: ownerA.ID,
		Body:     "two",
	})
	if err != nil {
		t.Fatalf("CreateComment 2: %v", err)
	}
	if comment1.Number != 1 || comment1.Ref != "comment-1" || comment2.Number != 2 || comment2.Ref != "comment-2" {
		t.Fatalf("comment refs = %+v %+v", comment1, comment2)
	}
	gotComment, err := s.GetCommentForIssueByNumber(ctx, issueA.ID, 2)
	if err != nil {
		t.Fatalf("GetCommentForIssueByNumber: %v", err)
	}
	if gotComment.ID != comment2.ID {
		t.Fatalf("comment lookup = %s, want %s", gotComment.ID, comment2.ID)
	}

	target1, err := s.CreateIssue(ctx, store.CreateIssueParams{ProjectID: projectA.ID, Title: "target 1"})
	if err != nil {
		t.Fatalf("CreateIssue target1: %v", err)
	}
	target2, err := s.CreateIssue(ctx, store.CreateIssueParams{ProjectID: projectA.ID, Title: "target 2"})
	if err != nil {
		t.Fatalf("CreateIssue target2: %v", err)
	}
	link1, err := s.CreateIssueLink(ctx, store.CreateIssueLinkParams{
		SourceID: issueA.ID,
		TargetID: target1.ID,
		LinkType: model.LinkTypeBlocks,
	})
	if err != nil {
		t.Fatalf("CreateIssueLink 1: %v", err)
	}
	link2, err := s.CreateIssueLink(ctx, store.CreateIssueLinkParams{
		SourceID: issueA.ID,
		TargetID: target2.ID,
		LinkType: model.LinkTypeRelatesTo,
	})
	if err != nil {
		t.Fatalf("CreateIssueLink 2: %v", err)
	}
	if link1.Number != 1 || link1.Ref != "link-1" || link2.Number != 2 || link2.Ref != "link-2" {
		t.Fatalf("link refs = %+v %+v", link1, link2)
	}
	gotLink, err := s.GetIssueLinkByProjectNumber(ctx, projectA.ID, 2)
	if err != nil {
		t.Fatalf("GetIssueLinkByProjectNumber: %v", err)
	}
	if gotLink.ID != link2.ID {
		t.Fatalf("link lookup = %s, want %s", gotLink.ID, link2.ID)
	}
}

func storePublicRefKey(t *testing.T) string {
	t.Helper()
	n := time.Now().UnixNano()
	out := make([]byte, 9)
	for i := 8; i >= 0; i-- {
		out[i] = byte('0' + (n % 10))
		n /= 10
	}
	return "P" + string(out)
}

func storePublicRefSuffix(t *testing.T) string {
	t.Helper()
	n := time.Now().UnixNano()
	out := make([]byte, 10)
	for i := 9; i >= 0; i-- {
		out[i] = byte('0' + (n % 10))
		n /= 10
	}
	return string(out)
}
