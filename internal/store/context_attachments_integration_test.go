package store_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/store"
)

func TestContextAttachmentCRUD(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	contextItem := mustCreateProjectContext(t, env, "Runbook", "# Runbook")
	object := mustCreateStorageObject(t, env, "projects/context/objects/one")

	created, err := env.store.CreateContextAttachment(env.ctx, store.CreateContextAttachmentParams{
		ProjectID: env.projectID, ContextID: contextItem.ID, StorageObjectID: object.ID, CreatedByID: project.OwnerID,
	})
	if err != nil {
		t.Fatalf("CreateContextAttachment: %v", err)
	}
	if created.ContextID != contextItem.ID || created.Object.Ref != object.Ref {
		t.Fatalf("created attachment = %+v", created)
	}
	if _, err := env.store.CreateContextAttachment(env.ctx, store.CreateContextAttachmentParams{
		ProjectID: env.projectID, ContextID: contextItem.ID, StorageObjectID: object.ID, CreatedByID: project.OwnerID,
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate attachment err = %v, want ErrConflict", err)
	}

	listed, more, err := env.store.ListContextAttachments(env.ctx, store.ListContextAttachmentsParams{ContextID: contextItem.ID, Limit: 10})
	if err != nil || more || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed = %+v more=%v err=%v", listed, more, err)
	}
	got, err := env.store.GetContextAttachmentByObjectNumber(env.ctx, contextItem.ID, object.Number)
	if err != nil || got.ID != created.ID {
		t.Fatalf("GetContextAttachmentByObjectNumber = %+v err=%v", got, err)
	}
	if _, err := env.store.GetContextAttachmentByObjectNumber(env.ctx, contextItem.ID, 999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing attachment err = %v", err)
	}

	deleted, err := env.store.DeleteContextAttachment(env.ctx, contextItem.ID, object.ID)
	if err != nil || deleted.Object.DeletedAt == nil {
		t.Fatalf("DeleteContextAttachment = %+v err=%v", deleted, err)
	}
	assertStorageObjectDeletion(t, env.store, env.ctx, deleted.Object)
	if _, err := env.store.GetContextAttachmentByObjectNumber(env.ctx, contextItem.ID, object.Number); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("attachment after delete err = %v", err)
	}
	if _, err := env.store.DeleteContextAttachment(env.ctx, contextItem.ID, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delete missing err = %v", err)
	}
}
