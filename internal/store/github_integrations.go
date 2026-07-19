package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bradleymackey/track-slash/internal/model"
)

type UpsertGitHubConnectionParams struct {
	ProjectID       uuid.UUID
	RepositoryID    int64
	RepositoryOwner string
	RepositoryName  string
	RepositoryURL   string
	Private         bool
	TokenCiphertext []byte
	TokenNonce      []byte
	CreatedByID     uuid.UUID
}

type GitHubConnectionSecret struct {
	Connection model.GitHubConnection
	Ciphertext []byte `json:"-"`
	Nonce      []byte `json:"-"`
}

type githubConnectionScanner interface {
	Scan(dest ...any) error
}

func scanGitHubConnection(row githubConnectionScanner) (model.GitHubConnection, error) {
	var out model.GitHubConnection
	err := row.Scan(
		&out.ID, &out.ProjectID, &out.RepositoryID, &out.RepositoryOwner, &out.RepositoryName,
		&out.RepositoryURL, &out.Private, &out.CreatedByID, &out.LastValidatedAt, &out.LastError,
		&out.DisabledAt, &out.CreatedAt, &out.UpdatedAt,
	)
	return out, err
}

const githubConnectionColumns = `
	id, project_id, repository_id, repository_owner, repository_name, repository_url,
	private, created_by_id, last_validated_at, last_error, disabled_at, created_at, updated_at
`

func (s *Store) UpsertGitHubConnection(ctx context.Context, p UpsertGitHubConnectionParams) (model.GitHubConnection, error) {
	var out model.GitHubConnection
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var projectExists bool
		if err := tx.QueryRow(ctx, `SELECT true FROM projects WHERE id = $1 AND deleted_at IS NULL`, p.ProjectID).Scan(&projectExists); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err // defensive: DB outage past the no-rows branch
		}
		var err error
		out, err = scanGitHubConnection(tx.QueryRow(ctx, `
			INSERT INTO github_repository_connections (
				project_id, repository_id, repository_owner, repository_name, repository_url,
				private, token_ciphertext, token_nonce, created_by_id
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (project_id, repository_id) DO UPDATE SET
				repository_owner = EXCLUDED.repository_owner,
				repository_name = EXCLUDED.repository_name,
				repository_url = EXCLUDED.repository_url,
				private = EXCLUDED.private,
				token_ciphertext = EXCLUDED.token_ciphertext,
				token_nonce = EXCLUDED.token_nonce,
				created_by_id = EXCLUDED.created_by_id,
				last_validated_at = now(),
				last_error = '',
				disabled_at = NULL,
				updated_at = now()
			RETURNING `+githubConnectionColumns,
			p.ProjectID, p.RepositoryID, p.RepositoryOwner, p.RepositoryName, p.RepositoryURL,
			p.Private, p.TokenCiphertext, p.TokenNonce, p.CreatedByID,
		))
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return fmt.Errorf("repository connection already exists: %w", ErrConflict)
			}
			return err
		}
		return appendProjectChangelog(ctx, tx, appendProjectChangelogParams{
			ProjectID:   out.ProjectID,
			Entity:      "github_connection",
			Op:          "insert",
			EntityID:    out.ID,
			TargetRef:   out.FullName(),
			TargetTitle: out.FullName(),
			Summary:     "Connected GitHub repository " + out.FullName(),
		})
	})
	if err != nil {
		return model.GitHubConnection{}, err
	}
	return out, nil
}

func (s *Store) ListGitHubConnections(ctx context.Context, projectID uuid.UUID) ([]model.GitHubConnection, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT `+githubConnectionColumns+`
		FROM github_repository_connections
		WHERE project_id = $1 AND disabled_at IS NULL
		ORDER BY lower(repository_owner), lower(repository_name), id
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.GitHubConnection, 0)
	for rows.Next() {
		connection, err := scanGitHubConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, connection)
	}
	return out, rows.Err()
}

func (s *Store) GetGitHubConnection(ctx context.Context, id uuid.UUID) (model.GitHubConnection, error) {
	out, err := scanGitHubConnection(s.db.QueryRow(ctx, `
		SELECT `+githubConnectionColumns+`
		FROM github_repository_connections
		WHERE id = $1 AND disabled_at IS NULL
	`, id))
	if isNoRows(err) {
		return model.GitHubConnection{}, ErrNotFound
	}
	return out, err
}

func (s *Store) GetGitHubConnectionSecret(ctx context.Context, id uuid.UUID) (GitHubConnectionSecret, error) {
	var out GitHubConnectionSecret
	err := s.db.QueryRow(ctx, `
		SELECT `+githubConnectionColumns+`, token_ciphertext, token_nonce
		FROM github_repository_connections
		WHERE id = $1 AND disabled_at IS NULL
	`, id).Scan(
		&out.Connection.ID, &out.Connection.ProjectID, &out.Connection.RepositoryID,
		&out.Connection.RepositoryOwner, &out.Connection.RepositoryName, &out.Connection.RepositoryURL,
		&out.Connection.Private, &out.Connection.CreatedByID, &out.Connection.LastValidatedAt,
		&out.Connection.LastError, &out.Connection.DisabledAt, &out.Connection.CreatedAt,
		&out.Connection.UpdatedAt, &out.Ciphertext, &out.Nonce,
	)
	if isNoRows(err) {
		return GitHubConnectionSecret{}, ErrNotFound
	}
	return out, err
}

func (s *Store) DisconnectGitHubConnection(ctx context.Context, projectID, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		connection, err := scanGitHubConnection(tx.QueryRow(ctx, `
			UPDATE github_repository_connections
			SET disabled_at = now(), token_ciphertext = decode(repeat('00', 17), 'hex'),
			    token_nonce = decode(repeat('00', 12), 'hex'), updated_at = now()
			WHERE id = $1 AND project_id = $2 AND disabled_at IS NULL
			RETURNING `+githubConnectionColumns,
			id, projectID,
		))
		if isNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE issue_github_links
			SET last_error = 'Repository connection is unavailable; showing the last known state',
			    refresh_locked_at = NULL, updated_at = now()
			WHERE connection_id = $1 AND deleted_at IS NULL
		`, connection.ID); err != nil {
			return err
		}
		return appendProjectChangelog(ctx, tx, appendProjectChangelogParams{
			ProjectID:   projectID,
			Entity:      "github_connection",
			Op:          "delete",
			EntityID:    connection.ID,
			TargetRef:   connection.FullName(),
			TargetTitle: connection.FullName(),
			Summary:     "Disconnected GitHub repository " + connection.FullName(),
		})
	})
}

type CreateGitHubIssueLinkParams struct {
	IssueID           uuid.UUID
	ConnectionID      uuid.UUID
	RepositoryID      int64
	RepositoryOwner   string
	RepositoryName    string
	ResourceType      model.GitHubResourceType
	BranchName        *string
	PullRequestID     *int64
	PullRequestNumber *int
	Title             string
	HTMLURL           string
	State             model.GitHubLinkState
	RefreshedAt       time.Time
	NextRefreshAt     time.Time
	CreatedByID       uuid.UUID
}

type githubIssueLinkScanner interface {
	Scan(dest ...any) error
}

const githubIssueLinkColumns = `
	id, project_id, issue_id, connection_id, repository_id, repository_owner, repository_name,
	resource_type, branch_name, pull_request_id, pull_request_no, title, html_url, state,
	last_refreshed_at, last_error, next_refresh_at, created_by_id, deleted_at, created_at, updated_at
`

const githubIssueLinkColumnsQualified = `
	l.id, l.project_id, l.issue_id, l.connection_id, l.repository_id, l.repository_owner, l.repository_name,
	l.resource_type, l.branch_name, l.pull_request_id, l.pull_request_no, l.title, l.html_url, l.state,
	l.last_refreshed_at, l.last_error, l.next_refresh_at, l.created_by_id, l.deleted_at, l.created_at, l.updated_at
`

func scanGitHubIssueLink(row githubIssueLinkScanner) (model.GitHubIssueLink, error) {
	var out model.GitHubIssueLink
	err := row.Scan(
		&out.ID, &out.ProjectID, &out.IssueID, &out.ConnectionID, &out.RepositoryID,
		&out.RepositoryOwner, &out.RepositoryName, &out.ResourceType, &out.BranchName,
		&out.PullRequestID, &out.PullRequestNumber, &out.Title, &out.HTMLURL, &out.State,
		&out.LastRefreshedAt, &out.LastError, &out.NextRefreshAt, &out.CreatedByID,
		&out.DeletedAt, &out.CreatedAt, &out.UpdatedAt,
	)
	return out, err
}

func (s *Store) CreateGitHubIssueLink(ctx context.Context, p CreateGitHubIssueLinkParams) (model.GitHubIssueLink, error) {
	var out model.GitHubIssueLink
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		issue, err := getIssueForChangelog(ctx, tx, p.IssueID, false)
		if err != nil {
			return err
		}
		var connectionProject uuid.UUID
		err = tx.QueryRow(ctx, `
			SELECT project_id FROM github_repository_connections
			WHERE id = $1 AND disabled_at IS NULL
		`, p.ConnectionID).Scan(&connectionProject)
		if isNoRows(err) {
			return fmt.Errorf("repository connection is unavailable: %w", ErrConflict)
		}
		if err != nil {
			return err
		}
		if connectionProject != issue.ProjectID {
			return fmt.Errorf("repository connection belongs to another project: %w", ErrConflict)
		}
		out, err = scanGitHubIssueLink(tx.QueryRow(ctx, `
			INSERT INTO issue_github_links (
				project_id, issue_id, connection_id, repository_id, repository_owner, repository_name,
				resource_type, branch_name, pull_request_id, pull_request_no, title, html_url, state,
				last_refreshed_at, next_refresh_at, created_by_id
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
			RETURNING `+githubIssueLinkColumns,
			issue.ProjectID, p.IssueID, p.ConnectionID, p.RepositoryID, p.RepositoryOwner,
			p.RepositoryName, p.ResourceType, p.BranchName, p.PullRequestID, p.PullRequestNumber,
			p.Title, p.HTMLURL, p.State, p.RefreshedAt, p.NextRefreshAt, p.CreatedByID,
		))
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				switch pgErr.Code {
				case "23505":
					return fmt.Errorf("GitHub link already exists: %w", ErrConflict)
				case "23503", "23514":
					return fmt.Errorf("invalid GitHub link: %w", ErrConflict)
				}
			}
			return err
		}
		return appendProjectChangelog(ctx, tx, appendProjectChangelogParams{
			ProjectID:   issue.ProjectID,
			Entity:      "github_issue_link",
			Op:          "insert",
			EntityID:    out.ID,
			IssueID:     &issue.ID,
			TargetRef:   issue.Identifier,
			TargetTitle: issue.Title,
			Summary:     fmt.Sprintf("Linked %s to %s", issue.Identifier, out.Title),
		})
	})
	if err != nil {
		return model.GitHubIssueLink{}, err
	}
	return out, nil
}

func (s *Store) ListGitHubIssueLinks(ctx context.Context, issueID uuid.UUID) ([]model.GitHubIssueLink, error) {
	if _, err := s.GetIssue(ctx, issueID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT `+githubIssueLinkColumns+`
		FROM issue_github_links
		WHERE issue_id = $1 AND deleted_at IS NULL
		ORDER BY created_at, id
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.GitHubIssueLink, 0)
	for rows.Next() {
		link, err := scanGitHubIssueLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, rows.Err()
}

func (s *Store) GetGitHubIssueLink(ctx context.Context, id uuid.UUID) (model.GitHubIssueLink, error) {
	out, err := scanGitHubIssueLink(s.db.QueryRow(ctx, `
		SELECT `+githubIssueLinkColumns+`
		FROM issue_github_links
		WHERE id = $1 AND deleted_at IS NULL
	`, id))
	if isNoRows(err) {
		return model.GitHubIssueLink{}, ErrNotFound
	}
	return out, err
}

func (s *Store) DeleteGitHubIssueLink(ctx context.Context, issueID, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		link, err := scanGitHubIssueLink(tx.QueryRow(ctx, `
			UPDATE issue_github_links
			SET deleted_at = now(), refresh_locked_at = NULL, updated_at = now()
			WHERE id = $1 AND issue_id = $2 AND deleted_at IS NULL
			RETURNING `+githubIssueLinkColumns,
			id, issueID,
		))
		if isNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		issue, err := getIssueForChangelog(ctx, tx, issueID, false)
		if err != nil {
			return err
		}
		return appendProjectChangelog(ctx, tx, appendProjectChangelogParams{
			ProjectID:   issue.ProjectID,
			Entity:      "github_issue_link",
			Op:          "delete",
			EntityID:    link.ID,
			IssueID:     &issue.ID,
			TargetRef:   issue.Identifier,
			TargetTitle: issue.Title,
			Summary:     fmt.Sprintf("Unlinked %s from %s", issue.Identifier, link.Title),
		})
	})
}

func (s *Store) ClaimGitHubIssueLinks(ctx context.Context, limit int, lease time.Duration) ([]model.GitHubIssueLink, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `
		WITH claimed AS (
			SELECT l.id
			FROM issue_github_links l
			JOIN github_repository_connections c ON c.id = l.connection_id
			WHERE l.deleted_at IS NULL AND c.disabled_at IS NULL
			  AND l.next_refresh_at <= now()
			  AND (l.refresh_locked_at IS NULL OR l.refresh_locked_at < now() - $2::interval)
			ORDER BY l.next_refresh_at, l.created_at
			FOR UPDATE OF l SKIP LOCKED
			LIMIT $1
		)
		UPDATE issue_github_links l
		SET refresh_locked_at = now(), updated_at = now()
		FROM claimed
		WHERE l.id = claimed.id
		RETURNING `+githubIssueLinkColumnsQualified,
		limit, lease.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.GitHubIssueLink, 0, limit)
	for rows.Next() {
		link, err := scanGitHubIssueLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, rows.Err()
}

type UpdateGitHubIssueLinkSnapshotParams struct {
	Title         string
	HTMLURL       string
	State         model.GitHubLinkState
	NextRefreshAt time.Time
}

func (s *Store) CompleteGitHubIssueLinkRefresh(ctx context.Context, id uuid.UUID, p UpdateGitHubIssueLinkSnapshotParams) (model.GitHubIssueLink, error) {
	out, err := scanGitHubIssueLink(s.db.QueryRow(ctx, `
		UPDATE issue_github_links
		SET title = $2, html_url = $3, state = $4, last_refreshed_at = now(),
		    last_error = '', next_refresh_at = $5, refresh_locked_at = NULL, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING `+githubIssueLinkColumns,
		id, p.Title, p.HTMLURL, p.State, p.NextRefreshAt,
	))
	if isNoRows(err) {
		return model.GitHubIssueLink{}, ErrNotFound
	}
	return out, err
}

func (s *Store) FailGitHubIssueLinkRefresh(ctx context.Context, id uuid.UUID, message string, nextRefreshAt time.Time) (model.GitHubIssueLink, error) {
	if len(message) > 500 {
		message = message[:500]
	}
	out, err := scanGitHubIssueLink(s.db.QueryRow(ctx, `
		UPDATE issue_github_links
		SET last_error = $2, next_refresh_at = $3, refresh_locked_at = NULL, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING `+githubIssueLinkColumns,
		id, message, nextRefreshAt,
	))
	if isNoRows(err) {
		return model.GitHubIssueLink{}, ErrNotFound
	}
	return out, err
}
