-- +goose Up
-- +goose StatementBegin
CREATE TABLE user_push_notification_preferences (
    user_id          UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    mentions         BOOLEAN NOT NULL DEFAULT true,
    assignments      BOOLEAN NOT NULL DEFAULT true,
    comments          BOOLEAN NOT NULL DEFAULT false,
    status_changes    BOOLEAN NOT NULL DEFAULT false,
    due_date_changes  BOOLEAN NOT NULL DEFAULT false,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE push_subscriptions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint        TEXT NOT NULL UNIQUE CHECK (length(endpoint) BETWEEN 1 AND 4096),
    p256dh          TEXT NOT NULL CHECK (length(p256dh) BETWEEN 1 AND 1024),
    auth_secret     TEXT NOT NULL CHECK (length(auth_secret) BETWEEN 1 AND 1024),
    user_agent      TEXT NOT NULL DEFAULT '' CHECK (length(user_agent) <= 500),
    failure_count   INTEGER NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    last_success_at TIMESTAMPTZ,
    disabled_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_push_subscriptions_user_active
    ON push_subscriptions(user_id, created_at DESC)
    WHERE disabled_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE push_notification_events (
    changelog_id    UUID PRIMARY KEY REFERENCES project_changelog_entries(id) ON DELETE CASCADE,
    attempt_count   INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at       TIMESTAMPTZ,
    last_error      TEXT NOT NULL DEFAULT '',
    processed_at    TIMESTAMPTZ,
    failed_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_push_notification_events_pending
    ON push_notification_events(next_attempt_at, created_at)
    WHERE processed_at IS NULL AND failed_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE push_notification_deliveries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    changelog_id    UUID NOT NULL REFERENCES project_changelog_entries(id) ON DELETE CASCADE,
    subscription_id UUID NOT NULL REFERENCES push_subscriptions(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id      UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    issue_id        UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    category        TEXT NOT NULL CHECK (category IN ('mentions', 'assignments', 'comments', 'status_changes', 'due_date_changes')),
    attempt_count   INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at       TIMESTAMPTZ,
    last_error      TEXT NOT NULL DEFAULT '',
    delivered_at    TIMESTAMPTZ,
    suppressed_at   TIMESTAMPTZ,
    failed_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (changelog_id, subscription_id)
);

CREATE INDEX idx_push_notification_deliveries_pending
    ON push_notification_deliveries(next_attempt_at, created_at)
    WHERE delivered_at IS NULL AND suppressed_at IS NULL AND failed_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION track_enqueue_push_notification_event() RETURNS trigger AS $$
BEGIN
    IF NEW.issue_id IS NOT NULL AND (
        (NEW.entity = 'issue' AND NEW.op IN ('insert', 'update')) OR
        (NEW.entity = 'comment' AND NEW.op = 'insert')
    ) THEN
        INSERT INTO push_notification_events (changelog_id) VALUES (NEW.id);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER project_changelog_push_notification_events
    AFTER INSERT ON project_changelog_entries
    FOR EACH ROW EXECUTE FUNCTION track_enqueue_push_notification_event();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS project_changelog_push_notification_events ON project_changelog_entries;
DROP FUNCTION IF EXISTS track_enqueue_push_notification_event();
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS push_notification_deliveries;
DROP TABLE IF EXISTS push_notification_events;
DROP TABLE IF EXISTS push_subscriptions;
DROP TABLE IF EXISTS user_push_notification_preferences;
-- +goose StatementEnd
