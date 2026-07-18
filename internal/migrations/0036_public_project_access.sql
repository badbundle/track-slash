-- +goose Up
-- +goose StatementBegin
ALTER TABLE projects
    ADD COLUMN is_public BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN public_issue_creation BOOLEAN NOT NULL DEFAULT false,
    ADD CONSTRAINT projects_public_issue_creation_requires_public
        CHECK (NOT public_issue_creation OR is_public);

CREATE TABLE project_user_blocks (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_by_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    version       BIGINT NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_user_blocks_project_user_key UNIQUE (project_id, user_id)
);

CREATE INDEX project_user_blocks_user_project
    ON project_user_blocks(user_id, project_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION track_emit_project_block_event() RETURNS trigger AS $$
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
        'entity',     'project_block',
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

CREATE TRIGGER project_user_blocks_events
    BEFORE INSERT OR UPDATE OR DELETE ON project_user_blocks
    FOR EACH ROW EXECUTE FUNCTION track_emit_project_block_event();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS project_user_blocks_events ON project_user_blocks;
DROP FUNCTION IF EXISTS track_emit_project_block_event();
DROP TABLE IF EXISTS project_user_blocks;
ALTER TABLE projects
    DROP CONSTRAINT IF EXISTS projects_public_issue_creation_requires_public,
    DROP COLUMN IF EXISTS public_issue_creation,
    DROP COLUMN IF EXISTS is_public;
-- +goose StatementEnd
