-- +goose Up
-- +goose StatementBegin
ALTER TABLE storage_objects
    ADD CONSTRAINT storage_objects_project_id_id_key UNIQUE (project_id, id);

CREATE TABLE issue_attachments (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    issue_id          UUID NOT NULL,
    storage_object_id UUID NOT NULL,
    created_by_id     UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    version           BIGINT NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT issue_attachments_issue_project_fk
        FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE,
    CONSTRAINT issue_attachments_object_project_fk
        FOREIGN KEY (project_id, storage_object_id) REFERENCES storage_objects(project_id, id) ON DELETE CASCADE,
    CONSTRAINT issue_attachments_issue_object_key UNIQUE (issue_id, storage_object_id),
    CONSTRAINT issue_attachments_storage_object_key UNIQUE (storage_object_id)
);

CREATE INDEX issue_attachments_issue_created
    ON issue_attachments(issue_id, created_at, id);

CREATE INDEX issue_attachments_project
    ON issue_attachments(project_id);
-- +goose StatementEnd

-- Extend realtime event payloads for issue attachment rows.
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

    IF entity IN ('issue', 'sprint', 'issue_link', 'project_context', 'issue_context_link', 'issue_tag', 'issue_tag_link', 'project_changelog', 'issue_attachment') THEN
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

    IF entity = 'issue_attachment' THEN
        rec_iid := rec.issue_id;
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
CREATE TRIGGER issue_attachments_events
    BEFORE INSERT OR UPDATE OR DELETE ON issue_attachments
    FOR EACH ROW EXECUTE FUNCTION track_emit_event('issue_attachment');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS issue_attachments_events ON issue_attachments;
DROP TABLE IF EXISTS issue_attachments;
ALTER TABLE storage_objects DROP CONSTRAINT IF EXISTS storage_objects_project_id_id_key;
-- +goose StatementEnd

-- Restore the 0019 version of track_emit_event.
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
