-- +goose Up
-- +goose StatementBegin
ALTER TABLE projects
    ADD COLUMN next_object_number INT NOT NULL DEFAULT 1;

CREATE TABLE storage_objects (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    number        INT NOT NULL,
    backend       TEXT NOT NULL CHECK (length(backend) BETWEEN 1 AND 100),
    bucket        TEXT NOT NULL CHECK (length(bucket) BETWEEN 1 AND 255),
    object_key    TEXT NOT NULL CHECK (length(object_key) BETWEEN 1 AND 1024),
    filename      TEXT NOT NULL CHECK (length(filename) BETWEEN 1 AND 255),
    content_type  TEXT NOT NULL CHECK (length(content_type) BETWEEN 1 AND 255),
    byte_size     BIGINT NOT NULL CHECK (byte_size >= 0),
    sha256        TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    created_by_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ,
    CONSTRAINT storage_objects_project_number_key UNIQUE (project_id, number),
    CONSTRAINT storage_objects_backend_bucket_key UNIQUE (backend, bucket, object_key)
);

CREATE INDEX storage_objects_project_number
    ON storage_objects(project_id, number)
    WHERE deleted_at IS NULL;

CREATE INDEX storage_objects_project_created
    ON storage_objects(project_id, created_at, id)
    WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS storage_objects;

ALTER TABLE projects
    DROP COLUMN IF EXISTS next_object_number;
-- +goose StatementEnd
