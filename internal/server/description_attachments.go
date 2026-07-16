package server

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
)

func createDescriptionAttachment[T any](s *Server, w http.ResponseWriter, r *http.Request, projectID uuid.UUID, link func(model.StorageObject) (T, error)) (T, bool) {
	var zero T
	if !s.requireObjectStorage(w) {
		return zero, false
	}
	object, ok := s.createStorageObjectFromRequest(w, r, projectID, currentUser(r).ID)
	if !ok {
		return zero, false
	}
	attachment, err := link(object)
	if err != nil {
		s.cleanupStorageObject(r.Context(), object)
		writeStoreError(w, err)
		return zero, false
	}
	return attachment, true
}

func deleteDescriptionAttachment[T any](s *Server, w http.ResponseWriter, r *http.Request, unlink func() (T, error), objectKey func(T) string) (T, bool) {
	var zero T
	deleted, err := unlink()
	if err != nil {
		writeStoreError(w, err)
		return zero, false
	}
	if s.objectStorage != nil {
		_ = s.deleteStorageBackendObject(r.Context(), objectKey(deleted))
	}
	return deleted, true
}
