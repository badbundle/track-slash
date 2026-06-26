-- +goose Up
-- +goose StatementBegin
CREATE TYPE project_context_kind AS ENUM ('text');

ALTER TABLE projects
    ADD COLUMN next_context_number INT NOT NULL DEFAULT 1;

ALTER TABLE issues
    ADD CONSTRAINT issues_project_id_id_key UNIQUE (project_id, id);

CREATE TABLE project_context (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    number          INT NOT NULL,
    title           TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    kind            project_context_kind NOT NULL DEFAULT 'text',
    content_type    TEXT NOT NULL DEFAULT 'text/plain; charset=utf-8',
    body            TEXT NOT NULL CHECK (length(body) BETWEEN 1 AND 100000),
    source_filename TEXT,
    created_by_id   UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    updated_by_id   UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    version         BIGINT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_context_project_number_key UNIQUE (project_id, number),
    CONSTRAINT project_context_project_id_id_key UNIQUE (project_id, id)
);

CREATE TABLE issue_context_links (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    issue_id   UUID NOT NULL,
    context_id UUID NOT NULL,
    version    BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT issue_context_links_issue_project_fk
        FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT issue_context_links_context_project_fk
        FOREIGN KEY (project_id, context_id) REFERENCES project_context(project_id, id) ON DELETE CASCADE,
    CONSTRAINT issue_context_links_unique UNIQUE (issue_id, context_id)
);

CREATE INDEX project_context_project_number ON project_context(project_id, number);
CREATE INDEX project_context_project_created ON project_context(project_id, created_at, id);
CREATE INDEX issue_context_links_issue ON issue_context_links(issue_id, created_at, id);
CREATE INDEX issue_context_links_context ON issue_context_links(context_id, created_at, id);
CREATE INDEX issue_context_links_project ON issue_context_links(project_id);
-- +goose StatementEnd

-- Extend realtime event payloads for project context and issue/context links.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION track_emit_event() RETURNS trigger AS $$
DECLARE
    payload             JSONB;
    entity              TEXT := TG_ARGV[0];
    rec                 RECORD;
    rec_iid             UUID;
    rec_pid             UUID;
    rec_context_id      UUID;
    rec_parent_issue_id UUID;
    rec_op              TEXT;
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

    IF entity IN ('issue', 'sprint', 'issue_link', 'project_context', 'issue_context_link') THEN
        rec_pid := rec.project_id;
    ELSIF entity = 'comment' THEN
        rec_iid := rec.issue_id;
        SELECT project_id INTO rec_pid FROM issues WHERE id = rec.issue_id;
    ELSE
        rec_iid := NULL;
        rec_pid := NULL;
    END IF;

    IF entity = 'issue' THEN
        rec_parent_issue_id := rec.parent_issue_id;
    ELSE
        rec_parent_issue_id := NULL;
    END IF;

    IF entity = 'issue_context_link' THEN
        rec_iid := rec.issue_id;
        rec_context_id := rec.context_id;
    ELSE
        rec_context_id := NULL;
    END IF;

    payload := jsonb_build_object(
        'op',              rec_op,
        'entity',          entity,
        'id',              rec.id,
        'issue_id',        rec_iid,
        'context_id',      rec_context_id,
        'parent_issue_id', rec_parent_issue_id,
        'project_id',      rec_pid,
        'version',         rec.version,
        'ts',              to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')
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
CREATE TRIGGER project_context_events
    BEFORE INSERT OR UPDATE OR DELETE ON project_context
    FOR EACH ROW EXECUTE FUNCTION track_emit_event('project_context');

CREATE TRIGGER issue_context_links_events
    BEFORE INSERT OR UPDATE OR DELETE ON issue_context_links
    FOR EACH ROW EXECUTE FUNCTION track_emit_event('issue_context_link');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS issue_context_links_events ON issue_context_links;
DROP TRIGGER IF EXISTS project_context_events ON project_context;
DROP TABLE IF EXISTS issue_context_links;
DROP TABLE IF EXISTS project_context;
ALTER TABLE issues DROP CONSTRAINT IF EXISTS issues_project_id_id_key;
ALTER TABLE projects DROP COLUMN IF EXISTS next_context_number;
DROP TYPE IF EXISTS project_context_kind;
-- +goose StatementEnd

-- Restore the 0010 sub-issue-aware event function.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION track_emit_event() RETURNS trigger AS $$
DECLARE
    payload             JSONB;
    entity              TEXT := TG_ARGV[0];
    rec                 RECORD;
    rec_iid             UUID;
    rec_pid             UUID;
    rec_parent_issue_id UUID;
    rec_op              TEXT;
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

    IF entity = 'issue' THEN
        rec_parent_issue_id := rec.parent_issue_id;
    ELSE
        rec_parent_issue_id := NULL;
    END IF;

    payload := jsonb_build_object(
        'op',              rec_op,
        'entity',          entity,
        'id',              rec.id,
        'issue_id',        rec_iid,
        'parent_issue_id', rec_parent_issue_id,
        'project_id',      rec_pid,
        'version',         rec.version,
        'ts',              to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')
    );

    PERFORM pg_notify('track_events', payload::text);

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
