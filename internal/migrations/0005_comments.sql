-- +goose Up
-- +goose StatementBegin
CREATE TABLE comments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id   UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    author_id  UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    body       TEXT NOT NULL CHECK (length(body) BETWEEN 1 AND 10000),
    version    BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX comments_issue_created ON comments(issue_id, created_at, id);
-- +goose StatementEnd

-- Extend track_emit_event so comment rows carry project_id via their issue.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION track_emit_event() RETURNS trigger AS $$
DECLARE
    payload JSONB;
    entity  TEXT := TG_ARGV[0];
    rec     RECORD;
    rec_iid UUID;
    rec_pid UUID;
BEGIN
    IF TG_OP = 'DELETE' THEN
        rec := OLD;
    ELSE
        IF TG_OP = 'UPDATE' THEN
            NEW.version := OLD.version + 1;
        END IF;
        rec := NEW;
    END IF;

    IF entity IN ('issue', 'sprint', 'issue_link') THEN
        rec_pid := rec.project_id;
    ELSIF entity = 'comment' THEN
        rec_iid := rec.issue_id;
        SELECT project_id INTO rec_pid FROM issues WHERE id = rec.issue_id;
    ELSE
        rec_iid := NULL;
        rec_pid := NULL;
    END IF;

    payload := jsonb_build_object(
        'op',         lower(TG_OP),
        'entity',     entity,
        'id',         rec.id,
        'issue_id',   rec_iid,
        'project_id', rec_pid,
        'version',    rec.version,
        'ts',         to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')
    );

    PERFORM pg_notify('track_events', payload::text);

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER comments_events
    BEFORE INSERT OR UPDATE OR DELETE ON comments
    FOR EACH ROW EXECUTE FUNCTION track_emit_event('comment');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS comments_events ON comments;
DROP TABLE IF EXISTS comments;
-- +goose StatementEnd

-- Restore the 0004 version of track_emit_event.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION track_emit_event() RETURNS trigger AS $$
DECLARE
    payload JSONB;
    entity  TEXT := TG_ARGV[0];
    rec     RECORD;
    rec_pid UUID;
BEGIN
    IF TG_OP = 'DELETE' THEN
        rec := OLD;
    ELSE
        IF TG_OP = 'UPDATE' THEN
            NEW.version := OLD.version + 1;
        END IF;
        rec := NEW;
    END IF;

    IF entity IN ('issue', 'sprint', 'issue_link') THEN
        rec_pid := rec.project_id;
    ELSE
        rec_pid := NULL;
    END IF;

    payload := jsonb_build_object(
        'op',         lower(TG_OP),
        'entity',     entity,
        'id',         rec.id,
        'project_id', rec_pid,
        'version',    rec.version,
        'ts',         to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')
    );

    PERFORM pg_notify('track_events', payload::text);

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
