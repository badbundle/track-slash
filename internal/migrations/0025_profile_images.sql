-- +goose Up
-- +goose StatementBegin
ALTER TABLE storage_objects
    ADD COLUMN owner_user_id UUID REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE storage_objects
    ALTER COLUMN project_id DROP NOT NULL;

ALTER TABLE storage_objects
    ADD CONSTRAINT storage_objects_scope_check CHECK (
        (project_id IS NOT NULL AND owner_user_id IS NULL AND number > 0)
        OR
        (project_id IS NULL AND owner_user_id IS NOT NULL AND number = 0)
    );

CREATE INDEX storage_objects_owner_user_created
    ON storage_objects(owner_user_id, created_at, id)
    WHERE deleted_at IS NULL AND owner_user_id IS NOT NULL;

ALTER TABLE users
    ADD COLUMN profile_image_object_id UUID REFERENCES storage_objects(id) ON DELETE SET NULL,
    ADD COLUMN profile_image_thumbnail_object_id UUID REFERENCES storage_objects(id) ON DELETE SET NULL,
    ADD CONSTRAINT users_profile_image_pair_check CHECK (
        (profile_image_object_id IS NULL AND profile_image_thumbnail_object_id IS NULL)
        OR
        (profile_image_object_id IS NOT NULL AND profile_image_thumbnail_object_id IS NOT NULL)
    );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_profile_image_pair_check,
    DROP COLUMN IF EXISTS profile_image_thumbnail_object_id,
    DROP COLUMN IF EXISTS profile_image_object_id;

DROP INDEX IF EXISTS storage_objects_owner_user_created;

ALTER TABLE storage_objects
    DROP CONSTRAINT IF EXISTS storage_objects_scope_check;

DELETE FROM storage_objects
WHERE project_id IS NULL;

ALTER TABLE storage_objects
    ALTER COLUMN project_id SET NOT NULL,
    DROP COLUMN IF EXISTS owner_user_id;
-- +goose StatementEnd
