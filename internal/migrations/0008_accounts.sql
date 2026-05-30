-- +goose Up
-- +goose StatementBegin
CREATE TYPE auth_credential_kind AS ENUM ('password', 'passkey');

ALTER TABLE users
    ADD COLUMN username TEXT;

WITH normalized AS (
    SELECT
        id,
        lower(regexp_replace(split_part(email, '@', 1), '[^a-z0-9_-]', '', 'g')) AS base
    FROM users
),
prepared AS (
    SELECT
        id,
        CASE
            WHEN base ~ '^[a-z0-9]' AND length(base) >= 3 THEN left(base, 32)
            ELSE 'user_' || left(replace(id::text, '-', ''), 27)
        END AS base
    FROM normalized
),
ranked AS (
    SELECT
        id,
        base,
        row_number() OVER (PARTITION BY base ORDER BY id) AS n
    FROM prepared
)
UPDATE users u
SET username = CASE
    WHEN ranked.n = 1 THEN ranked.base
    ELSE left(ranked.base, greatest(1, 32 - length('_' || ranked.n::text))) || '_' || ranked.n::text
END
FROM ranked
WHERE u.id = ranked.id;

ALTER TABLE users
    ALTER COLUMN username SET NOT NULL,
    ALTER COLUMN email DROP NOT NULL;

ALTER TABLE users
    ADD CONSTRAINT users_username_key UNIQUE (username);

CREATE TABLE auth_credentials (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          auth_credential_kind NOT NULL,
    identifier    TEXT NOT NULL,
    secret_hash   TEXT,
    public_key    BYTEA,
    sign_count    BIGINT,
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ,
    revoked_at    TIMESTAMPTZ
);

CREATE UNIQUE INDEX auth_credentials_active_kind_identifier
    ON auth_credentials(kind, identifier)
    WHERE revoked_at IS NULL;

CREATE INDEX auth_credentials_user_created ON auth_credentials(user_id, created_at, id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS auth_credentials;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_username_key;
ALTER TABLE users DROP COLUMN IF EXISTS username;
ALTER TABLE users ALTER COLUMN email SET NOT NULL;
DROP TYPE IF EXISTS auth_credential_kind;
-- +goose StatementEnd
