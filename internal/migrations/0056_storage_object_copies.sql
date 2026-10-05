-- +goose Up
-- +goose StatementBegin
-- A repeating issue's next repetition gets its own copy of each attachment its
-- description refers to. A copy is a storage_objects row of its own (its own
-- object-N, attachment link and deletion) that shares the original's backend
-- bytes, so creating one does no storage I/O and can't fail a completion.
-- copied_from_id names the row it was copied from. Backend bytes are deleted
-- only once no live row uses them.
ALTER TABLE storage_objects
    ADD COLUMN copied_from_id uuid REFERENCES storage_objects(id) ON DELETE SET NULL,
    DROP CONSTRAINT storage_objects_backend_bucket_key;
-- +goose StatementEnd

-- +goose StatementBegin
-- Uploads still get a key of their own; only copies share one.
CREATE UNIQUE INDEX storage_objects_backend_bucket_key
    ON storage_objects (backend, bucket, object_key)
    WHERE copied_from_id IS NULL;
CREATE INDEX storage_objects_live_key
    ON storage_objects (backend, bucket, object_key)
    WHERE deleted_at IS NULL;
CREATE INDEX storage_objects_copied_from
    ON storage_objects (copied_from_id)
    WHERE copied_from_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS storage_objects_copied_from;
DROP INDEX IF EXISTS storage_objects_live_key;
DROP INDEX IF EXISTS storage_objects_backend_bucket_key;
-- Copies can't keep a key of their own without their bytes, so they go, with
-- their attachment links. A deleted original whose deletion was kept because
-- a copy used its bytes gets its deletion queued again, so the bytes go too.
INSERT INTO storage_object_deletions (storage_object_id, backend, bucket, object_key, next_attempt_at)
SELECT o.id, o.backend, o.bucket, o.object_key, now()
FROM storage_objects o
WHERE o.copied_from_id IS NULL
  AND o.deleted_at IS NOT NULL
  AND EXISTS (
      SELECT 1 FROM storage_objects c
      WHERE c.copied_from_id IS NOT NULL
        AND c.backend = o.backend AND c.bucket = o.bucket AND c.object_key = o.object_key
  )
ON CONFLICT (storage_object_id) DO NOTHING;
DELETE FROM storage_objects WHERE copied_from_id IS NOT NULL;
ALTER TABLE storage_objects
    DROP COLUMN IF EXISTS copied_from_id,
    ADD CONSTRAINT storage_objects_backend_bucket_key UNIQUE (backend, bucket, object_key);
-- +goose StatementEnd
