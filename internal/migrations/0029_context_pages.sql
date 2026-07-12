-- +goose Up
-- +goose StatementBegin
ALTER TABLE project_context
    DROP CONSTRAINT project_context_body_check,
    ADD COLUMN position BIGINT;

WITH ranked AS (
    SELECT id,
           row_number() OVER (PARTITION BY project_id ORDER BY number ASC) AS position
    FROM project_context
    WHERE scope = 'project'
)
UPDATE project_context pc
SET position = ranked.position
FROM ranked
WHERE pc.id = ranked.id;

ALTER TABLE project_context
    ADD CONSTRAINT project_context_body_check
        CHECK (length(body) <= 100000 AND (scope = 'project' OR length(body) >= 1)),
    ADD CONSTRAINT project_context_position_scope_check
        CHECK ((scope = 'project' AND position IS NOT NULL AND position >= 1)
            OR (scope = 'issue' AND position IS NULL)),
    ADD CONSTRAINT project_context_content_type_check
        CHECK (content_type IN ('text/plain; charset=utf-8', 'text/markdown; charset=utf-8'));

CREATE UNIQUE INDEX project_context_project_position_key
    ON project_context(project_id, position)
    WHERE scope = 'project';

CREATE INDEX project_context_project_position
    ON project_context(project_id, position, id)
    WHERE scope = 'project';

CREATE TABLE context_attachments (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    context_id        UUID NOT NULL,
    storage_object_id UUID NOT NULL,
    created_by_id     UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    version           BIGINT NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT context_attachments_context_project_fk
        FOREIGN KEY (project_id, context_id) REFERENCES project_context(project_id, id) ON DELETE CASCADE,
    CONSTRAINT context_attachments_object_project_fk
        FOREIGN KEY (project_id, storage_object_id) REFERENCES storage_objects(project_id, id) ON DELETE CASCADE,
    CONSTRAINT context_attachments_storage_object_key UNIQUE (storage_object_id)
);

CREATE INDEX context_attachments_context_created
    ON context_attachments(context_id, created_at, id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION track_emit_context_attachment_event() RETURNS trigger AS $$
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
        'entity',     'context_attachment',
        'id',         rec.id,
        'context_id', rec.context_id,
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

CREATE TRIGGER context_attachments_events
    BEFORE INSERT OR UPDATE OR DELETE ON context_attachments
    FOR EACH ROW EXECUTE FUNCTION track_emit_context_attachment_event();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS context_attachments_events ON context_attachments;
DROP FUNCTION IF EXISTS track_emit_context_attachment_event();
DROP TABLE IF EXISTS context_attachments;
DROP INDEX IF EXISTS project_context_project_position;
DROP INDEX IF EXISTS project_context_project_position_key;

ALTER TABLE project_context
    DROP CONSTRAINT project_context_content_type_check,
    DROP CONSTRAINT project_context_position_scope_check,
    DROP CONSTRAINT project_context_body_check;

UPDATE project_context SET body = ' ' WHERE body = '';

ALTER TABLE project_context
    DROP COLUMN position,
    ADD CONSTRAINT project_context_body_check CHECK (length(body) BETWEEN 1 AND 100000);
-- +goose StatementEnd
