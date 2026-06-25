package server_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func (e *httpEnv) projectContextPath(contextItem model.ProjectContext) string {
	return e.projectPath() + "/context/" + contextItem.Ref
}

func (e *httpEnv) issueContextPath(iss model.Issue) string {
	return e.issuePath(iss) + "/context"
}

func (e *httpEnv) doMultipartContext(t *testing.T, fields map[string]string, filename, content string) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close: %v", err)
	}
	req, err := http.NewRequestWithContext(e.ctx, http.MethodPost, e.ts.URL+apiPath(e.projectPath()+"/context"), &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+e.authToken)
	res, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do multipart: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return res.StatusCode, body
}

func TestHTTPProjectContextCRUDAndIssueLinks(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	code, body := e.do(t, http.MethodPost, e.projectPath()+"/context", map[string]any{
		"title": "Architecture",
		"body":  "Use existing store transactions.",
	})
	if code != http.StatusCreated {
		t.Fatalf("create context code = %d body = %s", code, body)
	}
	contextItem := decode[model.ProjectContext](t, body)
	if contextItem.Ref != "context-1" || contextItem.Kind != model.ProjectContextKindText || contextItem.CreatedByID != e.adminID || contextItem.UpdatedByID != e.adminID {
		t.Fatalf("created context = %+v", contextItem)
	}

	code, body = e.do(t, http.MethodGet, e.projectPath()+"/context?limit=1", nil)
	if code != http.StatusOK {
		t.Fatalf("list context code = %d body = %s", code, body)
	}
	page := decodePage[model.ProjectContextSummary](t, body)
	if len(page.Items) != 1 || page.Items[0].Ref != contextItem.Ref || page.Items[0].LinkedIssueCount != 0 {
		t.Fatalf("list page = %+v", page)
	}

	code, body = e.do(t, http.MethodGet, e.projectContextPath(contextItem), nil)
	if code != http.StatusOK {
		t.Fatalf("get context code = %d body = %s", code, body)
	}
	gotContext := decode[model.ProjectContext](t, body)
	if gotContext.ID != contextItem.ID || gotContext.Body != contextItem.Body {
		t.Fatalf("got context = %+v, want %+v", gotContext, contextItem)
	}
	code, body = e.do(t, http.MethodGet, e.projectPath()+"/context/context-9999", nil)
	if code != http.StatusNotFound {
		t.Fatalf("unknown context code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPatch, e.projectContextPath(contextItem), map[string]any{
		"title": "Architecture guide",
		"body":  "Use existing store transactions and context links.",
	})
	if code != http.StatusOK {
		t.Fatalf("patch context code = %d body = %s", code, body)
	}
	contextItem = decode[model.ProjectContext](t, body)
	if contextItem.Title != "Architecture guide" || !strings.Contains(contextItem.Body, "context links") {
		t.Fatalf("patched context = %+v", contextItem)
	}

	code, body = e.doMultipartContext(t, map[string]string{}, "notes.md", "# Agent notes")
	if code != http.StatusCreated {
		t.Fatalf("multipart context code = %d body = %s", code, body)
	}
	uploaded := decode[model.ProjectContext](t, body)
	if uploaded.Ref != "context-2" || uploaded.Title != "notes" || uploaded.ContentType != "text/markdown; charset=utf-8" || uploaded.SourceFilename == nil || *uploaded.SourceFilename != "notes.md" {
		t.Fatalf("uploaded context = %+v", uploaded)
	}

	issue, err := e.store.CreateIssue(e.ctx, storeCreateIssue(e.projectID, "context target"))
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	code, body = e.do(t, http.MethodPost, e.issueContextPath(issue), map[string]any{"context": contextItem.Ref})
	if code != http.StatusCreated {
		t.Fatalf("link context code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, e.issueContextPath(issue), nil)
	if code != http.StatusOK {
		t.Fatalf("list issue context code = %d body = %s", code, body)
	}
	issueContexts := decodePage[model.ProjectContext](t, body)
	if len(issueContexts.Items) != 1 || issueContexts.Items[0].Body != contextItem.Body {
		t.Fatalf("issue contexts = %+v", issueContexts)
	}
	code, body = e.do(t, http.MethodPost, e.issueContextPath(issue), map[string]any{"context": contextItem.Ref})
	if code != http.StatusConflict {
		t.Fatalf("duplicate context code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodDelete, e.issueContextPath(issue)+"/"+contextItem.Ref, nil)
	if code != http.StatusNoContent {
		t.Fatalf("unlink context code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodDelete, e.projectContextPath(contextItem), nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete context code = %d body = %s", code, body)
	}
}

func TestHTTPProjectContextValidation(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{"empty title", map[string]any{"title": "", "body": "body"}},
		{"empty body", map[string]any{"title": "Title", "body": ""}},
		{"long title", map[string]any{"title": strings.Repeat("x", 201), "body": "body"}},
	} {
		code, body := e.do(t, http.MethodPost, e.projectPath()+"/context", tc.body)
		if code != http.StatusBadRequest {
			t.Fatalf("%s code = %d body = %s", tc.name, code, body)
		}
	}
	code, body := e.doMultipartContext(t, nil, "image.png", "not really an image")
	if code != http.StatusBadRequest {
		t.Fatalf("bad upload extension code = %d body = %s", code, body)
	}
}

func storeCreateIssue(projectID uuid.UUID, title string) store.CreateIssueParams {
	return store.CreateIssueParams{ProjectID: projectID, Title: title}
}
