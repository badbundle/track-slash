package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/realtime"
	"github.com/bradleymackey/track-slash/internal/server"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestHTTPAuthRequiredAndAdminOnly(t *testing.T) {
	e := newHTTPEnv(t)
	code, body := e.doUnauth(t, http.MethodGet, "/projects", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("unauth code = %d body = %s", code, body)
	}

	user, token := e.mustUserToken(t, "normal")
	code, body = e.doWithToken(t, token, http.MethodGet, "/users", nil)
	if code != http.StatusForbidden {
		t.Fatalf("non-admin /users code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, "/me", nil)
	if code != http.StatusOK {
		t.Fatalf("/me code = %d body = %s", code, body)
	}
	me := decode[struct {
		User      model.User          `json:"user"`
		TokenKind model.AuthTokenKind `json:"token_kind"`
	}](t, body)
	if me.User.ID != e.adminID || !me.User.IsAdmin || me.TokenKind != model.AuthTokenKindAPI {
		t.Fatalf("me = %+v", me)
	}

	code, body = e.do(t, http.MethodPut, fmt.Sprintf("/projects/%s/members/%s", e.projectID, user.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("grant code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, fmt.Sprintf("/projects/%s/members", e.projectID), nil)
	if code != http.StatusOK {
		t.Fatalf("list members code = %d body = %s", code, body)
	}
	members := decode[[]model.ProjectMember](t, body)
	if len(members) != 1 || members[0].UserID != user.ID {
		t.Fatalf("members = %+v", members)
	}
	code, body = e.do(t, http.MethodDelete, fmt.Sprintf("/projects/%s/members/%s", e.projectID, user.ID), nil)
	if code != http.StatusNoContent {
		t.Fatalf("revoke member code = %d body = %s", code, body)
	}
}

func TestHTTPAdminCanCreateUsersAndProjects(t *testing.T) {
	e := newHTTPEnv(t)
	code, body := e.do(t, http.MethodPost, "/users/", map[string]any{
		"email": "created-" + uuid.NewString() + "@example.com",
		"name":  "Created",
	})
	if code != http.StatusCreated {
		t.Fatalf("create user code = %d body = %s", code, body)
	}
	u := decode[model.User](t, body)
	if u.ID == uuid.Nil || u.IsAdmin {
		t.Fatalf("user = %+v", u)
	}

	code, body = e.do(t, http.MethodPost, "/projects/", map[string]any{
		"key":         uniqueProjectKey(t),
		"name":        "Created",
		"description": "via admin API",
	})
	if code != http.StatusCreated {
		t.Fatalf("create project code = %d body = %s", code, body)
	}
	p := decode[model.Project](t, body)
	if p.ID == uuid.Nil || p.Description != "via admin API" {
		t.Fatalf("project = %+v", p)
	}
}

func TestHTTPProjectMembershipFiltersAndForbids(t *testing.T) {
	e := newHTTPEnv(t)
	user, token := e.mustUserToken(t, "member")
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	other, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "other", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}

	code, body := e.doWithToken(t, token, http.MethodGet, "/projects", nil)
	if code != http.StatusOK {
		t.Fatalf("list projects code = %d body = %s", code, body)
	}
	page := decodePage[model.Project](t, body)
	if len(page.Items) != 1 || page.Items[0].ID != e.projectID {
		t.Fatalf("visible projects = %+v", page.Items)
	}

	code, body = e.doWithToken(t, token, http.MethodGet, fmt.Sprintf("/projects/%s", other.ID), nil)
	if code != http.StatusForbidden {
		t.Fatalf("forbidden project code = %d body = %s", code, body)
	}
}

func TestHTTPTokenAdminEndpoints(t *testing.T) {
	e := newHTTPEnv(t)
	user, _ := e.mustUserToken(t, "token-target")

	code, body := e.do(t, http.MethodPost, fmt.Sprintf("/users/%s/tokens", user.ID), map[string]any{
		"name": "portal-ready-session",
		"kind": "session",
	})
	if code != http.StatusCreated {
		t.Fatalf("create token code = %d body = %s", code, body)
	}
	created := decode[struct {
		model.AuthToken
		Token string `json:"token"`
	}](t, body)
	if created.Token == "" || created.Kind != model.AuthTokenKindSession {
		t.Fatalf("created token = %+v", created)
	}

	code, body = e.do(t, http.MethodGet, fmt.Sprintf("/users/%s/tokens", user.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("list tokens code = %d body = %s", code, body)
	}
	tokens := decode[[]model.AuthToken](t, body)
	found := false
	for _, tok := range tokens {
		if tok.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("created token missing from list: %+v", tokens)
	}

	code, body = e.do(t, http.MethodDelete, fmt.Sprintf("/tokens/%s", created.ID), nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete token code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, created.Token, http.MethodGet, "/me", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("revoked token /me code = %d body = %s", code, body)
	}
}

func TestHTTPProjectScopedIDRoutesForbidOtherProjects(t *testing.T) {
	e := newHTTPEnv(t)
	user, token := e.mustUserToken(t, "scoped")
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	own, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "own"})
	if err != nil {
		t.Fatalf("CreateIssue own: %v", err)
	}
	otherProject, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "other", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	otherIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: otherProject.ID, Title: "other"})
	if err != nil {
		t.Fatalf("CreateIssue other: %v", err)
	}

	code, body := e.doWithToken(t, token, http.MethodGet, fmt.Sprintf("/issues/%s", own.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("own issue code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, token, http.MethodGet, fmt.Sprintf("/issues/%s", otherIssue.ID), nil)
	if code != http.StatusForbidden {
		t.Fatalf("other issue code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, token, http.MethodGet, fmt.Sprintf("/issues?ids=%s,%s", own.ID, otherIssue.ID), nil)
	if code != http.StatusForbidden {
		t.Fatalf("mixed batch code = %d body = %s", code, body)
	}
}

func TestHTTPAuthenticatedAttribution(t *testing.T) {
	e := newHTTPEnv(t)
	user, token := e.mustUserToken(t, "writer")
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}

	code, body := e.doWithToken(t, token, http.MethodPost, fmt.Sprintf("/projects/%s/issues", e.projectID), map[string]any{
		"title": "reported by current user",
	})
	if code != http.StatusCreated {
		t.Fatalf("create issue code = %d body = %s", code, body)
	}
	iss := decode[model.Issue](t, body)
	if iss.ReporterID == nil || *iss.ReporterID != user.ID {
		t.Fatalf("reporter_id = %v, want %s", iss.ReporterID, user.ID)
	}

	code, body = e.doWithToken(t, token, http.MethodPost, fmt.Sprintf("/issues/%s/comments", iss.ID), map[string]any{
		"author_id": uuid.NewString(),
		"body":      "current user wins",
	})
	if code != http.StatusCreated {
		t.Fatalf("create comment code = %d body = %s", code, body)
	}
	comment := decode[model.Comment](t, body)
	if comment.AuthorID != user.ID {
		t.Fatalf("author_id = %s, want %s", comment.AuthorID, user.ID)
	}

	override, _ := e.mustUserToken(t, "override")
	code, body = e.do(t, http.MethodPost, fmt.Sprintf("/projects/%s/issues", e.projectID), map[string]any{
		"title":       "admin override",
		"reporter_id": override.ID,
	})
	if code != http.StatusCreated {
		t.Fatalf("admin override code = %d body = %s", code, body)
	}
	iss = decode[model.Issue](t, body)
	if iss.ReporterID == nil || *iss.ReporterID != override.ID {
		t.Fatalf("admin reporter_id = %v, want %s", iss.ReporterID, override.ID)
	}
}

func TestHTTPHealthzIsUnderAPINamespace(t *testing.T) {
	e := newHTTPEnv(t)
	res, err := http.Get(e.ts.URL + apiPath("/healthz"))
	if err != nil {
		t.Fatalf("get /api/v1/healthz: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/api/v1/healthz code = %d", res.StatusCode)
	}
}

func TestWebSocketAuthAndTopicPermission(t *testing.T) {
	e := newHTTPEnv(t)
	hub := realtime.NewHub()
	srv := server.New(e.store, hub, nil)
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + apiPath("/ws"))
	if err != nil {
		t.Fatalf("get /api/v1/ws: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth /api/v1/ws code = %d, want 401", res.StatusCode)
	}

	user, token := e.mustUserToken(t, "ws")
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	other, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "other", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}

	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+apiPath("/ws"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
	})
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	msg, err := json.Marshal(map[string]string{
		"action": "subscribe",
		"topic":  realtime.ProjectTopic(other.ID),
	})
	if err != nil {
		t.Fatalf("marshal subscribe: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read subscribe response: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal response: %v data=%s", err, data)
	}
	if got["error"] != "forbidden" {
		t.Fatalf("response = %s, want forbidden error", data)
	}
}

func (e *httpEnv) mustUserToken(t *testing.T, label string) (model.User, string) {
	t.Helper()
	u, err := e.store.CreateUser(e.ctx, label+"-"+uuid.NewString()+"@example.com", label)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	created, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{
		UserID: u.ID,
		Kind:   model.AuthTokenKindAPI,
		Name:   "test",
	})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}
	return u, created.RawToken
}
