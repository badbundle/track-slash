package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func mustCreateUserProfileObject(t *testing.T, env *sprintsTestEnv, ownerID uuid.UUID, variant string) model.StorageObject {
	t.Helper()
	id := uuid.New()
	object, err := env.store.CreateUserStorageObject(env.ctx, store.CreateUserStorageObjectParams{
		ID:          id,
		OwnerUserID: ownerID,
		Backend:     "local",
		Bucket:      "local",
		ObjectKey:   "users/" + ownerID.String() + "/profile-images/" + id.String() + "/" + variant,
		Filename:    "profile-" + variant + ".png",
		ContentType: "image/png",
		ByteSize:    12,
		SHA256:      strings.Repeat("a", 64),
		CreatedByID: ownerID,
	})
	if err != nil {
		t.Fatalf("CreateUserStorageObject %s: %v", variant, err)
	}
	return object
}

func TestProfileImageStoreReplaceRemoveAndProjectIsolation(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	ownerID := project.OwnerID

	projectObject := mustCreateStorageObject(t, env, "projects/profile-test/objects/1")
	original := mustCreateUserProfileObject(t, env, ownerID, "original")
	thumbnail := mustCreateUserProfileObject(t, env, ownerID, "thumbnail")
	if original.ProjectID != uuid.Nil || original.OwnerUserID == nil || *original.OwnerUserID != ownerID || original.Number != 0 || original.Ref != "" {
		t.Fatalf("original object = %+v, want user-owned unnumbered object", original)
	}
	if thumbnail.OwnerUserID == nil || *thumbnail.OwnerUserID != ownerID {
		t.Fatalf("thumbnail object = %+v, want owner user id", thumbnail)
	}

	projectObjects, more, err := env.store.ListStorageObjects(env.ctx, store.ListStorageObjectsParams{ProjectID: env.projectID, Limit: 10})
	if err != nil {
		t.Fatalf("ListStorageObjects: %v", err)
	}
	if more || len(projectObjects) != 1 || projectObjects[0].ID != projectObject.ID {
		t.Fatalf("project objects = %+v more=%v, want only project object", projectObjects, more)
	}
	if _, err := env.store.GetStorageObjectByProjectNumber(env.ctx, env.projectID, original.Number); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetStorageObjectByProjectNumber user object err = %v, want ErrNotFound", err)
	}

	replaced, err := env.store.ReplaceUserProfileImage(env.ctx, ownerID, original.ID, thumbnail.ID)
	if err != nil {
		t.Fatalf("ReplaceUserProfileImage: %v", err)
	}
	if replaced.User.ProfileImageObjectID == nil || *replaced.User.ProfileImageObjectID != original.ID {
		t.Fatalf("profile image id = %v, want %s", replaced.User.ProfileImageObjectID, original.ID)
	}
	if replaced.User.ProfileImageThumbnailObjectID == nil || *replaced.User.ProfileImageThumbnailObjectID != thumbnail.ID {
		t.Fatalf("profile thumbnail id = %v, want %s", replaced.User.ProfileImageThumbnailObjectID, thumbnail.ID)
	}
	if len(replaced.DeletedObjects) != 0 {
		t.Fatalf("first replace deleted objects = %+v, want none", replaced.DeletedObjects)
	}
	gotOriginal, err := env.store.GetUserProfileImageObject(env.ctx, ownerID, false)
	if err != nil || gotOriginal.ID != original.ID {
		t.Fatalf("GetUserProfileImageObject original = %+v err=%v", gotOriginal, err)
	}
	gotThumbnail, err := env.store.GetUserProfileImageObject(env.ctx, ownerID, true)
	if err != nil || gotThumbnail.ID != thumbnail.ID {
		t.Fatalf("GetUserProfileImageObject thumbnail = %+v err=%v", gotThumbnail, err)
	}

	nextOriginal := mustCreateUserProfileObject(t, env, ownerID, "original")
	nextThumbnail := mustCreateUserProfileObject(t, env, ownerID, "thumbnail")
	replaced, err = env.store.ReplaceUserProfileImage(env.ctx, ownerID, nextOriginal.ID, nextThumbnail.ID)
	if err != nil {
		t.Fatalf("ReplaceUserProfileImage second: %v", err)
	}
	if len(replaced.DeletedObjects) != 2 {
		t.Fatalf("second replace deleted objects = %+v, want previous pair", replaced.DeletedObjects)
	}
	deleted := map[uuid.UUID]bool{}
	for _, object := range replaced.DeletedObjects {
		if object.DeletedAt == nil {
			t.Fatalf("deleted object missing DeletedAt: %+v", object)
		}
		deleted[object.ID] = true
	}
	if !deleted[original.ID] || !deleted[thumbnail.ID] {
		t.Fatalf("second replace deleted = %+v, want original and thumbnail", replaced.DeletedObjects)
	}
	if _, err := env.store.DeleteStorageObject(env.ctx, original.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteStorageObject old original err = %v, want ErrNotFound", err)
	}

	removed, err := env.store.RemoveUserProfileImage(env.ctx, ownerID)
	if err != nil {
		t.Fatalf("RemoveUserProfileImage: %v", err)
	}
	if removed.User.ProfileImageObjectID != nil || removed.User.ProfileImageThumbnailObjectID != nil {
		t.Fatalf("removed user still has profile ids: %+v", removed.User)
	}
	if len(removed.DeletedObjects) != 2 {
		t.Fatalf("remove deleted objects = %+v, want current pair", removed.DeletedObjects)
	}
	if _, err := env.store.GetUserProfileImageObject(env.ctx, ownerID, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetUserProfileImageObject removed err = %v, want ErrNotFound", err)
	}
	removed, err = env.store.RemoveUserProfileImage(env.ctx, ownerID)
	if err != nil {
		t.Fatalf("RemoveUserProfileImage idempotent: %v", err)
	}
	if len(removed.DeletedObjects) != 0 {
		t.Fatalf("second remove deleted objects = %+v, want none", removed.DeletedObjects)
	}
}

func TestProfileImageStoreValidationAndDeletedUsers(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	ownerID := project.OwnerID
	original := mustCreateUserProfileObject(t, env, ownerID, "original")
	thumbnail := mustCreateUserProfileObject(t, env, ownerID, "thumbnail")
	projectObject := mustCreateStorageObject(t, env, "projects/profile-test/objects/project-owned")

	if _, err := env.store.ReplaceUserProfileImage(env.ctx, ownerID, original.ID, original.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("ReplaceUserProfileImage same object err = %v, want ErrConflict", err)
	}
	if _, err := env.store.ReplaceUserProfileImage(env.ctx, ownerID, projectObject.ID, thumbnail.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ReplaceUserProfileImage project object err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.ReplaceUserProfileImage(env.ctx, uuid.New(), original.ID, thumbnail.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ReplaceUserProfileImage missing user err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.GetUserProfileImageObject(env.ctx, ownerID, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetUserProfileImageObject unset err = %v, want ErrNotFound", err)
	}

	duplicate := store.CreateUserStorageObjectParams{
		ID:          uuid.New(),
		OwnerUserID: ownerID,
		Backend:     "local",
		Bucket:      "local",
		ObjectKey:   original.ObjectKey,
		Filename:    "duplicate.png",
		ContentType: "image/png",
		ByteSize:    1,
		SHA256:      strings.Repeat("b", 64),
		CreatedByID: ownerID,
	}
	if _, err := env.store.CreateUserStorageObject(env.ctx, duplicate); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("CreateUserStorageObject duplicate key err = %v, want ErrConflict", err)
	}
	duplicate.ObjectKey = "users/" + ownerID.String() + "/profile-images/bad-sha/original"
	duplicate.SHA256 = "nope"
	if _, err := env.store.CreateUserStorageObject(env.ctx, duplicate); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("CreateUserStorageObject bad sha err = %v, want ErrConflict", err)
	}
	duplicate.SHA256 = strings.Repeat("c", 64)
	duplicate.CreatedByID = uuid.New()
	duplicate.ObjectKey = "users/" + ownerID.String() + "/profile-images/bad-created-by/original"
	if _, err := env.store.CreateUserStorageObject(env.ctx, duplicate); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("CreateUserStorageObject missing created-by err = %v, want ErrConflict", err)
	}
	duplicate.CreatedByID = ownerID
	duplicate.OwnerUserID = uuid.New()
	duplicate.ObjectKey = "users/missing/profile-images/object/original"
	if _, err := env.store.CreateUserStorageObject(env.ctx, duplicate); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("CreateUserStorageObject missing owner err = %v, want ErrNotFound", err)
	}

	deletedUser, err := env.store.CreateUserProfile(env.ctx, "profile-deleted-"+uniqueProjectKey(t), "profile-deleted@example.com", "Deleted User")
	if err != nil {
		t.Fatalf("CreateUserProfile: %v", err)
	}
	if err := env.store.DeleteUser(env.ctx, deletedUser.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := env.store.CreateUserStorageObject(env.ctx, store.CreateUserStorageObjectParams{
		ID:          uuid.New(),
		OwnerUserID: deletedUser.ID,
		Backend:     "local",
		Bucket:      "local",
		ObjectKey:   "users/" + deletedUser.ID.String() + "/profile-images/object/original",
		Filename:    "deleted.png",
		ContentType: "image/png",
		ByteSize:    1,
		SHA256:      strings.Repeat("d", 64),
		CreatedByID: ownerID,
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("CreateUserStorageObject deleted owner err = %v, want ErrNotFound", err)
	}
}
