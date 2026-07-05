-- +goose Up
-- +goose StatementBegin
ALTER TABLE auth_credentials
    ADD COLUMN disabled_at TIMESTAMPTZ;

CREATE INDEX auth_credentials_active_user_kind
    ON auth_credentials(user_id, kind)
    WHERE revoked_at IS NULL AND disabled_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS auth_credentials_active_user_kind;

ALTER TABLE auth_credentials
    DROP COLUMN IF EXISTS disabled_at;
-- +goose StatementEnd
