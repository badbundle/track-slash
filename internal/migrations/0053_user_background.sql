-- +goose Up
-- +goose StatementBegin
-- The in-app background each user picked. The app validates the preset when
-- saving and renders an unknown value as the default, so a preset can be
-- retired without rewriting rows.
ALTER TABLE users
    ADD COLUMN background text NOT NULL DEFAULT 'indigo';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN IF EXISTS background;
-- +goose StatementEnd
