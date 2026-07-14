-- +goose Up
-- +goose StatementBegin
CREATE TABLE sprint_issue_snapshots (
    project_id     UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    sprint_id      UUID NOT NULL,
    issue_id       UUID NOT NULL,
    snapshotted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sprint_issue_snapshots_pkey PRIMARY KEY (sprint_id, issue_id),
    CONSTRAINT sprint_issue_snapshots_sprint_project_fk
        FOREIGN KEY (project_id, sprint_id) REFERENCES sprints(project_id, id) ON DELETE CASCADE,
    CONSTRAINT sprint_issue_snapshots_issue_project_fk
        FOREIGN KEY (project_id, issue_id) REFERENCES issues(project_id, id) ON DELETE CASCADE
);

CREATE INDEX sprint_issue_snapshots_project_sprint
    ON sprint_issue_snapshots(project_id, sprint_id);

INSERT INTO sprint_issue_snapshots (project_id, sprint_id, issue_id, snapshotted_at)
SELECT s.project_id, s.id, i.id, COALESCE(s.completed_at, s.updated_at)
FROM sprints s
JOIN issues i ON i.sprint_id = s.id AND i.project_id = s.project_id
WHERE s.status = 'completed' AND s.deleted_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS sprint_issue_snapshots;
-- +goose StatementEnd
