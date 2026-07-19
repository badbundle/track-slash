package githubintegration

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/bradleymackey/track-slash/internal/testutil"
)

type stubProvider struct {
	repository Repository
	snapshot   Snapshot
	err        error
	tokens     []string
	branches   []string
	pulls      []int
}

func (p *stubProvider) GetRepository(_ context.Context, token, _, _ string) (Repository, error) {
	p.tokens = append(p.tokens, token)
	return p.repository, p.err
}

func (p *stubProvider) GetBranch(_ context.Context, token, _, _, branch string) (Snapshot, error) {
	p.tokens = append(p.tokens, token)
	p.branches = append(p.branches, branch)
	return p.snapshot, p.err
}

func (p *stubProvider) GetPullRequest(_ context.Context, token, _, _ string, number int) (Snapshot, error) {
	p.tokens = append(p.tokens, token)
	p.pulls = append(p.pulls, number)
	return p.snapshot, p.err
}

func TestServiceEncryptsPrivateTokenCreatesRefreshesAndRetries(t *testing.T) {
	db := testutil.NewMigratedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	st := store.New(db.Pool)
	owner, err := st.CreateOrUpdateAdminUser(ctx, "github-service@example.com", "GitHub Service")
	if err != nil {
		t.Fatalf("CreateOrUpdateAdminUser: %v", err)
	}
	project, err := st.CreateProjectForUser(ctx, owner.ID, "GHINT", "GitHub", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser: %v", err)
	}
	issue, err := st.CreateIssue(ctx, store.CreateIssueParams{ProjectID: project.ID, Title: "Link me"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	cryptor, _ := NewCryptor(bytes.Repeat([]byte{9}, 32))
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	prID, prNumber := int64(88), 12
	provider := &stubProvider{
		repository: Repository{ID: 44, Owner: "acme", Name: "private", HTMLURL: "https://github.com/acme/private", Private: true},
		snapshot:   Snapshot{ResourceType: model.GitHubResourcePullRequest, PullRequestID: &prID, PullRequestNumber: &prNumber, Title: "Draft", HTMLURL: "https://github.com/acme/private/pull/12", State: model.GitHubLinkStateDraft},
	}
	service := NewService(st, provider, cryptor, ServiceOptions{Now: func() time.Time { return now }, RefreshInterval: time.Hour, ErrorRetry: 10 * time.Minute})
	if _, err := service.ConnectRepository(ctx, ConnectRepositoryParams{ProjectID: project.ID, Repository: "acme/private", CreatedByID: owner.ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing token error = %v", err)
	}
	connection, err := service.ConnectRepository(store.WithActor(ctx, owner.ID), ConnectRepositoryParams{ProjectID: project.ID, Repository: "acme/private", Token: "private-token", CreatedByID: owner.ID})
	if err != nil || !connection.Private {
		t.Fatalf("ConnectRepository = %+v, %v", connection, err)
	}
	var ciphertext []byte
	if err := db.Pool.QueryRow(ctx, `SELECT token_ciphertext FROM github_repository_connections WHERE id = $1`, connection.ID).Scan(&ciphertext); err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}
	if bytes.Contains(ciphertext, []byte("private-token")) {
		t.Fatal("token stored in plaintext")
	}
	otherProject, err := st.CreateProjectForUser(ctx, owner.ID, "GHOTHER", "Other", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser other: %v", err)
	}
	otherIssue, err := st.CreateIssue(ctx, store.CreateIssueParams{ProjectID: otherProject.ID, Title: "Other"})
	if err != nil {
		t.Fatalf("CreateIssue other: %v", err)
	}
	providerCalls := len(provider.tokens)
	if _, err := service.CreateLink(ctx, CreateLinkParams{IssueID: otherIssue.ID, ConnectionID: connection.ID, Reference: "#12", CreatedByID: owner.ID}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("cross-project CreateLink error = %v", err)
	}
	if len(provider.tokens) != providerCalls {
		t.Fatal("cross-project CreateLink called GitHub")
	}
	link, err := service.CreateLink(store.WithActor(ctx, owner.ID), CreateLinkParams{IssueID: issue.ID, ConnectionID: connection.ID, Reference: "#12", CreatedByID: owner.ID})
	if err != nil || link.State != model.GitHubLinkStateDraft || len(provider.pulls) != 1 || provider.tokens[len(provider.tokens)-1] != "private-token" {
		t.Fatalf("CreateLink = %+v, %v, provider=%+v", link, err, provider)
	}

	provider.snapshot.Title = "Merged"
	provider.snapshot.State = model.GitHubLinkStateMerged
	refreshed, err := service.RefreshLink(ctx, link.ID)
	if err != nil || refreshed.State != model.GitHubLinkStateMerged || refreshed.LastError != "" {
		t.Fatalf("RefreshLink = %+v, %v", refreshed, err)
	}
	provider.err = errors.New("network down")
	failed, err := service.RefreshLink(ctx, link.ID)
	if err == nil || failed.LastError != "GitHub refresh failed; showing the last known state" {
		t.Fatalf("generic failed refresh = %+v, %v", failed, err)
	}
	provider.err = &RateLimitError{RetryAt: now.Add(20 * time.Minute)}
	failed, err = service.RefreshLink(ctx, link.ID)
	if !errors.Is(err, ErrRateLimited) || failed.LastError == "" || !failed.NextRefreshAt.Equal(now.Add(20*time.Minute)) {
		t.Fatalf("rate-limited refresh = %+v, %v", failed, err)
	}
	provider.err = nil
	wrongID := int64(999)
	provider.snapshot.PullRequestID = &wrongID
	failed, err = service.RefreshLink(ctx, link.ID)
	if !errors.Is(err, ErrUnavailable) || failed.LastError == "" {
		t.Fatalf("identity-changing refresh = %+v, %v", failed, err)
	}

	provider.snapshot.PullRequestID = &prID
	provider.snapshot.Title = "Worker refreshed"
	if _, err := db.Pool.Exec(ctx, `UPDATE issue_github_links SET next_refresh_at = now() - interval '1 minute', refresh_locked_at = NULL WHERE id = $1`, link.ID); err != nil {
		t.Fatalf("make refresh due: %v", err)
	}
	worker := NewWorker(st, service, WorkerOptions{BatchSize: 1, Lease: time.Minute})
	worker.process(ctx)
	workerUpdated, err := st.GetGitHubIssueLink(ctx, link.ID)
	if err != nil || workerUpdated.Title != "Worker refreshed" || workerUpdated.LastError != "" {
		t.Fatalf("worker refresh = %+v, %v", workerUpdated, err)
	}
}

func TestServiceMarksTamperedAndDisconnectedCredentialsUnavailable(t *testing.T) {
	db := testutil.NewMigratedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	st := store.New(db.Pool)
	owner, _ := st.CreateOrUpdateAdminUser(ctx, "github-tamper@example.com", "GitHub Tamper")
	project, _ := st.CreateProjectForUser(ctx, owner.ID, "GHTAMP", "GitHub", "")
	issue, _ := st.CreateIssue(ctx, store.CreateIssueParams{ProjectID: project.ID, Title: "Link"})
	cryptor, _ := NewCryptor(bytes.Repeat([]byte{5}, 32))
	branch := "main"
	provider := &stubProvider{
		repository: Repository{ID: 51, Owner: "acme", Name: "repo", HTMLURL: "https://github.com/acme/repo"},
		snapshot:   Snapshot{ResourceType: model.GitHubResourceBranch, BranchName: &branch, Title: branch, HTMLURL: "https://github.com/acme/repo/tree/main", State: model.GitHubLinkStateBranch},
	}
	service := NewService(st, provider, cryptor, ServiceOptions{})
	connection, _ := service.ConnectRepository(ctx, ConnectRepositoryParams{ProjectID: project.ID, Repository: "acme/repo", Token: "token", CreatedByID: owner.ID})
	providerCalls := len(provider.tokens)
	if _, err := service.CreateLink(ctx, CreateLinkParams{IssueID: issue.ID, ConnectionID: connection.ID, Reference: "bad branch", CreatedByID: owner.ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid reference error = %v", err)
	}
	if len(provider.tokens) != providerCalls {
		t.Fatal("invalid reference called GitHub")
	}
	link, _ := service.CreateLink(ctx, CreateLinkParams{IssueID: issue.ID, ConnectionID: connection.ID, Reference: "main", CreatedByID: owner.ID})
	if _, err := db.Pool.Exec(ctx, `UPDATE github_repository_connections SET token_ciphertext = decode(repeat('ff', 17), 'hex') WHERE id = $1`, connection.ID); err != nil {
		t.Fatalf("tamper token: %v", err)
	}
	failed, err := service.RefreshLink(ctx, link.ID)
	if !errors.Is(err, ErrUnauthorized) || failed.LastError == "" {
		t.Fatalf("tampered refresh = %+v, %v", failed, err)
	}
	if err := st.DisconnectGitHubConnection(ctx, project.ID, connection.ID); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	failed, err = service.RefreshLink(ctx, link.ID)
	if !errors.Is(err, ErrUnavailable) || failed.LastError == "" {
		t.Fatalf("disconnected refresh = %+v, %v", failed, err)
	}
	if _, err := service.fetch(ctx, "token", connection, Reference{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid fetch error = %v", err)
	}
	if _, err := service.RefreshLink(ctx, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing link refresh error = %v", err)
	}
	cancelled, cancelWorker := context.WithCancel(ctx)
	cancelWorker()
	done := make(chan struct{})
	go func() {
		NewWorker(st, service, WorkerOptions{}).Run(cancelled)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop on cancellation")
	}
}
