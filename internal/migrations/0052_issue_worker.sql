-- +goose Up
-- +goose StatementBegin
-- Who is meant to complete an issue. NULL means nobody has said.
CREATE TYPE issue_worker AS ENUM ('agent', 'human');

ALTER TABLE issues
    ADD COLUMN worker issue_worker;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE issues DROP COLUMN IF EXISTS worker;
DROP TYPE IF EXISTS issue_worker;
-- +goose StatementEnd
