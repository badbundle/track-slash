package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

// A help-desk reporter never sees the project's own pages. These handlers are
// all they get: the form to file an issue, the list of issues they filed, and
// each of those issues with its shared comments and their replies.

func uiIsHelpDeskReporter(permissions store.ProjectPermissions) bool {
	return !permissions.CanRead && permissions.HelpDeskReporter
}

func (s *Server) renderUIHelpDesk(w http.ResponseWriter, r *http.Request, status int, panel *uiHelpDeskPanelData) {
	panel.CSRFToken = uiSessionCSRFToken(r)
	if isHTMXRequest(r) {
		renderUITemplate(w, status, "helpdesk-panel", panel)
		return
	}
	projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
	if err != nil {
		writeUIInternalError(w, "ui help desk visible projects", err)
		return
	}
	s.renderUIShell(w, r, status, uiShellData{
		User:          currentUser(r),
		Projects:      projects,
		HelpDeskPanel: panel,
	})
}

func (s *Server) uiRenderHelpDeskNew(w http.ResponseWriter, r *http.Request, status int, project model.Project, title, description, message string) {
	s.renderUIHelpDesk(w, r, status, &uiHelpDeskPanelData{
		View:        "new",
		Project:     project,
		Title:       title,
		Description: description,
		Error:       message,
	})
}

// uiCreateHelpDeskIssue files a reporter's issue. Only a title and description
// are taken; the project's members triage everything else.
func (s *Server) uiCreateHelpDeskIssue(w http.ResponseWriter, r *http.Request, project model.Project) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	rawTitle := r.Form.Get("title")
	description := r.Form.Get("description")
	title := strings.TrimSpace(rawTitle)
	if title == "" || len(title) > 200 {
		s.uiRenderHelpDeskNew(w, r, http.StatusOK, project, rawTitle, description, "Title required, max 200 chars.")
		return
	}
	reporterID := currentUser(r).ID
	created, err := s.store.CreateIssue(r.Context(), store.CreateIssueParams{
		ProjectID:   project.ID,
		Title:       title,
		Description: description,
		ReporterID:  &reporterID,
	})
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if !isHTMXRequest(r) {
		http.Redirect(w, r, uiIssuePath(created), http.StatusSeeOther)
		return
	}
	uiSetHXPushURL(w, r, uiIssuePath(created))
	s.uiRenderHelpDeskIssue(w, r, created, nil)
}

// uiReportedIssuesPage lists the issues a help-desk reporter filed in the
// project. Members have the project's own lists, so they are sent there.
func (s *Server) uiReportedIssuesPage(w http.ResponseWriter, r *http.Request) {
	if currentUser(r).ID == uuid.Nil {
		redirectUILogin(w, r)
		return
	}
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	permissions, err := s.uiProjectPermissions(r.Context(), currentUser(r), project.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if permissions.CanRead {
		http.Redirect(w, r, uiProjectViewPath(project, "all"), http.StatusSeeOther)
		return
	}
	if !permissions.HelpDeskReporter {
		writeUIStoreError(w, errUIForbidden)
		return
	}
	issues, next, err := s.reportedIssuesPage(r.Context(), project.ID, currentUser(r).ID, nil, MaxLimit)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	s.renderUIHelpDesk(w, r, http.StatusOK, &uiHelpDeskPanelData{
		View:          "issues",
		Project:       project,
		Issues:        issues,
		IssuesHasMore: next != nil,
	})
}

func (s *Server) uiBuildHelpDeskIssuePanel(ctx context.Context, issue model.Issue) (*uiHelpDeskPanelData, error) {
	project, err := s.store.GetProject(ctx, issue.ProjectID)
	if err != nil {
		return nil, err
	}
	// Oldest first, so the thread reads as a conversation ending at the reply
	// box. Only shared comments: a reporter is never a project member.
	comments, hasMore, err := s.store.ListCommentsForIssue(ctx, store.ListCommentsForIssueParams{
		IssueID: issue.ID,
		Limit:   MaxLimit,
	})
	if err != nil {
		return nil, err
	}
	items := make([]uiHelpDeskComment, 0, len(comments))
	for _, comment := range comments {
		author, err := s.uiOptionalUser(ctx, &comment.AuthorID)
		if err != nil {
			return nil, err
		}
		item := uiHelpDeskComment{
			Comment: comment,
			// Rendered without attachment targets: object-N refs in a
			// shared comment must not reach attachments the reporter
			// cannot see.
			BodyHTML:     renderMarkdown(comment.Body, nil),
			AuthorID:     comment.AuthorID,
			AuthorName:   "Unknown user",
			FromReporter: issue.ReporterID != nil && comment.AuthorID == *issue.ReporterID,
		}
		if author != nil {
			item.AuthorName = uiUserLabel(author.Name, author.Username, "")
			item.AuthorProfileImageThumbnailObjectID = author.ProfileImageThumbnailObjectID
		}
		items = append(items, item)
	}
	return &uiHelpDeskPanelData{
		View:            "issue",
		Project:         project,
		Issue:           model.NewReporterIssue(issue),
		DescriptionHTML: renderMarkdown(issue.Description, nil),
		Comments:        items,
		CommentsHasMore: hasMore,
	}, nil
}

func (s *Server) uiRenderHelpDeskIssue(w http.ResponseWriter, r *http.Request, issue model.Issue, mutate func(*uiHelpDeskPanelData)) {
	panel, err := s.uiBuildHelpDeskIssuePanel(r.Context(), issue)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if mutate != nil {
		mutate(panel)
	}
	s.renderUIHelpDesk(w, r, http.StatusOK, panel)
}

// uiIssueCommentHandler admits a project writer, or the help-desk reporter of
// the issue, to the comment routes. The handlers tell the two apart.
func (s *Server) uiIssueCommentHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		issue, deleted, ok := s.uiIssueFromRouteIncludingDeleted(w, r)
		if !ok {
			return
		}
		permissions, err := s.uiProjectPermissions(r.Context(), currentUser(r), issue.ProjectID)
		if err != nil {
			writeUIStoreError(w, err)
			return
		}
		if !permissions.CanWrite && (deleted || !followsIssue(permissions, currentUser(r), issue)) {
			writeUIStoreError(w, errUIForbidden)
			return
		}
		next(w, r)
	}
}

// uiFollowedIssue reports whether the route's issue is being read by its
// help-desk reporter, for the handlers uiIssueCommentHandler and the issue
// page share with members.
func (s *Server) uiFollowedIssue(ctx context.Context, user model.User, issue model.Issue) (bool, error) {
	permissions, err := s.uiProjectPermissions(ctx, user, issue.ProjectID)
	if err != nil {
		return false, err
	}
	return followsIssue(permissions, user, issue), nil
}

func (s *Server) uiCreateHelpDeskReply(w http.ResponseWriter, r *http.Request, issue model.Issue) {
	rawBody := r.Form.Get("body")
	body := strings.TrimSpace(rawBody)
	if body == "" || len(body) > 10000 {
		s.uiRenderHelpDeskIssue(w, r, issue, func(panel *uiHelpDeskPanelData) {
			panel.CommentBody = rawBody
			panel.CommentError = "Reply required, max 10000 chars."
		})
		return
	}
	if _, err := s.store.CreateComment(r.Context(), store.CreateCommentParams{
		IssueID:    issue.ID,
		AuthorID:   currentUser(r).ID,
		Body:       body,
		Visibility: model.CommentVisibilityShared,
	}); err != nil {
		writeUIStoreError(w, err)
		return
	}
	s.uiRenderHelpDeskIssue(w, r, issue, nil)
}

func (s *Server) uiUpdateHelpDeskReply(w http.ResponseWriter, r *http.Request, issue model.Issue, comment model.Comment) {
	rawBody := r.Form.Get("body")
	body := strings.TrimSpace(rawBody)
	if body == "" || len(body) > 10000 {
		s.uiRenderHelpDeskIssue(w, r, issue, func(panel *uiHelpDeskPanelData) {
			panel.EditCommentID = comment.ID
			panel.CommentEditBody = rawBody
			panel.CommentEditError = "Reply required, max 10000 chars."
		})
		return
	}
	if _, err := s.store.UpdateComment(r.Context(), store.UpdateCommentParams{
		ID:       comment.ID,
		AuthorID: currentUser(r).ID,
		Body:     body,
	}); err != nil {
		writeUIStoreError(w, err)
		return
	}
	s.uiRenderHelpDeskIssue(w, r, issue, nil)
}
