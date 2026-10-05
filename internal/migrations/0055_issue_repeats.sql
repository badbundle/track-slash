-- +goose Up
-- +goose StatementBegin
-- A repeating series: the schedule its issues follow. Only one issue in a
-- series is live at a time (issues.repeat_current); completing it creates the
-- next one.
CREATE TABLE issue_repeats (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    rule       text NOT NULL CHECK (char_length(rule) BETWEEN 1 AND 200),
    time_zone  text NOT NULL CHECK (char_length(time_zone) BETWEEN 1 AND 64),
    starts_on  date NOT NULL,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
-- repeat_occurrence is the repetition's date in the schedule, kept apart from
-- its due date so moving the due date doesn't move the schedule. The skipped
-- columns record the repetitions skipped just before this one, because the
-- previous repetition was completed after their dates.
ALTER TABLE issues
    ADD COLUMN repeat_id            uuid REFERENCES issue_repeats(id),
    ADD COLUMN repeat_current       boolean NOT NULL DEFAULT false,
    ADD COLUMN repeat_occurrence    date,
    ADD COLUMN repeat_skipped_count integer NOT NULL DEFAULT 0 CHECK (repeat_skipped_count >= 0),
    ADD COLUMN repeat_skipped_dates date[] NOT NULL DEFAULT '{}',
    ADD CONSTRAINT issues_repeat_shape CHECK (
        (repeat_id IS NULL AND NOT repeat_current AND repeat_occurrence IS NULL)
        OR (repeat_id IS NOT NULL AND repeat_occurrence IS NOT NULL)
    );
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX issues_repeat_current_idx ON issues (repeat_id) WHERE repeat_current;
CREATE INDEX issues_repeat_number_idx ON issues (repeat_id, number) WHERE repeat_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE issues
    DROP CONSTRAINT IF EXISTS issues_repeat_shape,
    DROP COLUMN IF EXISTS repeat_skipped_dates,
    DROP COLUMN IF EXISTS repeat_skipped_count,
    DROP COLUMN IF EXISTS repeat_occurrence,
    DROP COLUMN IF EXISTS repeat_current,
    DROP COLUMN IF EXISTS repeat_id;
DROP TABLE IF EXISTS issue_repeats;
-- +goose StatementEnd
