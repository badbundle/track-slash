package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

type createIssueReq struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	AssigneeID  *uuid.UUID `json:"assignee_id,omitempty"`
	ReporterID  *uuid.UUID `json:"reporter_id,omitempty"`
}

func (s *Server) createIssue(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
		return
	}
	var req createIssueReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" || len(req.Title) > 200 {
		writeError(w, http.StatusBadRequest, "title required, max 200 chars")
		return
	}
	reporterID := req.ReporterID
	if reporterID == nil {
		id := currentUser(r).ID
		reporterID = &id
	} else if !currentUser(r).IsAdmin && *reporterID != currentUser(r).ID {
		writeForbidden(w)
		return
	}

	iss, err := s.store.CreateIssue(r.Context(), store.CreateIssueParams{
		ProjectID:   projectID,
		Title:       req.Title,
		Description: req.Description,
		AssigneeID:  req.AssigneeID,
		ReporterID:  reporterID,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, iss)
}

func (s *Server) listIssues(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
		return
	}

	statusFilter := model.Status(r.URL.Query().Get("status"))
	if statusFilter != "" && !statusFilter.Valid() {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}

	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var cursor *store.IssuesCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.IssuesCursor
		if err := decodeCursor(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &c
	}

	params := store.ListIssuesParams{
		ProjectID: projectID,
		Status:    statusFilter,
		Cursor:    cursor,
		Limit:     limit,
	}

	sprintParam := r.URL.Query().Get("sprint")
	sprintIDParam := r.URL.Query().Get("sprint_id")
	if sprintParam != "" && sprintIDParam != "" {
		writeError(w, http.StatusBadRequest, "specify either sprint or sprint_id, not both")
		return
	}
	switch {
	case sprintParam == "backlog":
		params.Backlog = true
	case sprintParam != "":
		writeError(w, http.StatusBadRequest, "sprint must be 'backlog'")
		return
	case sprintIDParam != "":
		sid, err := uuid.Parse(sprintIDParam)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid sprint_id")
			return
		}
		params.SprintID = &sid
	}

	out, hasMore, err := s.store.ListIssues(r.Context(), params)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var next *string
	if hasMore {
		last := out[len(out)-1]
		enc := encodeCursor(store.IssuesCursor{Number: last.Number})
		next = &enc
	}
	writePage(w, out, next)
}

// maxBatchIssues caps the number of ids accepted by /issues?ids= to keep
// query cost bounded and prevent unbounded URL length abuse.
const maxBatchIssues = 200

func (s *Server) batchIssues(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("ids")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "ids query param required (comma-separated uuids)")
		return
	}
	parts := strings.Split(raw, ",")
	if len(parts) > maxBatchIssues {
		writeError(w, http.StatusBadRequest, "too many ids (max 200)")
		return
	}
	ids := make([]uuid.UUID, 0, len(parts))
	seen := make(map[uuid.UUID]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := uuid.Parse(p)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid uuid: "+p)
			return
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	out, err := s.store.ListIssuesByIDs(r.Context(), ids)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	for _, iss := range out {
		if !s.requireProjectAccess(w, r, iss.ProjectID) {
			return
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getIssue(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	projectID, err := s.store.ProjectIDForIssue(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
		return
	}
	iss, err := s.store.GetIssue(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, iss)
}

type updateIssueReq struct {
	Title       *string       `json:"title,omitempty"`
	Description *string       `json:"description,omitempty"`
	Status      *model.Status `json:"status,omitempty"`
	// AssigneeID: pointer-to-pointer pattern via json.RawMessage would be cleaner,
	// but v0 keeps it simple: assignee_id present sets it, assignee_id null clears.
	AssigneeID    *uuid.UUID `json:"assignee_id,omitempty"`
	ClearAssignee bool       `json:"clear_assignee,omitempty"`
	SprintID      *uuid.UUID `json:"sprint_id,omitempty"`
	ClearSprint   bool       `json:"clear_sprint,omitempty"`
}

func (s *Server) updateIssue(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	projectID, err := s.store.ProjectIDForIssue(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
		return
	}
	var req updateIssueReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Title != nil {
		t := strings.TrimSpace(*req.Title)
		if t == "" || len(t) > 200 {
			writeError(w, http.StatusBadRequest, "title must be 1..200 chars")
			return
		}
		req.Title = &t
	}
	if req.Status != nil && !req.Status.Valid() {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}

	iss, err := s.store.UpdateIssue(r.Context(), id, store.UpdateIssueParams{
		Title:         req.Title,
		Description:   req.Description,
		Status:        req.Status,
		AssigneeID:    req.AssigneeID,
		ClearAssignee: req.ClearAssignee,
		SprintID:      req.SprintID,
		ClearSprint:   req.ClearSprint,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, iss)
}

func (s *Server) deleteIssue(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	projectID, err := s.store.ProjectIDForIssue(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
		return
	}
	if err := s.store.DeleteIssue(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
