package server

import (
	"net/http"

	"github.com/bradleymackey/track-slash/internal/store"
)

func (s *Server) getProjectStats(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return
	}
	permissions, ok := s.requireProjectReadPermissions(w, r, project.ID)
	if !ok {
		return
	}
	stats, err := s.store.GetProjectStats(r.Context(), store.ProjectStatsParams{ProjectID: project.ID, IncludePrivate: permissions.CanReadMembersOnly})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
