package server_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestHTTPSubIssuesCRUDAndFiltering(t *testing.T) {
	e := newHTTPEnv(t)
	assignee := mustHTTPUser(t, e)

	code, body := e.do(t, http.MethodPost,
		fmt.Sprintf("/projects/%s/issues", e.projectID),
		map[string]any{"title": "parent", "description": "parent body"})
	if code != http.StatusCreated {
		t.Fatalf("create parent code = %d body = %s", code, body)
	}
	parent := decode[model.Issue](t, body)

	code, body = e.do(t, http.MethodPost,
		fmt.Sprintf("/issues/%s/sub-issues", parent.ID),
		map[string]any{
			"title":       "child",
			"description": "child body",
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

	code, body = e.do(t, http.MethodGet, fmt.Sprintf("/issues/%s/sub-issues", parent.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("list children code = %d body = %s", code, body)
	}
	children := decodePage[model.Issue](t, body).Items
	if len(children) != 1 || children[0].ID != child.ID {
		t.Fatalf("children = %+v, want %s", children, child.ID)
	}

	code, body = e.do(t, http.MethodGet, fmt.Sprintf("/issues/%s", child.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("get child code = %d body = %s", code, body)
	}
	got := decode[model.Issue](t, body)
	if got.ID != child.ID || got.ParentIssueID == nil {
		t.Fatalf("get child mismatch: %+v", got)
	}

	code, body = e.do(t, http.MethodPatch, fmt.Sprintf("/issues/%s", child.ID),
		map[string]any{"status": "in_progress"})
	if code != http.StatusOK {
		t.Fatalf("patch child status code = %d body = %s", code, body)
	}
	updated := decode[model.Issue](t, body)
	if updated.Status != model.StatusInProgress {
		t.Fatalf("child status = %s, want in_progress", updated.Status)
	}

	code, body = e.do(t, http.MethodPost, fmt.Sprintf("/issues/%s/comments", child.ID),
		map[string]any{"body": "child comment"})
	if code != http.StatusCreated {
		t.Fatalf("comment child code = %d body = %s", code, body)
	}

	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	code, body = e.do(t, http.MethodPatch, fmt.Sprintf("/issues/%s", child.ID),
		map[string]any{"sprint_id": sp.ID.String()})
	if code != http.StatusConflict {
		t.Fatalf("patch child sprint code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, fmt.Sprintf("/projects/%s/issues", e.projectID), nil)
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
		{"bad create id", http.MethodPost, "/issues/not-a-uuid/sub-issues", map[string]any{"title": "x"}, http.StatusBadRequest},
		{"bad list id", http.MethodGet, "/issues/not-a-uuid/sub-issues", nil, http.StatusBadRequest},
		{"missing title", http.MethodPost, fmt.Sprintf("/issues/%s/sub-issues", parent.ID), map[string]any{"title": "  "}, http.StatusBadRequest},
		{"long title", http.MethodPost, fmt.Sprintf("/issues/%s/sub-issues", parent.ID), map[string]any{"title": strings.Repeat("x", 201)}, http.StatusBadRequest},
		{"unknown parent", http.MethodPost, fmt.Sprintf("/issues/%s/sub-issues", uuid.New()), map[string]any{"title": "x"}, http.StatusNotFound},
		{"nested create", http.MethodPost, fmt.Sprintf("/issues/%s/sub-issues", child.ID), map[string]any{"title": "nested"}, http.StatusConflict},
		{"nested list", http.MethodGet, fmt.Sprintf("/issues/%s/sub-issues", child.ID), nil, http.StatusConflict},
		{"bad cursor", http.MethodGet, fmt.Sprintf("/issues/%s/sub-issues?cursor=bad", parent.ID), nil, http.StatusBadRequest},
		{"bad limit", http.MethodGet, fmt.Sprintf("/issues/%s/sub-issues?limit=0", parent.ID), nil, http.StatusBadRequest},
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
		e.ts.URL+apiPath(fmt.Sprintf("/issues/%s/sub-issues", parent.ID)),
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
		fmt.Sprintf("/issues/%s/sub-issues", parent.ID),
		map[string]any{"title": "denied"})
	if code != http.StatusForbidden {
		t.Fatalf("create denied code = %d body = %s", code, body)
	}

	code, body = e.doWithToken(t, token, http.MethodGet,
		fmt.Sprintf("/issues/%s/sub-issues", parent.ID), nil)
	if code != http.StatusForbidden {
		t.Fatalf("list denied code = %d body = %s", code, body)
	}
}
