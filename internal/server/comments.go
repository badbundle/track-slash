package server

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

type createCommentReq struct {
	AuthorID   uuid.UUID               `json:"author_id,omitempty"`
	Body       string                  `json:"body"`
	Visibility model.CommentVisibility `json:"visibility,omitempty"`
}

// updateCommentReq changes a comment's body, its visibility, or both.
type updateCommentReq struct {
	Body       *string                  `json:"body,omitempty"`
	Visibility *model.CommentVisibility `json:"visibility,omitempty"`
}

const commentVisibilityError = "visibility must be shared or members"

const reporterCommentVisibilityError = "a help desk reporter's comments are always shared"

// commentVisibleTo reports whether a reader with these permissions may see the
// comment at all. A hidden comment is reported as missing, not forbidden, so
// its existence does not leak.
func commentVisibleTo(permissions store.ProjectPermissions, comment model.Comment) bool {
	return comment.Visibility != model.CommentVisibilityMembers || permissions.CanReadMembersOnly
}

func (s *Server) createComment(w http.ResponseWriter, r *http.Request) {
	issue, permissions, ok := s.issueWithAccessFromRoute(w, r)
	if !ok {
		return
	}
	if !canCommentOnIssue(permissions, currentUser(r), issue) {
		writeForbidden(w)
		return
	}
	var req createCommentReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > 10000 {
		writeError(w, http.StatusBadRequest, "body required, max 10000 chars")
		return
	}
	if req.Visibility != "" && !req.Visibility.Valid() {
		writeError(w, http.StatusBadRequest, commentVisibilityError)
		return
	}
	// A reporter's replies are always visible to them.
	if followsIssue(permissions, currentUser(r), issue) {
		if req.Visibility == model.CommentVisibilityMembers {
			writeError(w, http.StatusBadRequest, reporterCommentVisibilityError)
			return
		}
		req.Visibility = model.CommentVisibilityShared
	}

	c, err := s.store.CreateComment(r.Context(), store.CreateCommentParams{
		IssueID:    issue.ID,
		AuthorID:   currentUser(r).ID,
		Body:       body,
		Visibility: req.Visibility,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	issue, permissions, ok := s.issueWithAccessFromRoute(w, r)
	if !ok {
		return
	}
	if !permissions.CanRead && !followsIssue(permissions, currentUser(r), issue) {
		writeForbidden(w)
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var cursor *store.CommentsCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.CommentsCursor
		if err := decodeCursor(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &c
	}

	comments, hasMore, err := s.store.ListCommentsForIssue(r.Context(), store.ListCommentsForIssueParams{
		IssueID:            issue.ID,
		Cursor:             cursor,
		Limit:              limit,
		IncludeMembersOnly: permissions.CanReadMembersOnly,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var next *string
	if hasMore {
		last := comments[len(comments)-1]
		enc := encodeCursor(store.CommentsCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		next = &enc
	}
	writePage(w, comments, next)
}

func (s *Server) getComment(w http.ResponseWriter, r *http.Request) {
	issue, permissions, comment, ok := s.commentWithAccessFromRoute(w, r)
	if !ok {
		return
	}
	if !permissions.CanRead && !followsIssue(permissions, currentUser(r), issue) {
		writeForbidden(w)
		return
	}
	if !commentVisibleTo(permissions, comment) {
		writeStoreError(w, store.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, comment)
}

func (s *Server) updateComment(w http.ResponseWriter, r *http.Request) {
	issue, permissions, comment, ok := s.commentWithAccessFromRoute(w, r)
	if !ok {
		return
	}
	var req updateCommentReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Body == nil && req.Visibility == nil {
		writeError(w, http.StatusBadRequest, "body or visibility required")
		return
	}
	body := comment.Body
	if req.Body != nil {
		body = strings.TrimSpace(*req.Body)
		if body == "" || len(body) > 10000 {
			writeError(w, http.StatusBadRequest, "body required, max 10000 chars")
			return
		}
	}
	if req.Visibility != nil && !req.Visibility.Valid() {
		writeError(w, http.StatusBadRequest, commentVisibilityError)
		return
	}
	user := currentUser(r)
	// A comment the caller cannot see is missing, as it is to getComment.
	if !commentVisibleTo(permissions, comment) {
		writeStoreError(w, store.ErrNotFound)
		return
	}
	if !canCommentOnIssue(permissions, user, issue) || comment.AuthorID != user.ID {
		writeForbidden(w)
		return
	}
	if followsIssue(permissions, user, issue) && req.Visibility != nil && *req.Visibility == model.CommentVisibilityMembers {
		writeError(w, http.StatusBadRequest, reporterCommentVisibilityError)
		return
	}
	c, err := s.store.UpdateComment(r.Context(), store.UpdateCommentParams{
		ID:         comment.ID,
		AuthorID:   user.ID,
		Body:       body,
		Visibility: req.Visibility,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) deleteComment(w http.ResponseWriter, r *http.Request) {
	issue, permissions, comment, ok := s.commentWithAccessFromRoute(w, r)
	if !ok {
		return
	}
	user := currentUser(r)
	if !commentVisibleTo(permissions, comment) {
		writeStoreError(w, store.ErrNotFound)
		return
	}
	if !canCommentOnIssue(permissions, user, issue) || comment.AuthorID != user.ID {
		writeForbidden(w)
		return
	}
	if err := s.store.DeleteComment(r.Context(), store.DeleteCommentParams{ID: comment.ID, AuthorID: user.ID}); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) commentWithAccessFromRoute(w http.ResponseWriter, r *http.Request) (model.Issue, store.ProjectPermissions, model.Comment, bool) {
	issue, permissions, ok := s.issueWithAccessFromRoute(w, r)
	if !ok {
		return model.Issue{}, store.ProjectPermissions{}, model.Comment{}, false
	}
	number, ok := parseTypedRefParam(w, r, "commentRef", "comment")
	if !ok {
		return model.Issue{}, store.ProjectPermissions{}, model.Comment{}, false
	}
	comment, err := s.store.GetCommentForIssueByNumber(r.Context(), issue.ID, number)
	if err != nil {
		writeStoreError(w, err)
		return model.Issue{}, store.ProjectPermissions{}, model.Comment{}, false
	}
	return issue, permissions, comment, true
}
