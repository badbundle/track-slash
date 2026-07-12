-- +goose Up
-- +goose StatementBegin
ALTER TABLE sprints
    ADD CONSTRAINT sprints_project_id_id_key UNIQUE (project_id, id);

CREATE TABLE sprint_attachments (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    sprint_id         UUID NOT NULL,
    storage_object_id UUID NOT NULL,
    created_by_id     UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    version           BIGINT NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sprint_attachments_sprint_project_fk
        FOREIGN KEY (project_id, sprint_id) REFERENCES sprints(project_id, id) ON DELETE CASCADE,
    CONSTRAINT sprint_attachments_object_project_fk
        FOREIGN KEY (project_id, storage_object_id) REFERENCES storage_objects(project_id, id) ON DELETE CASCADE,
    CONSTRAINT sprint_attachments_sprint_object_key UNIQUE (sprint_id, storage_object_id),
    CONSTRAINT sprint_attachments_storage_object_key UNIQUE (storage_object_id)
);

CREATE INDEX sprint_attachments_sprint_created
    ON sprint_attachments(sprint_id, created_at, id);

CREATE INDEX sprint_attachments_project
    ON sprint_attachments(project_id);
-- +goose StatementEnd

-- Sprint attachment rows use a dedicated trigger function so adding their
-- sprint_id routing field does not rewrite the global event function.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION track_emit_sprint_attachment_event() RETURNS trigger AS $$
DECLARE
    rec RECORD;
BEGIN
    IF TG_OP = 'DELETE' THEN
        rec := OLD;
    ELSE
        IF TG_OP = 'UPDATE' THEN
            NEW.version := OLD.version + 1;
        END IF;
        rec := NEW;
    END IF;

    PERFORM pg_notify('track_events', jsonb_build_object(
        'op',         lower(TG_OP),
        'entity',     'sprint_attachment',
        'id',         rec.id,
        'sprint_id',  rec.sprint_id,
        'project_id', rec.project_id,
        'version',    rec.version,
        'ts',         to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')
    )::text);

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER sprint_attachments_events
    BEFORE INSERT OR UPDATE OR DELETE ON sprint_attachments
    FOR EACH ROW EXECUTE FUNCTION track_emit_sprint_attachment_event();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS sprint_attachments_events ON sprint_attachments;
DROP FUNCTION IF EXISTS track_emit_sprint_attachment_event();
DROP TABLE IF EXISTS sprint_attachments;
ALTER TABLE sprints DROP CONSTRAINT IF EXISTS sprints_project_id_id_key;
-- +goose StatementEnd
