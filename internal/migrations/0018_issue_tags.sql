-- +goose Up
-- +goose StatementBegin
CREATE TYPE issue_tag_color AS ENUM (
    'slate',
    'red',
    'orange',
    'amber',
    'yellow',
    'green',
    'teal',
    'cyan',
    'blue',
    'violet',
    'pink'
);

ALTER TABLE projects
    ADD COLUMN next_tag_number INT NOT NULL DEFAULT 1;

CREATE TABLE issue_tags (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    number     INT NOT NULL,
    name       TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    color      issue_tag_color NOT NULL DEFAULT 'blue',
    version    BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT issue_tags_project_number_key UNIQUE (project_id, number),
    CONSTRAINT issue_tags_project_id_id_key UNIQUE (project_id, id)
);

CREATE TABLE issue_tag_links (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    issue_id   UUID NOT NULL,
    tag_id     UUID NOT NULL,
    version    BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT issue_tag_links_issue_project_fk
        FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT issue_tag_links_tag_project_fk
        FOREIGN KEY (project_id, tag_id) REFERENCES issue_tags(project_id, id) ON DELETE CASCADE,
    CONSTRAINT issue_tag_links_unique UNIQUE (issue_id, tag_id)
);

CREATE UNIQUE INDEX issue_tags_project_name_key
    ON issue_tags(project_id, name);
CREATE INDEX issue_tags_project_number ON issue_tags(project_id, number);
CREATE INDEX issue_tags_project_name ON issue_tags(project_id, name);
CREATE INDEX issue_tag_links_issue ON issue_tag_links(issue_id, created_at, id);
CREATE INDEX issue_tag_links_tag ON issue_tag_links(tag_id, created_at, id);
CREATE INDEX issue_tag_links_project ON issue_tag_links(project_id);
-- +goose StatementEnd

-- Extend realtime event payloads for issue tags and issue/tag links.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION track_emit_event() RETURNS trigger AS $$
DECLARE
    payload             JSONB;
    entity              TEXT := TG_ARGV[0];
    rec                 RECORD;
    rec_iid             UUID;
    rec_pid             UUID;
    rec_context_id      UUID;
    rec_tag_id          UUID;
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

    IF entity IN ('issue', 'sprint', 'issue_link', 'project_context', 'issue_context_link', 'issue_tag', 'issue_tag_link') THEN
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

    IF entity = 'issue_tag_link' THEN
        rec_iid := rec.issue_id;
        rec_tag_id := rec.tag_id;
    ELSE
        rec_tag_id := NULL;
    END IF;

    payload := jsonb_build_object(
        'op',              rec_op,
        'entity',          entity,
        'id',              rec.id,
        'issue_id',        rec_iid,
        'context_id',      rec_context_id,
        'tag_id',          rec_tag_id,
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
CREATE TRIGGER issue_tags_events
    BEFORE INSERT OR UPDATE OR DELETE ON issue_tags
    FOR EACH ROW EXECUTE FUNCTION track_emit_event('issue_tag');

CREATE TRIGGER issue_tag_links_events
    BEFORE INSERT OR UPDATE OR DELETE ON issue_tag_links
    FOR EACH ROW EXECUTE FUNCTION track_emit_event('issue_tag_link');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS issue_tag_links_events ON issue_tag_links;
DROP TRIGGER IF EXISTS issue_tags_events ON issue_tags;
DROP TABLE IF EXISTS issue_tag_links;
DROP TABLE IF EXISTS issue_tags;
ALTER TABLE projects DROP COLUMN IF EXISTS next_tag_number;
DROP TYPE IF EXISTS issue_tag_color;
-- +goose StatementEnd

-- Restore the 0016/0017 context-aware event function.
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
