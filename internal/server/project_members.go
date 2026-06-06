package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func (s *Server) grantProjectMember(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	project, user, ok := s.parseProjectMemberRoute(w, r)
	if !ok {
		return
	}
	member, err := s.store.GrantProjectAccess(r.Context(), project.ID, user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, member)
}

func (s *Server) revokeProjectMember(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	project, user, ok := s.parseProjectMemberRoute(w, r)
	if !ok {
		return
	}
	if err := s.store.RevokeProjectAccess(r.Context(), project.ID, user.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listProjectMembers(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return
	}
	members, err := s.store.ListProjectMembers(r.Context(), project.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, members)
}

func (s *Server) parseProjectMemberRoute(w http.ResponseWriter, r *http.Request) (model.Project, model.User, bool) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return model.Project{}, model.User{}, false
	}
	username, err := store.NormalizeUsername(chi.URLParam(r, "username"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return model.Project{}, model.User{}, false
	}
	user, err := s.store.GetUserByUsername(r.Context(), username)
	if err != nil {
		writeStoreError(w, err)
		return model.Project{}, model.User{}, false
	}
	return project, user, true
}
