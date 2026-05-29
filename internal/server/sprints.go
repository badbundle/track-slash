package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

const dateLayout = "2006-01-02"

type createSprintReq struct {
	Name      string `json:"name"`
	Goal      string `json:"goal"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

type updateSprintReq struct {
	Name      *string             `json:"name,omitempty"`
	Goal      *string             `json:"goal,omitempty"`
	StartDate *string             `json:"start_date,omitempty"`
	EndDate   *string             `json:"end_date,omitempty"`
	Status    *model.SprintStatus `json:"status,omitempty"`
}

func (s *Server) createSprint(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
		return
	}

	var req createSprintReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if len(req.Name) > 200 {
		writeError(w, http.StatusBadRequest, "name max 200 chars")
		return
	}
	if len(req.Goal) > 2000 {
		writeError(w, http.StatusBadRequest, "goal max 2000 chars")
		return
	}

	start, err := time.Parse(dateLayout, req.StartDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "start_date must be YYYY-MM-DD")
		return
	}
	end, err := time.Parse(dateLayout, req.EndDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "end_date must be YYYY-MM-DD")
		return
	}
	if end.Before(start) {
		writeError(w, http.StatusBadRequest, "end_date must be on or after start_date")
		return
	}

	sp, err := s.store.CreateSprint(r.Context(), store.CreateSprintParams{
		ProjectID: projectID,
		Name:      req.Name,
		Goal:      req.Goal,
		StartDate: start,
		EndDate:   end,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sp)
}

func (s *Server) listProjectSprints(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
		return
	}

	statusFilter := model.SprintStatus(r.URL.Query().Get("status"))
	if statusFilter != "" && !statusFilter.Valid() {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}

	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var cursor *store.SprintsCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.SprintsCursor
		if err := decodeCursor(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &c
	}

	out, hasMore, err := s.store.ListSprints(r.Context(), store.ListSprintsParams{
		ProjectID: projectID,
		Status:    statusFilter,
		Cursor:    cursor,
		Limit:     limit,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var next *string
	if hasMore {
		last := out[len(out)-1]
		enc := encodeCursor(store.SprintsCursor{
			StartDate: last.StartDate,
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		})
		next = &enc
	}
	writePage(w, out, next)
}

func (s *Server) getSprint(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	projectID, err := s.store.ProjectIDForSprint(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
		return
	}
	sp, err := s.store.GetSprint(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sp)
}

func (s *Server) updateSprint(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	projectID, err := s.store.ProjectIDForSprint(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
		return
	}
	var req updateSprintReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	params := store.UpdateSprintParams{}

	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if len(n) > 200 {
			writeError(w, http.StatusBadRequest, "name max 200 chars")
			return
		}
		params.Name = &n
	}
	if req.Goal != nil {
		if len(*req.Goal) > 2000 {
			writeError(w, http.StatusBadRequest, "goal max 2000 chars")
			return
		}
		params.Goal = req.Goal
	}
	if req.StartDate != nil {
		d, err := time.Parse(dateLayout, *req.StartDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "start_date must be YYYY-MM-DD")
			return
		}
		params.StartDate = &d
	}
	if req.EndDate != nil {
		d, err := time.Parse(dateLayout, *req.EndDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "end_date must be YYYY-MM-DD")
			return
		}
		params.EndDate = &d
	}
	if req.Status != nil {
		if !req.Status.Valid() {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		if *req.Status == model.SprintStatusCompleted {
			writeError(w, http.StatusBadRequest, "use POST /sprints/{id}/complete")
			return
		}
		params.Status = req.Status
	}

	sp, err := s.store.UpdateSprint(r.Context(), id, params)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sp)
}

func (s *Server) completeSprint(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	projectID, err := s.store.ProjectIDForSprint(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
		return
	}
	sp, err := s.store.CompleteSprint(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sp)
}

func (s *Server) deleteSprint(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	projectID, err := s.store.ProjectIDForSprint(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
		return
	}
	if err := s.store.DeleteSprint(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
