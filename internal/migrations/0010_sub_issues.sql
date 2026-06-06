-- +goose Up
-- +goose StatementBegin
ALTER TABLE issues
    ADD COLUMN parent_issue_id UUID REFERENCES issues(id) ON DELETE CASCADE,
    ADD CONSTRAINT issues_no_self_parent CHECK (parent_issue_id IS NULL OR parent_issue_id <> id),
    ADD CONSTRAINT issues_sub_issues_no_sprint CHECK (parent_issue_id IS NULL OR sprint_id IS NULL);

CREATE INDEX idx_issues_parent
    ON issues(parent_issue_id, number)
    WHERE parent_issue_id IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX idx_issues_project_top_level_number
    ON issues(project_id, number)
    WHERE parent_issue_id IS NULL AND deleted_at IS NULL;
-- +goose StatementEnd

-- Include parent_issue_id in issue events so clients subscribed to the parent
-- issue topic can refetch sub-issue lists after child mutations.
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

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_issues_project_top_level_number;
DROP INDEX IF EXISTS idx_issues_parent;
ALTER TABLE issues
    DROP CONSTRAINT IF EXISTS issues_sub_issues_no_sprint,
    DROP CONSTRAINT IF EXISTS issues_no_self_parent,
    DROP COLUMN IF EXISTS parent_issue_id;
-- +goose StatementEnd

-- Restore the 0006 soft-delete-aware event function.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION track_emit_event() RETURNS trigger AS $$
DECLARE
    payload JSONB;
    entity  TEXT := TG_ARGV[0];
    rec     RECORD;
    rec_iid UUID;
    rec_pid UUID;
    rec_op  TEXT;
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

    payload := jsonb_build_object(
        'op',         rec_op,
        'entity',     entity,
        'id',         rec.id,
        'issue_id',   rec_iid,
        'project_id', rec_pid,
        'version',    rec.version,
        'ts',         to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')
    );

    PERFORM pg_notify('track_events', payload::text);

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
