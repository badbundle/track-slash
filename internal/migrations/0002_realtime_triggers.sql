-- +goose Up
-- +goose StatementBegin
ALTER TABLE issues   ADD COLUMN version BIGINT NOT NULL DEFAULT 1;
ALTER TABLE projects ADD COLUMN version BIGINT NOT NULL DEFAULT 1;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION track_emit_event() RETURNS trigger AS $$
DECLARE
    payload    JSONB;
    entity     TEXT := TG_ARGV[0];
    rec        RECORD;
    rec_id     UUID;
    rec_pid    UUID;
    rec_ver    BIGINT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        rec := OLD;
    ELSE
        IF TG_OP = 'UPDATE' THEN
            NEW.version := OLD.version + 1;
        END IF;
        rec := NEW;
    END IF;

    rec_id  := rec.id;
    rec_ver := rec.version;
    IF entity = 'issue' THEN
        rec_pid := rec.project_id;
    ELSE
        rec_pid := NULL;
    END IF;

    payload := jsonb_build_object(
        'op',         lower(TG_OP),
        'entity',     entity,
        'id',         rec_id,
        'project_id', rec_pid,
        'version',    rec_ver,
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
CREATE TRIGGER issues_events
    BEFORE INSERT OR UPDATE OR DELETE ON issues
    FOR EACH ROW EXECUTE FUNCTION track_emit_event('issue');
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER projects_events
    BEFORE INSERT OR UPDATE OR DELETE ON projects
    FOR EACH ROW EXECUTE FUNCTION track_emit_event('project');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS issues_events ON issues;
DROP TRIGGER IF EXISTS projects_events ON projects;
DROP FUNCTION IF EXISTS track_emit_event();
ALTER TABLE issues   DROP COLUMN IF EXISTS version;
ALTER TABLE projects DROP COLUMN IF EXISTS version;
-- +goose StatementEnd
