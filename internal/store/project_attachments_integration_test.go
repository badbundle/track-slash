package store_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/store"
)

func TestProjectAttachmentCRUDPaginationAndIsolation(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	firstObject := mustCreateStorageObject(t, env, "projects/a/objects/project-attachment-1")
	secondObject := mustCreateStorageObject(t, env, "projects/a/objects/project-attachment-2")

	first, err := env.store.CreateProjectAttachment(env.ctx, store.CreateProjectAttachmentParams{ProjectID: project.ID, StorageObjectID: firstObject.ID, CreatedByID: project.OwnerID})
	if err != nil {
		t.Fatalf("CreateProjectAttachment first: %v", err)
	}
	second, err := env.store.CreateProjectAttachment(env.ctx, store.CreateProjectAttachmentParams{ProjectID: project.ID, StorageObjectID: secondObject.ID, CreatedByID: project.OwnerID})
	if err != nil {
		t.Fatalf("CreateProjectAttachment second: %v", err)
	}
	if first.ProjectID != project.ID || first.Object.Ref != "object-1" {
		t.Fatalf("first attachment = %+v", first)
	}
	entries, _, err := env.store.ListProjectChangelog(env.ctx, store.ListProjectChangelogParams{ProjectID: project.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListProjectChangelog: %v", err)
	}
	if len(entries) < 2 || entries[0].Entity != "project_attachment" || entries[0].Op != "insert" {
		t.Fatalf("attachment changelog entries = %+v", entries)
	}
	if got, err := env.store.ProjectIDForProjectAttachment(env.ctx, first.ID); err != nil || got != project.ID {
		t.Fatalf("ProjectIDForProjectAttachment = %s, %v", got, err)
	}
	page, more, err := env.store.ListProjectAttachments(env.ctx, store.ListProjectAttachmentsParams{ProjectID: project.ID, Limit: 1})
	if err != nil || len(page) != 1 || !more || page[0].ID != first.ID {
		t.Fatalf("page1 = %+v more=%v err=%v", page, more, err)
	}
	page, more, err = env.store.ListProjectAttachments(env.ctx, store.ListProjectAttachmentsParams{ProjectID: project.ID, Cursor: &store.ProjectAttachmentsCursor{Number: first.Object.Number}, Limit: 1})
	if err != nil || len(page) != 1 || more || page[0].ID != second.ID {
		t.Fatalf("page2 = %+v more=%v err=%v", page, more, err)
	}
	if got, err := env.store.GetProjectAttachmentByObjectNumber(env.ctx, project.ID, first.Object.Number); err != nil || got.ID != first.ID {
		t.Fatalf("GetProjectAttachmentByObjectNumber = %+v err=%v", got, err)
	}
	if _, err := env.store.CreateProjectAttachment(env.ctx, store.CreateProjectAttachmentParams{ProjectID: project.ID, StorageObjectID: firstObject.ID, CreatedByID: project.OwnerID}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate err = %v, want ErrConflict", err)
	}

	otherProject, err := env.store.CreateProjectForUser(env.ctx, project.OwnerID, uniqueProjectKey(t), "other", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser: %v", err)
	}
	otherObject, err := env.store.CreateStorageObject(env.ctx, storageObjectParams(otherProject.ID, project.OwnerID, "projects/b/objects/project-cross"))
	if err != nil {
		t.Fatalf("CreateStorageObject other: %v", err)
	}
	if _, err := env.store.CreateProjectAttachment(env.ctx, store.CreateProjectAttachmentParams{ProjectID: project.ID, StorageObjectID: otherObject.ID, CreatedByID: project.OwnerID}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("cross-project err = %v, want ErrConflict", err)
	}
	if _, err := env.store.CreateProjectAttachment(env.ctx, store.CreateProjectAttachmentParams{ProjectID: uuid.New(), StorageObjectID: otherObject.ID, CreatedByID: project.OwnerID}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing project err = %v, want ErrNotFound", err)
	}

	deleted, err := env.store.DeleteProjectAttachment(env.ctx, project.ID, firstObject.ID)
	if err != nil || deleted.Object.DeletedAt == nil {
		t.Fatalf("DeleteProjectAttachment = %+v err=%v", deleted, err)
	}
	if _, err := env.store.GetProjectAttachmentByObjectNumber(env.ctx, project.ID, first.Object.Number); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted lookup err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.ProjectIDForProjectAttachment(env.ctx, first.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted permission lookup err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.DeleteProjectAttachment(env.ctx, project.ID, firstObject.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete err = %v, want ErrNotFound", err)
	}
	entries, _, err = env.store.ListProjectChangelog(env.ctx, store.ListProjectChangelogParams{ProjectID: project.ID, Limit: 1})
	if err != nil || len(entries) != 1 || entries[0].Entity != "project_attachment" || entries[0].Op != "delete" {
		t.Fatalf("delete changelog entries = %+v err=%v", entries, err)
	}
}
