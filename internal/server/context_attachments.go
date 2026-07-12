package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/bradleymackey/track-slash/internal/model"
	objectstorage "github.com/bradleymackey/track-slash/internal/storage"
	"github.com/bradleymackey/track-slash/internal/store"
)

func (s *Server) createContextAttachment(w http.ResponseWriter, r *http.Request) {
	project, contextItem, ok := s.projectContextFromRoute(w, r)
	if !ok || !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	attachment, ok := s.createContextAttachmentForPage(w, r, project, contextItem)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
}

func (s *Server) listContextAttachments(w http.ResponseWriter, r *http.Request) {
	project, contextItem, ok := s.projectContextFromRoute(w, r)
	if !ok || !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var cursor *store.ContextAttachmentsCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var value store.ContextAttachmentsCursor
		if err := decodeCursor(raw, &value); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &value
	}
	attachments, hasMore, err := s.store.ListContextAttachments(r.Context(), store.ListContextAttachmentsParams{
		ContextID: contextItem.ID, Cursor: cursor, Limit: limit,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var next *string
	if hasMore {
		last := attachments[len(attachments)-1]
		encoded := encodeCursor(store.ContextAttachmentsCursor{Number: last.Object.Number})
		next = &encoded
	}
	writePage(w, attachments, next)
}

func (s *Server) getContextAttachmentContent(w http.ResponseWriter, r *http.Request) {
	_, _, attachment, ok := s.contextAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	s.streamStorageObjectContent(w, r, attachment.Object, r.URL.Query().Get("inline") == "1")
}

func (s *Server) deleteContextAttachment(w http.ResponseWriter, r *http.Request) {
	project, contextItem, attachment, ok := s.contextAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	if !s.deleteContextAttachmentForPage(w, r, project, contextItem, attachment) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) uiCreateContextAttachment(w http.ResponseWriter, r *http.Request) {
	project, contextItem, ok := s.uiProjectContextFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	attachment, ok := s.createContextAttachmentForPage(w, r, project, contextItem)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
}

func (s *Server) uiGetContextAttachmentContent(w http.ResponseWriter, r *http.Request) {
	_, _, attachment, ok := s.uiContextAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	s.streamStorageObjectContent(w, r, attachment.Object, r.URL.Query().Get("inline") == "1")
}

func (s *Server) uiDeleteContextAttachment(w http.ResponseWriter, r *http.Request) {
	project, contextItem, attachment, ok := s.uiContextAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	if !s.deleteContextAttachmentForPage(w, r, project, contextItem, attachment) {
		return
	}
	s.renderUIProjectContextManager(w, r, project.ID, func(panel *uiContextManagerData) {
		panel.Action = "edit"
		panel.ActiveContextID = contextItem.ID
		panel.ContextEditTitle = contextItem.Title
		panel.ContextEditBody = contextItem.Body
	})
}

func (s *Server) uiDeleteContextAttachmentJSON(w http.ResponseWriter, r *http.Request) {
	project, contextItem, attachment, ok := s.uiContextAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	if !s.deleteContextAttachmentForPage(w, r, project, contextItem, attachment) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createContextAttachmentForPage(w http.ResponseWriter, r *http.Request, project model.Project, contextItem model.ProjectContext) (model.ContextAttachment, bool) {
	return createDescriptionAttachment(s, w, r, project.ID, func(object model.StorageObject) (model.ContextAttachment, error) {
		return s.store.CreateContextAttachment(r.Context(), store.CreateContextAttachmentParams{
			ProjectID: project.ID, ContextID: contextItem.ID, StorageObjectID: object.ID, CreatedByID: currentUser(r).ID,
		})
	})
}

func (s *Server) deleteContextAttachmentForPage(w http.ResponseWriter, r *http.Request, _ model.Project, contextItem model.ProjectContext, attachment model.ContextAttachment) bool {
	_, ok := deleteDescriptionAttachment(s, w, r, func() (model.ContextAttachment, error) {
		return s.store.DeleteContextAttachment(r.Context(), contextItem.ID, attachment.StorageObjectID)
	}, func(deleted model.ContextAttachment) string { return deleted.Object.ObjectKey })
	return ok
}

func (s *Server) contextAttachmentFromRoute(w http.ResponseWriter, r *http.Request) (model.Project, model.ProjectContext, model.ContextAttachment, bool) {
	project, contextItem, ok := s.projectContextFromRoute(w, r)
	if !ok || !s.requireProjectAccess(w, r, project.ID) {
		return model.Project{}, model.ProjectContext{}, model.ContextAttachment{}, false
	}
	number, ok := parseTypedRefParam(w, r, "objectRef", "object")
	if !ok {
		return model.Project{}, model.ProjectContext{}, model.ContextAttachment{}, false
	}
	attachment, err := s.store.GetContextAttachmentByObjectNumber(r.Context(), contextItem.ID, number)
	if err != nil {
		writeStoreError(w, err)
		return model.Project{}, model.ProjectContext{}, model.ContextAttachment{}, false
	}
	return project, contextItem, attachment, true
}

func (s *Server) uiContextAttachmentFromRoute(w http.ResponseWriter, r *http.Request) (model.Project, model.ProjectContext, model.ContextAttachment, bool) {
	project, contextItem, ok := s.uiProjectContextFromRoute(w, r)
	if !ok {
		return model.Project{}, model.ProjectContext{}, model.ContextAttachment{}, false
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return model.Project{}, model.ProjectContext{}, model.ContextAttachment{}, false
	}
	number, ok := parseTypedRefParam(w, r, "objectRef", "object")
	if !ok {
		return model.Project{}, model.ProjectContext{}, model.ContextAttachment{}, false
	}
	attachment, err := s.store.GetContextAttachmentByObjectNumber(r.Context(), contextItem.ID, number)
	if err != nil {
		writeUIStoreError(w, err)
		return model.Project{}, model.ProjectContext{}, model.ContextAttachment{}, false
	}
	return project, contextItem, attachment, true
}

func (s *Server) deleteProjectContextAndObjects(ctx context.Context, contextItem model.ProjectContext) error {
	var objects []model.StorageObject
	var cursor *store.ContextAttachmentsCursor
	for {
		attachments, more, err := s.store.ListContextAttachments(ctx, store.ListContextAttachmentsParams{
			ContextID: contextItem.ID, Cursor: cursor, Limit: MaxLimit,
		})
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		for _, attachment := range attachments {
			objects = append(objects, attachment.Object)
		}
		if !more {
			break
		}
		last := attachments[len(attachments)-1]
		cursor = &store.ContextAttachmentsCursor{Number: last.Object.Number}
	}
	if err := s.store.DeleteProjectContext(ctx, contextItem.ID); err != nil {
		return err
	}
	if s.objectStorage != nil {
		for _, object := range objects {
			if err := s.deleteStorageBackendObject(ctx, object.ObjectKey); err != nil && !errors.Is(err, objectstorage.ErrNotFound) {
				continue
			}
		}
	}
	return nil
}
