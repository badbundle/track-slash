package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func mustHTTPUser(t *testing.T, e *httpEnv) model.User {
	t.Helper()
	u, err := e.store.CreateUser(e.ctx, "http-comment-"+uniqueProjectKey(t)+"@example.com", "Commenter")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u
}

func mustHTTPIssue(t *testing.T, e *httpEnv) model.Issue {
	t.Helper()
	iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "commented",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	return iss
}

func TestHTTPCommentsCRUDAndPagination(t *testing.T) {
	e := newHTTPEnv(t)
	iss := mustHTTPIssue(t, e)
	author := mustHTTPUser(t, e)

	code, body := e.do(t, http.MethodGet, e.issueCommentsPath(iss), nil)
	if code != http.StatusOK {
		t.Fatalf("empty list code = %d body = %s", code, body)
	}
	empty := decodePage[model.Comment](t, body)
	if len(empty.Items) != 0 || empty.NextCursor != nil {
		t.Fatalf("empty = %+v", empty)
	}

	var comments []model.Comment
	for _, txt := range []string{"one", "two", "three"} {
		code, body = e.do(t, http.MethodPost, e.issueCommentsPath(iss), map[string]any{
			"author_id": author.ID,
			"body":      txt,
		})
		if code != http.StatusCreated {
			t.Fatalf("create %q code = %d body = %s", txt, code, body)
		}
		c := decode[model.Comment](t, body)
		comments = append(comments, c)
	}

	code, body = e.do(t, http.MethodGet, e.issueCommentsPath(iss)+"?limit=2", nil)
	if code != http.StatusOK {
		t.Fatalf("page1 code = %d body = %s", code, body)
	}
	page1 := decodePage[model.Comment](t, body)
	if len(page1.Items) != 2 || page1.NextCursor == nil || page1.Items[0].ID != comments[0].ID || page1.Items[1].ID != comments[1].ID {
		t.Fatalf("page1 = %+v", page1)
	}

	code, body = e.do(t, http.MethodGet, e.issueCommentsPath(iss)+"?limit=2&cursor="+*page1.NextCursor, nil)
	if code != http.StatusOK {
		t.Fatalf("page2 code = %d body = %s", code, body)
	}
	page2 := decodePage[model.Comment](t, body)
	if len(page2.Items) != 1 || page2.NextCursor != nil || page2.Items[0].ID != comments[2].ID {
		t.Fatalf("page2 = %+v", page2)
	}

	commentPath := e.issueCommentsPath(iss) + "/" + comments[0].Ref
	code, body = e.do(t, http.MethodGet, commentPath, nil)
	if code != http.StatusOK {
		t.Fatalf("get code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPatch, commentPath, map[string]any{"body": "edited"})
	if code != http.StatusOK {
		t.Fatalf("patch code = %d body = %s", code, body)
	}
	updated := decode[model.Comment](t, body)
	if updated.Body != "edited" {
		t.Fatalf("Body = %q, want edited", updated.Body)
	}

	code, body = e.do(t, http.MethodDelete, commentPath, nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodDelete, commentPath, nil)
	if code != http.StatusNotFound {
		t.Fatalf("delete second code = %d body = %s", code, body)
	}
}

func TestHTTPCommentValidationAndNotFound(t *testing.T) {
	e := newHTTPEnv(t)
	iss := mustHTTPIssue(t, e)
	author := mustHTTPUser(t, e)
	code, body := e.do(t, http.MethodPost, e.issueCommentsPath(iss), map[string]any{
		"author_id": author.ID,
		"body":      "existing",
	})
	if code != http.StatusCreated {
		t.Fatalf("seed comment code = %d body = %s", code, body)
	}
	existing := decode[model.Comment](t, body)
	commentPath := e.issueCommentsPath(iss) + "/" + existing.Ref
	unknownIssueCommentsPath := "/" + e.ownerUsername + "/issues/" + e.projKey + "-999999/comments"
	unknownCommentPath := e.issueCommentsPath(iss) + "/comment-999999"

	cases := []struct {
		name string
		path string
		body any
	}{
		{"empty body", e.issueCommentsPath(iss), map[string]any{"author_id": author.ID, "body": ""}},
		{"long body", e.issueCommentsPath(iss), map[string]any{"author_id": author.ID, "body": strings.Repeat("x", 10001)}},
		{"bad issue ref", "/" + e.ownerUsername + "/issues/not-a-ref/comments", map[string]any{"author_id": author.ID, "body": "hello"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := e.do(t, http.MethodPost, tc.path, tc.body)
			if code != http.StatusBadRequest {
				t.Fatalf("code = %d body = %s", code, body)
			}
		})
	}

	code, body = e.do(t, http.MethodPost, unknownIssueCommentsPath, map[string]any{
		"author_id": author.ID,
		"body":      "hello",
	})
	if code != http.StatusNotFound {
		t.Fatalf("unknown issue code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPost, e.issueCommentsPath(iss), map[string]any{
		"author_id": author.ID,
		"body":      "hello",
	})
	if code != http.StatusCreated {
		t.Fatalf("ignored author code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, unknownIssueCommentsPath, nil)
	if code != http.StatusNotFound {
		t.Fatalf("unknown list issue code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, e.issueCommentsPath(iss)+"?cursor=bad", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad cursor code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, e.issueCommentsPath(iss)+"?limit=0", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad limit code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPatch, e.issueCommentsPath(iss)+"/not-a-comment", map[string]any{"body": "edited"})
	if code != http.StatusBadRequest {
		t.Fatalf("bad patch ref code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, e.issueCommentsPath(iss)+"/not-a-comment", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad get ref code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, "/"+e.ownerUsername+"/issues/not-a-ref/comments", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad list issue ref code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPatch, unknownCommentPath, map[string]any{"body": "edited"})
	if code != http.StatusNotFound {
		t.Fatalf("unknown patch code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPatch, commentPath, map[string]any{"body": strings.Repeat("x", 10001)})
	if code != http.StatusBadRequest {
		t.Fatalf("long patch body code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPatch, commentPath, map[string]any{"body": ""})
	if code != http.StatusBadRequest {
		t.Fatalf("empty patch body code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, unknownCommentPath, nil)
	if code != http.StatusNotFound {
		t.Fatalf("unknown get code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodDelete, e.issueCommentsPath(iss)+"/not-a-comment", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad delete ref code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodDelete, unknownCommentPath, nil)
	if code != http.StatusNotFound {
		t.Fatalf("unknown delete code = %d body = %s", code, body)
	}
}
