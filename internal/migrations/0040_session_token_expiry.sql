-- +goose Up
-- +goose StatementBegin
-- Web sessions accumulate indefinitely. They age out here rather than through an
-- external scheduler, so a self-hosted instance needs nothing beyond Postgres.
CREATE TABLE auth_token_sweeps (
    id            BOOLEAN PRIMARY KEY DEFAULT TRUE,
    last_swept_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (id)
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO auth_token_sweeps (id, last_swept_at) VALUES (TRUE, '-infinity');
-- +goose StatementEnd

-- +goose StatementBegin
-- Lets a sweep find its candidates without scanning every token ever issued.
CREATE INDEX auth_tokens_live_sessions_by_age
    ON auth_tokens(kind, created_at)
    WHERE revoked_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION track_sweep_expired_sessions() RETURNS trigger AS $$
BEGIN
    -- One sweep per interval, claimed by updating the single sweep row. Two
    -- concurrent refreshes serialise on that row lock, and under READ COMMITTED
    -- the loser re-evaluates the predicate against the winner's committed value
    -- and matches nothing, so exactly one of them pays for the sweep.
    UPDATE auth_token_sweeps
    SET last_swept_at = now()
    WHERE id
      AND last_swept_at < now() - INTERVAL '1 hour';
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;

    -- Sessions only: API tokens are long-lived by design. The token being
    -- refreshed is spared so an active session is never revoked out from under
    -- the request that is using it; the next sweep will reconsider it.
    -- The limit bounds how long a single refresh can be made to wait.
    UPDATE auth_tokens
    SET revoked_at = now()
    WHERE id IN (
        SELECT id
        FROM auth_tokens
        WHERE kind = 'session'
          AND revoked_at IS NULL
          AND created_at < now() - INTERVAL '3 years'
          AND id <> NEW.id
        LIMIT 1000
    );
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
-- Usage tracking already throttles how often last_used_at is written, so this
-- rides an existing write rather than adding one. The WHEN clause also stops the
-- sweep's own UPDATE from re-entering the trigger, since it never touches
-- last_used_at.
CREATE TRIGGER auth_tokens_sweep_expired_sessions
AFTER UPDATE OF last_used_at ON auth_tokens
FOR EACH ROW
WHEN (NEW.last_used_at IS DISTINCT FROM OLD.last_used_at)
EXECUTE FUNCTION track_sweep_expired_sessions();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS auth_tokens_sweep_expired_sessions ON auth_tokens;
DROP FUNCTION IF EXISTS track_sweep_expired_sessions();
DROP INDEX IF EXISTS auth_tokens_live_sessions_by_age;
DROP TABLE IF EXISTS auth_token_sweeps;
-- +goose StatementEnd
