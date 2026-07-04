package server

import (
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"net/http"
	"strings"
)

func (s *Server) uiIssuePage(w http.ResponseWriter, r *http.Request) {
	issue, deleted, ok := s.uiIssueFromRouteIncludingDeleted(w, r)
	if !ok {
		return
	}
	if deleted {
		panel, err := s.uiBuildDeletedIssuePanel(r.Context(), r, issue)
		if err != nil {
			writeUIStoreError(w, err)
			return
		}
		projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		s.renderUIShell(w, r, http.StatusOK, uiShellData{
			User:              currentUser(r),
			Projects:          projects,
			CurrentProjectID:  panel.Project.ID,
			CurrentView:       "projects",
			DeletedIssuePanel: panel,
		})
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.renderUIShell(w, r, http.StatusOK, uiShellData{
		User:             currentUser(r),
		Projects:         projects,
		CurrentProjectID: panel.Project.ID,
		CurrentView:      "projects",
		IssuePanel:       panel,
	})
}

func (s *Server) uiIssuePanel(w http.ResponseWriter, r *http.Request) {
	issue, deleted, ok := s.uiIssueFromRouteIncludingDeleted(w, r)
	if !ok {
		return
	}
	if deleted {
		panel, err := s.uiBuildDeletedIssuePanel(r.Context(), r, issue)
		if err != nil {
			writeUIStoreError(w, err)
			return
		}
		renderUITemplate(w, http.StatusOK, "deleted-issue-panel", panel)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiDeleteIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if err := s.store.DeleteIssue(r.Context(), issue.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	backHref := uiAppendDeletedIssueQuery(panel.BackHref, issue.Identifier)
	if !isHTMXRequest(r) {
		http.Redirect(w, r, backHref, http.StatusSeeOther)
		return
	}
	uiSetHXPushURL(w, r, backHref)
	s.renderUIIssueBackTarget(w, r, panel, &uiIssueDeleteNotice{Issue: issue})
}

func (s *Server) uiRestoreIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiDeletedIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	restored, err := s.store.RestoreIssue(r.Context(), issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if !isHTMXRequest(r) {
		http.Redirect(w, r, uiIssuePath(restored), http.StatusSeeOther)
		return
	}
	uiSetHXPushURL(w, r, uiIssuePath(restored))
	panel, err := s.uiBuildIssuePanel(r.Context(), r, restored.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssueTitle(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditTitle = true
	panel.TitleInput = issue.Title
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueTitle(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	titleInput := r.Form.Get("title")
	title := strings.TrimSpace(titleInput)
	if title == "" || len(title) > 200 {
		panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
		if err != nil {
			writeUIStoreError(w, err)
			return
		}
		panel.EditTitle = true
		panel.TitleInput = titleInput
		panel.TitleError = "Title required, max 200 chars."
		renderUITemplate(w, http.StatusOK, "issue-panel", panel)
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, store.UpdateIssueParams{
		Title: &title,
	})
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssueDescription(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditDescription = true
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueDescription(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	description := r.Form.Get("description")
	if strings.TrimSpace(description) == "" {
		description = ""
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, store.UpdateIssueParams{
		Description: &description,
	})
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssueStatus(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditStatus = true
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueStatus(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	status := model.Status(strings.TrimSpace(r.Form.Get("status")))
	if !status.Valid() {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	if status == model.StatusClosed && issue.CloseReason == nil {
		panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
		if err != nil {
			writeUIStoreError(w, err)
			return
		}
		panel.PendingCloseReason = true
		panel.Issue.Status = model.StatusClosed
		panel.CanEditSprint = false
		renderUITemplate(w, http.StatusOK, "issue-panel", panel)
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, store.UpdateIssueParams{
		Status: &status,
	})
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssueCloseReason(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if issue.Status == model.StatusClosed {
		panel.EditCloseReason = true
	}
	if issue.CloseReason != nil {
		panel.CloseReasonInput = string(*issue.CloseReason)
	} else if issue.Status != model.StatusClosed {
		panel.PendingCloseReason = true
		panel.Issue.Status = model.StatusClosed
		panel.CanEditSprint = false
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueCloseReason(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	reason := model.IssueCloseReason(strings.TrimSpace(r.Form.Get("close_reason")))
	if !reason.Valid() {
		s.renderUIIssuePanelWithCloseReasonError(w, r, issue.ID, string(reason), "Choose a close reason.")
		return
	}
	params := store.UpdateIssueParams{
		CloseReason: &reason,
	}
	if issue.Status != model.StatusClosed {
		status := model.StatusClosed
		params.Status = &status
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, params)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssuePriority(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditPriority = true
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssuePriority(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	priority := model.IssuePriority(strings.TrimSpace(r.Form.Get("priority")))
	if !priority.Valid() {
		http.Error(w, "invalid priority", http.StatusBadRequest)
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, store.UpdateIssueParams{
		Priority: &priority,
	})
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssueDueDate(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditDueDate = true
	panel.DueDateInput = uiDueDateValue(panel.Issue.DueDate)
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueDueDate(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	input := strings.TrimSpace(r.Form.Get("due_date"))
	params := store.UpdateIssueParams{}
	if input == "" {
		params.ClearDueDate = true
	} else {
		dueDate, err := model.ParseDate(input)
		if err != nil {
			s.renderUIIssuePanelWithDueDateError(w, r, issue.ID, input, "Use YYYY-MM-DD.")
			return
		}
		params.DueDate = &dueDate
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, params)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssueAssignee(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditAssignee = true
	panel.AssigneeInput = uiIssueUserInput(panel.Assignee)
	if err := s.uiPopulateIssueMemberOptions(r.Context(), panel); err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueAssignee(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	input := r.Form.Get("assignee")
	assigneeID, clear, message, err := s.uiIssuePersonID(r.Context(), issue.ProjectID, input)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if message != "" {
		s.renderUIIssuePanelWithAssigneeError(w, r, issue.ID, input, message)
		return
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, store.UpdateIssueParams{
		AssigneeID:    assigneeID,
		ClearAssignee: clear,
	})
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssueReporter(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditReporter = true
	panel.ReporterInput = uiIssueUserInput(panel.Reporter)
	if err := s.uiPopulateIssueMemberOptions(r.Context(), panel); err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueReporter(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	input := r.Form.Get("reporter")
	reporterID, clear, message, err := s.uiIssuePersonID(r.Context(), issue.ProjectID, input)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if message != "" {
		s.renderUIIssuePanelWithReporterError(w, r, issue.ID, input, message)
		return
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, store.UpdateIssueParams{
		ReporterID:    reporterID,
		ClearReporter: clear,
	})
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssueSprint(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if !panel.CanEditSprint {
		renderUITemplate(w, http.StatusOK, "issue-panel", panel)
		return
	}
	panel.EditSprint = true
	panel.SprintInput = uiIssueSprintInput(panel.Sprint)
	if err := s.uiPopulateIssueSprintOptions(r.Context(), panel); err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueSprint(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	if issue.ParentIssueID != nil || issue.Status.CountsAsDone() {
		writeUIStoreError(w, store.ErrConflict)
		return
	}
	input := r.Form.Get("sprint")
	sprintID, clear, message, err := s.uiIssueSprintID(r.Context(), issue.ProjectID, input)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if message != "" {
		s.renderUIIssuePanelWithSprintError(w, r, issue.ID, input, message)
		return
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, store.UpdateIssueParams{
		SprintID:    sprintID,
		ClearSprint: clear,
	})
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}
