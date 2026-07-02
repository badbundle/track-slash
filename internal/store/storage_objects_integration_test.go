package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func storageObjectParams(projectID, userID uuid.UUID, key string) store.CreateStorageObjectParams {
	return store.CreateStorageObjectParams{
		ID:          uuid.New(),
		ProjectID:   projectID,
		Backend:     "local",
		Bucket:      "local",
		ObjectKey:   key,
		Filename:    "image.png",
		ContentType: "image/png",
		ByteSize:    12,
		SHA256:      strings.Repeat("a", 64),
		CreatedByID: userID,
	}
}

func mustCreateStorageObject(t *testing.T, env *sprintsTestEnv, key string) model.StorageObject {
	t.Helper()
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	object, err := env.store.CreateStorageObject(env.ctx, storageObjectParams(env.projectID, project.OwnerID, key))
	if err != nil {
		t.Fatalf("CreateStorageObject: %v", err)
	}
	return object
}

func TestStorageObjectsCRUDAndPagination(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}

	first := mustCreateStorageObject(t, env, "projects/a/objects/1")
	second := mustCreateStorageObject(t, env, "projects/a/objects/2")
	if first.Ref != "object-1" || second.Ref != "object-2" {
		t.Fatalf("refs = %q %q, want object-1/object-2", first.Ref, second.Ref)
	}
	if first.ProjectID != env.projectID || first.CreatedByID != project.OwnerID || first.DeletedAt != nil {
		t.Fatalf("first object = %+v", first)
	}

	byNumber, err := env.store.GetStorageObjectByProjectNumber(env.ctx, env.projectID, first.Number)
	if err != nil {
		t.Fatalf("GetStorageObjectByProjectNumber: %v", err)
	}
	if byNumber.ID != first.ID || byNumber.Ref != first.Ref {
		t.Fatalf("byNumber = %+v, want first", byNumber)
	}
	if _, err := env.store.GetStorageObjectByProjectNumber(env.ctx, env.projectID, 9999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetStorageObjectByProjectNumber missing err = %v, want ErrNotFound", err)
	}

	page1, more, err := env.store.ListStorageObjects(env.ctx, store.ListStorageObjectsParams{
		ProjectID: env.projectID,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("ListStorageObjects page1: %v", err)
	}
	if len(page1) != 1 || !more || page1[0].ID != first.ID {
		t.Fatalf("page1 = %+v more=%v", page1, more)
	}
	page2, more, err := env.store.ListStorageObjects(env.ctx, store.ListStorageObjectsParams{
		ProjectID: env.projectID,
		Cursor:    &store.StorageObjectsCursor{Number: page1[0].Number},
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("ListStorageObjects page2: %v", err)
	}
	if len(page2) != 1 || more || page2[0].ID != second.ID {
		t.Fatalf("page2 = %+v more=%v", page2, more)
	}

	deleted, err := env.store.DeleteStorageObject(env.ctx, first.ID)
	if err != nil {
		t.Fatalf("DeleteStorageObject: %v", err)
	}
	if deleted.ID != first.ID || deleted.DeletedAt == nil {
		t.Fatalf("deleted = %+v", deleted)
	}
	if _, err := env.store.GetStorageObjectByProjectNumber(env.ctx, env.projectID, first.Number); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetStorageObjectByProjectNumber deleted err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.DeleteStorageObject(env.ctx, first.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteStorageObject deleted err = %v, want ErrNotFound", err)
	}
	remaining, more, err := env.store.ListStorageObjects(env.ctx, store.ListStorageObjectsParams{
		ProjectID: env.projectID,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListStorageObjects remaining: %v", err)
	}
	if len(remaining) != 1 || more || remaining[0].ID != second.ID {
		t.Fatalf("remaining = %+v more=%v", remaining, more)
	}

	if err := env.store.DeleteProject(env.ctx, env.projectID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if _, _, err := env.store.ListStorageObjects(env.ctx, store.ListStorageObjectsParams{ProjectID: env.projectID, Limit: 10}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ListStorageObjects deleted project err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.GetStorageObjectByProjectNumber(env.ctx, env.projectID, second.Number); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetStorageObjectByProjectNumber deleted project err = %v, want ErrNotFound", err)
	}
}

func TestStorageObjectProjectIsolationAndConflicts(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	first := mustCreateStorageObject(t, env, "projects/a/objects/1")
	second := mustCreateStorageObject(t, env, "projects/a/objects/2")
	otherProject, err := env.store.CreateProjectForUser(env.ctx, project.OwnerID, uniqueProjectKey(t), "other", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser other: %v", err)
	}
	other, err := env.store.CreateStorageObject(env.ctx, storageObjectParams(otherProject.ID, project.OwnerID, "projects/b/objects/1"))
	if err != nil {
		t.Fatalf("CreateStorageObject other: %v", err)
	}
	if other.Ref != "object-1" {
		t.Fatalf("other.Ref = %q, want object-1", other.Ref)
	}
	got, err := env.store.GetStorageObjectByProjectNumber(env.ctx, otherProject.ID, 1)
	if err != nil {
		t.Fatalf("GetStorageObjectByProjectNumber other: %v", err)
	}
	if got.ID != other.ID {
		t.Fatalf("got other = %+v, want %+v", got, other)
	}
	if _, err := env.store.GetStorageObjectByProjectNumber(env.ctx, otherProject.ID, second.Number); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project missing number err = %v, want ErrNotFound", err)
	}

	duplicate := storageObjectParams(env.projectID, project.OwnerID, first.ObjectKey)
	if _, err := env.store.CreateStorageObject(env.ctx, duplicate); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate object key err = %v, want ErrConflict", err)
	}
	invalidUser := storageObjectParams(env.projectID, uuid.New(), "projects/a/objects/bad-user")
	if _, err := env.store.CreateStorageObject(env.ctx, invalidUser); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("invalid user err = %v, want ErrConflict", err)
	}
	invalidMetadata := storageObjectParams(env.projectID, project.OwnerID, "projects/a/objects/bad-sha")
	invalidMetadata.SHA256 = "nope"
	if _, err := env.store.CreateStorageObject(env.ctx, invalidMetadata); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("invalid metadata err = %v, want ErrConflict", err)
	}
	missingProject := storageObjectParams(uuid.New(), project.OwnerID, "projects/missing/objects/1")
	if _, err := env.store.CreateStorageObject(env.ctx, missingProject); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing project err = %v, want ErrNotFound", err)
	}
}
