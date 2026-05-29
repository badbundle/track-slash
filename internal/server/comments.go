package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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
	issueID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue id")
		return
	}
	projectID, err := s.store.ProjectIDForIssue(r.Context(), issueID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
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
		IssueID:  issueID,
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
	issueID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue id")
		return
	}
	projectID, err := s.store.ProjectIDForIssue(r.Context(), issueID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
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
		IssueID: issueID,
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
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	projectID, err := s.store.ProjectIDForComment(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
		return
	}
	c, err := s.store.GetComment(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) updateComment(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
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
	projectID, err := s.store.ProjectIDForComment(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
		return
	}
	c, err := s.store.UpdateComment(r.Context(), id, body)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) deleteComment(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	projectID, err := s.store.ProjectIDForComment(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.requireProjectAccess(w, r, projectID) {
		return
	}
	if err := s.store.DeleteComment(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
