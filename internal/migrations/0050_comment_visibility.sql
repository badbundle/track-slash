-- +goose Up
-- +goose StatementBegin
-- A comment is either shared with everyone who can read its issue or kept to
-- project members. Existing comments were visible to every reader, so they
-- stay shared.
CREATE TYPE comment_visibility AS ENUM ('shared', 'members');

ALTER TABLE comments
    ADD COLUMN visibility comment_visibility NOT NULL DEFAULT 'shared';

-- Changelog entries about a members-only comment carry its body preview, so
-- they follow the comment's visibility.
ALTER TABLE project_changelog_entries
    ADD COLUMN members_only BOOLEAN NOT NULL DEFAULT false;
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
ALTER TABLE project_changelog_entries DROP COLUMN members_only;
ALTER TABLE comments DROP COLUMN visibility;
DROP TYPE comment_visibility;
-- +goose StatementEnd
