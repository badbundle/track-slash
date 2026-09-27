-- +goose Up
-- +goose StatementBegin
-- One access mode replaces the is_public and public_issue_creation pair, whose
-- CHECK already ruled out issue creation without public read. helpdesk is
-- declared now so the mode that uses it needs no second enum migration.
CREATE TYPE project_access_mode AS ENUM ('private', 'public', 'public_issues', 'helpdesk');

ALTER TABLE projects
    ADD COLUMN access_mode project_access_mode NOT NULL DEFAULT 'private';

UPDATE projects
SET access_mode = CASE
    WHEN is_public AND public_issue_creation THEN 'public_issues'::project_access_mode
    WHEN is_public THEN 'public'::project_access_mode
    ELSE 'private'::project_access_mode
END;

ALTER TABLE projects
    DROP CONSTRAINT projects_public_issue_creation_requires_public,
    DROP COLUMN public_issue_creation,
    DROP COLUMN is_public;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE projects
    ADD COLUMN is_public BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN public_issue_creation BOOLEAN NOT NULL DEFAULT false;

-- helpdesk has no equivalent in the old pair, so it goes back to private.
UPDATE projects
SET is_public = access_mode IN ('public', 'public_issues'),
    public_issue_creation = access_mode = 'public_issues';

ALTER TABLE projects
    ADD CONSTRAINT projects_public_issue_creation_requires_public
        CHECK (NOT public_issue_creation OR is_public),
    DROP COLUMN access_mode;

DROP TYPE project_access_mode;
-- +goose StatementEnd
