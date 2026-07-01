package server

import (
	"net/http"
	"strings"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

type createIssueTagReq struct {
	Name  string              `json:"name"`
	Color model.IssueTagColor `json:"color"`
}

type updateIssueTagReq struct {
	Name  *string              `json:"name,omitempty"`
	Color *model.IssueTagColor `json:"color,omitempty"`
}

type attachIssueTagReq struct {
	Tag    string `json:"tag"`
	TagRef string `json:"tag_ref"`
}

func (s *Server) createIssueTag(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	var req createIssueTagReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, ok := normalizeIssueTagNameResponse(w, req.Name)
	if !ok {
		return
	}
	color := model.IssueTagColorOrDefault(req.Color)
	if !color.Valid() {
		writeError(w, http.StatusBadRequest, "invalid color")
		return
	}
	tag, err := s.store.CreateIssueTag(r.Context(), store.CreateIssueTagParams{
		ProjectID: project.ID,
		Name:      name,
		Color:     color,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, tag)
}

func (s *Server) listIssueTags(w http.ResponseWriter, r *http.Request) {
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
	var cursor *store.IssueTagsCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.IssueTagsCursor
		if err := decodeCursor(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &c
	}
	tags, hasMore, err := s.store.ListIssueTags(r.Context(), store.ListIssueTagsParams{
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
		last := tags[len(tags)-1]
		enc := encodeCursor(store.IssueTagsCursor{Number: last.Number})
		next = &enc
	}
	writePage(w, tags, next)
}

func (s *Server) getIssueTag(w http.ResponseWriter, r *http.Request) {
	project, tag, ok := s.issueTagFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	writeJSON(w, http.StatusOK, tag)
}

func (s *Server) updateIssueTag(w http.ResponseWriter, r *http.Request) {
	project, tag, ok := s.issueTagFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	var req updateIssueTagReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var name *string
	if req.Name != nil {
		normalized, ok := normalizeIssueTagNameResponse(w, *req.Name)
		if !ok {
			return
		}
		name = &normalized
	}
	if req.Color != nil {
		color := model.IssueTagColorOrDefault(*req.Color)
		if !color.Valid() {
			writeError(w, http.StatusBadRequest, "invalid color")
			return
		}
		req.Color = &color
	}
	updated, err := s.store.UpdateIssueTag(r.Context(), store.UpdateIssueTagParams{
		ID:    tag.ID,
		Name:  name,
		Color: req.Color,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteIssueTag(w http.ResponseWriter, r *http.Request) {
	project, tag, ok := s.issueTagFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	if err := s.store.DeleteIssueTag(r.Context(), tag.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listTagsForIssue(w http.ResponseWriter, r *http.Request) {
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
	var cursor *store.IssueTagsCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.IssueTagsCursor
		if err := decodeCursor(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &c
	}
	tags, hasMore, err := s.store.ListTagsForIssue(r.Context(), store.ListTagsForIssueParams{
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
		last := tags[len(tags)-1]
		enc := encodeCursor(store.IssueTagsCursor{Number: last.Number})
		next = &enc
	}
	writePage(w, tags, next)
}

func (s *Server) attachIssueTag(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, issue.ProjectID) {
		return
	}
	var req attachIssueTagReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tag, ok := s.issueTagFromAttachRequest(w, r, issue, req)
	if !ok {
		return
	}
	if _, err := s.store.CreateIssueTagLink(r.Context(), store.CreateIssueTagLinkParams{
		IssueID: issue.ID,
		TagID:   tag.ID,
	}); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, tag)
}

func (s *Server) detachIssueTag(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, issue.ProjectID) {
		return
	}
	number, ok := parseTypedRefParam(w, r, "tagRef", "tag")
	if !ok {
		return
	}
	tag, err := s.store.GetIssueTagByProjectNumber(r.Context(), issue.ProjectID, number)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.store.DeleteIssueTagLink(r.Context(), issue.ID, tag.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) issueTagFromRoute(w http.ResponseWriter, r *http.Request) (model.Project, model.IssueTag, bool) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return model.Project{}, model.IssueTag{}, false
	}
	number, ok := parseTypedRefParam(w, r, "tagRef", "tag")
	if !ok {
		return model.Project{}, model.IssueTag{}, false
	}
	tag, err := s.store.GetIssueTagByProjectNumber(r.Context(), project.ID, number)
	if err != nil {
		writeStoreError(w, err)
		return model.Project{}, model.IssueTag{}, false
	}
	return project, tag, true
}

func (s *Server) issueTagFromAttachRequest(w http.ResponseWriter, r *http.Request, issue model.Issue, req attachIssueTagReq) (model.IssueTag, bool) {
	rawTag := strings.TrimSpace(req.Tag)
	rawRef := strings.TrimSpace(req.TagRef)
	if rawTag != "" && rawRef != "" {
		writeError(w, http.StatusBadRequest, "provide either tag or tag_ref")
		return model.IssueTag{}, false
	}
	if rawRef != "" {
		number, err := parseTypedRef(rawRef, "tag")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return model.IssueTag{}, false
		}
		tag, err := s.store.GetIssueTagByProjectNumber(r.Context(), issue.ProjectID, number)
		if err != nil {
			writeStoreError(w, err)
			return model.IssueTag{}, false
		}
		return tag, true
	}
	if rawTag == "" {
		writeError(w, http.StatusBadRequest, "tag or tag_ref required")
		return model.IssueTag{}, false
	}
	name, ok := normalizeIssueTagNameResponse(w, rawTag)
	if !ok {
		return model.IssueTag{}, false
	}
	tag, err := s.store.GetIssueTagByProjectName(r.Context(), issue.ProjectID, name)
	if err != nil {
		writeStoreError(w, err)
		return model.IssueTag{}, false
	}
	return tag, true
}

func normalizeIssueTagNameResponse(w http.ResponseWriter, raw string) (string, bool) {
	name, err := model.NormalizeIssueTagName(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return name, true
}
