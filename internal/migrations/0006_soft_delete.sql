-- +goose Up
-- +goose StatementBegin
ALTER TABLE issues   ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE sprints  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE users    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS issues_alive  ON issues(project_id)  WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS sprints_alive ON sprints(project_id) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- Emit op=delete for first soft-delete update on soft-deletable entities.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION track_emit_event() RETURNS trigger AS $$
DECLARE
    payload JSONB;
    entity  TEXT := TG_ARGV[0];
    rec     RECORD;
    rec_iid UUID;
    rec_pid UUID;
    rec_op  TEXT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        rec := OLD;
    ELSE
        IF TG_OP = 'UPDATE' THEN
            NEW.version := OLD.version + 1;
        END IF;
        rec := NEW;
    END IF;

    rec_op := lower(TG_OP);
    IF TG_OP = 'UPDATE' AND entity IN ('issue', 'project', 'sprint') THEN
        IF OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
            rec_op := 'delete';
        END IF;
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
        'op',         rec_op,
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

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS sprints_alive;
DROP INDEX IF EXISTS issues_alive;
ALTER TABLE users    DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE sprints  DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE projects DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE issues   DROP COLUMN IF EXISTS deleted_at;
-- +goose StatementEnd

-- Restore the 0005 version of track_emit_event.
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
