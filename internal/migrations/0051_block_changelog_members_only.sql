-- +goose Up
-- +goose StatementBegin
-- Who a project blocked is for its members, like the block list itself, so
-- the changelog entries about blocks join members-only comments in being
-- hidden from everyone else.
UPDATE project_changelog_entries SET members_only = true WHERE entity = 'project_block';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE project_changelog_entries SET members_only = false WHERE entity = 'project_block';
-- +goose StatementEnd
