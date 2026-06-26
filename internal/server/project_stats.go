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
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	stats, err := s.store.GetProjectStats(r.Context(), store.ProjectStatsParams{ProjectID: project.ID})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
