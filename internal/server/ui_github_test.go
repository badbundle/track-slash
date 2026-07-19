package server

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/githubintegration"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestGitHubIssuePanelRendersStatesFreshnessAndControls(t *testing.T) {
	now := time.Now()
	prNumber := 12
	prID := int64(44)
	branch := "feature/ship"
	panel := &uiIssuePanelData{
		Issue:    model.Issue{ID: uuid.New(), OwnerUsername: "owner", ProjectKey: "TRACK", Identifier: "TRACK-29"},
		Project:  model.Project{ID: uuid.New(), OwnerUsername: "owner", Key: "TRACK"},
		CanWrite: true, GitHubConfigured: true,
		GitHubConnections: []model.GitHubConnection{{ID: uuid.New(), RepositoryOwner: "acme", RepositoryName: "private", Private: true}},
		GitHubLinks: []uiGitHubIssueLink{
			{Link: model.GitHubIssueLink{ID: uuid.New(), ResourceType: model.GitHubResourcePullRequest, RepositoryOwner: "acme", RepositoryName: "private", PullRequestID: &prID, PullRequestNumber: &prNumber, Title: "Closed PR", HTMLURL: "https://github.com/acme/private/pull/12", State: model.GitHubLinkStateClosed, LastRefreshedAt: &now, LastError: "GitHub resource is unavailable; showing the last known state"}, LastRefreshedLabel: "today", Stale: true},
			{Link: model.GitHubIssueLink{ID: uuid.New(), ResourceType: model.GitHubResourceBranch, RepositoryOwner: "acme", RepositoryName: "private", BranchName: &branch, Title: branch, HTMLURL: "https://github.com/acme/private/tree/feature/ship", State: model.GitHubLinkStateBranch, LastRefreshedAt: &now}, LastRefreshedLabel: "today"},
		},
	}
	var out bytes.Buffer
	if err := uiTemplates.ExecuteTemplate(&out, "issue-panel", panel); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := out.String()
	for _, want := range []string{"Closed, not merged", "stale", "unavailable", "feature/ship", "Refresh GitHub link", "Remove GitHub link"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestUIGitHubActionMessages(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{errors.Join(errors.New("bad repository"), githubintegration.ErrInvalid), "bad repository"},
		{githubintegration.ErrUnauthorized, "does not allow access"},
		{githubintegration.ErrUnavailable, "could not find"},
		{githubintegration.ErrRateLimited, "rate limit"},
		{store.ErrConflict, "already linked"},
		{store.ErrNotFound, "no longer available"},
		{errors.New("network"), "could not be reached"},
	}
	for _, test := range tests {
		if got := uiGitHubActionMessage(test.err); !strings.Contains(got, test.want) {
			t.Errorf("uiGitHubActionMessage(%v) = %q", test.err, got)
		}
	}
}

func TestGitHubProjectPanelRendersPrivateRepositoryAndProtectedTokenField(t *testing.T) {
	panel := &uiProjectPanelData{
		Project: model.Project{ID: uuid.New(), OwnerUsername: "owner", Key: "TRACK", Name: "Track"},
		View:    "about", CanManageMembers: true, GitHubConfigured: true,
		GitHubConnections: []model.GitHubConnection{{ID: uuid.New(), RepositoryOwner: "acme", RepositoryName: "private", RepositoryURL: "https://github.com/acme/private", Private: true, LastValidatedAt: time.Now()}},
	}
	var out bytes.Buffer
	if err := uiTemplates.ExecuteTemplate(&out, "project-panel", panel); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := out.String()
	for _, want := range []string{"GitHub repositories", "acme/private", "Private", `type="password"`, `autocomplete="off"`, "Disconnect"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, "private-token") {
		t.Fatal("rendered a token")
	}
}
