-- +goose Up
-- +goose StatementBegin
CREATE TYPE auth_token_kind AS ENUM ('api', 'session');

ALTER TABLE users
    ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE auth_tokens (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          auth_token_kind NOT NULL,
    name          TEXT NOT NULL,
    token_hash    BYTEA NOT NULL UNIQUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ,
    expires_at    TIMESTAMPTZ,
    revoked_at    TIMESTAMPTZ
);

CREATE INDEX auth_tokens_user_created ON auth_tokens(user_id, created_at, id);
CREATE INDEX auth_tokens_active_hash ON auth_tokens(token_hash)
    WHERE revoked_at IS NULL;

CREATE TABLE project_members (
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX project_members_user_project ON project_members(user_id, project_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS project_members;
DROP TABLE IF EXISTS auth_tokens;
ALTER TABLE users DROP COLUMN IF EXISTS is_admin;
DROP TYPE IF EXISTS auth_token_kind;
-- +goose StatementEnd
