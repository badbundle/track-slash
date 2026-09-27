-- +goose Up
-- +goose StatementBegin
-- The issues each user opened most recently, one row per user and issue, for
-- the sidebar's Recents list. Opening an issue again bumps its viewed_at, and
-- the store trims each user's history so the table stays small.
CREATE TABLE recent_issue_views (
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    issue_id  UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    viewed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, issue_id)
);

CREATE INDEX recent_issue_views_user_viewed
    ON recent_issue_views(user_id, viewed_at DESC, issue_id);

CREATE INDEX recent_issue_views_issue
    ON recent_issue_views(issue_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS recent_issue_views;
-- +goose StatementEnd
