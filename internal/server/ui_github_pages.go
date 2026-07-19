package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/githubintegration"
	"github.com/bradleymackey/track-slash/internal/store"
)

func (s *Server) uiConnectGitHubRepository(w http.ResponseWriter, r *http.Request) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectMemberManagement(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	repository := strings.TrimSpace(r.FormValue("repository"))
	if s.githubIntegration == nil {
		s.renderUIGitHubProjectError(w, r, project.ID, repository, "GitHub integration is not configured on this server.")
		return
	}
	_, err := s.githubIntegration.ConnectRepository(r.Context(), githubintegration.ConnectRepositoryParams{
		ProjectID: project.ID, Repository: repository, Token: strings.TrimSpace(r.FormValue("token")), CreatedByID: currentUser(r).ID,
	})
	if err != nil {
		s.renderUIGitHubProjectError(w, r, project.ID, repository, uiGitHubActionMessage(err))
		return
	}
	s.renderUIProjectPanel(w, r, project.ID, "about", nil)
}

func (s *Server) uiDisconnectGitHubRepository(w http.ResponseWriter, r *http.Request) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectMemberManagement(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "connectionID"))
	if err != nil {
		writeUIStoreError(w, errUIBadRequest)
		return
	}
	if err := s.store.DisconnectGitHubConnection(r.Context(), project.ID, id); err != nil {
		writeUIStoreError(w, err)
		return
	}
	s.renderUIProjectPanel(w, r, project.ID, "about", nil)
}

func (s *Server) uiCreateGitHubIssueLink(w http.ResponseWriter, r *http.Request) {
	issue, _, ok := s.uiIssueFromRouteIncludingDeleted(w, r)
	if !ok {
		return
	}
	connectionRaw := strings.TrimSpace(r.FormValue("connection_id"))
	reference := strings.TrimSpace(r.FormValue("reference"))
	connectionID, err := uuid.Parse(connectionRaw)
	if err != nil {
		s.renderUIGitHubIssueError(w, r, issue.ID, connectionRaw, reference, "Choose a repository.")
		return
	}
	if s.githubIntegration == nil {
		s.renderUIGitHubIssueError(w, r, issue.ID, connectionRaw, reference, "GitHub integration is not configured on this server.")
		return
	}
	_, err = s.githubIntegration.CreateLink(r.Context(), githubintegration.CreateLinkParams{
		IssueID: issue.ID, ConnectionID: connectionID, Reference: reference, CreatedByID: currentUser(r).ID,
	})
	if err != nil {
		s.renderUIGitHubIssueError(w, r, issue.ID, connectionRaw, reference, uiGitHubActionMessage(err))
		return
	}
	s.renderUIGitHubIssueError(w, r, issue.ID, "", "", "")
}

func (s *Server) uiDeleteGitHubIssueLink(w http.ResponseWriter, r *http.Request) {
	issue, _, ok := s.uiIssueFromRouteIncludingDeleted(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "githubLinkID"))
	if err != nil {
		writeUIStoreError(w, errUIBadRequest)
		return
	}
	if err := s.store.DeleteGitHubIssueLink(r.Context(), issue.ID, id); err != nil {
		writeUIStoreError(w, err)
		return
	}
	s.renderUIGitHubIssueError(w, r, issue.ID, "", "", "")
}

func (s *Server) uiRefreshGitHubIssueLink(w http.ResponseWriter, r *http.Request) {
	issue, _, ok := s.uiIssueFromRouteIncludingDeleted(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "githubLinkID"))
	if err != nil {
		writeUIStoreError(w, errUIBadRequest)
		return
	}
	link, err := s.store.GetGitHubIssueLink(r.Context(), id)
	if err != nil || link.IssueID != issue.ID {
		if err == nil {
			err = store.ErrNotFound
		}
		writeUIStoreError(w, err)
		return
	}
	if s.githubIntegration != nil {
		_, _ = s.githubIntegration.RefreshLink(r.Context(), id)
	}
	s.renderUIGitHubIssueError(w, r, issue.ID, "", "", "")
}

func (s *Server) renderUIGitHubProjectError(w http.ResponseWriter, r *http.Request, projectID uuid.UUID, repository, message string) {
	s.renderUIProjectPanel(w, r, projectID, "about", func(panel *uiProjectPanelData) {
		panel.GitHubRepositoryInput = repository
		panel.GitHubConnectionError = message
	})
}

func (s *Server) renderUIGitHubIssueError(w http.ResponseWriter, r *http.Request, issueID uuid.UUID, connectionID, reference, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.GitHubConnectionID = connectionID
	panel.GitHubReference = reference
	panel.GitHubError = message
	s.renderUIIssuePanelResponse(w, r, panel)
}

func uiGitHubActionMessage(err error) string {
	switch {
	case errors.Is(err, githubintegration.ErrInvalid):
		return strings.TrimSuffix(err.Error(), ": "+githubintegration.ErrInvalid.Error())
	case errors.Is(err, githubintegration.ErrUnauthorized):
		return "The token does not allow access to that GitHub resource."
	case errors.Is(err, githubintegration.ErrUnavailable):
		return "GitHub could not find that repository, branch, or pull request."
	case errors.Is(err, githubintegration.ErrRateLimited):
		return "GitHub's rate limit was reached. Try again later."
	case errors.Is(err, store.ErrConflict):
		return "That GitHub resource is already linked."
	case errors.Is(err, store.ErrNotFound):
		return "The repository connection is no longer available."
	default:
		return "GitHub could not be reached. Try again later."
	}
}
