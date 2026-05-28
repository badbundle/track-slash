-- +goose Up
-- +goose StatementBegin
CREATE TYPE sprint_status AS ENUM ('planned', 'active', 'completed');

CREATE TABLE sprints (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name         TEXT NOT NULL DEFAULT '',
    goal         TEXT NOT NULL DEFAULT '',
    status       sprint_status NOT NULL DEFAULT 'planned',
    start_date   DATE NOT NULL,
    end_date     DATE NOT NULL,
    completed_at TIMESTAMPTZ,
    version      BIGINT NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sprints_date_order CHECK (end_date >= start_date)
);

CREATE UNIQUE INDEX uq_sprints_active_per_project
    ON sprints(project_id) WHERE status = 'active';
CREATE INDEX idx_sprints_project_status ON sprints(project_id, status);
CREATE INDEX idx_sprints_project_start  ON sprints(project_id, start_date);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE issues
    ADD COLUMN sprint_id UUID REFERENCES sprints(id) ON DELETE SET NULL;
CREATE INDEX idx_issues_sprint  ON issues(sprint_id) WHERE sprint_id IS NOT NULL;
CREATE INDEX idx_issues_backlog ON issues(project_id) WHERE sprint_id IS NULL;
-- +goose StatementEnd

-- Sprint events need project_id in the notify payload so the hub can fan them
-- out on both `sprint:{id}` and `project:{project_id}` topics. The original
-- function in 0002 only filled project_id for entity = 'issue'.
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

-- +goose StatementBegin
CREATE TRIGGER sprints_events
    BEFORE INSERT OR UPDATE OR DELETE ON sprints
    FOR EACH ROW EXECUTE FUNCTION track_emit_event('sprint');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS sprints_events ON sprints;
DROP INDEX IF EXISTS idx_issues_backlog;
DROP INDEX IF EXISTS idx_issues_sprint;
ALTER TABLE issues DROP COLUMN IF EXISTS sprint_id;
DROP TABLE IF EXISTS sprints;
DROP TYPE  IF EXISTS sprint_status;
-- +goose StatementEnd
