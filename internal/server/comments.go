package server

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

type createCommentReq struct {
	AuthorID uuid.UUID `json:"author_id,omitempty"`
	Body     string    `json:"body"`
}

type updateCommentReq struct {
	Body string `json:"body"`
}

func (s *Server) createComment(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, issue.ProjectID) {
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

	c, err := s.store.CreateComment(r.Context(), store.CreateCommentParams{
		IssueID:  issue.ID,
		AuthorID: currentUser(r).ID,
		Body:     body,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, issue.ProjectID) {
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
		IssueID: issue.ID,
		Cursor:  cursor,
		Limit:   limit,
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
	issue, comment, ok := s.commentFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, issue.ProjectID) {
		return
	}
	writeJSON(w, http.StatusOK, comment)
}

func (s *Server) updateComment(w http.ResponseWriter, r *http.Request) {
	issue, comment, ok := s.commentFromRoute(w, r)
	if !ok {
		return
	}
	var req updateCommentReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > 10000 {
		writeError(w, http.StatusBadRequest, "body required, max 10000 chars")
		return
	}
	if !s.requireProjectAccess(w, r, issue.ProjectID) {
		return
	}
	user := currentUser(r)
	if comment.AuthorID != user.ID {
		writeForbidden(w)
		return
	}
	c, err := s.store.UpdateComment(r.Context(), store.UpdateCommentParams{
		ID:       comment.ID,
		AuthorID: user.ID,
		Body:     body,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) deleteComment(w http.ResponseWriter, r *http.Request) {
	issue, comment, ok := s.commentFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, issue.ProjectID) {
		return
	}
	user := currentUser(r)
	if comment.AuthorID != user.ID {
		writeForbidden(w)
		return
	}
	if err := s.store.DeleteComment(r.Context(), store.DeleteCommentParams{ID: comment.ID, AuthorID: user.ID}); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) commentFromRoute(w http.ResponseWriter, r *http.Request) (model.Issue, model.Comment, bool) {
	issue, ok := s.issueFromRoute(w, r)
	if !ok {
		return model.Issue{}, model.Comment{}, false
	}
	number, ok := parseTypedRefParam(w, r, "commentRef", "comment")
	if !ok {
		return model.Issue{}, model.Comment{}, false
	}
	comment, err := s.store.GetCommentForIssueByNumber(r.Context(), issue.ID, number)
	if err != nil {
		writeStoreError(w, err)
		return model.Issue{}, model.Comment{}, false
	}
	return issue, comment, true
}
