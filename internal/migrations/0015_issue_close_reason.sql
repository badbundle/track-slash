-- +goose Up
-- +goose StatementBegin
CREATE TYPE issue_close_reason AS ENUM ('duplicate', 'wont_do', 'invalid');

ALTER TABLE issues
    ADD COLUMN close_reason issue_close_reason;

UPDATE issues i
SET close_reason = CASE
    WHEN EXISTS (
        SELECT 1
        FROM issue_links il
        WHERE il.source_id = i.id
          AND il.link_type = 'duplicates'
    ) THEN 'duplicate'::issue_close_reason
    ELSE 'wont_do'::issue_close_reason
END
WHERE i.status = 'closed';

ALTER TABLE issues
    ADD CONSTRAINT issues_close_reason_status_check
    CHECK (
        (status = 'closed' AND close_reason IS NOT NULL)
        OR
        (status <> 'closed' AND close_reason IS NULL)
    );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE issues
    DROP CONSTRAINT IF EXISTS issues_close_reason_status_check;

ALTER TABLE issues
    DROP COLUMN IF EXISTS close_reason;

DROP TYPE IF EXISTS issue_close_reason;
-- +goose StatementEnd
