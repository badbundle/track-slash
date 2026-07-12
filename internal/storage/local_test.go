package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestLocalBackendPutOpenDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backend, err := NewLocalBackend(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBackend: %v", err)
	}
	info, err := backend.Put(ctx, "projects/p1/objects/o1", strings.NewReader("hello"), 10)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	wantHash := sha256.Sum256([]byte("hello"))
	if info.Size != 5 || info.SHA256 != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("info = %+v", info)
	}
	rc, err := backend.Open(ctx, "projects/p1/objects/o1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("body = %q, want hello", body)
	}
	if err := backend.Delete(ctx, "projects/p1/objects/o1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := backend.Open(ctx, "projects/p1/objects/o1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open deleted err = %v, want ErrNotFound", err)
	}
}

func TestLocalBackendValidation(t *testing.T) {
	t.Parallel()
	if _, err := NewLocalBackend(""); err == nil {
		t.Fatal("NewLocalBackend empty err = nil, want error")
	}
	backend, err := NewLocalBackend(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBackend: %v", err)
	}
	for _, key := range []string{"", "/absolute", "../escape", "projects\\bad"} {
		if _, err := backend.Put(context.Background(), key, strings.NewReader("x"), 10); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Put key %q err = %v, want ErrInvalidKey", key, err)
		}
	}
}

func TestLocalBackendOpenDeleteErrors(t *testing.T) {
	t.Parallel()
	backend, err := NewLocalBackend(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBackend: %v", err)
	}
	if _, err := backend.Open(context.Background(), "../bad"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Open invalid key err = %v, want ErrInvalidKey", err)
	}
	if err := backend.Delete(context.Background(), "../bad"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Delete invalid key err = %v, want ErrInvalidKey", err)
	}
	if err := backend.Delete(context.Background(), "projects/p1/objects/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete missing err = %v, want ErrNotFound", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := backend.Open(ctx, "projects/p1/objects/missing"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open canceled err = %v, want context.Canceled", err)
	}
	if err := backend.Delete(ctx, "projects/p1/objects/missing"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete canceled err = %v, want context.Canceled", err)
	}
	if _, err := backend.Put(context.Background(), "projects/p1/objects/o1", strings.NewReader("x"), 0); err == nil {
		t.Fatal("Put max 0 err = nil, want error")
	}
}

func TestLocalBackendTooLargeRemovesTemp(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	backend, err := NewLocalBackend(root)
	if err != nil {
		t.Fatalf("NewLocalBackend: %v", err)
	}
	if _, err := backend.Put(context.Background(), "projects/p1/objects/large", strings.NewReader("toolong"), 3); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Put too large err = %v, want ErrTooLarge", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "projects", "p1", "objects"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries after failed put = %v, want empty", entries)
	}
}

func TestLocalBackendReadErrorsRemoveTemp(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	backend, err := NewLocalBackend(root)
	if err != nil {
		t.Fatalf("NewLocalBackend: %v", err)
	}
	if _, err := backend.Put(context.Background(), "projects/p1/objects/read-error", errReader{}, 10); err == nil || !strings.Contains(err.Error(), "read storage source") {
		t.Fatalf("Put read error = %v, want wrapped read error", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "projects", "p1", "objects"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries after read error = %v, want empty", entries)
	}
}

func TestLocalBackendPreventsOverwrite(t *testing.T) {
	t.Parallel()
	backend, err := NewLocalBackend(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBackend: %v", err)
	}
	key := "projects/p1/objects/o1"
	if _, err := backend.Put(context.Background(), key, strings.NewReader("first"), 10); err != nil {
		t.Fatalf("Put first: %v", err)
	}
	if _, err := backend.Put(context.Background(), key, strings.NewReader("second"), 10); !errors.Is(err, ErrExists) {
		t.Fatalf("Put overwrite err = %v, want ErrExists", err)
	}
	rc, err := backend.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	_ = rc.Close()
	if string(body) != "first" {
		t.Fatalf("body = %q, want first", body)
	}
}

type errReader struct{}

func (errReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("boom")
}

func TestServiceGeneratesObjectKeys(t *testing.T) {
	t.Parallel()
	svc, err := NewLocalService(t.TempDir(), "local", 10)
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}
	if svc.MaxUploadBytes() != 10 {
		t.Fatalf("MaxUploadBytes = %d, want 10", svc.MaxUploadBytes())
	}
	projectID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	objectID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	stored, err := svc.Put(context.Background(), projectID, objectID, strings.NewReader("hi"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if stored.Backend != "local" || stored.Bucket != "local" || stored.ObjectKey != "projects/11111111-1111-1111-1111-111111111111/objects/22222222-2222-2222-2222-222222222222" {
		t.Fatalf("stored = %+v", stored)
	}
	rc, err := svc.Open(context.Background(), stored.ObjectKey)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	_ = rc.Close()
	if string(body) != "hi" {
		t.Fatalf("body = %q, want hi", body)
	}

	userID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	profileObjectID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	profileStored, err := svc.PutUserProfileImage(context.Background(), userID, profileObjectID, "thumbnail", strings.NewReader("avatar"))
	if err != nil {
		t.Fatalf("PutUserProfileImage: %v", err)
	}
	if profileStored.ObjectKey != "users/33333333-3333-3333-3333-333333333333/profile-images/44444444-4444-4444-4444-444444444444/thumbnail" {
		t.Fatalf("profile object key = %q", profileStored.ObjectKey)
	}
	rc, err = svc.Open(context.Background(), profileStored.ObjectKey)
	if err != nil {
		t.Fatalf("Open profile: %v", err)
	}
	body, err = io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll profile: %v", err)
	}
	_ = rc.Close()
	if string(body) != "avatar" {
		t.Fatalf("profile body = %q, want avatar", body)
	}
	projectImageID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	projectImageStored, err := svc.PutProjectImage(context.Background(), projectID, projectImageID, "thumbnail", strings.NewReader("icon"))
	if err != nil {
		t.Fatalf("PutProjectImage: %v", err)
	}
	if projectImageStored.ObjectKey != "projects/11111111-1111-1111-1111-111111111111/images/55555555-5555-5555-5555-555555555555/thumbnail" {
		t.Fatalf("project image object key = %q", projectImageStored.ObjectKey)
	}
	if err := svc.Delete(context.Background(), stored.ObjectKey); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := svc.Delete(context.Background(), profileStored.ObjectKey); err != nil {
		t.Fatalf("Delete profile: %v", err)
	}
}

func TestServiceValidation(t *testing.T) {
	t.Parallel()
	backend, err := NewLocalBackend(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBackend: %v", err)
	}
	for _, tc := range []struct {
		name    string
		backend string
		bucket  string
		max     int64
		impl    Backend
	}{
		{name: "backend", backend: "", bucket: "local", max: 1, impl: backend},
		{name: "bucket", backend: "local", bucket: "", max: 1, impl: backend},
		{name: "max", backend: "local", bucket: "local", max: 0, impl: backend},
		{name: "impl", backend: "local", bucket: "local", max: 1, impl: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewService(tc.backend, tc.bucket, tc.max, tc.impl); err == nil {
				t.Fatal("NewService err = nil, want error")
			}
		})
	}
}
