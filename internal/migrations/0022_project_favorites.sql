-- +goose Up
-- +goose StatementBegin
CREATE TABLE project_favorites (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, project_id)
);

CREATE INDEX project_favorites_user_created
    ON project_favorites(user_id, created_at DESC, project_id);

CREATE INDEX project_favorites_project_user
    ON project_favorites(project_id, user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS project_favorites;
-- +goose StatementEnd
