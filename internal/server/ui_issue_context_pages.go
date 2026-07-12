package server

import (
	"context"
	"errors"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"strings"
)

func (s *Server) uiCreateIssueContextLink(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}

	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxProjectContextUploadBytes+1024*1024)
		upload, err := readProjectContextUpload(r)
		if err != nil {
			s.renderUIIssuePanelWithContextModal(w, r, issue.ID, func(panel *uiIssuePanelData) {
				panel.ContextAction = "create"
				panel.ContextUploadError = err.Error()
			})
			return
		}
		if _, err := s.store.CreateIssueContext(r.Context(), store.CreateIssueContextParams{
			IssueID:        issue.ID,
			Title:          upload.Title,
			Kind:           model.ProjectContextKindText,
			ContentType:    upload.ContentType,
			Body:           upload.Body,
			SourceFilename: upload.SourceFilename,
			CreatedByID:    currentUser(r).ID,
		}); err != nil {
			writeUIStoreError(w, err)
			return
		}
		uiSetHXReplaceURL(w, r, uiIssuePath(issue))
		s.renderUIIssuePanelWithContextModal(w, r, issue.ID, nil)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	if r.Form.Get("mode") == "create" {
		titleInput := r.Form.Get("title")
		bodyInput := r.Form.Get("body")
		title, err := validateProjectContextTitle(titleInput)
		if err != nil {
			s.renderUIIssuePanelWithContextModal(w, r, issue.ID, func(panel *uiIssuePanelData) {
				panel.ContextAction = "create"
				panel.ContextTitle = titleInput
				panel.ContextBody = bodyInput
				panel.ContextCreateError = err.Error()
			})
			return
		}
		body, err := validateIssueContextBody(bodyInput)
		if err != nil {
			s.renderUIIssuePanelWithContextModal(w, r, issue.ID, func(panel *uiIssuePanelData) {
				panel.ContextAction = "create"
				panel.ContextTitle = titleInput
				panel.ContextBody = bodyInput
				panel.ContextCreateError = err.Error()
			})
			return
		}
		if _, err := s.store.CreateIssueContext(r.Context(), store.CreateIssueContextParams{
			IssueID:     issue.ID,
			Title:       title,
			Kind:        model.ProjectContextKindText,
			ContentType: "text/plain; charset=utf-8",
			Body:        body,
			CreatedByID: currentUser(r).ID,
		}); err != nil {
			writeUIStoreError(w, err)
			return
		}
		uiSetHXReplaceURL(w, r, uiIssuePath(issue))
		s.renderUIIssuePanelWithContextModal(w, r, issue.ID, nil)
		return
	}

	input := strings.TrimSpace(r.Form.Get("context"))
	contextItem, message, err := s.uiProjectContextInput(r.Context(), issue.ProjectID, input)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if message != "" {
		s.renderUIIssuePanelWithContextModal(w, r, issue.ID, func(panel *uiIssuePanelData) {
			panel.ContextAction = "attach"
			panel.ContextInput = input
			panel.ContextError = message
		})
		return
	}
	if _, err := s.store.CreateIssueContextLink(r.Context(), issue.ID, contextItem.ID); err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUIIssuePanelWithContextModal(w, r, issue.ID, func(panel *uiIssuePanelData) {
				panel.ContextAction = "attach"
				panel.ContextInput = input
				panel.ContextError = "Context already linked."
			})
			return
		}
		writeUIStoreError(w, err)
		return
	}
	uiSetHXReplaceURL(w, r, uiIssuePath(issue))
	s.renderUIIssuePanelWithContextModal(w, r, issue.ID, nil)
}

func (s *Server) uiNewIssueContextLink(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	s.renderUIIssuePanelWithContextModal(w, r, issue.ID, func(panel *uiIssuePanelData) {
		panel.ContextAction = "attach"
	})
}

func (s *Server) uiNewIssueContext(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	s.renderUIIssuePanelWithContextModal(w, r, issue.ID, func(panel *uiIssuePanelData) {
		panel.ContextAction = "create"
	})
}

func (s *Server) uiViewIssueContext(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	s.renderUIIssuePanelWithContextModal(w, r, issue.ID, nil)
}

func (s *Server) uiViewIssueContextItem(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	contextItem, ok := s.uiIssueContextFromRoute(w, r, issue)
	if !ok {
		return
	}
	s.renderUIIssuePanelWithContextModal(w, r, issue.ID, func(panel *uiIssuePanelData) {
		panel.ContextAction = "view"
		panel.ActiveContextID = contextItem.ID
		panel.ActiveContext = contextItem
	})
}

func (s *Server) uiEditIssueContext(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	contextItem, ok := s.uiIssueContextFromRoute(w, r, issue)
	if !ok {
		return
	}
	s.renderUIIssuePanelWithContextModal(w, r, issue.ID, func(panel *uiIssuePanelData) {
		panel.ContextAction = "edit"
		panel.ActiveContextID = contextItem.ID
		panel.ActiveContext = contextItem
		panel.ContextEditTitle = contextItem.Title
		panel.ContextEditBody = contextItem.Body
	})
}

func (s *Server) uiUpdateIssueContext(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	contextItem, ok := s.uiIssueContextFromRoute(w, r, issue)
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
	titleInput := r.Form.Get("title")
	bodyInput := r.Form.Get("body")
	title, err := validateProjectContextTitle(titleInput)
	if err != nil {
		s.renderUIIssuePanelWithContextModal(w, r, issue.ID, func(panel *uiIssuePanelData) {
			panel.ContextAction = "edit"
			panel.ActiveContextID = contextItem.ID
			panel.ActiveContext = contextItem
			panel.ContextEditTitle = titleInput
			panel.ContextEditBody = bodyInput
			panel.ContextEditError = err.Error()
		})
		return
	}
	var body string
	var bodyErr error
	if contextItem.Scope == model.ProjectContextScopeProject {
		body, bodyErr = validateProjectContextBody(bodyInput)
	} else {
		body, bodyErr = validateIssueContextBody(bodyInput)
	}
	if bodyErr != nil {
		s.renderUIIssuePanelWithContextModal(w, r, issue.ID, func(panel *uiIssuePanelData) {
			panel.ContextAction = "edit"
			panel.ActiveContextID = contextItem.ID
			panel.ActiveContext = contextItem
			panel.ContextEditTitle = titleInput
			panel.ContextEditBody = bodyInput
			panel.ContextEditError = bodyErr.Error()
		})
		return
	}
	if _, err := s.store.UpdateProjectContext(r.Context(), store.UpdateProjectContextParams{
		ID:          contextItem.ID,
		Title:       &title,
		Body:        &body,
		UpdatedByID: currentUser(r).ID,
	}); err != nil {
		writeUIStoreError(w, err)
		return
	}
	uiSetHXReplaceURL(w, r, uiIssuePath(issue))
	s.renderUIIssuePanelWithContextModal(w, r, issue.ID, nil)
}

func (s *Server) uiIssueContextFromRoute(w http.ResponseWriter, r *http.Request, issue model.Issue) (model.ProjectContext, bool) {
	number, err := parseTypedRef(chi.URLParam(r, "contextRef"), "context")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return model.ProjectContext{}, false
	}
	contexts, _, err := s.store.ListContextsForIssue(r.Context(), store.ListContextsForIssueParams{
		IssueID: issue.ID,
		Limit:   MaxLimit,
	})
	if err != nil {
		writeUIStoreError(w, err)
		return model.ProjectContext{}, false
	}
	for _, contextItem := range contexts {
		if contextItem.Number == number {
			return contextItem, true
		}
	}
	writeUIStoreError(w, store.ErrNotFound)
	return model.ProjectContext{}, false
}

func (s *Server) uiDeleteIssueContextLink(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	number, err := parseTypedRef(chi.URLParam(r, "contextRef"), "context")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	contextItem, err := s.store.GetProjectContextByProjectNumber(r.Context(), issue.ProjectID, number)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if err := s.store.DeleteIssueContextLink(r.Context(), issue.ID, contextItem.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	if contextItem.Scope == model.ProjectContextScopeIssue {
		if err := s.deleteProjectContextAndObjects(r.Context(), contextItem); err != nil {
			writeUIStoreError(w, err)
			return
		}
	}
	uiSetHXReplaceURL(w, r, uiIssuePath(issue))
	s.renderUIIssuePanelWithContextModal(w, r, issue.ID, nil)
}

func (s *Server) uiProjectContextInput(ctx context.Context, projectID uuid.UUID, raw string) (model.ProjectContext, string, error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return model.ProjectContext{}, "Context required.", nil
	}
	contexts, _, err := s.store.ListProjectContexts(ctx, store.ListProjectContextsParams{
		ProjectID: projectID,
		Limit:     MaxLimit,
	})
	if err != nil {
		return model.ProjectContext{}, "", err
	}
	var match model.ProjectContextSummary
	matches := 0
	for _, contextItem := range contexts {
		if strings.EqualFold(contextItem.Title, input) {
			match = contextItem
			matches++
		}
	}
	switch matches {
	case 1:
		contextItem, err := s.store.GetProjectContextByProjectNumber(ctx, projectID, match.Number)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return model.ProjectContext{}, "Context not found.", nil
			}
			return model.ProjectContext{}, "", err
		}
		return contextItem, "", nil
	case 0:
	default:
		return model.ProjectContext{}, "Multiple context items match that title.", nil
	}

	number, err := parseTypedRef(input, "context")
	if err != nil {
		return model.ProjectContext{}, "Choose project context by title.", nil
	}
	contextItem, err := s.store.GetProjectContextByProjectNumber(ctx, projectID, number)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return model.ProjectContext{}, "Context not found.", nil
		}
		return model.ProjectContext{}, "", err
	}
	if contextItem.Scope != model.ProjectContextScopeProject {
		return model.ProjectContext{}, "Context not found.", nil
	}
	return contextItem, "", nil
}
