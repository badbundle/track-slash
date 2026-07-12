package server

import (
	"net/http"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func (s *Server) createSprintAttachment(w http.ResponseWriter, r *http.Request) {
	project, sprint, ok := s.sprintFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	attachment, ok := s.createSprintAttachmentForSprint(w, r, sprint)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
}

func (s *Server) listSprintAttachments(w http.ResponseWriter, r *http.Request) {
	project, sprint, ok := s.sprintFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var cursor *store.SprintAttachmentsCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.SprintAttachmentsCursor
		if err := decodeCursor(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &c
	}
	attachments, hasMore, err := s.store.ListSprintAttachments(r.Context(), store.ListSprintAttachmentsParams{
		SprintID: sprint.ID,
		Cursor:   cursor,
		Limit:    limit,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var next *string
	if hasMore {
		last := attachments[len(attachments)-1]
		enc := encodeCursor(store.SprintAttachmentsCursor{Number: last.Object.Number})
		next = &enc
	}
	writePage(w, attachments, next)
}

func (s *Server) getSprintAttachmentContent(w http.ResponseWriter, r *http.Request) {
	_, _, attachment, ok := s.sprintAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	s.streamStorageObjectContent(w, r, attachment.Object, r.URL.Query().Get("inline") == "1")
}

func (s *Server) deleteSprintAttachment(w http.ResponseWriter, r *http.Request) {
	_, sprint, attachment, ok := s.sprintAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	if !s.deleteSprintAttachmentForSprint(w, r, sprint, attachment) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) uiCreateSprintAttachment(w http.ResponseWriter, r *http.Request) {
	_, sprint, ok := s.uiProjectSprintFromRoute(w, r)
	if !ok {
		return
	}
	attachment, ok := s.createSprintAttachmentForSprint(w, r, sprint)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
}

func (s *Server) uiGetSprintAttachmentContent(w http.ResponseWriter, r *http.Request) {
	_, _, attachment, ok := s.uiSprintAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	s.streamStorageObjectContent(w, r, attachment.Object, r.URL.Query().Get("inline") == "1")
}

func (s *Server) uiDeleteSprintAttachment(w http.ResponseWriter, r *http.Request) {
	project, sprint, attachment, ok := s.uiSprintAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	if !s.deleteSprintAttachmentForSprint(w, r, sprint, attachment) {
		return
	}
	s.renderUIProjectPanel(w, r, project.ID, uiProjectSprintView(sprint), nil)
}

func (s *Server) uiDeleteSprintAttachmentJSON(w http.ResponseWriter, r *http.Request) {
	_, sprint, attachment, ok := s.uiSprintAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	if !s.deleteSprintAttachmentForSprint(w, r, sprint, attachment) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createSprintAttachmentForSprint(w http.ResponseWriter, r *http.Request, sprint model.Sprint) (model.SprintAttachment, bool) {
	return createDescriptionAttachment(s, w, r, sprint.ProjectID, func(object model.StorageObject) (model.SprintAttachment, error) {
		return s.store.CreateSprintAttachment(r.Context(), store.CreateSprintAttachmentParams{
			SprintID:        sprint.ID,
			StorageObjectID: object.ID,
			CreatedByID:     currentUser(r).ID,
		})
	})
}

func (s *Server) deleteSprintAttachmentForSprint(w http.ResponseWriter, r *http.Request, sprint model.Sprint, attachment model.SprintAttachment) bool {
	_, ok := deleteDescriptionAttachment(s, w, r, func() (model.SprintAttachment, error) {
		return s.store.DeleteSprintAttachment(r.Context(), sprint.ID, attachment.StorageObjectID)
	}, func(deleted model.SprintAttachment) string {
		return deleted.Object.ObjectKey
	})
	return ok
}

func (s *Server) sprintAttachmentFromRoute(w http.ResponseWriter, r *http.Request) (model.Project, model.Sprint, model.SprintAttachment, bool) {
	project, sprint, ok := s.sprintFromRoute(w, r)
	if !ok {
		return model.Project{}, model.Sprint{}, model.SprintAttachment{}, false
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return model.Project{}, model.Sprint{}, model.SprintAttachment{}, false
	}
	number, ok := parseTypedRefParam(w, r, "objectRef", "object")
	if !ok {
		return model.Project{}, model.Sprint{}, model.SprintAttachment{}, false
	}
	attachment, err := s.store.GetSprintAttachmentByObjectNumber(r.Context(), sprint.ID, number)
	if err != nil {
		writeStoreError(w, err)
		return model.Project{}, model.Sprint{}, model.SprintAttachment{}, false
	}
	return project, sprint, attachment, true
}

func (s *Server) uiSprintAttachmentFromRoute(w http.ResponseWriter, r *http.Request) (model.Project, model.Sprint, model.SprintAttachment, bool) {
	project, sprint, ok := s.uiProjectSprintFromRoute(w, r)
	if !ok {
		return model.Project{}, model.Sprint{}, model.SprintAttachment{}, false
	}
	number, ok := parseTypedRefParam(w, r, "objectRef", "object")
	if !ok {
		return model.Project{}, model.Sprint{}, model.SprintAttachment{}, false
	}
	attachment, err := s.store.GetSprintAttachmentByObjectNumber(r.Context(), sprint.ID, number)
	if err != nil {
		writeUIStoreError(w, err)
		return model.Project{}, model.Sprint{}, model.SprintAttachment{}, false
	}
	return project, sprint, attachment, true
}
