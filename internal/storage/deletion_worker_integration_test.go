package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/bradleymackey/track-slash/internal/testutil"
)

type deletionWorkerTestEnv struct {
	ctx       context.Context
	pool      *pgxpool.Pool
	store     *store.Store
	projectID uuid.UUID
	userID    uuid.UUID
}

func newDeletionWorkerTestEnv(t *testing.T) *deletionWorkerTestEnv {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	db := testutil.NewMigratedDatabase(t)
	st := store.New(db.Pool)
	suffix := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))
	user, err := st.CreateOrUpdateAdminUser(ctx, "worker-"+suffix+"@example.com", "Worker")
	if err != nil {
		t.Fatalf("CreateOrUpdateAdminUser: %v", err)
	}
	project, err := st.CreateProjectForUser(ctx, user.ID, "W"+suffix[:9], "worker-test", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser: %v", err)
	}
	return &deletionWorkerTestEnv{ctx: ctx, pool: db.Pool, store: st, projectID: project.ID, userID: user.ID}
}

func (e *deletionWorkerTestEnv) queue(t *testing.T, backend, bucket, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	object, err := e.store.CreateStorageObject(e.ctx, store.CreateStorageObjectParams{
		ID: id, ProjectID: e.projectID, Backend: backend, Bucket: bucket, ObjectKey: key,
		Filename: "delete.bin", ContentType: "application/octet-stream", ByteSize: 1,
		SHA256: strings.Repeat("a", 64), CreatedByID: e.userID,
	})
	if err != nil {
		t.Fatalf("CreateStorageObject: %v", err)
	}
	if _, err := e.store.DeleteStorageObject(e.ctx, object.ID); err != nil {
		t.Fatalf("DeleteStorageObject: %v", err)
	}
	return object.ID
}

type deletionWorkerBackend struct {
	delete func(context.Context, string) error
}

func (b *deletionWorkerBackend) Put(context.Context, string, io.Reader, int64) (WrittenObject, error) {
	return WrittenObject{}, errors.New("unexpected Put call")
}

func (b *deletionWorkerBackend) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, ErrNotFound
}

func (b *deletionWorkerBackend) Delete(ctx context.Context, key string) error {
	if b.delete == nil {
		return nil
	}
	return b.delete(ctx, key)
}

func newDeletionWorkerService(t *testing.T, backend Backend) *Service {
	t.Helper()
	service, err := NewService("local", "local", 1024, backend)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

func TestDeletionWorkerRunOnceCompletesDeletedAndMissingObjects(t *testing.T) {
	t.Parallel()
	env := newDeletionWorkerTestEnv(t)
	deletedID := env.queue(t, "local", "local", "objects/deleted")
	missingID := env.queue(t, "local", "local", "objects/missing")
	service := newDeletionWorkerService(t, &deletionWorkerBackend{delete: func(_ context.Context, key string) error {
		if key == "objects/missing" {
			return ErrNotFound
		}
		return nil
	}})
	worker := NewDeletionWorker(env.store, service, DeletionWorkerOptions{})

	result, err := worker.RunOnce(env.ctx)
	if err != nil || result != (DeletionRunResult{Claimed: 2, Deleted: 2}) {
		t.Fatalf("RunOnce = %+v, %v", result, err)
	}
	for _, id := range []uuid.UUID{deletedID, missingID} {
		if _, err := env.store.GetStorageObjectDeletion(env.ctx, id); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("completed job %s err = %v, want ErrNotFound", id, err)
		}
	}
	if result, err := worker.RunOnce(env.ctx); err != nil || result != (DeletionRunResult{}) {
		t.Fatalf("empty RunOnce = %+v, %v", result, err)
	}
}

func TestDeletionWorkerRunOnceRetriesThenRecordsTerminalFailure(t *testing.T) {
	t.Parallel()
	env := newDeletionWorkerTestEnv(t)
	id := env.queue(t, "local", "local", "objects/failing")
	service := newDeletionWorkerService(t, &deletionWorkerBackend{delete: func(context.Context, string) error {
		return errors.New("delete failed")
	}})
	var logs bytes.Buffer
	worker := NewDeletionWorker(env.store, service, DeletionWorkerOptions{
		MaxAttempts: 2,
		MinBackoff:  time.Nanosecond,
		MaxBackoff:  time.Nanosecond,
		Logger:      log.New(&logs, "", 0),
	})

	first, err := worker.RunOnce(env.ctx)
	if err != nil || first != (DeletionRunResult{Claimed: 1, Retried: 1}) {
		t.Fatalf("first RunOnce = %+v, %v", first, err)
	}
	retried, err := env.store.GetStorageObjectDeletion(env.ctx, id)
	if err != nil || retried.Status != store.StorageObjectDeletionPending || retried.AttemptCount != 1 ||
		retried.NextAttemptAt == nil || retried.LastError != "delete failed" {
		t.Fatalf("retried job = %+v, %v", retried, err)
	}
	if _, err := env.pool.Exec(env.ctx, "UPDATE storage_object_deletions SET next_attempt_at = now() WHERE storage_object_id = $1", id); err != nil {
		t.Fatalf("make retry due: %v", err)
	}
	second, err := worker.RunOnce(env.ctx)
	if err != nil || second != (DeletionRunResult{Claimed: 1, Failed: 1}) {
		t.Fatalf("second RunOnce = %+v, %v", second, err)
	}
	failed, err := env.store.GetStorageObjectDeletion(env.ctx, id)
	if err != nil || failed.Status != store.StorageObjectDeletionFailed || failed.AttemptCount != 2 ||
		failed.FailedAt == nil || failed.LastError != "delete failed" {
		t.Fatalf("failed job = %+v, %v", failed, err)
	}
	if !strings.Contains(logs.String(), "storage deletion terminal failure") || !strings.Contains(logs.String(), id.String()) {
		t.Fatalf("terminal log = %q", logs.String())
	}
}

func TestDeletionWorkerRunOnceRejectsMismatchedStorageLocator(t *testing.T) {
	t.Parallel()
	env := newDeletionWorkerTestEnv(t)
	id := env.queue(t, "retired", "old-bucket", "objects/retired")
	worker := NewDeletionWorker(env.store, newDeletionWorkerService(t, &deletionWorkerBackend{}), DeletionWorkerOptions{
		Logger: log.New(io.Discard, "", 0),
	})

	result, err := worker.RunOnce(env.ctx)
	if err != nil || result != (DeletionRunResult{Claimed: 1, Failed: 1}) {
		t.Fatalf("RunOnce = %+v, %v", result, err)
	}
	failed, err := env.store.GetStorageObjectDeletion(env.ctx, id)
	if err != nil || failed.Status != store.StorageObjectDeletionFailed ||
		!strings.Contains(failed.LastError, "job requires retired/old-bucket") {
		t.Fatalf("failed locator job = %+v, %v", failed, err)
	}
}

func TestDeletionWorkerRunOnceReportsStoreErrors(t *testing.T) {
	t.Run("claim", func(t *testing.T) {
		env := newDeletionWorkerTestEnv(t)
		service := newDeletionWorkerService(t, &deletionWorkerBackend{})
		ctx, cancel := context.WithCancel(env.ctx)
		cancel()
		if _, err := NewDeletionWorker(env.store, service, DeletionWorkerOptions{}).RunOnce(ctx); err == nil || !strings.Contains(err.Error(), "claim jobs") {
			t.Fatalf("RunOnce error = %v, want claim jobs", err)
		}
	})

	for _, tc := range []struct {
		name        string
		deleteError error
		maxAttempts int
		want        string
	}{
		{name: "complete", want: "complete job"},
		{name: "retry", deleteError: errors.New("temporary"), maxAttempts: 2, want: "retry job"},
		{name: "fail", deleteError: errors.New("terminal"), maxAttempts: 1, want: "fail job"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newDeletionWorkerTestEnv(t)
			env.queue(t, "local", "local", "objects/"+tc.name)
			ctx, cancel := context.WithCancel(env.ctx)
			service := newDeletionWorkerService(t, &deletionWorkerBackend{delete: func(context.Context, string) error {
				cancel()
				return tc.deleteError
			}})
			worker := NewDeletionWorker(env.store, service, DeletionWorkerOptions{
				MaxAttempts: tc.maxAttempts,
				Logger:      log.New(io.Discard, "", 0),
			})
			if _, err := worker.RunOnce(ctx); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("RunOnce error = %v, want %q", err, tc.want)
			}
		})
	}
}
