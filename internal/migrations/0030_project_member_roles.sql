-- +goose Up
-- +goose StatementBegin
CREATE TYPE project_member_role AS ENUM ('member', 'readonly');

ALTER TABLE project_members
    ADD COLUMN role project_member_role NOT NULL DEFAULT 'member';

INSERT INTO project_members (project_id, user_id, role)
SELECT p.id, p.owner_id, 'member'
FROM projects p
JOIN users u ON u.id = p.owner_id AND u.deleted_at IS NULL
ON CONFLICT (project_id, user_id) DO UPDATE SET role = 'member';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE project_members DROP COLUMN role;
DROP TYPE project_member_role;
-- +goose StatementEnd
