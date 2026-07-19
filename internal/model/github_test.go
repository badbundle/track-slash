package model

import (
	"testing"
	"time"
)

func TestGitHubEnumsAndDisplayHelpers(t *testing.T) {
	for _, resourceType := range []GitHubResourceType{GitHubResourceBranch, GitHubResourcePullRequest} {
		if !resourceType.Valid() {
			t.Errorf("resource type %q is invalid", resourceType)
		}
	}
	if GitHubResourceType("tag").Valid() {
		t.Fatal("tag resource type is valid")
	}
	for _, state := range []GitHubLinkState{GitHubLinkStateBranch, GitHubLinkStateDraft, GitHubLinkStateOpen, GitHubLinkStateMerged, GitHubLinkStateClosed, GitHubLinkStateUnknown} {
		if !state.Valid() {
			t.Errorf("state %q is invalid", state)
		}
	}
	if GitHubLinkState("deleted").Valid() {
		t.Fatal("deleted state is valid")
	}
	connection := GitHubConnection{RepositoryOwner: "acme", RepositoryName: "widgets"}
	link := GitHubIssueLink{RepositoryOwner: "acme", RepositoryName: "widgets"}
	if connection.FullName() != "acme/widgets" || link.RepositoryFullName() != "acme/widgets" {
		t.Fatalf("display names = %q, %q", connection.FullName(), link.RepositoryFullName())
	}
	now := time.Now()
	if !link.Stale(now, time.Minute) {
		t.Fatal("never-refreshed link is fresh")
	}
	refreshed := now.Add(-2 * time.Minute)
	link.LastRefreshedAt = &refreshed
	if !link.Stale(now, time.Minute) {
		t.Fatal("old link is fresh")
	}
	refreshed = now
	if link.Stale(now, time.Minute) {
		t.Fatal("recent link is stale")
	}
}
