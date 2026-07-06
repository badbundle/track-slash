-- +goose Up
-- +goose StatementBegin
ALTER TABLE sprints
    DROP CONSTRAINT IF EXISTS sprints_date_order;

ALTER TABLE sprints
    ALTER COLUMN start_date DROP NOT NULL,
    ALTER COLUMN end_date DROP NOT NULL;

ALTER TABLE sprints
    ADD CONSTRAINT sprints_date_order CHECK (
        (start_date IS NULL AND end_date IS NULL)
        OR (start_date IS NOT NULL AND end_date IS NOT NULL AND end_date >= start_date)
    );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sprints
    DROP CONSTRAINT IF EXISTS sprints_date_order;

UPDATE sprints
SET
    start_date = COALESCE(start_date, end_date, created_at::date),
    end_date = COALESCE(end_date, start_date, created_at::date)
WHERE start_date IS NULL OR end_date IS NULL;

ALTER TABLE sprints
    ALTER COLUMN start_date SET NOT NULL,
    ALTER COLUMN end_date SET NOT NULL;

ALTER TABLE sprints
    ADD CONSTRAINT sprints_date_order CHECK (end_date >= start_date);
-- +goose StatementEnd
