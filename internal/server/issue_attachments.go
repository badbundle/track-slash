package server

import (
	"errors"
	"net/http"

	"github.com/bradleymackey/track-slash/internal/model"
	objectstorage "github.com/bradleymackey/track-slash/internal/storage"
	"github.com/bradleymackey/track-slash/internal/store"
)

func (s *Server) createIssueAttachment(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, issue.ProjectID) {
		return
	}
	attachment, ok := s.createIssueAttachmentForIssue(w, r, issue)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
}

func (s *Server) listIssueAttachments(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, issue.ProjectID) {
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var cursor *store.IssueAttachmentsCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.IssueAttachmentsCursor
		if err := decodeCursor(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &c
	}
	attachments, hasMore, err := s.store.ListIssueAttachments(r.Context(), store.ListIssueAttachmentsParams{
		IssueID: issue.ID,
		Cursor:  cursor,
		Limit:   limit,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var next *string
	if hasMore {
		last := attachments[len(attachments)-1]
		enc := encodeCursor(store.IssueAttachmentsCursor{Number: last.Object.Number})
		next = &enc
	}
	writePage(w, attachments, next)
}

func (s *Server) getIssueAttachmentContent(w http.ResponseWriter, r *http.Request) {
	_, attachment, ok := s.issueAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	s.streamStorageObjectContent(w, r, attachment.Object, r.URL.Query().Get("inline") == "1")
}

func (s *Server) deleteIssueAttachment(w http.ResponseWriter, r *http.Request) {
	issue, attachment, ok := s.issueAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	if !s.deleteIssueAttachmentForIssue(w, r, issue, attachment) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) uiCreateIssueAttachment(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	attachment, ok := s.createIssueAttachmentForIssue(w, r, issue)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
}

func (s *Server) uiGetIssueAttachmentContent(w http.ResponseWriter, r *http.Request) {
	_, attachment, ok := s.uiIssueAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	s.streamStorageObjectContent(w, r, attachment.Object, r.URL.Query().Get("inline") == "1")
}

func (s *Server) uiDeleteIssueAttachment(w http.ResponseWriter, r *http.Request) {
	issue, attachment, ok := s.uiIssueAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	if !s.deleteIssueAttachmentForIssue(w, r, issue, attachment) {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiDeleteIssueAttachmentJSON(w http.ResponseWriter, r *http.Request) {
	issue, attachment, ok := s.uiIssueAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	if !s.deleteIssueAttachmentForIssue(w, r, issue, attachment) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createIssueAttachmentForIssue(w http.ResponseWriter, r *http.Request, issue model.Issue) (model.IssueAttachment, bool) {
	if !s.requireObjectStorage(w) {
		return model.IssueAttachment{}, false
	}
	object, ok := s.createStorageObjectFromRequest(w, r, issue.ProjectID, currentUser(r).ID)
	if !ok {
		return model.IssueAttachment{}, false
	}
	attachment, err := s.store.CreateIssueAttachment(r.Context(), store.CreateIssueAttachmentParams{
		IssueID:         issue.ID,
		StorageObjectID: object.ID,
		CreatedByID:     currentUser(r).ID,
	})
	if err != nil {
		s.cleanupStorageObject(r.Context(), object)
		writeStoreError(w, err)
		return model.IssueAttachment{}, false
	}
	return attachment, true
}

func (s *Server) deleteIssueAttachmentForIssue(w http.ResponseWriter, r *http.Request, issue model.Issue, attachment model.IssueAttachment) bool {
	if !s.requireObjectStorage(w) {
		return false
	}
	deleted, err := s.store.DeleteIssueAttachment(r.Context(), issue.ID, attachment.StorageObjectID)
	if err != nil {
		writeStoreError(w, err)
		return false
	}
	if err := s.deleteStorageBackendObject(r.Context(), deleted.Object.ObjectKey); err != nil && !errors.Is(err, objectstorage.ErrNotFound) {
		writeStorageError(w, err)
		return false
	}
	return true
}

func (s *Server) issueAttachmentFromRoute(w http.ResponseWriter, r *http.Request) (model.Issue, model.IssueAttachment, bool) {
	issue, ok := s.issueFromRoute(w, r)
	if !ok {
		return model.Issue{}, model.IssueAttachment{}, false
	}
	if !s.requireProjectAccess(w, r, issue.ProjectID) {
		return model.Issue{}, model.IssueAttachment{}, false
	}
	number, ok := parseTypedRefParam(w, r, "objectRef", "object")
	if !ok {
		return model.Issue{}, model.IssueAttachment{}, false
	}
	attachment, err := s.store.GetIssueAttachmentByObjectNumber(r.Context(), issue.ID, number)
	if err != nil {
		writeStoreError(w, err)
		return model.Issue{}, model.IssueAttachment{}, false
	}
	return issue, attachment, true
}

func (s *Server) uiIssueAttachmentFromRoute(w http.ResponseWriter, r *http.Request) (model.Issue, model.IssueAttachment, bool) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return model.Issue{}, model.IssueAttachment{}, false
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return model.Issue{}, model.IssueAttachment{}, false
	}
	number, ok := parseTypedRefParam(w, r, "objectRef", "object")
	if !ok {
		return model.Issue{}, model.IssueAttachment{}, false
	}
	attachment, err := s.store.GetIssueAttachmentByObjectNumber(r.Context(), issue.ID, number)
	if err != nil {
		writeUIStoreError(w, err)
		return model.Issue{}, model.IssueAttachment{}, false
	}
	return issue, attachment, true
}
