-- +goose Up
-- +goose StatementBegin
CREATE TABLE github_repository_connections (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    repository_id     BIGINT NOT NULL CHECK (repository_id > 0),
    repository_owner  TEXT NOT NULL CHECK (length(repository_owner) BETWEEN 1 AND 100),
    repository_name   TEXT NOT NULL CHECK (length(repository_name) BETWEEN 1 AND 100),
    repository_url    TEXT NOT NULL CHECK (length(repository_url) BETWEEN 1 AND 2048),
    private           BOOLEAN NOT NULL DEFAULT false,
    token_ciphertext  BYTEA NOT NULL CHECK (octet_length(token_ciphertext) > 16),
    token_nonce       BYTEA NOT NULL CHECK (octet_length(token_nonce) = 12),
    created_by_id     UUID NOT NULL REFERENCES users(id),
    last_validated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error        TEXT NOT NULL DEFAULT '' CHECK (length(last_error) <= 500),
    disabled_at       TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, repository_id)
);

CREATE UNIQUE INDEX idx_github_connections_project_name
    ON github_repository_connections(project_id, lower(repository_owner), lower(repository_name))
    WHERE disabled_at IS NULL;
CREATE INDEX idx_github_connections_project_active
    ON github_repository_connections(project_id, created_at)
    WHERE disabled_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE issue_github_links (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    issue_id          UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    connection_id     UUID NOT NULL REFERENCES github_repository_connections(id),
    repository_id     BIGINT NOT NULL CHECK (repository_id > 0),
    repository_owner  TEXT NOT NULL CHECK (length(repository_owner) BETWEEN 1 AND 100),
    repository_name   TEXT NOT NULL CHECK (length(repository_name) BETWEEN 1 AND 100),
    resource_type     TEXT NOT NULL CHECK (resource_type IN ('branch', 'pull_request')),
    branch_name       TEXT CHECK (branch_name IS NULL OR length(branch_name) BETWEEN 1 AND 255),
    pull_request_id   BIGINT CHECK (pull_request_id IS NULL OR pull_request_id > 0),
    pull_request_no   INTEGER CHECK (pull_request_no IS NULL OR pull_request_no > 0),
    title             TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 500),
    html_url          TEXT NOT NULL CHECK (length(html_url) BETWEEN 1 AND 2048),
    state             TEXT NOT NULL CHECK (state IN ('branch', 'draft', 'open', 'merged', 'closed', 'unknown')),
    last_refreshed_at TIMESTAMPTZ,
    last_error        TEXT NOT NULL DEFAULT '' CHECK (length(last_error) <= 500),
    next_refresh_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    refresh_locked_at TIMESTAMPTZ,
    created_by_id     UUID NOT NULL REFERENCES users(id),
    deleted_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (resource_type = 'branch' AND branch_name IS NOT NULL AND pull_request_id IS NULL AND pull_request_no IS NULL) OR
        (resource_type = 'pull_request' AND branch_name IS NULL AND pull_request_id IS NOT NULL AND pull_request_no IS NOT NULL)
    )
);

CREATE UNIQUE INDEX idx_issue_github_links_branch_active
    ON issue_github_links(issue_id, repository_id, branch_name)
    WHERE resource_type = 'branch' AND deleted_at IS NULL;
CREATE UNIQUE INDEX idx_issue_github_links_pr_active
    ON issue_github_links(issue_id, repository_id, pull_request_id)
    WHERE resource_type = 'pull_request' AND deleted_at IS NULL;
CREATE INDEX idx_issue_github_links_issue_active
    ON issue_github_links(issue_id, created_at)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_issue_github_links_refresh_due
    ON issue_github_links(next_refresh_at, created_at)
    WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS issue_github_links;
DROP TABLE IF EXISTS github_repository_connections;
-- +goose StatementEnd
