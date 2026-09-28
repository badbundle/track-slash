-- +goose Up
-- +goose StatementBegin
-- A private issue is for the project's members and its reporter only, even on
-- a public project. Everyone else is answered as though it didn't exist.
ALTER TABLE issues
    ADD COLUMN private boolean NOT NULL DEFAULT false;
-- +goose StatementEnd

-- +goose StatementBegin
-- The other issues a changelog entry names besides issue_id, such as a link's
-- target. An entry naming a private issue is for the project's members.
ALTER TABLE project_changelog_entries
    ADD COLUMN related_issue_ids uuid[] NOT NULL DEFAULT '{}';

UPDATE project_changelog_entries e
SET related_issue_ids = ARRAY[l.source_id, l.target_id]
FROM issue_links l
WHERE e.entity = 'issue_link' AND e.entity_id = l.id;
-- +goose StatementEnd

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
    rec_members_only    BOOLEAN := false;
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

    -- A members-only comment, a private issue, and everything about either
    -- (comments, links, context and tag links, attachments and changelog
    -- entries) reach only subscribers who may read members-only content; the
    -- hub filters on this.
    IF entity = 'issue' THEN
        rec_members_only := rec.private;
    ELSIF entity = 'comment' THEN
        rec_members_only := rec.visibility = 'members'
            OR EXISTS (SELECT 1 FROM issues i WHERE i.id = rec.issue_id AND i.private);
    ELSIF entity = 'project_changelog' THEN
        rec_members_only := rec.members_only
            OR EXISTS (SELECT 1 FROM issues i WHERE (i.id = rec.issue_id OR i.id = ANY(rec.related_issue_ids)) AND i.private);
    ELSIF entity = 'issue_link' THEN
        rec_members_only := EXISTS (SELECT 1 FROM issues i WHERE i.id IN (rec.source_id, rec.target_id) AND i.private);
    ELSIF entity IN ('issue_context_link', 'issue_tag_link', 'issue_attachment') THEN
        rec_members_only := EXISTS (SELECT 1 FROM issues i WHERE i.id = rec.issue_id AND i.private);
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
        'members_only',    rec_members_only,
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

-- +goose Down
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
    rec_members_only    BOOLEAN := false;
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

    -- A members-only comment, and the changelog entries about one, reach only
    -- subscribers who may read members-only content; the hub filters on this.
    IF entity = 'comment' THEN
        rec_members_only := rec.visibility = 'members';
    ELSIF entity = 'project_changelog' THEN
        rec_members_only := rec.members_only;
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
        'members_only',    rec_members_only,
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
ALTER TABLE project_changelog_entries DROP COLUMN IF EXISTS related_issue_ids;
ALTER TABLE issues DROP COLUMN IF EXISTS private;
-- +goose StatementEnd
