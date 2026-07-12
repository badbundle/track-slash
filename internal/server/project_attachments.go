package server

import (
	"net/http"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func (s *Server) createProjectAttachment(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	attachment, ok := s.createProjectAttachmentForProject(w, r, project)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
}

func (s *Server) listProjectAttachments(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFromRoute(w, r)
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
	var cursor *store.ProjectAttachmentsCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.ProjectAttachmentsCursor
		if err := decodeCursor(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &c
	}
	attachments, hasMore, err := s.store.ListProjectAttachments(r.Context(), store.ListProjectAttachmentsParams{
		ProjectID: project.ID, Cursor: cursor, Limit: limit,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var next *string
	if hasMore {
		last := attachments[len(attachments)-1]
		encoded := encodeCursor(store.ProjectAttachmentsCursor{Number: last.Object.Number})
		next = &encoded
	}
	writePage(w, attachments, next)
}

func (s *Server) getProjectAttachmentContent(w http.ResponseWriter, r *http.Request) {
	_, attachment, ok := s.projectAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	s.streamStorageObjectContent(w, r, attachment.Object, r.URL.Query().Get("inline") == "1")
}

func (s *Server) deleteProjectAttachment(w http.ResponseWriter, r *http.Request) {
	project, attachment, ok := s.projectAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	if !s.deleteProjectAttachmentForProject(w, r, project, attachment) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) uiCreateProjectAttachment(w http.ResponseWriter, r *http.Request) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	attachment, ok := s.createProjectAttachmentForProject(w, r, project)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
}

func (s *Server) uiGetProjectAttachmentContent(w http.ResponseWriter, r *http.Request) {
	_, attachment, ok := s.uiProjectAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	s.streamStorageObjectContent(w, r, attachment.Object, r.URL.Query().Get("inline") == "1")
}

func (s *Server) uiDeleteProjectAttachment(w http.ResponseWriter, r *http.Request) {
	project, attachment, ok := s.uiProjectAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	if !s.deleteProjectAttachmentForProject(w, r, project, attachment) {
		return
	}
	s.renderUIProjectPanel(w, r, project.ID, "about", nil)
}

func (s *Server) uiDeleteProjectAttachmentJSON(w http.ResponseWriter, r *http.Request) {
	project, attachment, ok := s.uiProjectAttachmentFromRoute(w, r)
	if !ok {
		return
	}
	if !s.deleteProjectAttachmentForProject(w, r, project, attachment) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createProjectAttachmentForProject(w http.ResponseWriter, r *http.Request, project model.Project) (model.ProjectAttachment, bool) {
	return createDescriptionAttachment(s, w, r, project.ID, func(object model.StorageObject) (model.ProjectAttachment, error) {
		return s.store.CreateProjectAttachment(r.Context(), store.CreateProjectAttachmentParams{
			ProjectID: project.ID, StorageObjectID: object.ID, CreatedByID: currentUser(r).ID,
		})
	})
}

func (s *Server) deleteProjectAttachmentForProject(w http.ResponseWriter, r *http.Request, project model.Project, attachment model.ProjectAttachment) bool {
	_, ok := deleteDescriptionAttachment(s, w, r, func() (model.ProjectAttachment, error) {
		return s.store.DeleteProjectAttachment(r.Context(), project.ID, attachment.StorageObjectID)
	}, func(deleted model.ProjectAttachment) string { return deleted.Object.ObjectKey })
	return ok
}

func (s *Server) projectAttachmentFromRoute(w http.ResponseWriter, r *http.Request) (model.Project, model.ProjectAttachment, bool) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return model.Project{}, model.ProjectAttachment{}, false
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return model.Project{}, model.ProjectAttachment{}, false
	}
	number, ok := parseTypedRefParam(w, r, "objectRef", "object")
	if !ok {
		return model.Project{}, model.ProjectAttachment{}, false
	}
	attachment, err := s.store.GetProjectAttachmentByObjectNumber(r.Context(), project.ID, number)
	if err != nil {
		writeStoreError(w, err)
		return model.Project{}, model.ProjectAttachment{}, false
	}
	return project, attachment, true
}

func (s *Server) uiProjectAttachmentFromRoute(w http.ResponseWriter, r *http.Request) (model.Project, model.ProjectAttachment, bool) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return model.Project{}, model.ProjectAttachment{}, false
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return model.Project{}, model.ProjectAttachment{}, false
	}
	number, ok := parseTypedRefParam(w, r, "objectRef", "object")
	if !ok {
		return model.Project{}, model.ProjectAttachment{}, false
	}
	attachment, err := s.store.GetProjectAttachmentByObjectNumber(r.Context(), project.ID, number)
	if err != nil {
		writeUIStoreError(w, err)
		return model.Project{}, model.ProjectAttachment{}, false
	}
	return project, attachment, true
}
