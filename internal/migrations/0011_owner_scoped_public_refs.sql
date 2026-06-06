-- +goose Up
-- +goose StatementBegin
ALTER TABLE projects
    ADD COLUMN owner_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    ADD COLUMN next_sprint_number INT NOT NULL DEFAULT 1,
    ADD COLUMN next_issue_link_number INT NOT NULL DEFAULT 1;

ALTER TABLE issues
    ADD COLUMN next_comment_number INT NOT NULL DEFAULT 1;

ALTER TABLE sprints
    ADD COLUMN number INT;

ALTER TABLE issue_links
    ADD COLUMN number INT;

ALTER TABLE comments
    ADD COLUMN number INT;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE projects p
SET owner_id = COALESCE(
    (
        SELECT pm.user_id
        FROM project_members pm
        JOIN users u ON u.id = pm.user_id
        WHERE pm.project_id = p.id
          AND u.deleted_at IS NULL
        ORDER BY pm.created_at ASC, pm.user_id ASC
        LIMIT 1
    ),
    (
        SELECT id
        FROM users
        WHERE deleted_at IS NULL
        ORDER BY is_admin DESC, created_at ASC, id ASC
        LIMIT 1
    )
)
WHERE owner_id IS NULL;

UPDATE sprints s
SET number = ranked.n
FROM (
    SELECT id, row_number() OVER (PARTITION BY project_id ORDER BY created_at ASC, id ASC)::INT AS n
    FROM sprints
) ranked
WHERE s.id = ranked.id;

UPDATE issue_links il
SET number = ranked.n
FROM (
    SELECT id, row_number() OVER (PARTITION BY project_id ORDER BY created_at ASC, id ASC)::INT AS n
    FROM issue_links
) ranked
WHERE il.id = ranked.id;

UPDATE comments c
SET number = ranked.n
FROM (
    SELECT id, row_number() OVER (PARTITION BY issue_id ORDER BY created_at ASC, id ASC)::INT AS n
    FROM comments
) ranked
WHERE c.id = ranked.id;

UPDATE projects p
SET next_sprint_number = COALESCE((
        SELECT max(number) + 1
        FROM sprints s
        WHERE s.project_id = p.id
    ), 1),
    next_issue_link_number = COALESCE((
        SELECT max(number) + 1
        FROM issue_links il
        WHERE il.project_id = p.id
    ), 1);

UPDATE issues i
SET next_comment_number = COALESCE((
    SELECT max(number) + 1
    FROM comments c
    WHERE c.issue_id = i.id
), 1);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE projects
    ALTER COLUMN owner_id SET NOT NULL;

ALTER TABLE sprints
    ALTER COLUMN number SET NOT NULL;

ALTER TABLE issue_links
    ALTER COLUMN number SET NOT NULL;

ALTER TABLE comments
    ALTER COLUMN number SET NOT NULL;

ALTER TABLE projects
    DROP CONSTRAINT IF EXISTS projects_key_key,
    ADD CONSTRAINT projects_owner_key_key UNIQUE (owner_id, key);

ALTER TABLE sprints
    ADD CONSTRAINT sprints_project_number_key UNIQUE (project_id, number);

ALTER TABLE issue_links
    ADD CONSTRAINT issue_links_project_number_key UNIQUE (project_id, number);

ALTER TABLE comments
    ADD CONSTRAINT comments_issue_number_key UNIQUE (issue_id, number);

CREATE INDEX projects_owner_key_alive ON projects(owner_id, key) WHERE deleted_at IS NULL;
CREATE INDEX sprints_project_number_alive ON sprints(project_id, number) WHERE deleted_at IS NULL;
CREATE INDEX issue_links_project_number ON issue_links(project_id, number);
CREATE INDEX comments_issue_number ON comments(issue_id, number);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS comments_issue_number;
DROP INDEX IF EXISTS issue_links_project_number;
DROP INDEX IF EXISTS sprints_project_number_alive;
DROP INDEX IF EXISTS projects_owner_key_alive;

ALTER TABLE comments DROP CONSTRAINT IF EXISTS comments_issue_number_key;
ALTER TABLE issue_links DROP CONSTRAINT IF EXISTS issue_links_project_number_key;
ALTER TABLE sprints DROP CONSTRAINT IF EXISTS sprints_project_number_key;
ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_owner_key_key;
ALTER TABLE projects ADD CONSTRAINT projects_key_key UNIQUE (key);

ALTER TABLE comments DROP COLUMN IF EXISTS number;
ALTER TABLE issue_links DROP COLUMN IF EXISTS number;
ALTER TABLE sprints DROP COLUMN IF EXISTS number;
ALTER TABLE issues DROP COLUMN IF EXISTS next_comment_number;
ALTER TABLE projects
    DROP COLUMN IF EXISTS next_issue_link_number,
    DROP COLUMN IF EXISTS next_sprint_number,
    DROP COLUMN IF EXISTS owner_id;
-- +goose StatementEnd
