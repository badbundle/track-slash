package store_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/store"
)

func TestSprintAttachmentCRUDPaginationAndCounts(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	sprint := mustCreateSprint(t, env, "Attachment sprint", date(2026, 7, 1), date(2026, 7, 14))
	firstObject := mustCreateStorageObject(t, env, "projects/a/objects/sprint-attachment-1")
	secondObject := mustCreateStorageObject(t, env, "projects/a/objects/sprint-attachment-2")

	first, err := env.store.CreateSprintAttachment(env.ctx, store.CreateSprintAttachmentParams{SprintID: sprint.ID, StorageObjectID: firstObject.ID, CreatedByID: project.OwnerID})
	if err != nil {
		t.Fatalf("CreateSprintAttachment first: %v", err)
	}
	second, err := env.store.CreateSprintAttachment(env.ctx, store.CreateSprintAttachmentParams{SprintID: sprint.ID, StorageObjectID: secondObject.ID, CreatedByID: project.OwnerID})
	if err != nil {
		t.Fatalf("CreateSprintAttachment second: %v", err)
	}
	otherSprint := mustCreateSprint(t, env, "Other attachment sprint", date(2026, 7, 15), date(2026, 7, 28))
	thirdObject := mustCreateStorageObject(t, env, "projects/a/objects/sprint-attachment-3")
	third, err := env.store.CreateSprintAttachment(env.ctx, store.CreateSprintAttachmentParams{SprintID: otherSprint.ID, StorageObjectID: thirdObject.ID, CreatedByID: project.OwnerID})
	if err != nil {
		t.Fatalf("CreateSprintAttachment third: %v", err)
	}
	if first.ProjectID != env.projectID || first.SprintID != sprint.ID || first.Object.Ref != "object-1" {
		t.Fatalf("first attachment = %+v", first)
	}

	page, more, err := env.store.ListSprintAttachments(env.ctx, store.ListSprintAttachmentsParams{SprintID: sprint.ID, Limit: 1})
	if err != nil || len(page) != 1 || !more || page[0].ID != first.ID {
		t.Fatalf("page1 = %+v more=%v err=%v", page, more, err)
	}
	page, more, err = env.store.ListSprintAttachments(env.ctx, store.ListSprintAttachmentsParams{SprintID: sprint.ID, Cursor: &store.SprintAttachmentsCursor{Number: first.Object.Number}, Limit: 1})
	if err != nil || len(page) != 1 || more || page[0].ID != second.ID {
		t.Fatalf("page2 = %+v more=%v err=%v", page, more, err)
	}

	counts, err := env.store.CountSprintAttachments(env.ctx, []uuid.UUID{sprint.ID, uuid.New()})
	if err != nil || counts[sprint.ID] != 2 {
		t.Fatalf("counts = %+v err=%v", counts, err)
	}
	grouped, err := env.store.ListSprintAttachmentsForSprints(env.ctx, store.ListSprintAttachmentsForSprintsParams{
		ProjectID: env.projectID,
		SprintIDs: []uuid.UUID{sprint.ID, otherSprint.ID, uuid.New()},
		Limit:     1,
	})
	if err != nil || len(grouped[sprint.ID]) != 1 || grouped[sprint.ID][0].ID != first.ID || len(grouped[otherSprint.ID]) != 1 || grouped[otherSprint.ID][0].ID != third.ID {
		t.Fatalf("grouped attachments = %+v err=%v", grouped, err)
	}
	emptyGrouped, err := env.store.ListSprintAttachmentsForSprints(env.ctx, store.ListSprintAttachmentsForSprintsParams{ProjectID: env.projectID, Limit: 1})
	if err != nil || len(emptyGrouped) != 0 {
		t.Fatalf("empty grouped attachments = %+v err=%v", emptyGrouped, err)
	}
	wrongProjectGrouped, err := env.store.ListSprintAttachmentsForSprints(env.ctx, store.ListSprintAttachmentsForSprintsParams{
		ProjectID: uuid.New(),
		SprintIDs: []uuid.UUID{sprint.ID},
		Limit:     1,
	})
	if err != nil || len(wrongProjectGrouped) != 0 {
		t.Fatalf("wrong-project grouped attachments = %+v err=%v", wrongProjectGrouped, err)
	}
	emptyCounts, err := env.store.CountSprintAttachments(env.ctx, nil)
	if err != nil || len(emptyCounts) != 0 {
		t.Fatalf("empty counts = %+v err=%v", emptyCounts, err)
	}

	got, err := env.store.GetSprintAttachmentByObjectNumber(env.ctx, sprint.ID, firstObject.Number)
	if err != nil || got.ID != first.ID {
		t.Fatalf("GetSprintAttachmentByObjectNumber = %+v err=%v", got, err)
	}
	deleted, err := env.store.DeleteSprintAttachment(env.ctx, sprint.ID, firstObject.ID)
	if err != nil || deleted.Object.DeletedAt == nil {
		t.Fatalf("DeleteSprintAttachment = %+v err=%v", deleted, err)
	}
	assertStorageObjectDeletion(t, env.store, env.ctx, deleted.Object)
	if _, err := env.store.GetSprintAttachmentByObjectNumber(env.ctx, sprint.ID, firstObject.Number); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted lookup err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.DeleteSprintAttachment(env.ctx, sprint.ID, firstObject.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.GetSprintAttachmentByObjectNumber(env.ctx, sprint.ID, 999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing lookup err = %v, want ErrNotFound", err)
	}
	if _, _, err := env.store.ListSprintAttachments(env.ctx, store.ListSprintAttachmentsParams{SprintID: uuid.New(), Limit: 10}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing sprint list err = %v, want ErrNotFound", err)
	}
}

func TestSprintAttachmentConflictsAndParentIsolation(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	sprint := mustCreateSprint(t, env, "Attachment conflicts", date(2026, 8, 1), date(2026, 8, 14))
	object := mustCreateStorageObject(t, env, "projects/a/objects/sprint-conflict")
	params := store.CreateSprintAttachmentParams{SprintID: sprint.ID, StorageObjectID: object.ID, CreatedByID: project.OwnerID}
	if _, err := env.store.CreateSprintAttachment(env.ctx, params); err != nil {
		t.Fatalf("CreateSprintAttachment: %v", err)
	}
	if _, err := env.store.CreateSprintAttachment(env.ctx, params); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate err = %v, want ErrConflict", err)
	}

	otherProject, err := env.store.CreateProjectForUser(env.ctx, project.OwnerID, uniqueProjectKey(t), "other", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser: %v", err)
	}
	otherObject, err := env.store.CreateStorageObject(env.ctx, storageObjectParams(otherProject.ID, project.OwnerID, "projects/b/objects/sprint-cross"))
	if err != nil {
		t.Fatalf("CreateStorageObject other: %v", err)
	}
	if _, err := env.store.CreateSprintAttachment(env.ctx, store.CreateSprintAttachmentParams{SprintID: sprint.ID, StorageObjectID: otherObject.ID, CreatedByID: project.OwnerID}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("cross-project err = %v, want ErrConflict", err)
	}
	if _, err := env.store.CreateSprintAttachment(env.ctx, store.CreateSprintAttachmentParams{SprintID: uuid.New(), StorageObjectID: otherObject.ID, CreatedByID: project.OwnerID}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing sprint err = %v, want ErrNotFound", err)
	}

	deletedSprint := mustCreateSprint(t, env, "Deleted attachment sprint", date(2026, 9, 1), date(2026, 9, 14))
	deletedObject := mustCreateStorageObject(t, env, "projects/a/objects/deleted-sprint-attachment")
	if _, err := env.store.CreateSprintAttachment(env.ctx, store.CreateSprintAttachmentParams{SprintID: deletedSprint.ID, StorageObjectID: deletedObject.ID, CreatedByID: project.OwnerID}); err != nil {
		t.Fatalf("CreateSprintAttachment deleted setup: %v", err)
	}
	if err := env.store.DeleteSprint(env.ctx, deletedSprint.ID); err != nil {
		t.Fatalf("DeleteSprint: %v", err)
	}
	if _, _, err := env.store.ListSprintAttachments(env.ctx, store.ListSprintAttachmentsParams{SprintID: deletedSprint.ID, Limit: 10}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted sprint list err = %v, want ErrNotFound", err)
	}
	counts, err := env.store.CountSprintAttachments(env.ctx, []uuid.UUID{deletedSprint.ID})
	if err != nil || counts[deletedSprint.ID] != 0 {
		t.Fatalf("deleted sprint counts = %+v err=%v", counts, err)
	}
}
