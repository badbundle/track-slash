package server

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

var projectKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

type createProjectReq struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	req.Name = strings.TrimSpace(req.Name)
	if !projectKeyRe.MatchString(req.Key) {
		writeError(w, http.StatusBadRequest, "key must match ^[A-Z][A-Z0-9]{1,9}$")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}

	p, err := s.store.CreateProjectForUser(r.Context(), currentUser(r).ID, req.Key, req.Name, req.Description)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var cursor *store.ProjectsCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.ProjectsCursor
		if err := decodeCursor(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &c
	}

	out, hasMore, err := s.store.ListProjects(r.Context(), store.ListProjectsParams{
		Cursor:        cursor,
		Limit:         limit,
		VisibleToUser: visibleProjectUser(currentUser(r)),
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var next *string
	if hasMore {
		last := out[len(out)-1]
		enc := encodeCursor(store.ProjectsCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		next = &enc
	}
	writePage(w, out, next)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !s.requireProjectAccess(w, r, id) {
		return
	}
	p, err := s.store.GetProject(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteProject(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func visibleProjectUser(u model.User) *uuid.UUID {
	if u.IsAdmin {
		return nil
	}
	return &u.ID
}
