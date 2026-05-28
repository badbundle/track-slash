package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

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

	code, body := e.do(t, http.MethodGet, fmt.Sprintf("/issues/%s/comments", iss.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("empty list code = %d body = %s", code, body)
	}
	empty := decodePage[model.Comment](t, body)
	if len(empty.Items) != 0 || empty.NextCursor != nil {
		t.Fatalf("empty = %+v", empty)
	}

	var ids []uuid.UUID
	for _, txt := range []string{"one", "two", "three"} {
		code, body = e.do(t, http.MethodPost, fmt.Sprintf("/issues/%s/comments", iss.ID), map[string]any{
			"author_id": author.ID,
			"body":      txt,
		})
		if code != http.StatusCreated {
			t.Fatalf("create %q code = %d body = %s", txt, code, body)
		}
		c := decode[model.Comment](t, body)
		ids = append(ids, c.ID)
	}

	code, body = e.do(t, http.MethodGet, fmt.Sprintf("/issues/%s/comments?limit=2", iss.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("page1 code = %d body = %s", code, body)
	}
	page1 := decodePage[model.Comment](t, body)
	if len(page1.Items) != 2 || page1.NextCursor == nil || page1.Items[0].ID != ids[0] || page1.Items[1].ID != ids[1] {
		t.Fatalf("page1 = %+v", page1)
	}

	code, body = e.do(t, http.MethodGet, fmt.Sprintf("/issues/%s/comments?limit=2&cursor=%s", iss.ID, *page1.NextCursor), nil)
	if code != http.StatusOK {
		t.Fatalf("page2 code = %d body = %s", code, body)
	}
	page2 := decodePage[model.Comment](t, body)
	if len(page2.Items) != 1 || page2.NextCursor != nil || page2.Items[0].ID != ids[2] {
		t.Fatalf("page2 = %+v", page2)
	}

	code, body = e.do(t, http.MethodGet, fmt.Sprintf("/comments/%s", ids[0]), nil)
	if code != http.StatusOK {
		t.Fatalf("get code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPatch, fmt.Sprintf("/comments/%s", ids[0]), map[string]any{"body": "edited"})
	if code != http.StatusOK {
		t.Fatalf("patch code = %d body = %s", code, body)
	}
	updated := decode[model.Comment](t, body)
	if updated.Body != "edited" {
		t.Fatalf("Body = %q, want edited", updated.Body)
	}

	code, body = e.do(t, http.MethodDelete, fmt.Sprintf("/comments/%s", ids[0]), nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodDelete, fmt.Sprintf("/comments/%s", ids[0]), nil)
	if code != http.StatusNotFound {
		t.Fatalf("delete second code = %d body = %s", code, body)
	}
}

func TestHTTPCommentValidationAndNotFound(t *testing.T) {
	e := newHTTPEnv(t)
	iss := mustHTTPIssue(t, e)
	author := mustHTTPUser(t, e)

	cases := []struct {
		name string
		path string
		body any
	}{
		{"empty body", fmt.Sprintf("/issues/%s/comments", iss.ID), map[string]any{"author_id": author.ID, "body": ""}},
		{"long body", fmt.Sprintf("/issues/%s/comments", iss.ID), map[string]any{"author_id": author.ID, "body": strings.Repeat("x", 10001)}},
		{"missing author", fmt.Sprintf("/issues/%s/comments", iss.ID), map[string]any{"body": "hello"}},
		{"bad issue id", "/issues/not-a-uuid/comments", map[string]any{"author_id": author.ID, "body": "hello"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := e.do(t, http.MethodPost, tc.path, tc.body)
			if code != http.StatusBadRequest {
				t.Fatalf("code = %d body = %s", code, body)
			}
		})
	}

	code, body := e.do(t, http.MethodPost, fmt.Sprintf("/issues/%s/comments", uuid.New()), map[string]any{
		"author_id": author.ID,
		"body":      "hello",
	})
	if code != http.StatusNotFound {
		t.Fatalf("unknown issue code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPost, fmt.Sprintf("/issues/%s/comments", iss.ID), map[string]any{
		"author_id": uuid.New(),
		"body":      "hello",
	})
	if code != http.StatusNotFound {
		t.Fatalf("unknown author code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, fmt.Sprintf("/issues/%s/comments", uuid.New()), nil)
	if code != http.StatusNotFound {
		t.Fatalf("unknown list issue code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, fmt.Sprintf("/issues/%s/comments?cursor=bad", iss.ID), nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad cursor code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, fmt.Sprintf("/issues/%s/comments?limit=0", iss.ID), nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad limit code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPatch, "/comments/not-a-uuid", map[string]any{"body": "edited"})
	if code != http.StatusBadRequest {
		t.Fatalf("bad patch id code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, "/comments/not-a-uuid", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad get id code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, "/issues/not-a-uuid/comments", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad list issue id code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPatch, fmt.Sprintf("/comments/%s", uuid.New()), map[string]any{"body": "edited"})
	if code != http.StatusNotFound {
		t.Fatalf("unknown patch code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPatch, fmt.Sprintf("/comments/%s", uuid.New()), map[string]any{"body": strings.Repeat("x", 10001)})
	if code != http.StatusBadRequest {
		t.Fatalf("long patch body code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPatch, fmt.Sprintf("/comments/%s", uuid.New()), map[string]any{"body": ""})
	if code != http.StatusBadRequest {
		t.Fatalf("empty patch body code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, fmt.Sprintf("/comments/%s", uuid.New()), nil)
	if code != http.StatusNotFound {
		t.Fatalf("unknown get code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodDelete, "/comments/not-a-uuid", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad delete id code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodDelete, fmt.Sprintf("/comments/%s", uuid.New()), nil)
	if code != http.StatusNotFound {
		t.Fatalf("unknown delete code = %d body = %s", code, body)
	}
}
