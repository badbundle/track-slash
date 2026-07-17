package server_test

import (
	"bytes"
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
	"github.com/go-webauthn/webauthn/webauthn"
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
	if bytes.Contains(body, []byte(target.Email)) {
		t.Fatalf("member search exposed email: %s", body)
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

func TestHTTPProjectMemberRolesAndReadonlyAccess(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	owner, ownerToken := e.mustUserToken(t, "role-owner")
	member, memberToken := e.mustUserToken(t, "role-editor")
	readonly, readonlyToken := e.mustUserToken(t, "role-readonly")
	candidate, _ := e.mustUserToken(t, "role-candidate")

	key := uniqueProjectKey(t)
	project, err := e.store.CreateProjectForUser(e.ctx, owner.ID, key, "Role Project", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser: %v", err)
	}
	path := "/" + project.OwnerUsername + "/projects/" + project.Key

	code, body := e.doWithToken(t, ownerToken, http.MethodPut, path+"/members/"+member.Username, map[string]any{"role": "member"})
	if code != http.StatusOK {
		t.Fatalf("owner add member code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, ownerToken, http.MethodPut, path+"/members/"+readonly.Username, map[string]any{"role": "readonly"})
	if code != http.StatusOK {
		t.Fatalf("owner add readonly code = %d body = %s", code, body)
	}
	gotReadonly := decode[model.ProjectMember](t, body)
	if gotReadonly.Role != model.ProjectMemberRoleReadonly || gotReadonly.Username != readonly.Username || gotReadonly.Name != readonly.Name {
		t.Fatalf("readonly member = %+v", gotReadonly)
	}

	code, body = e.doWithToken(t, readonlyToken, http.MethodGet, path, nil)
	if code != http.StatusOK {
		t.Fatalf("readonly get project code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, readonlyToken, http.MethodGet, path+"/members", nil)
	if code != http.StatusOK {
		t.Fatalf("readonly list members code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, readonlyToken, http.MethodPatch, path, map[string]any{"name": "Denied"})
	if code != http.StatusForbidden {
		t.Fatalf("readonly update project code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, readonlyToken, http.MethodPost, path+"/issues", map[string]any{"title": "Denied"})
	if code != http.StatusForbidden {
		t.Fatalf("readonly create issue code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, readonlyToken, http.MethodPut, path+"/favorite", nil)
	if code != http.StatusOK {
		t.Fatalf("readonly favorite code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, readonlyToken, http.MethodGet, path+"/members/candidates", nil)
	if code != http.StatusForbidden {
		t.Fatalf("readonly candidates code = %d body = %s", code, body)
	}

	code, body = e.doWithToken(t, memberToken, http.MethodPatch, path, map[string]any{"name": "Member Updated"})
	if code != http.StatusOK {
		t.Fatalf("member update project code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, memberToken, http.MethodPut, path+"/members/"+candidate.Username, nil)
	if code != http.StatusForbidden {
		t.Fatalf("member manage members code = %d body = %s", code, body)
	}

	code, body = e.doWithToken(t, ownerToken, http.MethodGet, path+"/members/candidates?q="+url.QueryEscape(candidate.Username), nil)
	if code != http.StatusOK {
		t.Fatalf("owner candidates code = %d body = %s", code, body)
	}
	candidates := decode[[]model.ProjectMemberCandidate](t, body)
	if len(candidates) != 1 || candidates[0].ID != candidate.ID {
		t.Fatalf("candidates = %+v", candidates)
	}
	code, body = e.doWithToken(t, ownerToken, http.MethodPut, path+"/members/"+owner.Username, map[string]any{"role": "readonly"})
	if code != http.StatusConflict {
		t.Fatalf("downgrade owner code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, ownerToken, http.MethodDelete, path+"/members/"+owner.Username, nil)
	if code != http.StatusConflict {
		t.Fatalf("remove owner code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, ownerToken, http.MethodPut, path+"/members/"+candidate.Username, map[string]any{"role": "invalid"})
	if code != http.StatusBadRequest {
		t.Fatalf("invalid role code = %d body = %s", code, body)
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
	if created.Token == "" || created.Kind != model.AuthTokenKindSession || created.ExpiresAt == nil {
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
	if session.Token == "" || session.Kind != model.AuthTokenKindSession || session.UserID != u.ID || session.ExpiresAt == nil {
		t.Fatalf("session = %+v", session)
	}
	if remaining := time.Until(*session.ExpiresAt); remaining < 7*24*time.Hour-time.Minute || remaining > 7*24*time.Hour+time.Minute {
		t.Fatalf("session expiry remaining = %v, want about 168h", remaining)
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
	if apiToken.Token == "" || apiToken.Kind != model.AuthTokenKindAPI || apiToken.UserID != u.ID || apiToken.ExpiresAt != nil {
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

func TestHTTPPasskeyManagementPasswordReauth(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	username := "passkeyapi" + strings.ToLower(uniqueProjectKey(t))
	password := "correct-horse-battery"
	u, err := e.store.CreateAccount(e.ctx, store.CreateAccountParams{
		Username: username,
		Password: password,
		Name:     "Passkey API",
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

	code, body := e.doUnauth(t, http.MethodGet, "/me/passkeys", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("unauth list passkeys code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, session.RawToken, http.MethodGet, "/me/passkeys", nil)
	if code != http.StatusOK {
		t.Fatalf("list passkeys code = %d body = %s", code, body)
	}
	passkeys := decode[[]model.PasskeyCredential](t, body)
	if len(passkeys) != 0 {
		t.Fatalf("passkeys = %+v, want empty", passkeys)
	}

	code, body = e.doWithToken(t, session.RawToken, http.MethodPost, "/me/reauth/password", map[string]any{
		"current_password": "wrong-password",
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("bad password reauth code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, session.RawToken, http.MethodPost, "/me/reauth/password", map[string]any{
		"current_password": password,
	})
	if code != http.StatusCreated {
		t.Fatalf("password reauth code = %d body = %s", code, body)
	}
	reauth := decode[struct {
		Token string `json:"reauth_token"`
	}](t, body)
	if reauth.Token == "" {
		t.Fatalf("reauth token empty: %+v", reauth)
	}
	code, body = e.doWithToken(t, session.RawToken, http.MethodPost, "/me/passkeys/options", map[string]any{
		"name":         "Laptop",
		"reauth_token": "bad-token",
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("bad reauth add options code = %d body = %s", code, body)
	}
}

func TestHTTPPasswordLoginStateAndToggle(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	username := "pwdlogin" + strings.ToLower(uniqueProjectKey(t))
	password := "correct-horse-battery"
	u, err := e.store.CreateAccount(e.ctx, store.CreateAccountParams{
		Username: username,
		Password: password,
		Name:     "Password Login",
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

	code, body := e.doUnauth(t, http.MethodGet, "/me/password-login", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("unauth password-login code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, session.RawToken, http.MethodGet, "/me/password-login", nil)
	if code != http.StatusOK {
		t.Fatalf("password-login state code = %d body = %s", code, body)
	}
	state := decode[model.PasswordLoginState](t, body)
	if !state.HasPassword || !state.Enabled || state.CanDisable || state.ActivePasskeys != 0 {
		t.Fatalf("initial state = %+v", state)
	}

	code, body = e.doWithToken(t, session.RawToken, http.MethodPatch, "/me/password-login", map[string]any{
		"reauth_token": "bad-token",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("missing enabled code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, session.RawToken, http.MethodPatch, "/me/password-login", map[string]any{
		"enabled":      false,
		"reauth_token": "bad-token",
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("bad reauth token code = %d body = %s", code, body)
	}

	code, body = e.doWithToken(t, session.RawToken, http.MethodPost, "/me/reauth/password", map[string]any{
		"current_password": password,
	})
	if code != http.StatusCreated {
		t.Fatalf("password reauth code = %d body = %s", code, body)
	}
	noPasskeyReauth := decode[struct {
		Token string `json:"reauth_token"`
	}](t, body)
	code, body = e.doWithToken(t, session.RawToken, http.MethodPatch, "/me/password-login", map[string]any{
		"enabled":      false,
		"reauth_token": noPasskeyReauth.Token,
	})
	if code != http.StatusConflict {
		t.Fatalf("disable without passkey code = %d body = %s", code, body)
	}

	if _, err := e.store.AddPasskeyCredential(e.ctx, u.ID, "localhost", "Laptop", serverPasskeyCredential("credential-"+uniqueProjectKey(t), 1)); err != nil {
		t.Fatalf("AddPasskeyCredential: %v", err)
	}
	code, body = e.doWithToken(t, session.RawToken, http.MethodPost, "/me/reauth/password", map[string]any{
		"current_password": password,
	})
	if code != http.StatusCreated {
		t.Fatalf("first password reauth code = %d body = %s", code, body)
	}
	disableReauth := decode[struct {
		Token string `json:"reauth_token"`
	}](t, body)
	code, body = e.doWithToken(t, session.RawToken, http.MethodPost, "/me/reauth/password", map[string]any{
		"current_password": password,
	})
	if code != http.StatusCreated {
		t.Fatalf("second password reauth code = %d body = %s", code, body)
	}
	staleReauth := decode[struct {
		Token string `json:"reauth_token"`
	}](t, body)

	code, body = e.doWithToken(t, session.RawToken, http.MethodPatch, "/me/password-login", map[string]any{
		"enabled":      false,
		"reauth_token": disableReauth.Token,
	})
	if code != http.StatusOK {
		t.Fatalf("disable password-login code = %d body = %s", code, body)
	}
	state = decode[model.PasswordLoginState](t, body)
	if !state.HasPassword || state.Enabled || state.CanDisable || state.ActivePasskeys != 1 {
		t.Fatalf("disabled state = %+v", state)
	}
	code, body = e.doUnauth(t, http.MethodPost, "/session", map[string]any{
		"username": username,
		"password": password,
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("disabled password session code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, session.RawToken, http.MethodPost, "/me/reauth/password", map[string]any{
		"current_password": password,
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("disabled password reauth code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, session.RawToken, http.MethodPatch, "/me/password-login", map[string]any{
		"enabled":      true,
		"reauth_token": staleReauth.Token,
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("stale reauth enable code = %d body = %s", code, body)
	}

	passkeyReauth, err := e.store.CreatePasskeyReauthToken(e.ctx, u.ID)
	if err != nil {
		t.Fatalf("CreatePasskeyReauthToken: %v", err)
	}
	code, body = e.doWithToken(t, session.RawToken, http.MethodPatch, "/me/password-login", map[string]any{
		"enabled":      true,
		"reauth_token": passkeyReauth,
	})
	if code != http.StatusOK {
		t.Fatalf("enable password-login code = %d body = %s", code, body)
	}
	state = decode[model.PasswordLoginState](t, body)
	if !state.HasPassword || !state.Enabled || !state.CanDisable || state.ActivePasskeys != 1 {
		t.Fatalf("enabled state = %+v", state)
	}
	code, body = e.doUnauth(t, http.MethodPost, "/session", map[string]any{
		"username": username,
		"password": password,
	})
	if code != http.StatusCreated {
		t.Fatalf("re-enabled password session code = %d body = %s", code, body)
	}
}

func serverPasskeyCredential(id string, signCount uint32) webauthn.Credential {
	return webauthn.Credential{
		ID:        []byte(id),
		PublicKey: []byte("public-key-" + id),
		Flags: webauthn.CredentialFlags{
			UserPresent:    true,
			UserVerified:   true,
			BackupEligible: true,
			BackupState:    true,
		},
		Authenticator: webauthn.Authenticator{SignCount: signCount},
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
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, override.ID); err != nil {
		t.Fatalf("GrantProjectAccess override: %v", err)
	}
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

func TestExpiredSessionsRejectedAcrossHTTPAndWebSockets(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	hub := realtime.NewHub()
	srv := server.NewWithOptions(e.store, hub, server.Options{SessionTTL: time.Hour})
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	past := time.Now().Add(-time.Minute)
	expired, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{
		UserID:    e.adminID,
		Kind:      model.AuthTokenKindSession,
		Name:      "expired session",
		ExpiresAt: &past,
	})
	if err != nil {
		t.Fatalf("CreateAuthToken expired session: %v", err)
	}

	tests := []struct {
		name            string
		path            string
		useCookie       bool
		wantStatus      int
		wantClearCookie bool
	}{
		{name: "API bearer", path: apiPath("/me"), wantStatus: http.StatusUnauthorized},
		{name: "UI cookie", path: "/projects", useCookie: true, wantStatus: http.StatusSeeOther, wantClearCookie: true},
		{name: "API websocket", path: apiPath("/ws"), wantStatus: http.StatusUnauthorized},
		{name: "UI websocket", path: "/realtime", useCookie: true, wantStatus: http.StatusSeeOther, wantClearCookie: true},
	}

	client := *ts.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(e.ctx, http.MethodGet, ts.URL+tt.path, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if tt.useCookie {
				req.AddCookie(&http.Cookie{Name: uiCookieNameForTest, Value: expired.RawToken, Path: "/"})
			} else {
				req.Header.Set("Authorization", "Bearer "+expired.RawToken)
			}
			res, err := client.Do(req)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer res.Body.Close()
			if res.StatusCode != tt.wantStatus {
				t.Fatalf("GET %s code = %d, want %d", tt.path, res.StatusCode, tt.wantStatus)
			}
			setCookie := res.Header.Get("Set-Cookie")
			if tt.wantClearCookie && (!strings.Contains(setCookie, uiCookieNameForTest+"=") || !strings.Contains(setCookie, "Max-Age=0")) {
				t.Fatalf("GET %s Set-Cookie = %q, want cleared session", tt.path, setCookie)
			}
			if !tt.wantClearCookie && setCookie != "" {
				t.Fatalf("GET %s Set-Cookie = %q, want none", tt.path, setCookie)
			}
		})
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
		HTTPHeader: http.Header{
			"Cookie": []string{uiCookieNameForTest + "=" + session.RawToken},
			"Origin": []string{ts.URL},
		},
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
