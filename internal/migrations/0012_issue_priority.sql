-- +goose Up
-- +goose StatementBegin
CREATE TYPE issue_priority AS ENUM ('P0', 'P1', 'P2', 'P3', 'P4');

ALTER TABLE issues
    ADD COLUMN priority issue_priority NOT NULL DEFAULT 'P2';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE issues DROP COLUMN IF EXISTS priority;
DROP TYPE IF EXISTS issue_priority;
-- +goose StatementEnd
