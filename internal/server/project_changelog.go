package server

import (
	"net/http"

	"github.com/bradleymackey/track-slash/internal/store"
)

func (s *Server) listProjectChangelog(w http.ResponseWriter, r *http.Request) {
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
	var cursor *store.ProjectChangelogCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.ProjectChangelogCursor
		if err := decodeCursor(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &c
	}

	entries, hasMore, err := s.store.ListProjectChangelog(r.Context(), store.ListProjectChangelogParams{
		ProjectID: project.ID,
		Cursor:    cursor,
		Limit:     limit,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var next *string
	if hasMore {
		last := entries[len(entries)-1]
		enc := encodeCursor(store.ProjectChangelogCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		next = &enc
	}
	writePage(w, entries, next)
}
