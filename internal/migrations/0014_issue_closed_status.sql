-- +goose Up
-- +goose StatementBegin
ALTER TYPE issue_status ADD VALUE IF NOT EXISTS 'closed';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE issues
    ALTER COLUMN status DROP DEFAULT;

UPDATE issues
SET status = 'done'
WHERE status = 'closed';

CREATE TYPE issue_status_old AS ENUM ('todo', 'in_progress', 'done');

ALTER TABLE issues
    ALTER COLUMN status TYPE issue_status_old
    USING status::text::issue_status_old;

ALTER TABLE issues
    ALTER COLUMN status SET DEFAULT 'todo';

DROP TYPE issue_status;

ALTER TYPE issue_status_old RENAME TO issue_status;
-- +goose StatementEnd
