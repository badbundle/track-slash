package server

import (
	"bytes"
	"net/http"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func (s *Server) createProjectImage(w http.ResponseWriter, r *http.Request) {
	project, ok := s.replaceProjectImageFromRoute(w, r)
	if !ok {
		return
	}
	out, err := s.projectResponse(r.Context(), currentUser(r), project)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteProjectImage(w http.ResponseWriter, r *http.Request) {
	project, ok := s.removeProjectImageFromRoute(w, r)
	if !ok {
		return
	}
	out, err := s.projectResponse(r.Context(), currentUser(r), project)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getProjectImageContent(w http.ResponseWriter, r *http.Request) {
	s.getProjectImageContentVariant(w, r, false)
}

func (s *Server) getProjectImageThumbnailContent(w http.ResponseWriter, r *http.Request) {
	s.getProjectImageContentVariant(w, r, true)
}

func (s *Server) uiUpdateProjectImage(w http.ResponseWriter, r *http.Request) {
	project, ok := s.replaceProjectImageFromRoute(w, r)
	if !ok {
		return
	}
	s.renderUIProjectPanel(w, r, project.ID, "about", nil)
}

func (s *Server) uiDeleteProjectImage(w http.ResponseWriter, r *http.Request) {
	project, ok := s.removeProjectImageFromRoute(w, r)
	if !ok {
		return
	}
	s.renderUIProjectPanel(w, r, project.ID, "about", nil)
}

func (s *Server) uiGetProjectImageContent(w http.ResponseWriter, r *http.Request) {
	s.getProjectImageContentVariant(w, r, false)
}

func (s *Server) uiGetProjectImageThumbnailContent(w http.ResponseWriter, r *http.Request) {
	s.getProjectImageContentVariant(w, r, true)
}

func (s *Server) replaceProjectImageFromRoute(w http.ResponseWriter, r *http.Request) (model.Project, bool) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return model.Project{}, false
	}
	if !s.requireProjectWriteAccess(w, r, project.ID) || !s.requireObjectStorage(w) {
		return model.Project{}, false
	}
	upload, ok := parseProjectImageUpload(w, r, s.objectStorage.MaxUploadBytes())
	if !ok {
		return model.Project{}, false
	}
	original, thumbnail, ok := s.createProjectImageStorageObjects(w, r, project.ID, currentUser(r).ID, upload)
	if !ok {
		return model.Project{}, false
	}
	replaced, err := s.store.ReplaceProjectImage(r.Context(), project.ID, original.ID, thumbnail.ID)
	if err != nil {
		s.cleanupStorageObject(r.Context(), original)
		s.cleanupStorageObject(r.Context(), thumbnail)
		writeStoreError(w, err)
		return model.Project{}, false
	}
	s.deleteBackendObjectsBestEffort(r, replaced.DeletedObjects)
	return replaced.Project, true
}

func (s *Server) removeProjectImageFromRoute(w http.ResponseWriter, r *http.Request) (model.Project, bool) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return model.Project{}, false
	}
	if !s.requireProjectWriteAccess(w, r, project.ID) || !s.requireObjectStorage(w) {
		return model.Project{}, false
	}
	removed, err := s.store.RemoveProjectImage(r.Context(), project.ID)
	if err != nil {
		writeStoreError(w, err)
		return model.Project{}, false
	}
	s.deleteBackendObjectsBestEffort(r, removed.DeletedObjects)
	return removed.Project, true
}

func (s *Server) getProjectImageContentVariant(w http.ResponseWriter, r *http.Request, thumbnail bool) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	object, err := s.store.GetProjectImageObject(r.Context(), project.ID, thumbnail)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.streamStorageObjectContent(w, r, object, true)
}

func (s *Server) createProjectImageStorageObjects(w http.ResponseWriter, r *http.Request, projectID, userID uuid.UUID, upload profileImageUpload) (model.StorageObject, model.StorageObject, bool) {
	originalID := uuid.New()
	storedOriginal, err := s.objectStorage.PutProjectImage(r.Context(), projectID, originalID, profileImageOriginalVariant, bytes.NewReader(upload.Original))
	if err != nil {
		writeStorageError(w, err)
		return model.StorageObject{}, model.StorageObject{}, false
	}
	original, err := s.store.CreateStorageObject(r.Context(), store.CreateStorageObjectParams{
		ID: originalID, ProjectID: projectID, Backend: storedOriginal.Backend, Bucket: storedOriginal.Bucket,
		ObjectKey: storedOriginal.ObjectKey, Filename: upload.Filename, ContentType: upload.ContentType,
		ByteSize: storedOriginal.ByteSize, SHA256: storedOriginal.SHA256, CreatedByID: userID,
	})
	if err != nil {
		_ = s.deleteStorageBackendObject(r.Context(), storedOriginal.ObjectKey)
		writeStoreError(w, err)
		return model.StorageObject{}, model.StorageObject{}, false
	}

	thumbnailID := uuid.New()
	storedThumbnail, err := s.objectStorage.PutProjectImage(r.Context(), projectID, thumbnailID, profileImageThumbnailVariant, bytes.NewReader(upload.ThumbnailPNG))
	if err != nil {
		s.cleanupStorageObject(r.Context(), original)
		writeStorageError(w, err)
		return model.StorageObject{}, model.StorageObject{}, false
	}
	thumbnail, err := s.store.CreateStorageObject(r.Context(), store.CreateStorageObjectParams{
		ID: thumbnailID, ProjectID: projectID, Backend: storedThumbnail.Backend, Bucket: storedThumbnail.Bucket,
		ObjectKey: storedThumbnail.ObjectKey, Filename: "project-thumbnail.png", ContentType: "image/png",
		ByteSize: storedThumbnail.ByteSize, SHA256: storedThumbnail.SHA256, CreatedByID: userID,
	})
	if err != nil {
		s.cleanupStorageObject(r.Context(), original)
		_ = s.deleteStorageBackendObject(r.Context(), storedThumbnail.ObjectKey)
		writeStoreError(w, err)
		return model.StorageObject{}, model.StorageObject{}, false
	}
	return original, thumbnail, true
}
