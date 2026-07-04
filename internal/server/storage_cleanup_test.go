package server

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"

	objectstorage "github.com/bradleymackey/track-slash/internal/storage"
)

type recordingStorageBackend struct {
	mu        sync.Mutex
	deleteKey string
	deleteErr error
}

func (b *recordingStorageBackend) Put(context.Context, string, io.Reader, int64) (objectstorage.WrittenObject, error) {
	return objectstorage.WrittenObject{Size: 1, SHA256: "sha"}, nil
}

func (b *recordingStorageBackend) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (b *recordingStorageBackend) Delete(ctx context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deleteKey = key
	b.deleteErr = ctx.Err()
	return b.deleteErr
}

func TestStorageCleanupContextIgnoresParentCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()

	ctx, cancelCleanup := storageCleanupContext(parent)
	defer cancelCleanup()

	if err := ctx.Err(); err != nil {
		t.Fatalf("cleanup ctx err = %v, want nil", err)
	}
	if _, ok := ctx.Deadline(); !ok {
		t.Fatalf("cleanup ctx missing deadline")
	}
}

func TestDeleteStorageBackendObjectUsesCleanupContext(t *testing.T) {
	backend := &recordingStorageBackend{}
	service, err := objectstorage.NewService("test", "bucket", 1024, backend)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	s := &Server{objectStorage: service}
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()

	if err := s.deleteStorageBackendObject(parent, "objects/1"); err != nil {
		t.Fatalf("deleteStorageBackendObject: %v", err)
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.deleteKey != "objects/1" {
		t.Fatalf("delete key = %q, want objects/1", backend.deleteKey)
	}
	if backend.deleteErr != nil {
		t.Fatalf("backend delete ctx err = %v, want nil", backend.deleteErr)
	}
}
