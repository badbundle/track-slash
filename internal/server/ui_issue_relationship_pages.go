package server

import (
	"errors"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/google/uuid"
	"net/http"
	"strings"
)

func (s *Server) uiNewIssueLink(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.AddLink = true
	panel.LinkRelation = string(model.LinkTypeRelatesTo)
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiCreateIssueLink(w http.ResponseWriter, r *http.Request) {
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
	target := strings.TrimSpace(r.Form.Get("target_issue"))
	relation := strings.TrimSpace(r.Form.Get("relation"))
	params, message, err := s.uiIssueLinkFormParams(r.Context(), issue, target, relation)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if message != "" {
		s.renderUIIssuePanelWithLinkError(w, r, issue.ID, uuid.Nil, target, relation, message)
		return
	}
	if _, err := s.store.CreateIssueLink(r.Context(), store.CreateIssueLinkParams(params)); err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUIIssuePanelWithLinkError(w, r, issue.ID, uuid.Nil, target, relation, "Link already exists or cannot be created.")
			return
		}
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssueLink(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	link, ok := s.uiIssueLinkFromRoute(w, r, issue)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditLinkID = link.ID
	panel.LinkRelation = uiIssueLinkRelation(link, issue.ID)
	panel.LinkTarget = s.uiIssueLinkTargetIdentifier(r.Context(), issue.ID, link)
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueLink(w http.ResponseWriter, r *http.Request) {
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
	link, ok := s.uiIssueLinkFromRoute(w, r, issue)
	if !ok {
		return
	}
	target := strings.TrimSpace(r.Form.Get("target_issue"))
	relation := strings.TrimSpace(r.Form.Get("relation"))
	params, message, err := s.uiIssueLinkFormParams(r.Context(), issue, target, relation)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if message != "" {
		s.renderUIIssuePanelWithLinkError(w, r, issue.ID, link.ID, target, relation, message)
		return
	}
	if _, err := s.store.UpdateIssueLink(r.Context(), link.ID, params); err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUIIssuePanelWithLinkError(w, r, issue.ID, link.ID, target, relation, "Link already exists or cannot be updated.")
			return
		}
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiDeleteIssueLink(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	link, ok := s.uiIssueLinkFromRoute(w, r, issue)
	if !ok {
		return
	}
	if err := s.store.DeleteIssueLink(r.Context(), link.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiNewSubIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.AddSubIssue = true
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiCreateSubIssue(w http.ResponseWriter, r *http.Request) {
	parent, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(r.Form.Get("title"))
	if title == "" || len(title) > 200 {
		s.renderUIIssuePanelWithSubIssueError(w, r, parent.ID, r.Form.Get("title"), "Title required, max 200 chars.")
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), parent.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	reporterID := currentUser(r).ID
	if _, err := s.store.CreateSubIssue(r.Context(), store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         title,
		Priority:      model.PriorityP2,
		ReporterID:    &reporterID,
	}); err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUIIssuePanelWithSubIssueError(w, r, parent.ID, r.Form.Get("title"), "Sub-issue could not be created for this issue.")
			return
		}
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, parent.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiCreateComment(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	body := strings.TrimSpace(r.Form.Get("body"))
	if body == "" || len(body) > 10000 {
		s.renderUIIssuePanelWithCommentError(w, r, issue.ID, r.Form.Get("body"), "Comment required, max 10000 chars.")
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	if _, err := s.store.CreateComment(r.Context(), store.CreateCommentParams{
		IssueID:  issue.ID,
		AuthorID: currentUser(r).ID,
		Body:     body,
	}); err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditComment(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	user := currentUser(r)
	if err := s.uiRequireProjectAccess(r.Context(), user, issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	comment, ok := s.uiCommentFromRoute(w, r, issue)
	if !ok {
		return
	}
	if comment.AuthorID != user.ID {
		writeUIStoreError(w, errUIForbidden)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditCommentID = comment.ID
	panel.CommentEditBody = comment.Body
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateComment(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	user := currentUser(r)
	if err := s.uiRequireProjectAccess(r.Context(), user, issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	comment, ok := s.uiCommentFromRoute(w, r, issue)
	if !ok {
		return
	}
	if comment.AuthorID != user.ID {
		writeUIStoreError(w, errUIForbidden)
		return
	}
	body := strings.TrimSpace(r.Form.Get("body"))
	if body == "" || len(body) > 10000 {
		s.renderUIIssuePanelWithCommentEditError(w, r, issue.ID, comment.ID, r.Form.Get("body"), "Comment required, max 10000 chars.")
		return
	}
	updated, err := s.store.UpdateComment(r.Context(), store.UpdateCommentParams{
		ID:       comment.ID,
		AuthorID: user.ID,
		Body:     body,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUIIssuePanelWithCommentEditError(w, r, issue.ID, comment.ID, r.Form.Get("body"), "Comment required, max 10000 chars.")
			return
		}
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.IssueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) renderUIIssuePanelWithLinkError(w http.ResponseWriter, r *http.Request, issueID, editLinkID uuid.UUID, target, relation, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.AddLink = editLinkID == uuid.Nil
	panel.EditLinkID = editLinkID
	panel.LinkTarget = target
	panel.LinkRelation = relation
	panel.LinkError = message
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}
