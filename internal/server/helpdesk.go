package server

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

// errIssueRouteForbidden refuses a user blocked from the issue's project. Each
// surface maps it to its own forbidden response.
var errIssueRouteForbidden = errors.New("forbidden")

// issueRouteAccess is the check every route that names an issue makes before
// anything else, and the permissions it returns are for that issue. A user
// blocked from the project is refused outright, even for issues they filed. A
// help-desk reporter is told that an issue they did not report does not
// exist, so the issues of other reporters cannot be counted or probed. So is
// anyone outside the project asking for a private issue, except its reporter,
// who follows it like a help-desk reporter. Routes that serve reporters decide
// what to show of their own.
func (s *Server) issueRouteAccess(ctx context.Context, user model.User, issue model.Issue) (store.ProjectPermissions, error) {
	permissions, err := s.store.ProjectPermissionsForUser(ctx, user, issue.ProjectID)
	if err != nil {
		return store.ProjectPermissions{}, err
	}
	if permissions.HidesIssue(issue) {
		narrowed := permissions.ForIssue(user, issue)
		if !narrowed.CanFollowIssue(user, issue) {
			return store.ProjectPermissions{}, store.ErrNotFound
		}
		return narrowed, nil
	}
	if permissions.CanRead {
		return permissions, nil
	}
	if permissions.IsBlocked {
		return store.ProjectPermissions{}, errIssueRouteForbidden
	}
	if permissions.HelpDeskReporter && !permissions.CanFollowIssue(user, issue) {
		return store.ProjectPermissions{}, store.ErrNotFound
	}
	return permissions, nil
}

// deletedIssueRouteAccess is issueRouteAccess for deleted issues, which a
// reporter who follows their issue never sees, not even one they filed.
func (s *Server) deletedIssueRouteAccess(ctx context.Context, user model.User, issue model.Issue) (store.ProjectPermissions, error) {
	permissions, err := s.issueRouteAccess(ctx, user, issue)
	if err != nil {
		return store.ProjectPermissions{}, err
	}
	if !permissions.CanRead && (permissions.HelpDeskReporter || permissions.PrivateIssueReporter) {
		return store.ProjectPermissions{}, store.ErrNotFound
	}
	return permissions, nil
}

// followsIssue reports whether the user reads this issue as its reporter (in
// a help desk, or of a private issue) rather than as someone who can read it.
func followsIssue(permissions store.ProjectPermissions, user model.User, issue model.Issue) bool {
	return !permissions.CanRead && permissions.CanFollowIssue(user, issue)
}

// canCommentOnIssue reports whether the user may add to the issue's comments:
// a project writer, or the help-desk reporter on their own issue.
func canCommentOnIssue(permissions store.ProjectPermissions, user model.User, issue model.Issue) bool {
	return permissions.CanWrite || followsIssue(permissions, user, issue)
}

const helpDeskIssueFieldsError = "help desk issues take only a title and description"

// reportedIssuesPage lists the issues a help-desk reporter filed in a project,
// newest first, reduced to what they may see.
func (s *Server) reportedIssuesPage(ctx context.Context, projectID, reporterID uuid.UUID, cursor *store.IssuesCursor, limit int) ([]model.ReporterIssue, *string, error) {
	issues, hasMore, err := s.store.ListIssues(ctx, store.ListIssuesParams{
		ProjectID:        projectID,
		ReporterID:       &reporterID,
		IncludeSubIssues: true,
		// The reporter's own issues, private ones included.
		IncludePrivate: true,
		Cursor:         cursor,
		Limit:          limit,
		Sort:           store.ListIssuesSortCreated,
		Direction:      store.ListIssuesSortDescending,
	})
	if err != nil {
		return nil, nil, err
	}
	out := make([]model.ReporterIssue, 0, len(issues))
	for _, issue := range issues {
		out = append(out, model.NewReporterIssue(issue))
	}
	var next *string
	if hasMore {
		enc := encodeCursor(issueListCursor(issues[len(issues)-1], store.ListIssuesSortCreated))
		next = &enc
	}
	return out, next, nil
}
