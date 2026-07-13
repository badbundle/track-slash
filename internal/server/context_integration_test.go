package server_test

import (
	"bytes"
	"errors"
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
	return e.doMultipartContextAt(t, e.projectPath()+"/context", fields, filename, content)
}

func (e *httpEnv) doMultipartContextAt(t *testing.T, path string, fields map[string]string, filename, content string) (int, []byte) {
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
	req, err := http.NewRequestWithContext(e.ctx, http.MethodPost, e.ts.URL+apiPath(path), &buf)
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
	if contextItem.Ref != "context-1" || contextItem.Scope != model.ProjectContextScopeProject || contextItem.Kind != model.ProjectContextKindText || contextItem.ContentType != "text/markdown; charset=utf-8" || contextItem.Position == nil || *contextItem.Position != 1 || contextItem.CreatedByID != e.adminID || contextItem.UpdatedByID != e.adminID {
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
	if uploaded.Ref != "context-2" || uploaded.Scope != model.ProjectContextScopeProject || uploaded.Title != "notes" || uploaded.ContentType != "text/markdown; charset=utf-8" || uploaded.SourceFilename == nil || *uploaded.SourceFilename != "notes.md" {
		t.Fatalf("uploaded context = %+v", uploaded)
	}
	code, body = e.do(t, http.MethodPatch, e.projectContextPath(contextItem), map[string]any{
		"position": 2, "content_type": "text/plain",
	})
	if code != http.StatusOK {
		t.Fatalf("reposition context code = %d body = %s", code, body)
	}
	contextItem = decode[model.ProjectContext](t, body)
	if contextItem.Position == nil || *contextItem.Position != 2 || contextItem.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("repositioned context = %+v", contextItem)
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

	code, body = e.do(t, http.MethodPost, e.issueContextPath(issue), map[string]any{
		"title": "Issue only",
		"body":  "Only this issue needs it.",
	})
	if code != http.StatusCreated {
		t.Fatalf("create issue-only context code = %d body = %s", code, body)
	}
	issueOnly := decode[model.ProjectContext](t, body)
	if issueOnly.Scope != model.ProjectContextScopeIssue || issueOnly.Ref != "context-3" || issueOnly.ProjectID != e.projectID || issueOnly.CreatedByID != e.adminID {
		t.Fatalf("issue-only context = %+v", issueOnly)
	}
	code, body = e.do(t, http.MethodGet, e.projectContextPath(issueOnly), nil)
	if code != http.StatusNotFound {
		t.Fatalf("project get issue-only context code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, e.projectPath()+"/context?limit=10", nil)
	if code != http.StatusOK {
		t.Fatalf("project list after issue-only code = %d body = %s", code, body)
	}
	projectContexts := decodePage[model.ProjectContextSummary](t, body)
	if len(projectContexts.Items) != 2 {
		t.Fatalf("project context list = %+v, want only two project-scoped items", projectContexts)
	}

	code, body = e.doMultipartContextAt(t, e.issueContextPath(issue), map[string]string{"title": "Issue upload"}, "trace.txt", "Uploaded just for this issue.")
	if code != http.StatusCreated {
		t.Fatalf("issue multipart context code = %d body = %s", code, body)
	}
	issueUpload := decode[model.ProjectContext](t, body)
	if issueUpload.Scope != model.ProjectContextScopeIssue || issueUpload.Ref != "context-4" || issueUpload.SourceFilename == nil || *issueUpload.SourceFilename != "trace.txt" {
		t.Fatalf("issue upload context = %+v", issueUpload)
	}

	code, body = e.do(t, http.MethodGet, e.issueContextPath(issue), nil)
	if code != http.StatusOK {
		t.Fatalf("list issue context with issue-only code = %d body = %s", code, body)
	}
	issueContexts = decodePage[model.ProjectContext](t, body)
	if len(issueContexts.Items) != 3 || issueContexts.Items[0].ID != contextItem.ID || issueContexts.Items[1].ID != issueOnly.ID || issueContexts.Items[2].ID != issueUpload.ID {
		t.Fatalf("issue contexts with issue-only = %+v", issueContexts)
	}

	code, body = e.do(t, http.MethodPost, e.issueContextPath(issue), map[string]any{
		"context": contextItem.Ref,
		"title":   "Ambiguous",
		"body":    "Nope",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("ambiguous issue context code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodDelete, e.issueContextPath(issue)+"/"+issueOnly.Ref, nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete issue-only context code = %d body = %s", code, body)
	}
	if _, err := e.store.GetProjectContext(e.ctx, issueOnly.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("issue-only context err after issue delete = %v, want ErrNotFound", err)
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
		{"long title", map[string]any{"title": strings.Repeat("x", 201), "body": "body"}},
	} {
		code, body := e.do(t, http.MethodPost, e.projectPath()+"/context", tc.body)
		if code != http.StatusBadRequest {
			t.Fatalf("%s code = %d body = %s", tc.name, code, body)
		}
	}
	code, body := e.do(t, http.MethodPost, e.projectPath()+"/context", map[string]any{"title": "Blank page", "body": ""})
	if code != http.StatusCreated {
		t.Fatalf("blank project page code = %d body = %s", code, body)
	}
	blank := decode[model.ProjectContext](t, body)
	if blank.Body != "" || blank.ContentType != "text/markdown; charset=utf-8" {
		t.Fatalf("blank project page = %+v", blank)
	}
	code, body = e.doMultipartContext(t, nil, "image.png", "not really an image")
	if code != http.StatusBadRequest {
		t.Fatalf("bad upload extension code = %d body = %s", code, body)
	}

	issue, err := e.store.CreateIssue(e.ctx, storeCreateIssue(e.projectID, "issue context validation"))
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	code, body = e.do(t, http.MethodPost, e.issueContextPath(issue), map[string]any{"title": "", "body": "body"})
	if code != http.StatusBadRequest {
		t.Fatalf("bad issue context json code = %d body = %s", code, body)
	}
	code, body = e.doMultipartContextAt(t, e.issueContextPath(issue), nil, "image.png", "not really an image")
	if code != http.StatusBadRequest {
		t.Fatalf("bad issue upload extension code = %d body = %s", code, body)
	}
}

func TestHTTPBulkIssueContextLinks(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	createContext := func(title string) model.ProjectContext {
		code, body := e.do(t, http.MethodPost, e.projectPath()+"/context", map[string]any{"title": title, "body": title + " body"})
		if code != http.StatusCreated {
			t.Fatalf("create context %q code = %d body = %s", title, code, body)
		}
		return decode[model.ProjectContext](t, body)
	}
	firstContext := createContext("Architecture")
	secondContext := createContext("Runbook")
	issueA, err := e.store.CreateIssue(e.ctx, storeCreateIssue(e.projectID, "A"))
	if err != nil {
		t.Fatalf("CreateIssue A: %v", err)
	}
	issueB, err := e.store.CreateIssue(e.ctx, storeCreateIssue(e.projectID, "B"))
	if err != nil {
		t.Fatalf("CreateIssue B: %v", err)
	}
	if _, err := e.store.CreateIssueContextLink(e.ctx, issueA.ID, firstContext.ID); err != nil {
		t.Fatalf("CreateIssueContextLink existing: %v", err)
	}

	path := e.projectPath() + "/context-links"
	links := []map[string]string{
		{"issue": issueA.Identifier, "context": firstContext.Ref},
		{"issue": issueA.Identifier, "context": secondContext.Ref},
		{"issue": issueB.Identifier, "context": firstContext.Ref},
		{"issue": issueB.Identifier, "context": firstContext.Ref},
	}
	code, body := e.do(t, http.MethodPost, path, map[string]any{"links": links})
	if code != http.StatusOK {
		t.Fatalf("bulk link code = %d body = %s", code, body)
	}
	result := decode[store.CreateIssueContextLinksResult](t, body)
	if result != (store.CreateIssueContextLinksResult{Requested: 4, Created: 2, Unchanged: 2}) {
		t.Fatalf("bulk result = %+v", result)
	}
	code, body = e.do(t, http.MethodPost, path, map[string]any{"links": links})
	if code != http.StatusOK {
		t.Fatalf("repeat bulk link code = %d body = %s", code, body)
	}
	result = decode[store.CreateIssueContextLinksResult](t, body)
	if result != (store.CreateIssueContextLinksResult{Requested: 4, Created: 0, Unchanged: 4}) {
		t.Fatalf("repeat bulk result = %+v", result)
	}
	code, body = e.do(t, http.MethodGet, e.issueContextPath(issueB), nil)
	if code != http.StatusOK {
		t.Fatalf("list issue B context code = %d body = %s", code, body)
	}
	contextsB := decodePage[model.ProjectContext](t, body)
	if len(contextsB.Items) != 1 || contextsB.Items[0].ID != firstContext.ID {
		t.Fatalf("issue B contexts = %+v", contextsB.Items)
	}

	readonly, readonlyToken := e.mustUserToken(t, "bulk-context-readonly")
	if _, err := e.store.SetProjectMemberRole(e.ctx, e.projectID, readonly.ID, model.ProjectMemberRoleReadonly); err != nil {
		t.Fatalf("SetProjectMemberRole readonly: %v", err)
	}
	code, body = e.doWithToken(t, readonlyToken, http.MethodPost, path, map[string]any{"links": links[:1]})
	if code != http.StatusForbidden {
		t.Fatalf("readonly bulk link code = %d body = %s", code, body)
	}
	code, body = e.doUnauth(t, http.MethodPost, path, map[string]any{"links": links[:1]})
	if code != http.StatusUnauthorized {
		t.Fatalf("unauthorized bulk link code = %d body = %s", code, body)
	}
}

func TestHTTPBulkIssueContextLinkValidationAndAtomicity(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, body := e.do(t, http.MethodPost, e.projectPath()+"/context", map[string]any{"title": "Shared", "body": "Shared body"})
	if code != http.StatusCreated {
		t.Fatalf("create shared context code = %d body = %s", code, body)
	}
	shared := decode[model.ProjectContext](t, body)
	target, err := e.store.CreateIssue(e.ctx, storeCreateIssue(e.projectID, "Atomic target"))
	if err != nil {
		t.Fatalf("CreateIssue target: %v", err)
	}
	scopeOwner, err := e.store.CreateIssue(e.ctx, storeCreateIssue(e.projectID, "Scope owner"))
	if err != nil {
		t.Fatalf("CreateIssue scope owner: %v", err)
	}
	issueScoped, err := e.store.CreateIssueContext(e.ctx, store.CreateIssueContextParams{
		IssueID:     scopeOwner.ID,
		Title:       "Issue only",
		Body:        "Issue scoped body.",
		CreatedByID: e.adminID,
	})
	if err != nil {
		t.Fatalf("CreateIssueContext: %v", err)
	}
	otherProject, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "Other", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	otherIssue, err := e.store.CreateIssue(e.ctx, storeCreateIssue(otherProject.ID, "Other issue"))
	if err != nil {
		t.Fatalf("CreateIssue other: %v", err)
	}

	oversized := make([]map[string]string, 201)
	for i := range oversized {
		oversized[i] = map[string]string{"issue": target.Identifier, "context": shared.Ref}
	}
	path := e.projectPath() + "/context-links"
	code, body = e.do(t, http.MethodPost, "/"+e.ownerUsername+"/projects/"+uniqueProjectKey(t)+"/context-links", map[string]any{
		"links": []map[string]string{{"issue": target.Identifier, "context": shared.Ref}},
	})
	if code != http.StatusNotFound {
		t.Fatalf("unknown project bulk link code = %d body = %s", code, body)
	}
	for _, test := range []struct {
		name string
		body any
	}{
		{name: "empty body", body: nil},
		{name: "missing links", body: map[string]any{}},
		{name: "wrong links type", body: map[string]any{"links": "bad"}},
		{name: "too many links", body: map[string]any{"links": oversized}},
		{name: "malformed issue", body: map[string]any{"links": []map[string]string{{"issue": "bad", "context": shared.Ref}}}},
		{name: "cross project issue", body: map[string]any{"links": []map[string]string{{"issue": otherIssue.Identifier, "context": shared.Ref}}}},
		{name: "malformed context", body: map[string]any{"links": []map[string]string{{"issue": target.Identifier, "context": "bad"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			code, body := e.do(t, http.MethodPost, path, test.body)
			if code != http.StatusBadRequest {
				t.Fatalf("code = %d body = %s", code, body)
			}
		})
	}

	valid := map[string]string{"issue": target.Identifier, "context": shared.Ref}
	for _, test := range []struct {
		name    string
		invalid map[string]string
	}{
		{name: "missing issue", invalid: map[string]string{"issue": e.projKey + "-999999", "context": shared.Ref}},
		{name: "missing context", invalid: map[string]string{"issue": target.Identifier, "context": "context-999999"}},
		{name: "issue scoped context", invalid: map[string]string{"issue": target.Identifier, "context": issueScoped.Ref}},
	} {
		t.Run(test.name, func(t *testing.T) {
			code, body := e.do(t, http.MethodPost, path, map[string]any{"links": []map[string]string{valid, test.invalid}})
			if code != http.StatusNotFound {
				t.Fatalf("code = %d body = %s", code, body)
			}
			contexts, _, err := e.store.ListContextsForIssue(e.ctx, store.ListContextsForIssueParams{IssueID: target.ID, Limit: 10})
			if err != nil {
				t.Fatalf("ListContextsForIssue: %v", err)
			}
			if len(contexts) != 0 {
				t.Fatalf("target contexts after rejected batch = %+v", contexts)
			}
		})
	}
}

func storeCreateIssue(projectID uuid.UUID, title string) store.CreateIssueParams {
	return store.CreateIssueParams{ProjectID: projectID, Title: title}
}
