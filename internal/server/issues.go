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

	iss, err := s.store.CreateIssue(r.Context(), store.CreateIssueParams{
		ProjectID:   projectID,
		Title:       req.Title,
		Description: req.Description,
		AssigneeID:  req.AssigneeID,
		ReporterID:  req.ReporterID,
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

	statusFilter := model.Status(r.URL.Query().Get("status"))
	if statusFilter != "" && !statusFilter.Valid() {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}

	out, err := s.store.ListIssues(r.Context(), store.ListIssuesParams{
		ProjectID: projectID,
		Status:    statusFilter,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
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
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getIssue(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
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
}

func (s *Server) updateIssue(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
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
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, iss)
}
