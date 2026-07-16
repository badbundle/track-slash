-- +goose Up
-- +goose StatementBegin
CREATE TABLE storage_object_deletions (
    storage_object_id UUID PRIMARY KEY REFERENCES storage_objects(id) ON DELETE CASCADE,
    backend           TEXT NOT NULL,
    bucket            TEXT NOT NULL,
    object_key        TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'pending',
    attempt_count     INTEGER NOT NULL DEFAULT 0,
    next_attempt_at   TIMESTAMPTZ,
    locked_at         TIMESTAMPTZ,
    last_error        TEXT NOT NULL DEFAULT '',
    failed_at         TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT storage_object_deletions_status_check
        CHECK (status IN ('pending', 'processing', 'failed')),
    CONSTRAINT storage_object_deletions_attempt_count_check
        CHECK (attempt_count >= 0),
    CONSTRAINT storage_object_deletions_state_check CHECK (
        (status = 'pending' AND next_attempt_at IS NOT NULL AND locked_at IS NULL AND failed_at IS NULL)
        OR (status = 'processing' AND next_attempt_at IS NULL AND locked_at IS NOT NULL AND failed_at IS NULL)
        OR (status = 'failed' AND next_attempt_at IS NULL AND locked_at IS NULL AND failed_at IS NOT NULL)
    )
);

CREATE INDEX storage_object_deletions_pending
    ON storage_object_deletions(next_attempt_at, created_at)
    WHERE status = 'pending';

CREATE INDEX storage_object_deletions_processing
    ON storage_object_deletions(locked_at)
    WHERE status = 'processing';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION track_enqueue_storage_object_deletion() RETURNS trigger AS $$
BEGIN
    INSERT INTO storage_object_deletions (
        storage_object_id, backend, bucket, object_key, status, attempt_count,
        next_attempt_at, locked_at, last_error, failed_at, created_at, updated_at
    )
    VALUES (
        NEW.id, NEW.backend, NEW.bucket, NEW.object_key, 'pending', 0,
        now(), NULL, '', NULL, now(), now()
    )
    ON CONFLICT (storage_object_id) DO UPDATE
    SET backend = EXCLUDED.backend,
        bucket = EXCLUDED.bucket,
        object_key = EXCLUDED.object_key,
        status = 'pending',
        attempt_count = 0,
        next_attempt_at = now(),
        locked_at = NULL,
        last_error = '',
        failed_at = NULL,
        updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO storage_object_deletions (
    storage_object_id, backend, bucket, object_key, status, attempt_count,
    next_attempt_at, locked_at, last_error, failed_at, created_at, updated_at
)
SELECT id, backend, bucket, object_key, 'pending', 0,
       COALESCE(deleted_at, now()), NULL, '', NULL,
       COALESCE(deleted_at, now()), now()
FROM storage_objects
WHERE deleted_at IS NOT NULL
ON CONFLICT (storage_object_id) DO NOTHING;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER storage_objects_enqueue_deletion
AFTER UPDATE OF deleted_at ON storage_objects
FOR EACH ROW
WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
EXECUTE FUNCTION track_enqueue_storage_object_deletion();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS storage_objects_enqueue_deletion ON storage_objects;
DROP FUNCTION IF EXISTS track_enqueue_storage_object_deletion();
DROP TABLE IF EXISTS storage_object_deletions;
-- +goose StatementEnd
