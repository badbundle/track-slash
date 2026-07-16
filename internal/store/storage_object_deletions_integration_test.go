package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/bradleymackey/track-slash/internal/store"
)

func TestStorageObjectDeletionLifecycle(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	first := mustCreateStorageObject(t, env, "projects/deletions/objects/1")
	if _, err := env.store.DeleteStorageObject(env.ctx, first.ID); err != nil {
		t.Fatalf("DeleteStorageObject first: %v", err)
	}
	if _, err := env.store.ClaimStorageObjectDeletions(env.ctx, 0, time.Minute); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("ClaimStorageObjectDeletions invalid limit err = %v, want ErrConflict", err)
	}
	if _, err := env.store.ClaimStorageObjectDeletions(env.ctx, 1, 0); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("ClaimStorageObjectDeletions invalid lease err = %v, want ErrConflict", err)
	}

	claimed, err := env.store.ClaimStorageObjectDeletions(env.ctx, 1, time.Minute)
	if err != nil {
		t.Fatalf("ClaimStorageObjectDeletions: %v", err)
	}
	if len(claimed) != 1 || claimed[0].StorageObjectID != first.ID ||
		claimed[0].Status != store.StorageObjectDeletionProcessing || claimed[0].AttemptCount != 1 ||
		claimed[0].NextAttemptAt != nil || claimed[0].LockedAt == nil {
		t.Fatalf("claimed = %+v", claimed)
	}
	if again, err := env.store.ClaimStorageObjectDeletions(env.ctx, 1, time.Minute); err != nil || len(again) != 0 {
		t.Fatalf("claim active lease = %+v, %v, want none", again, err)
	}

	nextAttempt := time.Now().Add(time.Hour)
	if err := env.store.RetryStorageObjectDeletion(env.ctx, first.ID, 1, nextAttempt, "temporary failure"); err != nil {
		t.Fatalf("RetryStorageObjectDeletion: %v", err)
	}
	retried := assertStorageObjectDeletion(t, env.store, env.ctx, first)
	if retried.Status != store.StorageObjectDeletionPending || retried.NextAttemptAt == nil ||
		retried.LockedAt != nil || retried.LastError != "temporary failure" {
		t.Fatalf("retried job = %+v", retried)
	}
	if early, err := env.store.ClaimStorageObjectDeletions(env.ctx, 1, time.Minute); err != nil || len(early) != 0 {
		t.Fatalf("claim before retry = %+v, %v, want none", early, err)
	}
	if _, err := env.pool.Exec(env.ctx, "UPDATE storage_object_deletions SET next_attempt_at = now() WHERE storage_object_id = $1", first.ID); err != nil {
		t.Fatalf("make retry due: %v", err)
	}
	claimed, err = env.store.ClaimStorageObjectDeletions(env.ctx, 1, time.Minute)
	if err != nil || len(claimed) != 1 || claimed[0].AttemptCount != 2 {
		t.Fatalf("claim retry = %+v, %v", claimed, err)
	}
	if err := env.store.FailStorageObjectDeletion(env.ctx, first.ID, 2, "terminal failure"); err != nil {
		t.Fatalf("FailStorageObjectDeletion: %v", err)
	}
	failed := assertStorageObjectDeletion(t, env.store, env.ctx, first)
	if failed.Status != store.StorageObjectDeletionFailed || failed.FailedAt == nil ||
		failed.NextAttemptAt != nil || failed.LockedAt != nil || failed.LastError != "terminal failure" {
		t.Fatalf("failed job = %+v", failed)
	}
	if afterFailure, err := env.store.ClaimStorageObjectDeletions(env.ctx, 1, time.Minute); err != nil || len(afterFailure) != 0 {
		t.Fatalf("claim failed job = %+v, %v, want none", afterFailure, err)
	}

	second := mustCreateStorageObject(t, env, "projects/deletions/objects/2")
	if _, err := env.store.DeleteStorageObject(env.ctx, second.ID); err != nil {
		t.Fatalf("DeleteStorageObject second: %v", err)
	}
	claimed, err = env.store.ClaimStorageObjectDeletions(env.ctx, 1, time.Minute)
	if err != nil || len(claimed) != 1 || claimed[0].StorageObjectID != second.ID || claimed[0].AttemptCount != 1 {
		t.Fatalf("claim second = %+v, %v", claimed, err)
	}
	if _, err := env.pool.Exec(env.ctx, "UPDATE storage_object_deletions SET locked_at = now() - interval '2 minutes' WHERE storage_object_id = $1", second.ID); err != nil {
		t.Fatalf("make lease stale: %v", err)
	}
	reclaimed, err := env.store.ClaimStorageObjectDeletions(env.ctx, 1, time.Minute)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].StorageObjectID != second.ID || reclaimed[0].AttemptCount != 2 {
		t.Fatalf("reclaim stale = %+v, %v", reclaimed, err)
	}
	if err := env.store.CompleteStorageObjectDeletion(env.ctx, second.ID, 1); err != nil {
		t.Fatalf("stale CompleteStorageObjectDeletion: %v", err)
	}
	if got := assertStorageObjectDeletion(t, env.store, env.ctx, second); got.AttemptCount != 2 {
		t.Fatalf("job after stale completion = %+v", got)
	}
	if err := env.store.CompleteStorageObjectDeletion(env.ctx, second.ID, 2); err != nil {
		t.Fatalf("CompleteStorageObjectDeletion: %v", err)
	}
	if _, err := env.store.GetStorageObjectDeletion(env.ctx, second.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("completed job err = %v, want ErrNotFound", err)
	}
	if err := env.store.CompleteStorageObjectDeletion(env.ctx, second.ID, 2); err != nil {
		t.Fatalf("idempotent completion: %v", err)
	}
}

func TestStorageObjectDeletionQueueIsTransactional(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	object := mustCreateStorageObject(t, env, "projects/deletions/objects/transactional")
	if _, err := env.pool.Exec(env.ctx, "CREATE FUNCTION fail_storage_deletion_enqueue() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'forced queue failure'; END; $$ LANGUAGE plpgsql"); err != nil {
		t.Fatalf("create failing queue function: %v", err)
	}
	if _, err := env.pool.Exec(env.ctx, "CREATE TRIGGER fail_storage_deletion_enqueue BEFORE INSERT ON storage_object_deletions FOR EACH ROW EXECUTE FUNCTION fail_storage_deletion_enqueue()"); err != nil {
		t.Fatalf("create failing queue trigger: %v", err)
	}

	if _, err := env.store.DeleteStorageObject(env.ctx, object.ID); err == nil {
		t.Fatal("DeleteStorageObject err = nil, want queue failure")
	}
	got, err := env.store.GetStorageObjectByProjectNumber(env.ctx, env.projectID, object.Number)
	if err != nil || got.ID != object.ID || got.DeletedAt != nil {
		t.Fatalf("object after rolled-back delete = %+v, %v", got, err)
	}
	if _, err := env.store.GetStorageObjectDeletion(env.ctx, object.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deletion job after rollback err = %v, want ErrNotFound", err)
	}
}
