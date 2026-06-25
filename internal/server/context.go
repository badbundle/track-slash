package server

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

const (
	maxProjectContextTitleLength = 200
	maxProjectContextBodyLength  = 100000
	maxProjectContextUploadBytes = maxProjectContextBodyLength + 1
)

type createProjectContextReq struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type updateProjectContextReq struct {
	Title *string `json:"title,omitempty"`
	Body  *string `json:"body,omitempty"`
}

type linkIssueContextReq struct {
	Context    string `json:"context"`
	ContextRef string `json:"context_ref"`
}

type projectContextUploadData struct {
	Title          string
	Body           string
	ContentType    string
	SourceFilename *string
}

func (s *Server) createProjectContext(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	params, ok := s.projectContextCreateParams(w, r, project.ID)
	if !ok {
		return
	}
	params.CreatedByID = currentUser(r).ID
	created, err := s.store.CreateProjectContext(r.Context(), params)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) listProjectContexts(w http.ResponseWriter, r *http.Request) {
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
	var cursor *store.ProjectContextsCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.ProjectContextsCursor
		if err := decodeCursor(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &c
	}
	out, hasMore, err := s.store.ListProjectContexts(r.Context(), store.ListProjectContextsParams{
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
		last := out[len(out)-1]
		enc := encodeCursor(store.ProjectContextsCursor{Number: last.Number})
		next = &enc
	}
	writePage(w, out, next)
}

func (s *Server) getProjectContext(w http.ResponseWriter, r *http.Request) {
	project, contextItem, ok := s.projectContextFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	writeJSON(w, http.StatusOK, contextItem)
}

func (s *Server) updateProjectContext(w http.ResponseWriter, r *http.Request) {
	project, contextItem, ok := s.projectContextFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	var req updateProjectContextReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var title *string
	if req.Title != nil {
		t, err := validateProjectContextTitle(*req.Title)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		title = &t
	}
	var body *string
	if req.Body != nil {
		b, err := validateProjectContextBody(*req.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		body = &b
	}
	updated, err := s.store.UpdateProjectContext(r.Context(), store.UpdateProjectContextParams{
		ID:          contextItem.ID,
		Title:       title,
		Body:        body,
		UpdatedByID: currentUser(r).ID,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteProjectContext(w http.ResponseWriter, r *http.Request) {
	project, contextItem, ok := s.projectContextFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	if err := s.store.DeleteProjectContext(r.Context(), contextItem.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listIssueContexts(w http.ResponseWriter, r *http.Request) {
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
	var cursor *store.ProjectContextsCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.ProjectContextsCursor
		if err := decodeCursor(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &c
	}
	out, hasMore, err := s.store.ListContextsForIssue(r.Context(), store.ListContextsForIssueParams{
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
		last := out[len(out)-1]
		enc := encodeCursor(store.ProjectContextsCursor{Number: last.Number})
		next = &enc
	}
	writePage(w, out, next)
}

func (s *Server) createIssueContextLink(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, issue.ProjectID) {
		return
	}
	var req linkIssueContextReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	raw := strings.TrimSpace(req.Context)
	if raw == "" {
		raw = strings.TrimSpace(req.ContextRef)
	}
	number, err := parseTypedRef(raw, "context")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	contextItem, err := s.store.GetProjectContextByProjectNumber(r.Context(), issue.ProjectID, number)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if _, err := s.store.CreateIssueContextLink(r.Context(), issue.ID, contextItem.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, contextItem)
}

func (s *Server) deleteIssueContextLink(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, issue.ProjectID) {
		return
	}
	number, ok := parseTypedRefParam(w, r, "contextRef", "context")
	if !ok {
		return
	}
	contextItem, err := s.store.GetProjectContextByProjectNumber(r.Context(), issue.ProjectID, number)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.store.DeleteIssueContextLink(r.Context(), issue.ID, contextItem.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) projectContextFromRoute(w http.ResponseWriter, r *http.Request) (model.Project, model.ProjectContext, bool) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return model.Project{}, model.ProjectContext{}, false
	}
	number, ok := parseTypedRefParam(w, r, "contextRef", "context")
	if !ok {
		return model.Project{}, model.ProjectContext{}, false
	}
	contextItem, err := s.store.GetProjectContextByProjectNumber(r.Context(), project.ID, number)
	if err != nil {
		writeStoreError(w, err)
		return model.Project{}, model.ProjectContext{}, false
	}
	return project, contextItem, true
}

func (s *Server) projectContextCreateParams(w http.ResponseWriter, r *http.Request, projectID uuid.UUID) (store.CreateProjectContextParams, bool) {
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		return s.projectContextUploadParams(w, r, projectID)
	}
	var req createProjectContextReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return store.CreateProjectContextParams{}, false
	}
	title, err := validateProjectContextTitle(req.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return store.CreateProjectContextParams{}, false
	}
	body, err := validateProjectContextBody(req.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return store.CreateProjectContextParams{}, false
	}
	return store.CreateProjectContextParams{
		ProjectID:   projectID,
		Title:       title,
		Kind:        model.ProjectContextKindText,
		ContentType: "text/plain; charset=utf-8",
		Body:        body,
	}, true
}

func (s *Server) projectContextUploadParams(w http.ResponseWriter, r *http.Request, projectID uuid.UUID) (store.CreateProjectContextParams, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxProjectContextUploadBytes+1024*1024)
	upload, err := readProjectContextUpload(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return store.CreateProjectContextParams{}, false
	}
	return store.CreateProjectContextParams{
		ProjectID:      projectID,
		Title:          upload.Title,
		Kind:           model.ProjectContextKindText,
		ContentType:    upload.ContentType,
		Body:           upload.Body,
		SourceFilename: upload.SourceFilename,
	}, true
}

func readProjectContextUpload(r *http.Request) (projectContextUploadData, error) {
	if err := r.ParseMultipartForm(maxProjectContextUploadBytes + 1024*1024); err != nil {
		return projectContextUploadData{}, errors.New("unable to read upload")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return projectContextUploadData{}, errors.New("file required")
	}
	defer file.Close()

	filename := filepath.Base(header.Filename)
	ext := strings.ToLower(filepath.Ext(filename))
	contentType := "text/plain; charset=utf-8"
	switch ext {
	case ".txt":
	case ".md", ".markdown":
		contentType = "text/markdown; charset=utf-8"
	default:
		return projectContextUploadData{}, errors.New("file must be .txt, .md, or .markdown")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxProjectContextUploadBytes))
	if err != nil {
		return projectContextUploadData{}, errors.New("unable to read upload")
	}
	if len(data) > maxProjectContextBodyLength {
		return projectContextUploadData{}, errors.New("body required, max 100000 chars")
	}
	body := string(data)
	if !utf8.ValidString(body) {
		return projectContextUploadData{}, errors.New("file must be UTF-8 text")
	}
	titleRaw := r.FormValue("title")
	if strings.TrimSpace(titleRaw) == "" {
		titleRaw = strings.TrimSuffix(filename, ext)
	}
	title, err := validateProjectContextTitle(titleRaw)
	if err != nil {
		return projectContextUploadData{}, err
	}
	body, err = validateProjectContextBody(body)
	if err != nil {
		return projectContextUploadData{}, err
	}
	return projectContextUploadData{
		Title:          title,
		Body:           body,
		ContentType:    contentType,
		SourceFilename: &filename,
	}, nil
}

func validateProjectContextTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" || len(title) > maxProjectContextTitleLength {
		return "", errors.New("title required, max 200 chars")
	}
	return title, nil
}

func validateProjectContextBody(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", errors.New("body must be UTF-8 text")
	}
	if strings.TrimSpace(raw) == "" || len(raw) > maxProjectContextBodyLength {
		return "", errors.New("body required, max 100000 chars")
	}
	return raw, nil
}
