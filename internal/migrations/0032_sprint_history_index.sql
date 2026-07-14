-- +goose Up
-- +goose StatementBegin
CREATE INDEX idx_sprints_project_completed_at
    ON sprints(project_id, completed_at DESC, id DESC)
    WHERE status = 'completed' AND deleted_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_sprints_project_completed_at;
-- +goose StatementEnd
