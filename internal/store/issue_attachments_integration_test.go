package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/store"
)

func TestIssueAttachmentCRUDAndPagination(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	issue := mustCreateIssue(t, env, "Attachment issue")
	firstObject := mustCreateStorageObject(t, env, "projects/a/objects/attachment-1")
	secondObject := mustCreateStorageObject(t, env, "projects/a/objects/attachment-2")

	first, err := env.store.CreateIssueAttachment(env.ctx, store.CreateIssueAttachmentParams{
		IssueID:         issue.ID,
		StorageObjectID: firstObject.ID,
		CreatedByID:     project.OwnerID,
	})
	if err != nil {
		t.Fatalf("CreateIssueAttachment first: %v", err)
	}
	if first.ProjectID != env.projectID || first.IssueID != issue.ID || first.StorageObjectID != firstObject.ID || first.CreatedByID != project.OwnerID {
		t.Fatalf("first attachment = %+v", first)
	}
	if first.Object.ID != firstObject.ID || first.Object.Ref != "object-1" || first.Object.Filename != firstObject.Filename {
		t.Fatalf("first nested object = %+v, want %+v", first.Object, firstObject)
	}

	second, err := env.store.CreateIssueAttachment(env.ctx, store.CreateIssueAttachmentParams{
		IssueID:         issue.ID,
		StorageObjectID: secondObject.ID,
		CreatedByID:     project.OwnerID,
	})
	if err != nil {
		t.Fatalf("CreateIssueAttachment second: %v", err)
	}

	byNumber, err := env.store.GetIssueAttachmentByObjectNumber(env.ctx, issue.ID, firstObject.Number)
	if err != nil {
		t.Fatalf("GetIssueAttachmentByObjectNumber: %v", err)
	}
	if byNumber.ID != first.ID || byNumber.Object.ID != firstObject.ID {
		t.Fatalf("byNumber = %+v, want first", byNumber)
	}

	page1, more, err := env.store.ListIssueAttachments(env.ctx, store.ListIssueAttachmentsParams{
		IssueID: issue.ID,
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("ListIssueAttachments page1: %v", err)
	}
	if len(page1) != 1 || !more || page1[0].ID != first.ID {
		t.Fatalf("page1 = %+v more=%v", page1, more)
	}
	page2, more, err := env.store.ListIssueAttachments(env.ctx, store.ListIssueAttachmentsParams{
		IssueID: issue.ID,
		Cursor:  &store.IssueAttachmentsCursor{Number: page1[0].Object.Number},
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("ListIssueAttachments page2: %v", err)
	}
	if len(page2) != 1 || more || page2[0].ID != second.ID {
		t.Fatalf("page2 = %+v more=%v", page2, more)
	}

	deleted, err := env.store.DeleteIssueAttachment(env.ctx, issue.ID, firstObject.ID)
	if err != nil {
		t.Fatalf("DeleteIssueAttachment: %v", err)
	}
	if deleted.ID != first.ID || deleted.Object.DeletedAt == nil {
		t.Fatalf("deleted = %+v", deleted)
	}
	assertStorageObjectDeletion(t, env.store, env.ctx, deleted.Object)
	if _, err := env.store.GetIssueAttachmentByObjectNumber(env.ctx, issue.ID, firstObject.Number); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetIssueAttachmentByObjectNumber deleted err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.GetStorageObjectByProjectNumber(env.ctx, env.projectID, firstObject.Number); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetStorageObjectByProjectNumber deleted err = %v, want ErrNotFound", err)
	}
	remaining, more, err := env.store.ListIssueAttachments(env.ctx, store.ListIssueAttachmentsParams{
		IssueID: issue.ID,
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ListIssueAttachments remaining: %v", err)
	}
	if len(remaining) != 1 || more || remaining[0].ID != second.ID {
		t.Fatalf("remaining = %+v more=%v", remaining, more)
	}
}

func TestIssueAttachmentConflictsAndSameProjectEnforcement(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	issue := mustCreateIssue(t, env, "Attachment conflicts")
	object := mustCreateStorageObject(t, env, "projects/a/objects/conflict")

	if _, err := env.store.CreateIssueAttachment(env.ctx, store.CreateIssueAttachmentParams{
		IssueID:         issue.ID,
		StorageObjectID: object.ID,
		CreatedByID:     project.OwnerID,
	}); err != nil {
		t.Fatalf("CreateIssueAttachment: %v", err)
	}
	if _, err := env.store.CreateIssueAttachment(env.ctx, store.CreateIssueAttachmentParams{
		IssueID:         issue.ID,
		StorageObjectID: object.ID,
		CreatedByID:     project.OwnerID,
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate err = %v, want ErrConflict", err)
	}

	otherProject, err := env.store.CreateProjectForUser(env.ctx, project.OwnerID, uniqueProjectKey(t), "other", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser other: %v", err)
	}
	otherIssue, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID: otherProject.ID,
		Title:     "Other issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue other: %v", err)
	}
	otherParams := storageObjectParams(otherProject.ID, project.OwnerID, "projects/b/objects/cross-project")
	otherObject, err := env.store.CreateStorageObject(env.ctx, otherParams)
	if err != nil {
		t.Fatalf("CreateStorageObject other: %v", err)
	}
	if _, err := env.store.CreateIssueAttachment(env.ctx, store.CreateIssueAttachmentParams{
		IssueID:         issue.ID,
		StorageObjectID: otherObject.ID,
		CreatedByID:     project.OwnerID,
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("cross-project object err = %v, want ErrConflict", err)
	}
	if _, err := env.store.CreateIssueAttachment(env.ctx, store.CreateIssueAttachmentParams{
		IssueID:         otherIssue.ID,
		StorageObjectID: mustCreateStorageObject(t, env, "projects/a/objects/cross-project-2").ID,
		CreatedByID:     project.OwnerID,
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("cross-project issue err = %v, want ErrConflict", err)
	}

	missingUser := mustCreateStorageObject(t, env, "projects/a/objects/missing-user")
	if _, err := env.store.CreateIssueAttachment(env.ctx, store.CreateIssueAttachmentParams{
		IssueID:         issue.ID,
		StorageObjectID: missingUser.ID,
		CreatedByID:     uuid.New(),
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("missing user err = %v, want ErrConflict", err)
	}
	if _, err := env.store.CreateIssueAttachment(env.ctx, store.CreateIssueAttachmentParams{
		IssueID:         uuid.New(),
		StorageObjectID: missingUser.ID,
		CreatedByID:     project.OwnerID,
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing issue err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.CreateIssueAttachment(env.ctx, store.CreateIssueAttachmentParams{
		IssueID:         issue.ID,
		StorageObjectID: uuid.New(),
		CreatedByID:     project.OwnerID,
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing object err = %v, want ErrNotFound", err)
	}
}

func TestIssueAttachmentDeletedFiltering(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	issue := mustCreateIssue(t, env, "Attachment deleted filters")
	object := mustCreateStorageObject(t, env, "projects/a/objects/deleted-filter")
	attachment, err := env.store.CreateIssueAttachment(env.ctx, store.CreateIssueAttachmentParams{
		IssueID:         issue.ID,
		StorageObjectID: object.ID,
		CreatedByID:     project.OwnerID,
	})
	if err != nil {
		t.Fatalf("CreateIssueAttachment: %v", err)
	}
	if gotProjectID, err := env.store.ProjectIDForIssueAttachment(env.ctx, attachment.ID); err != nil || gotProjectID != env.projectID {
		t.Fatalf("ProjectIDForIssueAttachment = %s, %v; want %s", gotProjectID, err, env.projectID)
	}

	if _, err := env.store.DeleteStorageObject(env.ctx, object.ID); err != nil {
		t.Fatalf("DeleteStorageObject: %v", err)
	}
	if _, err := env.store.ProjectIDForIssueAttachment(env.ctx, attachment.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ProjectIDForIssueAttachment deleted object err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.GetIssueAttachmentByObjectNumber(env.ctx, issue.ID, object.Number); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetIssueAttachmentByObjectNumber deleted object err = %v, want ErrNotFound", err)
	}
	listed, more, err := env.store.ListIssueAttachments(env.ctx, store.ListIssueAttachmentsParams{IssueID: issue.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListIssueAttachments deleted object: %v", err)
	}
	if len(listed) != 0 || more {
		t.Fatalf("listed deleted object attachment = %+v more=%v", listed, more)
	}

	issueDeleted := mustCreateIssue(t, env, "Deleted issue")
	objectForDeletedIssue := mustCreateStorageObject(t, env, "projects/a/objects/deleted-issue")
	if _, err := env.store.CreateIssueAttachment(env.ctx, store.CreateIssueAttachmentParams{
		IssueID:         issueDeleted.ID,
		StorageObjectID: objectForDeletedIssue.ID,
		CreatedByID:     project.OwnerID,
	}); err != nil {
		t.Fatalf("CreateIssueAttachment deleted issue setup: %v", err)
	}
	if err := env.store.DeleteIssue(env.ctx, issueDeleted.ID); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	if _, _, err := env.store.ListIssueAttachments(env.ctx, store.ListIssueAttachmentsParams{IssueID: issueDeleted.ID, Limit: 10}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ListIssueAttachments deleted issue err = %v, want ErrNotFound", err)
	}

	issueDeletedProject := mustCreateIssue(t, env, "Deleted project")
	objectForDeletedProject := mustCreateStorageObject(t, env, "projects/a/objects/deleted-project")
	if _, err := env.store.CreateIssueAttachment(env.ctx, store.CreateIssueAttachmentParams{
		IssueID:         issueDeletedProject.ID,
		StorageObjectID: objectForDeletedProject.ID,
		CreatedByID:     project.OwnerID,
	}); err != nil {
		t.Fatalf("CreateIssueAttachment deleted project setup: %v", err)
	}
	if err := env.store.DeleteProject(env.ctx, env.projectID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if _, _, err := env.store.ListIssueAttachments(env.ctx, store.ListIssueAttachmentsParams{IssueID: issueDeletedProject.ID, Limit: 10}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ListIssueAttachments deleted project err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.GetIssueAttachmentByObjectNumber(env.ctx, issueDeletedProject.ID, objectForDeletedProject.Number); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetIssueAttachmentByObjectNumber deleted project err = %v, want ErrNotFound", err)
	}
}

func TestIssueAttachmentRespectsObjectMetadataTable(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	issue := mustCreateIssue(t, env, "Attachment metadata")
	params := storageObjectParams(env.projectID, project.OwnerID, "projects/a/objects/metadata")
	params.Filename = "notes.txt"
	params.ContentType = "text/plain; charset=utf-8"
	params.ByteSize = 5
	params.SHA256 = strings.Repeat("f", 64)
	object, err := env.store.CreateStorageObject(env.ctx, params)
	if err != nil {
		t.Fatalf("CreateStorageObject: %v", err)
	}
	attachment, err := env.store.CreateIssueAttachment(env.ctx, store.CreateIssueAttachmentParams{
		IssueID:         issue.ID,
		StorageObjectID: object.ID,
		CreatedByID:     project.OwnerID,
	})
	if err != nil {
		t.Fatalf("CreateIssueAttachment: %v", err)
	}
	if attachment.Object.ID != object.ID || attachment.Object.Filename != "notes.txt" || attachment.Object.ContentType != "text/plain; charset=utf-8" || attachment.Object.ByteSize != 5 || attachment.Object.SHA256 != params.SHA256 {
		t.Fatalf("attachment nested object = %+v, want storage object metadata %+v", attachment.Object, object)
	}
}
