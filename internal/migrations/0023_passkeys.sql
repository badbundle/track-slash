-- +goose Up
-- +goose StatementBegin
CREATE TABLE webauthn_user_handles (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rp_id       TEXT NOT NULL,
    handle      BYTEA NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (rp_id, user_id),
    UNIQUE (rp_id, handle)
);

CREATE TABLE webauthn_sessions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind             TEXT NOT NULL CHECK (kind IN ('signup', 'login', 'add', 'reauth')),
    rp_id            TEXT NOT NULL,
    user_id          UUID REFERENCES users(id) ON DELETE CASCADE,
    username         TEXT NOT NULL DEFAULT '',
    display_name     TEXT NOT NULL DEFAULT '',
    credential_name  TEXT NOT NULL DEFAULT '',
    user_handle      BYTEA,
    challenge        TEXT NOT NULL UNIQUE,
    session_data     JSONB NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at       TIMESTAMPTZ NOT NULL,
    consumed_at      TIMESTAMPTZ
);

CREATE INDEX webauthn_sessions_active
    ON webauthn_sessions(kind, expires_at)
    WHERE consumed_at IS NULL;

CREATE INDEX webauthn_sessions_user
    ON webauthn_sessions(user_id, created_at DESC)
    WHERE user_id IS NOT NULL;

CREATE TABLE passkey_reauth_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   BYTEA NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    consumed_at  TIMESTAMPTZ
);

CREATE INDEX passkey_reauth_tokens_active
    ON passkey_reauth_tokens(user_id, expires_at)
    WHERE consumed_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS passkey_reauth_tokens;
DROP TABLE IF EXISTS webauthn_sessions;
DROP TABLE IF EXISTS webauthn_user_handles;
-- +goose StatementEnd
