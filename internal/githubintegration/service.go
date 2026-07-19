package githubintegration

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

type Provider interface {
	GetRepository(context.Context, string, string, string) (Repository, error)
	GetBranch(context.Context, string, string, string, string) (Snapshot, error)
	GetPullRequest(context.Context, string, string, string, int) (Snapshot, error)
}

type ServiceOptions struct {
	RefreshInterval time.Duration
	ErrorRetry      time.Duration
	Now             func() time.Time
}

type Service struct {
	store           *store.Store
	provider        Provider
	cryptor         *Cryptor
	refreshInterval time.Duration
	errorRetry      time.Duration
	now             func() time.Time
}

func NewService(s *store.Store, provider Provider, cryptor *Cryptor, opts ServiceOptions) *Service {
	if opts.RefreshInterval <= 0 {
		opts.RefreshInterval = 15 * time.Minute
	}
	if opts.ErrorRetry <= 0 {
		opts.ErrorRetry = 5 * time.Minute
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{store: s, provider: provider, cryptor: cryptor, refreshInterval: opts.RefreshInterval, errorRetry: opts.ErrorRetry, now: opts.Now}
}

func credentialAAD(projectID uuid.UUID, repositoryID int64) []byte {
	return []byte(projectID.String() + "\x00" + strconv.FormatInt(repositoryID, 10))
}

type ConnectRepositoryParams struct {
	ProjectID   uuid.UUID
	Repository  string
	Token       string
	CreatedByID uuid.UUID
}

func (s *Service) ConnectRepository(ctx context.Context, p ConnectRepositoryParams) (model.GitHubConnection, error) {
	owner, name, err := ParseRepository(p.Repository)
	if err != nil {
		return model.GitHubConnection{}, err
	}
	if p.Token == "" {
		return model.GitHubConnection{}, fmt.Errorf("GitHub token is required: %w", ErrInvalid)
	}
	repository, err := s.provider.GetRepository(ctx, p.Token, owner, name)
	if err != nil {
		return model.GitHubConnection{}, err
	}
	ciphertext, nonce, err := s.cryptor.Encrypt([]byte(p.Token), credentialAAD(p.ProjectID, repository.ID))
	if err != nil {
		return model.GitHubConnection{}, err
	}
	return s.store.UpsertGitHubConnection(ctx, store.UpsertGitHubConnectionParams{
		ProjectID: p.ProjectID, RepositoryID: repository.ID, RepositoryOwner: repository.Owner,
		RepositoryName: repository.Name, RepositoryURL: repository.HTMLURL, Private: repository.Private,
		TokenCiphertext: ciphertext, TokenNonce: nonce, CreatedByID: p.CreatedByID,
	})
}

type CreateLinkParams struct {
	IssueID      uuid.UUID
	ConnectionID uuid.UUID
	Reference    string
	CreatedByID  uuid.UUID
}

func (s *Service) CreateLink(ctx context.Context, p CreateLinkParams) (model.GitHubIssueLink, error) {
	secret, err := s.store.GetGitHubConnectionSecret(ctx, p.ConnectionID)
	if err != nil {
		return model.GitHubIssueLink{}, err
	}
	issue, err := s.store.GetIssue(ctx, p.IssueID)
	if err != nil {
		return model.GitHubIssueLink{}, err
	}
	if issue.ProjectID != secret.Connection.ProjectID {
		return model.GitHubIssueLink{}, fmt.Errorf("repository connection belongs to another project: %w", store.ErrConflict)
	}
	reference, err := ParseReference(p.Reference, secret.Connection)
	if err != nil {
		return model.GitHubIssueLink{}, err
	}
	token, err := s.decrypt(secret)
	if err != nil {
		return model.GitHubIssueLink{}, err
	}
	snapshot, err := s.fetch(ctx, string(token), secret.Connection, reference)
	if err != nil {
		return model.GitHubIssueLink{}, err
	}
	now := s.now()
	return s.store.CreateGitHubIssueLink(ctx, store.CreateGitHubIssueLinkParams{
		IssueID: p.IssueID, ConnectionID: secret.Connection.ID, RepositoryID: secret.Connection.RepositoryID,
		RepositoryOwner: secret.Connection.RepositoryOwner, RepositoryName: secret.Connection.RepositoryName,
		ResourceType: snapshot.ResourceType, BranchName: snapshot.BranchName, PullRequestID: snapshot.PullRequestID,
		PullRequestNumber: snapshot.PullRequestNumber, Title: snapshot.Title, HTMLURL: snapshot.HTMLURL,
		State: snapshot.State, RefreshedAt: now, NextRefreshAt: now.Add(s.refreshInterval), CreatedByID: p.CreatedByID,
	})
}

func (s *Service) RefreshLink(ctx context.Context, id uuid.UUID) (model.GitHubIssueLink, error) {
	link, err := s.store.GetGitHubIssueLink(ctx, id)
	if err != nil {
		return model.GitHubIssueLink{}, err
	}
	secret, err := s.store.GetGitHubConnectionSecret(ctx, link.ConnectionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return s.fail(ctx, link.ID, ErrUnavailable, time.Time{})
		}
		return link, err
	}
	token, err := s.decrypt(secret)
	if err != nil {
		return s.fail(ctx, link.ID, ErrUnauthorized, time.Time{})
	}
	reference := Reference{ResourceType: link.ResourceType}
	if link.BranchName != nil {
		reference.BranchName = *link.BranchName
	}
	if link.PullRequestNumber != nil {
		reference.PullRequestNumber = *link.PullRequestNumber
	}
	snapshot, err := s.fetch(ctx, string(token), secret.Connection, reference)
	if err != nil {
		retryAt := time.Time{}
		var rateLimit *RateLimitError
		if errors.As(err, &rateLimit) {
			retryAt = rateLimit.RetryAt
		}
		return s.fail(ctx, link.ID, err, retryAt)
	}
	if link.PullRequestID != nil && snapshot.PullRequestID != nil && *link.PullRequestID != *snapshot.PullRequestID {
		return s.fail(ctx, link.ID, ErrUnavailable, time.Time{})
	}
	updated, updateErr := s.store.CompleteGitHubIssueLinkRefresh(ctx, link.ID, store.UpdateGitHubIssueLinkSnapshotParams{
		Title: snapshot.Title, HTMLURL: snapshot.HTMLURL, State: snapshot.State,
		NextRefreshAt: s.now().Add(s.refreshInterval),
	})
	if updateErr != nil {
		return link, updateErr
	}
	return updated, nil
}

func (s *Service) decrypt(secret store.GitHubConnectionSecret) ([]byte, error) {
	return s.cryptor.Decrypt(secret.Ciphertext, secret.Nonce, credentialAAD(secret.Connection.ProjectID, secret.Connection.RepositoryID))
}

func (s *Service) fetch(ctx context.Context, token string, connection model.GitHubConnection, reference Reference) (Snapshot, error) {
	switch reference.ResourceType {
	case model.GitHubResourceBranch:
		return s.provider.GetBranch(ctx, token, connection.RepositoryOwner, connection.RepositoryName, reference.BranchName)
	case model.GitHubResourcePullRequest:
		return s.provider.GetPullRequest(ctx, token, connection.RepositoryOwner, connection.RepositoryName, reference.PullRequestNumber)
	default:
		return Snapshot{}, ErrInvalid
	}
}

func (s *Service) fail(ctx context.Context, id uuid.UUID, cause error, retryAt time.Time) (model.GitHubIssueLink, error) {
	if retryAt.IsZero() || retryAt.Before(s.now()) {
		retryAt = s.now().Add(s.errorRetry)
	}
	message := providerErrorMessage(cause)
	updated, err := s.store.FailGitHubIssueLinkRefresh(ctx, id, message, retryAt)
	if err != nil {
		return model.GitHubIssueLink{}, err
	}
	return updated, cause
}

func providerErrorMessage(err error) string {
	switch {
	case errors.Is(err, ErrRateLimited):
		return "GitHub rate limit reached; showing the last known state"
	case errors.Is(err, ErrUnauthorized):
		return "GitHub credentials no longer allow access; showing the last known state"
	case errors.Is(err, ErrUnavailable):
		return "GitHub resource is unavailable; showing the last known state"
	default:
		return "GitHub refresh failed; showing the last known state"
	}
}
