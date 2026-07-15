-- +goose Up
-- +goose StatementBegin
ALTER TABLE sprint_issue_snapshots
    ADD COLUMN status issue_status;

UPDATE sprint_issue_snapshots sis
SET status = i.status
FROM issues i
WHERE i.id = sis.issue_id AND i.project_id = sis.project_id;

ALTER TABLE sprint_issue_snapshots
    ALTER COLUMN status SET NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sprint_issue_snapshots
    DROP COLUMN IF EXISTS status;
-- +goose StatementEnd
