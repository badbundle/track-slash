package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	t.Parallel()
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

	code, body = e.do(t, http.MethodPut, e.projectPath()+"/members/"+user.Username, nil)
	if code != http.StatusOK {
		t.Fatalf("grant code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, e.projectPath()+"/members", nil)
	if code != http.StatusOK {
		t.Fatalf("list members code = %d body = %s", code, body)
	}
	members := decode[[]model.ProjectMember](t, body)
	seenMembers := map[uuid.UUID]bool{}
	for _, member := range members {
		seenMembers[member.UserID] = true
	}
	if len(members) != 2 || !seenMembers[e.adminID] || !seenMembers[user.ID] {
		t.Fatalf("members = %+v", members)
	}
	code, body = e.do(t, http.MethodDelete, e.projectPath()+"/members/"+user.Username, nil)
	if code != http.StatusNoContent {
		t.Fatalf("revoke member code = %d body = %s", code, body)
	}
}

func TestHTTPListProjectMembersExcludesDeletedUsers(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, _ := e.mustUserToken(t, "deleted-member")
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	if err := e.store.DeleteUser(e.ctx, user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	code, body := e.do(t, http.MethodGet, e.projectPath()+"/members", nil)
	if code != http.StatusOK {
		t.Fatalf("list members code = %d body = %s", code, body)
	}
	members := decode[[]model.ProjectMember](t, body)
	for _, member := range members {
		if member.UserID == user.ID {
			t.Fatalf("deleted user returned as project member: %+v", members)
		}
	}
}

func TestHTTPProjectMemberSearch(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	member, memberToken := e.mustUserToken(t, "search-member")
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, member.ID); err != nil {
		t.Fatalf("GrantProjectAccess member: %v", err)
	}
	target, err := e.store.CreateUser(e.ctx, "search-target-"+uniqueProjectKey(t)+"@example.com", "Search Target")
	if err != nil {
		t.Fatalf("CreateUser target: %v", err)
	}
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, target.ID); err != nil {
		t.Fatalf("GrantProjectAccess target: %v", err)
	}
	outsider, outsiderToken := e.mustUserToken(t, "search-outsider")
	nonMember, err := e.store.CreateUser(e.ctx, "search-nonmember-"+uniqueProjectKey(t)+"@example.com", "Search Target Nonmember")
	if err != nil {
		t.Fatalf("CreateUser nonmember: %v", err)
	}

	code, body := e.doWithToken(t, memberToken, http.MethodGet, e.projectPath()+"/members/search?q="+url.QueryEscape("target")+"&limit=10", nil)
	if code != http.StatusOK {
		t.Fatalf("member search code = %d body = %s", code, body)
	}
	users := decode[[]model.User](t, body)
	if len(users) != 1 || users[0].ID != target.ID {
		t.Fatalf("member search users = %+v, want target %s", users, target.ID)
	}
	if users[0].ID == nonMember.ID || users[0].ID == outsider.ID {
		t.Fatalf("search included non-member users: %+v", users)
	}

	code, body = e.doWithToken(t, outsiderToken, http.MethodGet, e.projectPath()+"/members/search?q=target", nil)
	if code != http.StatusForbidden {
		t.Fatalf("outsider search code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodGet, e.projectPath()+"/members/search?limit=0", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad limit search code = %d body = %s", code, body)
	}
}

func TestHTTPAdminCanCreateUsersAndProjects(t *testing.T) {
	t.Parallel()
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

func TestHTTPMemberCanCreateProjectAndGetsAccess(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustUserToken(t, "project-creator")
	key := uniqueProjectKey(t)
	code, body := e.doWithToken(t, token, http.MethodPost, "/projects/", map[string]any{
		"key":         key,
		"name":        "Member Project",
		"description": "via member API",
	})
	if code != http.StatusCreated {
		t.Fatalf("create project code = %d body = %s", code, body)
	}
	p := decode[model.Project](t, body)
	if p.ID == uuid.Nil || p.Key != key || p.Description != "via member API" {
		t.Fatalf("project = %+v", p)
	}

	code, body = e.doWithToken(t, token, http.MethodGet, "/projects", nil)
	if code != http.StatusOK {
		t.Fatalf("list projects code = %d body = %s", code, body)
	}
	page := decodePage[model.Project](t, body)
	if len(page.Items) != 1 || page.Items[0].ID != p.ID {
		t.Fatalf("visible projects = %+v", page.Items)
	}

	code, body = e.doWithToken(t, token, http.MethodGet, "/"+p.OwnerUsername+"/projects/"+p.Key, nil)
	if code != http.StatusOK {
		t.Fatalf("get created project code = %d body = %s", code, body)
	}
	got := decode[model.Project](t, body)
	if got.ID != p.ID {
		t.Fatalf("got project = %+v, want %s", got, p.ID)
	}

	members, err := e.store.ListProjectMembers(e.ctx, p.ID)
	if err != nil {
		t.Fatalf("ListProjectMembers: %v", err)
	}
	if len(members) != 1 || members[0].UserID != user.ID {
		t.Fatalf("members = %+v", members)
	}

	code, body = e.doWithToken(t, token, http.MethodPost, "/projects/", map[string]any{
		"key":  "bad",
		"name": "Bad",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("bad key code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, token, http.MethodPost, "/projects/", map[string]any{
		"key":  key,
		"name": "Duplicate",
	})
	if code != http.StatusConflict {
		t.Fatalf("duplicate key code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, token, http.MethodPost, "/projects/", map[string]any{
		"key":  uniqueProjectKey(t),
		"name": " ",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("blank name code = %d body = %s", code, body)
	}
}

func TestHTTPProjectMembershipFiltersAndForbids(t *testing.T) {
	t.Parallel()
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

	code, body = e.doWithToken(t, token, http.MethodGet, "/"+other.OwnerUsername+"/projects/"+other.Key, nil)
	if code != http.StatusForbidden {
		t.Fatalf("forbidden project code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, token, http.MethodGet, "/"+other.OwnerUsername+"/projects/"+other.Key+"/issues/deleted", nil)
	if code != http.StatusForbidden {
		t.Fatalf("forbidden deleted issues code = %d body = %s", code, body)
	}
}

func TestHTTPTokenAdminEndpoints(t *testing.T) {
	t.Parallel()
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

func TestHTTPAccountsSessionsAndSelfTokens(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	username := "acct" + strings.ToLower(uniqueProjectKey(t))
	password := "correct-horse-battery"
	code, body := e.doUnauth(t, http.MethodPost, "/accounts", map[string]any{
		"username": username,
		"password": password,
	})
	if code != http.StatusCreated {
		t.Fatalf("create account code = %d body = %s", code, body)
	}
	u := decode[model.User](t, body)
	if u.Username != username || u.Email != "" || u.IsAdmin {
		t.Fatalf("created account = %+v", u)
	}

	code, body = e.doUnauth(t, http.MethodPost, "/accounts", map[string]any{
		"username": strings.ToUpper(username),
		"password": password,
	})
	if code != http.StatusConflict {
		t.Fatalf("duplicate account code = %d body = %s", code, body)
	}
	code, body = e.doUnauth(t, http.MethodPost, "/accounts", map[string]any{
		"username": "bad!",
		"password": password,
	})
	if code != http.StatusBadRequest {
		t.Fatalf("bad account code = %d body = %s", code, body)
	}

	code, body = e.doUnauth(t, http.MethodPost, "/session", map[string]any{
		"username": username,
		"password": password,
	})
	if code != http.StatusCreated {
		t.Fatalf("session code = %d body = %s", code, body)
	}
	session := decode[struct {
		model.AuthToken
		Token string `json:"token"`
	}](t, body)
	if session.Token == "" || session.Kind != model.AuthTokenKindSession || session.UserID != u.ID {
		t.Fatalf("session = %+v", session)
	}
	code, body = e.doWithToken(t, session.Token, http.MethodGet, "/me", nil)
	if code != http.StatusOK {
		t.Fatalf("session /me code = %d body = %s", code, body)
	}

	code, body = e.doWithToken(t, session.Token, http.MethodPost, "/me/tokens", map[string]any{
		"name": "self api",
		"kind": "api",
	})
	if code != http.StatusCreated {
		t.Fatalf("create my token code = %d body = %s", code, body)
	}
	apiToken := decode[struct {
		model.AuthToken
		Token string `json:"token"`
	}](t, body)
	if apiToken.Token == "" || apiToken.Kind != model.AuthTokenKindAPI || apiToken.UserID != u.ID {
		t.Fatalf("api token = %+v", apiToken)
	}
	code, body = e.doWithToken(t, session.Token, http.MethodGet, "/me/tokens", nil)
	if code != http.StatusOK {
		t.Fatalf("list my tokens code = %d body = %s", code, body)
	}
	tokens := decode[[]model.AuthToken](t, body)
	if len(tokens) < 2 {
		t.Fatalf("tokens = %+v", tokens)
	}

	other, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{
		UserID: e.adminID,
		Kind:   model.AuthTokenKindAPI,
		Name:   "other",
	})
	if err != nil {
		t.Fatalf("CreateAuthToken other: %v", err)
	}
	code, body = e.doWithToken(t, session.Token, http.MethodDelete, fmt.Sprintf("/me/tokens/%s", other.Token.ID), nil)
	if code != http.StatusNotFound {
		t.Fatalf("revoke other code = %d body = %s", code, body)
	}

	code, body = e.doWithToken(t, session.Token, http.MethodDelete, fmt.Sprintf("/me/tokens/%s", apiToken.ID), nil)
	if code != http.StatusNoContent {
		t.Fatalf("revoke my token code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, apiToken.Token, http.MethodGet, "/me", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("revoked my token /me code = %d body = %s", code, body)
	}
}

func TestHTTPUpdateMySettings(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	username := "settings" + strings.ToLower(uniqueProjectKey(t))
	oldPassword := "correct-horse-battery"
	newPassword := "new-correct-horse"
	u, err := e.store.CreateAccount(e.ctx, store.CreateAccountParams{
		Username: username,
		Password: oldPassword,
		Name:     "Old Name",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	session, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{
		UserID: u.ID,
		Kind:   model.AuthTokenKindSession,
		Name:   "session",
	})
	if err != nil {
		t.Fatalf("CreateAuthToken session: %v", err)
	}

	code, body := e.doWithToken(t, session.RawToken, http.MethodPatch, "/me/settings", map[string]any{
		"name":  "New Name",
		"email": "new@example.com",
	})
	if code != http.StatusOK {
		t.Fatalf("profile settings code = %d body = %s", code, body)
	}
	updated := decode[model.User](t, body)
	if updated.Name != "New Name" || updated.Email != "new@example.com" || updated.Username != username {
		t.Fatalf("updated = %+v", updated)
	}

	code, body = e.doWithToken(t, session.RawToken, http.MethodPatch, "/me/settings", map[string]any{
		"current_password": "wrong-password",
		"new_password":     newPassword,
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("wrong password code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, session.RawToken, http.MethodPatch, "/me/settings", map[string]any{
		"current_password": oldPassword,
	})
	if code != http.StatusBadRequest {
		t.Fatalf("missing new password code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, session.RawToken, http.MethodPatch, "/me/settings", map[string]any{
		"name": "",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("blank name code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, session.RawToken, http.MethodPatch, "/me/settings", map[string]any{})
	if code != http.StatusBadRequest {
		t.Fatalf("empty settings code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, session.RawToken, http.MethodPatch, "/me/settings", map[string]any{
		"current_password": oldPassword,
		"new_password":     newPassword,
	})
	if code != http.StatusOK {
		t.Fatalf("password settings code = %d body = %s", code, body)
	}
	if _, err := e.store.AuthenticatePassword(e.ctx, username, oldPassword); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("old password err = %v, want ErrUnauthorized", err)
	}
	if _, err := e.store.AuthenticatePassword(e.ctx, username, newPassword); err != nil {
		t.Fatalf("new password auth: %v", err)
	}
}

func TestHTTPProjectScopedIDRoutesForbidOtherProjects(t *testing.T) {
	t.Parallel()
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

	code, body := e.doWithToken(t, token, http.MethodGet, e.issuePath(own), nil)
	if code != http.StatusOK {
		t.Fatalf("own issue code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, token, http.MethodGet, e.issuePath(otherIssue), nil)
	if code != http.StatusForbidden {
		t.Fatalf("other issue code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, token, http.MethodGet, "/"+e.ownerUsername+"/issues?refs="+own.Identifier+","+otherIssue.Identifier, nil)
	if code != http.StatusForbidden {
		t.Fatalf("mixed batch code = %d body = %s", code, body)
	}
	if err := e.store.DeleteIssue(e.ctx, otherIssue.ID); err != nil {
		t.Fatalf("DeleteIssue other: %v", err)
	}
	code, body = e.doWithToken(t, token, http.MethodPost, e.issuePath(otherIssue)+"/restore", nil)
	if code != http.StatusForbidden {
		t.Fatalf("other issue restore code = %d body = %s", code, body)
	}
}

func TestHTTPAuthenticatedAttribution(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustUserToken(t, "writer")
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}

	code, body := e.doWithToken(t, token, http.MethodPost, e.projectIssuesPath(), map[string]any{
		"title": "reported by current user",
	})
	if code != http.StatusCreated {
		t.Fatalf("create issue code = %d body = %s", code, body)
	}
	iss := decode[model.Issue](t, body)
	if iss.ReporterID == nil || *iss.ReporterID != user.ID {
		t.Fatalf("reporter_id = %v, want %s", iss.ReporterID, user.ID)
	}

	code, body = e.doWithToken(t, token, http.MethodPost, e.issueCommentsPath(iss), map[string]any{
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
	code, body = e.do(t, http.MethodPost, e.projectIssuesPath(), map[string]any{
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
	t.Parallel()
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
	t.Parallel()
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

func TestUIWebSocketUsesSessionCookieAndAPIWebSocketRequiresBearer(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	hub := realtime.NewHub()
	srv := server.New(e.store, hub, nil)
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	session, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{
		UserID: e.adminID,
		Kind:   model.AuthTokenKindSession,
		Name:   "ui websocket",
	})
	if err != nil {
		t.Fatalf("CreateAuthToken session: %v", err)
	}
	iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "websocket changelog source",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	entries, _, err := e.store.ListProjectChangelog(e.ctx, store.ListProjectChangelogParams{
		ProjectID: e.projectID,
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("ListProjectChangelog: %v", err)
	}
	if len(entries) == 0 || entries[0].IssueID == nil || *entries[0].IssueID != iss.ID {
		t.Fatalf("latest changelog entry = %+v, want issue %s", entries, iss.ID)
	}
	entry := entries[0]

	apiReq, err := http.NewRequest(http.MethodGet, ts.URL+apiPath("/ws"), nil)
	if err != nil {
		t.Fatalf("NewRequest /api/v1/ws: %v", err)
	}
	apiReq.AddCookie(&http.Cookie{Name: uiCookieNameForTest, Value: session.RawToken, Path: "/"})
	apiRes, err := ts.Client().Do(apiReq)
	if err != nil {
		t.Fatalf("cookie-only /api/v1/ws: %v", err)
	}
	_ = apiRes.Body.Close()
	if apiRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("cookie-only /api/v1/ws code = %d, want 401", apiRes.StatusCode)
	}

	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/realtime", &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": []string{uiCookieNameForTest + "=" + session.RawToken}},
	})
	if err != nil {
		t.Fatalf("ui websocket dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	msg, err := json.Marshal(map[string]string{
		"action": "subscribe",
		"topic":  realtime.ProjectChangelogTopic(entry.ID),
	})
	if err != nil {
		t.Fatalf("marshal subscribe: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}

	publishCtx, stopPublish := context.WithCancel(ctx)
	defer stopPublish()
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		ev := realtime.Event{
			Op:        realtime.OpInsert,
			Entity:    realtime.EntityChangelog,
			ID:        entry.ID,
			ProjectID: &e.projectID,
			Version:   entry.Version,
		}
		for {
			select {
			case <-publishCtx.Done():
				return
			case <-ticker.C:
				hub.Publish(ev)
			}
		}
	}()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read ui websocket: %v", err)
		}
		var got struct {
			Error  string          `json:"error"`
			Entity realtime.Entity `json:"entity"`
			ID     uuid.UUID       `json:"id"`
		}
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal ui websocket event: %v data=%s", err, data)
		}
		if got.Error != "" {
			t.Fatalf("ui websocket subscribe error = %q", got.Error)
		}
		if got.Entity == realtime.EntityChangelog && got.ID == entry.ID {
			return
		}
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
