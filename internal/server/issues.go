package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

type createIssueReq struct {
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Priority    *model.IssuePriority `json:"priority,omitempty"`
	AssigneeID  *uuid.UUID           `json:"assignee_id,omitempty"`
	ReporterID  *uuid.UUID           `json:"reporter_id,omitempty"`
	DueDate     *model.Date          `json:"due_date,omitempty"`
}

func (s *Server) createIssue(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectIssueCreation(w, r, project.ID) {
		return
	}
	permissions, err := s.store.ProjectPermissionsForUser(r.Context(), currentUser(r), project.ID)
	if err != nil {
		writeStoreError(w, err)
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
	priority := model.PriorityP2
	if req.Priority != nil {
		if !req.Priority.Valid() {
			writeError(w, http.StatusBadRequest, "invalid priority")
			return
		}
		priority = *req.Priority
	}
	if !permissions.CanWrite && req.AssigneeID != nil {
		writeForbidden(w)
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
		ProjectID:   project.ID,
		Title:       req.Title,
		Description: req.Description,
		Priority:    priority,
		AssigneeID:  req.AssigneeID,
		ReporterID:  reporterID,
		DueDate:     req.DueDate,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, iss)
}

func (s *Server) createSubIssue(w http.ResponseWriter, r *http.Request) {
	parent, ok := s.issueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectWriteAccess(w, r, parent.ProjectID) {
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
	priority := model.PriorityP2
	if req.Priority != nil {
		if !req.Priority.Valid() {
			writeError(w, http.StatusBadRequest, "invalid priority")
			return
		}
		priority = *req.Priority
	}
	reporterID := req.ReporterID
	if reporterID == nil {
		id := currentUser(r).ID
		reporterID = &id
	} else if !currentUser(r).IsAdmin && *reporterID != currentUser(r).ID {
		writeForbidden(w)
		return
	}

	iss, err := s.store.CreateSubIssue(r.Context(), store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         req.Title,
		Description:   req.Description,
		Priority:      priority,
		AssigneeID:    req.AssigneeID,
		ReporterID:    reporterID,
		DueDate:       req.DueDate,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, iss)
}

func (s *Server) listIssues(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}

	query, err := parseIssueListQueryValues(r.URL.Query(), issueListQueryOptions{
		DefaultSort:      store.ListIssuesSortNumber,
		IncludeAssignees: true,
		AllowNumberSort:  true,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
		ProjectID:   project.ID,
		Statuses:    query.Statuses,
		Priorities:  query.Priorities,
		AssigneeIDs: query.AssigneeIDs,
		TagNames:    query.TagNames,
		Cursor:      cursor,
		Limit:       limit,
		Sort:        query.Sort,
		Direction:   query.Direction,
	}

	sprintParam := r.URL.Query().Get("sprint")
	sprintIDParam := r.URL.Query().Get("sprint_id")
	if sprintIDParam != "" {
		writeError(w, http.StatusBadRequest, "use sprint=sprint-N")
		return
	}
	switch {
	case sprintParam == "backlog":
		params.Backlog = true
	case sprintParam != "":
		number, err := parseTypedRef(sprintParam, "sprint")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		sprint, err := s.store.GetSprintByProjectNumber(r.Context(), project.ID, number)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		params.SprintID = &sprint.ID
	}

	out, hasMore, err := s.store.ListIssues(r.Context(), params)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var next *string
	if hasMore {
		last := out[len(out)-1]
		enc := encodeCursor(issueListCursor(last, query.Sort))
		next = &enc
	}
	writePage(w, out, next)
}

func (s *Server) listDeletedIssues(w http.ResponseWriter, r *http.Request) {
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
	var cursor *store.IssuesCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.IssuesCursor
		if err := decodeCursor(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &c
	}

	out, hasMore, err := s.store.ListDeletedIssues(r.Context(), store.ListDeletedIssuesParams{
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
		last := out[len(out)-1]
		enc := encodeCursor(store.IssuesCursor{Number: last.Number})
		next = &enc
	}
	writePage(w, out, next)
}

func (s *Server) listSubIssues(w http.ResponseWriter, r *http.Request) {
	parent, ok := s.issueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, parent.ProjectID) {
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

	out, hasMore, err := s.store.ListSubIssuesForIssue(r.Context(), store.ListSubIssuesForIssueParams{
		ParentIssueID: parent.ID,
		Cursor:        cursor,
		Limit:         limit,
	})
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

// maxBatchIssues caps the number of refs accepted by /{owner}/issues?refs= to keep
// query cost bounded and prevent unbounded URL length abuse.
const maxBatchIssues = 200

func (s *Server) batchIssues(w http.ResponseWriter, r *http.Request) {
	owner, ok := normalizeOwnerParam(w, r)
	if !ok {
		return
	}
	raw := r.URL.Query().Get("refs")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "refs query param required (comma-separated issue refs)")
		return
	}
	parts := strings.Split(raw, ",")
	if len(parts) > maxBatchIssues {
		writeError(w, http.StatusBadRequest, "too many refs (max 200)")
		return
	}
	out := make([]model.Issue, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		ref, err := parseIssueRef(p)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		key := ref.ProjectKey + "-" + strconv.Itoa(ref.Number)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		iss, err := s.store.GetIssueByOwnerKeyNumber(r.Context(), owner, ref.ProjectKey, ref.Number)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			writeStoreError(w, err)
			return
		}
		if !s.requireProjectAccess(w, r, iss.ProjectID) {
			return
		}
		out = append(out, iss)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getIssue(w http.ResponseWriter, r *http.Request) {
	iss, ok := s.issueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, iss.ProjectID) {
		return
	}
	writeJSON(w, http.StatusOK, iss)
}

type updateIssueReq struct {
	Title       *string                 `json:"title,omitempty"`
	Description *string                 `json:"description,omitempty"`
	Status      *model.Status           `json:"status,omitempty"`
	CloseReason *model.IssueCloseReason `json:"close_reason,omitempty"`
	Priority    *model.IssuePriority    `json:"priority,omitempty"`
	// AssigneeID: pointer-to-pointer pattern via json.RawMessage would be cleaner,
	// but v0 keeps it simple: assignee_id present sets it, assignee_id null clears.
	AssigneeID    *uuid.UUID  `json:"assignee_id,omitempty"`
	ClearAssignee bool        `json:"clear_assignee,omitempty"`
	ReporterID    *uuid.UUID  `json:"reporter_id,omitempty"`
	ClearReporter bool        `json:"clear_reporter,omitempty"`
	Sprint        *string     `json:"sprint,omitempty"`
	ClearSprint   bool        `json:"clear_sprint,omitempty"`
	DueDate       *model.Date `json:"due_date,omitempty"`
	ClearDueDate  bool        `json:"clear_due_date,omitempty"`
}

func (s *Server) updateIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectWriteAccess(w, r, issue.ProjectID) {
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
	if req.CloseReason != nil && !req.CloseReason.Valid() {
		writeError(w, http.StatusBadRequest, "invalid close_reason")
		return
	}
	if req.Status != nil && *req.Status == model.StatusClosed && issue.Status != model.StatusClosed && req.CloseReason == nil {
		writeError(w, http.StatusBadRequest, "close_reason required when closing issue")
		return
	}
	effectiveStatus := issue.Status
	if req.Status != nil {
		effectiveStatus = *req.Status
	}
	if req.CloseReason != nil && effectiveStatus != model.StatusClosed {
		writeError(w, http.StatusBadRequest, "close_reason only applies to closed issues")
		return
	}
	if req.Priority != nil && !req.Priority.Valid() {
		writeError(w, http.StatusBadRequest, "invalid priority")
		return
	}
	var sprintID *uuid.UUID
	if req.Sprint != nil && !req.ClearSprint {
		number, err := parseTypedRef(*req.Sprint, "sprint")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		sprint, err := s.store.GetSprintByProjectNumber(r.Context(), issue.ProjectID, number)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		sprintID = &sprint.ID
	}

	iss, err := s.store.UpdateIssue(r.Context(), issue.ID, store.UpdateIssueParams{
		Title:         req.Title,
		Description:   req.Description,
		Status:        req.Status,
		CloseReason:   req.CloseReason,
		Priority:      req.Priority,
		AssigneeID:    req.AssigneeID,
		ClearAssignee: req.ClearAssignee,
		ReporterID:    req.ReporterID,
		ClearReporter: req.ClearReporter,
		SprintID:      sprintID,
		ClearSprint:   req.ClearSprint,
		DueDate:       req.DueDate,
		ClearDueDate:  req.ClearDueDate,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, iss)
}

func (s *Server) deleteIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectWriteAccess(w, r, issue.ProjectID) {
		return
	}
	if err := s.store.DeleteIssue(r.Context(), issue.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) restoreIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.deletedIssueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectWriteAccess(w, r, issue.ProjectID) {
		return
	}
	restored, err := s.store.RestoreIssue(r.Context(), issue.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, restored)
}
