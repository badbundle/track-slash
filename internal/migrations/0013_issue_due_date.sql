-- +goose Up
-- +goose StatementBegin
ALTER TABLE issues
    ADD COLUMN due_date DATE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE issues
    DROP COLUMN IF EXISTS due_date;
-- +goose StatementEnd
