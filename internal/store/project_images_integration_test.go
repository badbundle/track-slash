package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func mustCreateProjectImageObject(t *testing.T, env *sprintsTestEnv, projectID, createdByID uuid.UUID, variant string) model.StorageObject {
	t.Helper()
	id := uuid.New()
	object, err := env.store.CreateStorageObject(env.ctx, store.CreateStorageObjectParams{
		ID:          id,
		ProjectID:   projectID,
		Backend:     "local",
		Bucket:      "local",
		ObjectKey:   "projects/" + projectID.String() + "/images/" + id.String() + "/" + variant,
		Filename:    "project-" + variant + ".png",
		ContentType: "image/png",
		ByteSize:    12,
		SHA256:      strings.Repeat("a", 64),
		CreatedByID: createdByID,
	})
	if err != nil {
		t.Fatalf("CreateStorageObject %s: %v", variant, err)
	}
	return object
}

func TestProjectImageStoreReplaceRemoveAndObjectIsolation(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	original := mustCreateProjectImageObject(t, env, project.ID, project.OwnerID, "original")
	thumbnail := mustCreateProjectImageObject(t, env, project.ID, project.OwnerID, "thumbnail")

	replaced, err := env.store.ReplaceProjectImage(env.ctx, project.ID, original.ID, thumbnail.ID)
	if err != nil {
		t.Fatalf("ReplaceProjectImage: %v", err)
	}
	if replaced.Project.ImageObjectID == nil || *replaced.Project.ImageObjectID != original.ID || replaced.Project.ImageThumbnailObjectID == nil || *replaced.Project.ImageThumbnailObjectID != thumbnail.ID {
		t.Fatalf("replaced project image ids = %+v", replaced.Project)
	}
	if len(replaced.DeletedObjects) != 0 {
		t.Fatalf("first replace deleted objects = %+v, want none", replaced.DeletedObjects)
	}
	for thumbnailVariant, wantID := range map[bool]uuid.UUID{false: original.ID, true: thumbnail.ID} {
		got, err := env.store.GetProjectImageObject(env.ctx, project.ID, thumbnailVariant)
		if err != nil || got.ID != wantID {
			t.Fatalf("GetProjectImageObject thumbnail=%v = %+v err=%v, want %s", thumbnailVariant, got, err, wantID)
		}
	}
	objects, more, err := env.store.ListStorageObjects(env.ctx, store.ListStorageObjectsParams{ProjectID: project.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListStorageObjects: %v", err)
	}
	if more || len(objects) != 0 {
		t.Fatalf("project objects = %+v more=%v, want image pair hidden", objects, more)
	}
	for _, object := range []model.StorageObject{original, thumbnail} {
		if _, err := env.store.GetStorageObjectByProjectNumber(env.ctx, project.ID, object.Number); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetStorageObjectByProjectNumber image %s err = %v, want ErrNotFound", object.ID, err)
		}
	}

	nextOriginal := mustCreateProjectImageObject(t, env, project.ID, project.OwnerID, "original")
	nextThumbnail := mustCreateProjectImageObject(t, env, project.ID, project.OwnerID, "thumbnail")
	replaced, err = env.store.ReplaceProjectImage(env.ctx, project.ID, nextOriginal.ID, nextThumbnail.ID)
	if err != nil {
		t.Fatalf("ReplaceProjectImage second: %v", err)
	}
	if len(replaced.DeletedObjects) != 2 {
		t.Fatalf("second replace deleted objects = %+v, want previous pair", replaced.DeletedObjects)
	}
	deleted := map[uuid.UUID]bool{}
	for _, object := range replaced.DeletedObjects {
		if object.DeletedAt == nil {
			t.Fatalf("deleted object missing DeletedAt: %+v", object)
		}
		assertStorageObjectDeletion(t, env.store, env.ctx, object)
		deleted[object.ID] = true
	}
	if !deleted[original.ID] || !deleted[thumbnail.ID] {
		t.Fatalf("second replace deleted = %+v, want original pair", replaced.DeletedObjects)
	}

	removed, err := env.store.RemoveProjectImage(env.ctx, project.ID)
	if err != nil {
		t.Fatalf("RemoveProjectImage: %v", err)
	}
	if removed.Project.ImageObjectID != nil || removed.Project.ImageThumbnailObjectID != nil || len(removed.DeletedObjects) != 2 {
		t.Fatalf("removed project image = %+v", removed)
	}
	for _, object := range removed.DeletedObjects {
		assertStorageObjectDeletion(t, env.store, env.ctx, object)
	}
	if _, err := env.store.GetProjectImageObject(env.ctx, project.ID, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetProjectImageObject removed err = %v, want ErrNotFound", err)
	}
	removed, err = env.store.RemoveProjectImage(env.ctx, project.ID)
	if err != nil || len(removed.DeletedObjects) != 0 {
		t.Fatalf("RemoveProjectImage idempotent = %+v err=%v", removed, err)
	}
}

func TestProjectImageStoreValidation(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	original := mustCreateProjectImageObject(t, env, project.ID, project.OwnerID, "original")
	thumbnail := mustCreateProjectImageObject(t, env, project.ID, project.OwnerID, "thumbnail")
	if _, err := env.store.ReplaceProjectImage(env.ctx, project.ID, original.ID, original.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("ReplaceProjectImage same object err = %v, want ErrConflict", err)
	}

	other, err := env.store.CreateProjectForUser(env.ctx, project.OwnerID, uniqueProjectKey(t), "Other project", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser: %v", err)
	}
	otherObject := mustCreateProjectImageObject(t, env, other.ID, project.OwnerID, "original")
	if _, err := env.store.ReplaceProjectImage(env.ctx, project.ID, otherObject.ID, thumbnail.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ReplaceProjectImage cross-project object err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.ReplaceProjectImage(env.ctx, uuid.New(), original.ID, thumbnail.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ReplaceProjectImage missing project err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.GetProjectImageObject(env.ctx, project.ID, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetProjectImageObject unset err = %v, want ErrNotFound", err)
	}
}
