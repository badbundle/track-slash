-- +goose Up
-- +goose StatementBegin
ALTER TABLE sprints
    ADD COLUMN planned_order BIGINT;

WITH ranked AS (
    SELECT
        id,
        row_number() OVER (
            PARTITION BY project_id
            ORDER BY start_date ASC, created_at ASC, id ASC
        ) AS planned_order
    FROM sprints
    WHERE status = 'planned' AND deleted_at IS NULL
)
UPDATE sprints
SET planned_order = ranked.planned_order
FROM ranked
WHERE sprints.id = ranked.id;

CREATE UNIQUE INDEX uq_sprints_project_planned_order
    ON sprints(project_id, planned_order)
    WHERE status = 'planned' AND deleted_at IS NULL;
CREATE INDEX idx_sprints_project_planned_order
    ON sprints(project_id, planned_order, id)
    WHERE status = 'planned' AND deleted_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_sprints_project_planned_order;
DROP INDEX IF EXISTS uq_sprints_project_planned_order;
ALTER TABLE sprints
    DROP COLUMN IF EXISTS planned_order;
-- +goose StatementEnd
