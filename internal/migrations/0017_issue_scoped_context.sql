-- +goose Up
-- +goose StatementBegin
CREATE TYPE project_context_scope AS ENUM ('project', 'issue');

ALTER TABLE project_context
    ADD COLUMN scope project_context_scope NOT NULL DEFAULT 'project';

CREATE INDEX project_context_project_scope_number ON project_context(project_id, scope, number);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS project_context_project_scope_number;
ALTER TABLE project_context DROP COLUMN IF EXISTS scope;
DROP TYPE IF EXISTS project_context_scope;
-- +goose StatementEnd
