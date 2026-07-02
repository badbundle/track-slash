-- +goose Up
-- +goose StatementBegin
CREATE TABLE project_changelog_entries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    actor_id        UUID REFERENCES users(id) ON DELETE SET NULL,
    entity          TEXT NOT NULL CHECK (entity <> ''),
    op              TEXT NOT NULL CHECK (op IN ('insert', 'update', 'delete', 'restore', 'reorder', 'complete', 'grant', 'revoke')),
    entity_id       UUID NOT NULL,
    issue_id        UUID,
    parent_issue_id UUID,
    target_ref      TEXT NOT NULL DEFAULT '',
    target_title    TEXT NOT NULL DEFAULT '',
    summary         TEXT NOT NULL CHECK (summary <> ''),
    details         JSONB NOT NULL DEFAULT '{}'::jsonb,
    version         BIGINT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_project_changelog_project_created
    ON project_changelog_entries(project_id, created_at DESC, id DESC);

CREATE INDEX idx_project_changelog_issue_created
    ON project_changelog_entries(issue_id, created_at DESC, id DESC)
    WHERE issue_id IS NOT NULL;
-- +goose StatementEnd

-- Extend realtime event payloads for changelog rows.
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

    IF entity IN ('issue', 'sprint', 'issue_link', 'project_context', 'issue_context_link', 'issue_tag', 'issue_tag_link', 'project_changelog') THEN
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
    ELSIF entity = 'project_changelog' THEN
        rec_iid := rec.issue_id;
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
CREATE TRIGGER project_changelog_events
    AFTER INSERT ON project_changelog_entries
    FOR EACH ROW EXECUTE FUNCTION track_emit_event('project_changelog');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS project_changelog_events ON project_changelog_entries;
DROP TABLE IF EXISTS project_changelog_entries;
-- +goose StatementEnd

-- Restore the 0018 version of track_emit_event.
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
