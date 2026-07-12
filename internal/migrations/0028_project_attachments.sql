-- +goose Up
-- +goose StatementBegin
CREATE TABLE project_attachments (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    storage_object_id UUID NOT NULL,
    created_by_id     UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    version           BIGINT NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_attachments_object_project_fk
        FOREIGN KEY (project_id, storage_object_id) REFERENCES storage_objects(project_id, id) ON DELETE CASCADE,
    CONSTRAINT project_attachments_storage_object_key UNIQUE (storage_object_id)
);

CREATE INDEX project_attachments_project_created
    ON project_attachments(project_id, created_at, id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION track_emit_project_attachment_event() RETURNS trigger AS $$
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
        'entity',     'project_attachment',
        'id',         rec.id,
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

CREATE TRIGGER project_attachments_events
    BEFORE INSERT OR UPDATE OR DELETE ON project_attachments
    FOR EACH ROW EXECUTE FUNCTION track_emit_project_attachment_event();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS project_attachments_events ON project_attachments;
DROP FUNCTION IF EXISTS track_emit_project_attachment_event();
DROP TABLE IF EXISTS project_attachments;
-- +goose StatementEnd
