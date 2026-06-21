package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

// pageDecoded mirrors the server.page envelope. Kept private to the test
// package so a refactor of the envelope shape forces tests to update.
type pageDecoded[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func decodePage[T any](t *testing.T, body []byte) pageDecoded[T] {
	t.Helper()
	var p pageDecoded[T]
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("decode page: %v body=%s", err, body)
	}
	return p
}

// ---------- pagination wiring on /{owner}/projects/{key}/issues ----------

func TestPaginationIssuesRoundTrip(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	const n = 5
	created := make([]uuid.UUID, n)
	for i := 0; i < n; i++ {
		iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
			ProjectID: e.projectID, Title: fmt.Sprintf("issue-%d", i),
		})
		if err != nil {
			t.Fatalf("CreateIssue %d: %v", i, err)
		}
		created[i] = iss.ID
	}

	path := e.projectIssuesPath() + "?limit=2"
	code, body := e.do(t, http.MethodGet, path, nil)
	if code != http.StatusOK {
		t.Fatalf("page 1 code = %d body = %s", code, body)
	}
	p1 := decodePage[model.Issue](t, body)
	if len(p1.Items) != 2 {
		t.Fatalf("page 1 len = %d, want 2", len(p1.Items))
	}
	if p1.NextCursor == nil {
		t.Fatal("page 1 next_cursor is nil; want a cursor")
	}
	if p1.Items[0].ID != created[0] || p1.Items[1].ID != created[1] {
		t.Fatalf("page 1 items = %+v, want first two created", p1.Items)
	}

	path = e.projectIssuesPath() + "?limit=2&cursor=" + url.QueryEscape(*p1.NextCursor)
	code, body = e.do(t, http.MethodGet, path, nil)
	if code != http.StatusOK {
		t.Fatalf("page 2 code = %d body = %s", code, body)
	}
	p2 := decodePage[model.Issue](t, body)
	if len(p2.Items) != 2 || p2.NextCursor == nil {
		t.Fatalf("page 2 = %+v (next=%v)", p2.Items, p2.NextCursor)
	}
	if p2.Items[0].ID != created[2] || p2.Items[1].ID != created[3] {
		t.Fatalf("page 2 items = %+v, want issues 3..4", p2.Items)
	}

	path = e.projectIssuesPath() + "?limit=2&cursor=" + url.QueryEscape(*p2.NextCursor)
	code, body = e.do(t, http.MethodGet, path, nil)
	if code != http.StatusOK {
		t.Fatalf("page 3 code = %d body = %s", code, body)
	}
	p3 := decodePage[model.Issue](t, body)
	if len(p3.Items) != 1 {
		t.Fatalf("page 3 len = %d, want 1", len(p3.Items))
	}
	if p3.NextCursor != nil {
		t.Fatalf("page 3 next_cursor = %q, want nil (last page)", *p3.NextCursor)
	}
}

func TestPaginationIssuesExactLimitNoCursor(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	for i := 0; i < 3; i++ {
		if _, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
			ProjectID: e.projectID, Title: fmt.Sprintf("x-%d", i),
		}); err != nil {
			t.Fatalf("CreateIssue: %v", err)
		}
	}
	code, body := e.do(t, http.MethodGet,
		e.projectIssuesPath()+"?limit=3", nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	p := decodePage[model.Issue](t, body)
	if len(p.Items) != 3 {
		t.Fatalf("len = %d, want 3", len(p.Items))
	}
	if p.NextCursor != nil {
		t.Fatalf("next_cursor = %q, want nil (exactly limit rows)", *p.NextCursor)
	}
}

func TestPaginationEmptyEnvelope(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, body := e.do(t, http.MethodGet,
		e.projectIssuesPath(), nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	p := decodePage[model.Issue](t, body)
	if len(p.Items) != 0 || p.NextCursor != nil {
		t.Fatalf("envelope = %+v, want empty items + nil cursor", p)
	}
}

func TestPaginationBadCursor(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	for _, raw := range []string{"not-base64!!!", "Zm9v"} { // second decodes to "foo" — invalid JSON
		path := e.projectIssuesPath() + "?cursor=" + raw
		code, _ := e.do(t, http.MethodGet, path, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("cursor %q: code = %d, want 400", raw, code)
		}
	}
}

func TestPaginationBadLimit(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	for _, raw := range []string{"0", "-1", "abc"} {
		path := e.projectIssuesPath() + "?limit=" + raw
		code, _ := e.do(t, http.MethodGet, path, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("limit=%s: code = %d, want 400", raw, code)
		}
	}
}

func TestPaginationLimitClampedAboveMax(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	// Create 3 issues; limit 500 (> MaxLimit) must succeed and return all 3.
	for i := 0; i < 3; i++ {
		if _, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
			ProjectID: e.projectID, Title: fmt.Sprintf("c-%d", i),
		}); err != nil {
			t.Fatalf("CreateIssue: %v", err)
		}
	}
	code, body := e.do(t, http.MethodGet,
		e.projectIssuesPath()+"?limit=500", nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	p := decodePage[model.Issue](t, body)
	if len(p.Items) != 3 || p.NextCursor != nil {
		t.Fatalf("clamped page = %+v", p)
	}
}

// ---------- pagination wiring on the other list endpoints (smoke) ----------

func TestPaginationUsersAndProjectsAndSprintsAndLinks(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	for i := 0; i < 3; i++ {
		if _, err := e.store.CreateUser(e.ctx,
			fmt.Sprintf("u%d-%s@x", i, uuid.NewString()), fmt.Sprintf("U%d", i),
		); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
	}
	code, body := e.do(t, http.MethodGet, "/users?limit=2", nil)
	if code != http.StatusOK {
		t.Fatalf("users code = %d", code)
	}
	up := decodePage[model.User](t, body)
	if len(up.Items) != 2 || up.NextCursor == nil {
		t.Fatalf("users page = %+v", up)
	}

	code, body = e.do(t, http.MethodGet, "/users?limit=2&cursor="+url.QueryEscape(*up.NextCursor), nil)
	if code != http.StatusOK {
		t.Fatalf("users page2 code = %d", code)
	}
	up2 := decodePage[model.User](t, body)
	if len(up2.Items) == 0 {
		t.Fatalf("users page2 items empty; cursor didn't advance")
	}

	if _, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "p2", ""); err != nil {
		t.Fatalf("CreateProject p2: %v", err)
	}
	code, body = e.do(t, http.MethodGet, "/projects?limit=1", nil)
	if code != http.StatusOK {
		t.Fatalf("projects code = %d", code)
	}
	pp := decodePage[model.Project](t, body)
	if len(pp.Items) != 1 || pp.NextCursor == nil {
		t.Fatalf("projects page = %+v", pp)
	}
	code, body = e.do(t, http.MethodGet, "/projects?limit=1&cursor="+url.QueryEscape(*pp.NextCursor), nil)
	if code != http.StatusOK {
		t.Fatalf("projects page2 code = %d", code)
	}
	pp2 := decodePage[model.Project](t, body)
	if len(pp2.Items) != 1 {
		t.Fatalf("projects page2 len = %d, want 1", len(pp2.Items))
	}

	for i := 0; i < 3; i++ {
		e.do(t, http.MethodPost,
			e.projectSprintsPath(),
			map[string]any{
				"name":       fmt.Sprintf("S%d", i),
				"start_date": fmt.Sprintf("2026-06-%02d", 1+i*7),
				"end_date":   fmt.Sprintf("2026-06-%02d", 7+i*7),
			})
	}
	code, body = e.do(t, http.MethodGet,
		e.projectSprintsPath()+"?limit=2", nil)
	if code != http.StatusOK {
		t.Fatalf("sprints code = %d", code)
	}
	sp := decodePage[model.Sprint](t, body)
	if len(sp.Items) != 2 || sp.NextCursor == nil {
		t.Fatalf("sprints page = %+v", sp)
	}
	code, body = e.do(t, http.MethodGet,
		e.projectSprintsPath()+"?limit=2&cursor="+url.QueryEscape(*sp.NextCursor), nil)
	if code != http.StatusOK {
		t.Fatalf("sprints page2 code = %d", code)
	}
	sp2 := decodePage[model.Sprint](t, body)
	if len(sp2.Items) != 1 || sp2.NextCursor != nil {
		t.Fatalf("sprints page2 = %+v", sp2)
	}

	a := e.mustCreateIssue(t, "A")
	for i := 0; i < 3; i++ {
		b := e.mustCreateIssue(t, fmt.Sprintf("B%d", i))
		code, _ := e.do(t, http.MethodPost, e.issueLinksPath(a),
			map[string]any{"target_issue": b.Identifier, "link_type": "blocks"})
		if code != http.StatusCreated {
			t.Fatalf("create link %d: %d", i, code)
		}
	}
	code, body = e.do(t, http.MethodGet,
		e.issueLinksPath(a)+"?limit=2", nil)
	if code != http.StatusOK {
		t.Fatalf("links code = %d", code)
	}
	lp := decodePage[linkView](t, body)
	if len(lp.Items) != 2 || lp.NextCursor == nil {
		t.Fatalf("links page = %+v", lp)
	}
	code, body = e.do(t, http.MethodGet,
		e.issueLinksPath(a)+"?limit=2&cursor="+url.QueryEscape(*lp.NextCursor), nil)
	if code != http.StatusOK {
		t.Fatalf("links page2 code = %d", code)
	}
	lp2 := decodePage[linkView](t, body)
	if len(lp2.Items) != 1 || lp2.NextCursor != nil {
		t.Fatalf("links page2 = %+v", lp2)
	}
}

// TestPaginationBadCursorOnAllListEndpoints ensures every list handler maps an
// invalid cursor to 400, not 500. Catches a future addition that forgets to
// wire the parser.
func TestPaginationBadCursorOnAllListEndpoints(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	a := e.mustCreateIssue(t, "A")
	const bad = "!!!"
	paths := []string{
		"/users?cursor=" + bad,
		"/projects?cursor=" + bad,
		e.projectIssuesPath() + "?cursor=" + bad,
		e.projectSprintsPath() + "?cursor=" + bad,
		e.issueLinksPath(a) + "?cursor=" + bad,
	}
	for _, p := range paths {
		code, _ := e.do(t, http.MethodGet, p, nil)
		if code != http.StatusBadRequest {
			t.Errorf("%s: code = %d, want 400", p, code)
		}
	}
}

// TestPaginationBadLimitOnAllListEndpoints mirrors the cursor test for limit.
func TestPaginationBadLimitOnAllListEndpoints(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	a := e.mustCreateIssue(t, "A")
	paths := []string{
		"/users?limit=0",
		"/projects?limit=-1",
		e.projectIssuesPath() + "?limit=abc",
		e.projectSprintsPath() + "?limit=0",
		e.issueLinksPath(a) + "?limit=-5",
	}
	for _, p := range paths {
		code, _ := e.do(t, http.MethodGet, p, nil)
		if code != http.StatusBadRequest {
			t.Errorf("%s: code = %d, want 400", p, code)
		}
	}
}
