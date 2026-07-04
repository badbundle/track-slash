package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

type issueRef struct {
	ProjectKey string
	Number     int
}

func normalizeOwnerParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	owner, err := store.NormalizeUsername(chi.URLParam(r, "owner"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return owner, true
}

func normalizeProjectKeyParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "key")))
	if !projectKeyRe.MatchString(key) {
		writeError(w, http.StatusBadRequest, "invalid project key")
		return "", false
	}
	return key, true
}

func parseIssueRef(raw string) (issueRef, error) {
	raw = strings.TrimSpace(raw)
	key, numberRaw, ok := strings.Cut(raw, "-")
	if !ok {
		return issueRef{}, fmt.Errorf("issue ref must look like KEY-123")
	}
	key = strings.ToUpper(strings.TrimSpace(key))
	if !projectKeyRe.MatchString(key) {
		return issueRef{}, fmt.Errorf("invalid issue project key")
	}
	n, err := strconv.Atoi(strings.TrimSpace(numberRaw))
	if err != nil || n < 1 {
		return issueRef{}, fmt.Errorf("invalid issue number")
	}
	return issueRef{ProjectKey: key, Number: n}, nil
}

func requireIssueRefProject(ref issueRef, projectKey string) error {
	if ref.ProjectKey != strings.ToUpper(strings.TrimSpace(projectKey)) {
		return fmt.Errorf("issues belong to different projects: %w", store.ErrConflict)
	}
	return nil
}

func parseIssueRefParam(w http.ResponseWriter, r *http.Request) (issueRef, bool) {
	ref, err := parseIssueRef(chi.URLParam(r, "issueRef"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return issueRef{}, false
	}
	return ref, true
}

func parseTypedRef(raw, prefix string) (int, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	want := prefix + "-"
	if !strings.HasPrefix(raw, want) {
		return 0, fmt.Errorf("%s ref must look like %s1", prefix, want)
	}
	n, err := strconv.Atoi(strings.TrimPrefix(raw, want))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid %s number", prefix)
	}
	return n, nil
}

func parseTypedRefParam(w http.ResponseWriter, r *http.Request, param, prefix string) (int, bool) {
	n, err := parseTypedRef(chi.URLParam(r, param), prefix)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return 0, false
	}
	return n, true
}

func (s *Server) projectFromRoute(w http.ResponseWriter, r *http.Request) (model.Project, bool) {
	owner, ok := normalizeOwnerParam(w, r)
	if !ok {
		return model.Project{}, false
	}
	key, ok := normalizeProjectKeyParam(w, r)
	if !ok {
		return model.Project{}, false
	}
	project, err := s.store.GetProjectByOwnerKey(r.Context(), owner, key)
	if err != nil {
		writeStoreError(w, err)
		return model.Project{}, false
	}
	return project, true
}

func (s *Server) issueFromRoute(w http.ResponseWriter, r *http.Request) (model.Issue, bool) {
	owner, ok := normalizeOwnerParam(w, r)
	if !ok {
		return model.Issue{}, false
	}
	ref, ok := parseIssueRefParam(w, r)
	if !ok {
		return model.Issue{}, false
	}
	issue, err := s.store.GetIssueByOwnerKeyNumber(r.Context(), owner, ref.ProjectKey, ref.Number)
	if err != nil {
		writeStoreError(w, err)
		return model.Issue{}, false
	}
	return issue, true
}

func (s *Server) deletedIssueFromRoute(w http.ResponseWriter, r *http.Request) (model.Issue, bool) {
	owner, ok := normalizeOwnerParam(w, r)
	if !ok {
		return model.Issue{}, false
	}
	ref, ok := parseIssueRefParam(w, r)
	if !ok {
		return model.Issue{}, false
	}
	issue, err := s.store.GetDeletedIssueByOwnerKeyNumber(r.Context(), owner, ref.ProjectKey, ref.Number)
	if err != nil {
		writeStoreError(w, err)
		return model.Issue{}, false
	}
	return issue, true
}
