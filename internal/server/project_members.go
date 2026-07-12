package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

type projectMemberReq struct {
	Role model.ProjectMemberRole `json:"role,omitempty"`
}

func (s *Server) grantProjectMember(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectMemberManagement(w, r, project.ID) {
		return
	}
	user, ok := s.projectMemberUserFromRoute(w, r)
	if !ok {
		return
	}
	req := projectMemberReq{Role: model.ProjectMemberRoleMember}
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Role == "" {
			req.Role = model.ProjectMemberRoleMember
		}
	}
	if !req.Role.Valid() {
		writeError(w, http.StatusBadRequest, "role must be member or readonly")
		return
	}
	member, err := s.store.SetProjectMemberRole(r.Context(), project.ID, user.ID, req.Role)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, member)
}

func (s *Server) revokeProjectMember(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectMemberManagement(w, r, project.ID) {
		return
	}
	user, ok := s.projectMemberUserFromRoute(w, r)
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
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	members, err := s.store.ListProjectMembers(r.Context(), project.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, members)
}

func (s *Server) searchAvailableProjectMembers(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectMemberManagement(w, r, project.ID) {
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	candidates, err := s.store.SearchAvailableProjectMembers(r.Context(), store.SearchAvailableProjectMembersParams{
		ProjectID: project.ID,
		Query:     r.URL.Query().Get("q"),
		Limit:     limit,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, candidates)
}

func (s *Server) listProjectAssignees(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	assignees, err := s.store.ListProjectAssignees(r.Context(), project.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, assignees)
}

func (s *Server) searchProjectMembers(w http.ResponseWriter, r *http.Request) {
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
	users, err := s.store.SearchProjectMembers(r.Context(), store.SearchProjectMembersParams{
		ProjectID: project.ID,
		Query:     r.URL.Query().Get("q"),
		Limit:     limit,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, safeProjectMemberIdentities(users))
}

func safeProjectMemberIdentities(users []model.User) []model.ProjectMemberCandidate {
	out := make([]model.ProjectMemberCandidate, 0, len(users))
	for _, user := range users {
		out = append(out, model.ProjectMemberCandidate{
			ID:                            user.ID,
			Username:                      user.Username,
			Name:                          user.Name,
			ProfileImageThumbnailObjectID: user.ProfileImageThumbnailObjectID,
		})
	}
	return out
}

func (s *Server) projectMemberUserFromRoute(w http.ResponseWriter, r *http.Request) (model.User, bool) {
	username, err := store.NormalizeUsername(chi.URLParam(r, "username"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return model.User{}, false
	}
	user, err := s.store.GetUserByUsername(r.Context(), username)
	if err != nil {
		writeStoreError(w, err)
		return model.User{}, false
	}
	return user, true
}
