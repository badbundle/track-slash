-- +goose Up
-- +goose StatementBegin
CREATE TYPE issue_link_type AS ENUM ('blocks', 'duplicates', 'relates_to', 'clones');

CREATE TABLE issue_links (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_id  UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    target_id  UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    link_type  issue_link_type NOT NULL,
    version    BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT issue_links_no_self CHECK (source_id <> target_id),
    CONSTRAINT issue_links_unique UNIQUE (source_id, target_id, link_type)
);

CREATE INDEX idx_issue_links_source  ON issue_links(source_id);
CREATE INDEX idx_issue_links_target  ON issue_links(target_id);
CREATE INDEX idx_issue_links_project ON issue_links(project_id);
-- +goose StatementEnd

-- Extend track_emit_event so issue_link rows carry project_id in the
-- notify payload, matching the issue/sprint pattern. Mirrors 0003.
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

-- +goose StatementBegin
CREATE TRIGGER issue_links_events
    BEFORE INSERT OR UPDATE OR DELETE ON issue_links
    FOR EACH ROW EXECUTE FUNCTION track_emit_event('issue_link');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS issue_links_events ON issue_links;
DROP TABLE IF EXISTS issue_links;
DROP TYPE  IF EXISTS issue_link_type;
-- +goose StatementEnd

-- Restore the 0003 version of track_emit_event so a clean down to 0003
-- matches that migration's state.
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

    IF entity IN ('issue', 'sprint') THEN
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
