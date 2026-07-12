-- +goose Up
-- +goose StatementBegin
ALTER TABLE projects
    ADD COLUMN image_object_id UUID REFERENCES storage_objects(id) ON DELETE SET NULL,
    ADD COLUMN image_thumbnail_object_id UUID REFERENCES storage_objects(id) ON DELETE SET NULL,
    ADD CONSTRAINT projects_image_pair_check CHECK (
        (image_object_id IS NULL AND image_thumbnail_object_id IS NULL)
        OR
        (image_object_id IS NOT NULL AND image_thumbnail_object_id IS NOT NULL)
    );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE projects
    DROP CONSTRAINT IF EXISTS projects_image_pair_check,
    DROP COLUMN IF EXISTS image_thumbnail_object_id,
    DROP COLUMN IF EXISTS image_object_id;
-- +goose StatementEnd
