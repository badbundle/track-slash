package server_test

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestHTTPSubIssuesCRUDAndFiltering(t *testing.T) {
	e := newHTTPEnv(t)
	assignee := mustHTTPUser(t, e)

	code, body := e.do(t, http.MethodPost,
		e.projectIssuesPath(),
		map[string]any{"title": "parent", "description": "parent body"})
	if code != http.StatusCreated {
		t.Fatalf("create parent code = %d body = %s", code, body)
	}
	parent := decode[model.Issue](t, body)

	code, body = e.do(t, http.MethodPost,
		e.issueSubIssuesPath(parent),
		map[string]any{
			"title":       "child",
			"description": "child body",
			"priority":    string(model.PriorityP1),
			"assignee_id": assignee.ID,
		})
	if code != http.StatusCreated {
		t.Fatalf("create child code = %d body = %s", code, body)
	}
	child := decode[model.Issue](t, body)
	if child.ParentIssueID == nil || *child.ParentIssueID != parent.ID {
		t.Fatalf("child parent = %v, want %s", child.ParentIssueID, parent.ID)
	}
	if child.SprintID != nil {
		t.Fatalf("child sprint = %v, want nil", child.SprintID)
	}
	if child.AssigneeID == nil || *child.AssigneeID != assignee.ID {
		t.Fatalf("child assignee = %v, want %s", child.AssigneeID, assignee.ID)
	}
	if child.Priority != model.PriorityP1 {
		t.Fatalf("child priority = %s, want %s", child.Priority, model.PriorityP1)
	}

	code, body = e.do(t, http.MethodGet, e.issueSubIssuesPath(parent), nil)
	if code != http.StatusOK {
		t.Fatalf("list children code = %d body = %s", code, body)
	}
	children := decodePage[model.Issue](t, body).Items
	if len(children) != 1 || children[0].ID != child.ID || children[0].Priority != model.PriorityP1 {
		t.Fatalf("children = %+v, want %s", children, child.ID)
	}

	code, body = e.do(t, http.MethodGet, e.issuePath(child), nil)
	if code != http.StatusOK {
		t.Fatalf("get child code = %d body = %s", code, body)
	}
	got := decode[model.Issue](t, body)
	if got.ID != child.ID || got.ParentIssueID == nil {
		t.Fatalf("get child mismatch: %+v", got)
	}

	code, body = e.do(t, http.MethodPatch, e.issuePath(child),
		map[string]any{"status": "in_progress"})
	if code != http.StatusOK {
		t.Fatalf("patch child status code = %d body = %s", code, body)
	}
	updated := decode[model.Issue](t, body)
	if updated.Status != model.StatusInProgress {
		t.Fatalf("child status = %s, want in_progress", updated.Status)
	}

	code, body = e.do(t, http.MethodPost, e.issueCommentsPath(child),
		map[string]any{"body": "child comment"})
	if code != http.StatusCreated {
		t.Fatalf("comment child code = %d body = %s", code, body)
	}

	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	code, body = e.do(t, http.MethodPatch, e.issuePath(child),
		map[string]any{"sprint": sp.Ref})
	if code != http.StatusConflict {
		t.Fatalf("patch child sprint code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, e.projectIssuesPath(), nil)
	if code != http.StatusOK {
		t.Fatalf("list project issues code = %d body = %s", code, body)
	}
	topLevel := decodePage[model.Issue](t, body).Items
	if len(topLevel) != 1 || topLevel[0].ID != parent.ID {
		t.Fatalf("project issues = %+v, want only parent %s", topLevel, parent.ID)
	}
}

func TestHTTPSubIssueValidation(t *testing.T) {
	e := newHTTPEnv(t)
	parent, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "parent"})
	if err != nil {
		t.Fatalf("CreateIssue parent: %v", err)
	}
	child, err := e.store.CreateSubIssue(e.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "child",
	})
	if err != nil {
		t.Fatalf("CreateSubIssue: %v", err)
	}

	cases := []struct {
		name   string
		method string
		path   string
		body   any
		want   int
	}{
		{"bad create ref", http.MethodPost, "/" + e.ownerUsername + "/issues/not-a-ref/sub-issues", map[string]any{"title": "x"}, http.StatusBadRequest},
		{"bad list ref", http.MethodGet, "/" + e.ownerUsername + "/issues/not-a-ref/sub-issues", nil, http.StatusBadRequest},
		{"missing title", http.MethodPost, e.issueSubIssuesPath(parent), map[string]any{"title": "  "}, http.StatusBadRequest},
		{"long title", http.MethodPost, e.issueSubIssuesPath(parent), map[string]any{"title": strings.Repeat("x", 201)}, http.StatusBadRequest},
		{"unknown parent", http.MethodPost, "/" + e.ownerUsername + "/issues/" + e.projKey + "-999999/sub-issues", map[string]any{"title": "x"}, http.StatusNotFound},
		{"nested create", http.MethodPost, e.issueSubIssuesPath(child), map[string]any{"title": "nested"}, http.StatusConflict},
		{"nested list", http.MethodGet, e.issueSubIssuesPath(child), nil, http.StatusConflict},
		{"bad cursor", http.MethodGet, e.issueSubIssuesPath(parent) + "?cursor=bad", nil, http.StatusBadRequest},
		{"bad limit", http.MethodGet, e.issueSubIssuesPath(parent) + "?limit=0", nil, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := e.do(t, tc.method, tc.path, tc.body)
			if code != tc.want {
				t.Fatalf("code = %d body = %s, want %d", code, body, tc.want)
			}
		})
	}

	req, err := http.NewRequestWithContext(e.ctx, http.MethodPost,
		e.ts.URL+apiPath(e.issueSubIssuesPath(parent)),
		bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatalf("new bad json request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.authToken)
	res, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do bad json: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("bad json code = %d body = %s", res.StatusCode, body)
	}
}

func TestHTTPSubIssuesRequireProjectAccess(t *testing.T) {
	e := newHTTPEnv(t)
	parent, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "parent"})
	if err != nil {
		t.Fatalf("CreateIssue parent: %v", err)
	}
	_, token := e.mustUserToken(t, "sub-denied")

	code, body := e.doWithToken(t, token, http.MethodPost,
		e.issueSubIssuesPath(parent),
		map[string]any{"title": "denied"})
	if code != http.StatusForbidden {
		t.Fatalf("create denied code = %d body = %s", code, body)
	}

	code, body = e.doWithToken(t, token, http.MethodGet,
		e.issueSubIssuesPath(parent), nil)
	if code != http.StatusForbidden {
		t.Fatalf("list denied code = %d body = %s", code, body)
	}
}
