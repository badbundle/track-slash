package server_test

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestUIRedirectsUnauthenticatedApp(t *testing.T) {
	e := newHTTPEnv(t)
	res := e.uiDoNoRedirect(t, http.MethodGet, "/", "", nil)
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("code = %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); !strings.HasPrefix(loc, "/login?next=") {
		t.Fatalf("Location = %q", loc)
	}
}

func TestUILoginRejectsBadCredentials(t *testing.T) {
	e := newHTTPEnv(t)
	form := url.Values{"username": {"not-a-user"}, "password": {"not-a-password"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, "/login", "", strings.NewReader(form.Encode()))
	defer res.Body.Close()

	body := readBody(t, res)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("code = %d body = %s", res.StatusCode, body)
	}
	if strings.Contains(res.Header.Get("Set-Cookie"), uiCookieNameForTest) {
		t.Fatalf("unexpected auth cookie: %s", res.Header.Get("Set-Cookie"))
	}
	if !strings.Contains(body, "Username or password not accepted.") {
		t.Fatalf("body missing login error: %s", body)
	}
}

func TestUILoginSetsCookie(t *testing.T) {
	e := newHTTPEnv(t)
	username := "uilogin" + strings.ToLower(uniqueProjectKey(t))
	password := "correct-horse-battery"
	if _, err := e.store.CreateAccount(e.ctx, store.CreateAccountParams{Username: username, Password: password, Name: "UI Login"}); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	next := "/projects/" + e.projectID.String() + "/sprint"
	form := url.Values{"username": {username}, "password": {password}, "next": {next}}
	res := e.uiDoNoRedirect(t, http.MethodPost, "/login", "", strings.NewReader(form.Encode()))
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("code = %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != next {
		t.Fatalf("Location = %q", loc)
	}
	cookie := findUICookie(t, res.Cookies())
	if !cookie.HttpOnly {
		t.Fatal("ui auth cookie is not HttpOnly")
	}
	if cookie.Path != "/" {
		t.Fatalf("cookie Path = %q", cookie.Path)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie SameSite = %v", cookie.SameSite)
	}
}

func TestUISignupCreatesAccountAndSetsCookie(t *testing.T) {
	e := newHTTPEnv(t)
	username := "uisignup" + strings.ToLower(uniqueProjectKey(t))
	form := url.Values{"username": {username}, "password": {"correct-horse-battery"}, "next": {"/tokens"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, "/signup", "", strings.NewReader(form.Encode()))
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/tokens" {
		t.Fatalf("Location = %q", loc)
	}
	cookie := findUICookie(t, res.Cookies())
	if cookie.Value == "" || !cookie.HttpOnly {
		t.Fatalf("cookie = %+v", cookie)
	}
	if _, err := e.store.AuthenticatePassword(e.ctx, username, "correct-horse-battery"); err != nil {
		t.Fatalf("AuthenticatePassword after signup: %v", err)
	}
}

func TestUIRendersWorkSidebar(t *testing.T) {
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-member")

	body := e.uiGet(t, "/me", token)
	for _, want := range []string{">Me<", ">Projects<", `href="/settings"`, `href="/tokens"`, `data-lucide="user"`, `data-lucide="folder"`, "data-nav-loader"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `data-lucide="key-round"`) {
		t.Fatalf("body still has tokens sidebar icon: %s", body)
	}
	for _, notWant := range []string{"Assigned to me", "Active work board", "Across projects"} {
		if strings.Contains(body, notWant) {
			t.Fatalf("body still has sidebar subtitle %q: %s", notWant, body)
		}
	}
	for _, notWant := range []string{">Sprint<", ">Backlog<", e.projKey, `href="/sprint"`, `href="/backlog"`, `href="/projects/` + e.projectID.String() + `/sprint"`, `href="/projects/` + e.projectID.String() + `/backlog"`, `hx-get="/sprint/panel"`, `hx-get="/backlog/panel"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("body still has global work link %q: %s", notWant, body)
		}
	}
	if !strings.Contains(body, user.Name) {
		t.Fatalf("body missing current user: %s", body)
	}
	if strings.Contains(body, "/app") {
		t.Fatalf("body contains legacy /app path: %s", body)
	}
}

func TestUIProjectsPageListsVisibleProjectsAndCreatesProject(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-projects")
	hidden, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "Hidden UI Project", "")
	if err != nil {
		t.Fatalf("CreateProject hidden: %v", err)
	}

	body := e.uiGet(t, "/projects", token)
	for _, want := range []string{"Projects", "Projects you can access.", "Create project", e.projKey, "http-test", `href="/projects/` + e.projectID.String() + `/sprint"`, `href="/projects/` + e.projectID.String() + `/backlog"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("projects body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, hidden.Name) {
		t.Fatalf("projects body included inaccessible project: %s", body)
	}

	form := url.Values{"key": {"bad"}, "name": {"Bad"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, "/projects", token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "Key must match") {
		t.Fatalf("bad key code = %d body = %s", res.StatusCode, body)
	}

	form = url.Values{"key": {e.projKey}, "name": {"Duplicate"}}
	res = e.uiDoNoRedirect(t, http.MethodPost, "/projects", token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusConflict || !strings.Contains(body, "Project key already exists.") {
		t.Fatalf("duplicate code = %d body = %s", res.StatusCode, body)
	}

	key := uniqueProjectKey(t)
	form = url.Values{"key": {key}, "name": {"Created UI Project"}, "description": {"from UI"}}
	res = e.uiDoNoRedirect(t, http.MethodPost, "/projects", token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("create code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	loc := res.Header.Get("Location")
	if !strings.HasPrefix(loc, "/projects/") || !strings.HasSuffix(loc, "/sprint") {
		t.Fatalf("Location = %q", loc)
	}
	body = e.uiGet(t, loc, token)
	if !strings.Contains(body, "Created UI Project") || !strings.Contains(body, "from UI") {
		t.Fatalf("created project page missing values: %s", body)
	}
}

func TestUITokensPageCreatesAndRevokesToken(t *testing.T) {
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-tokens")

	body := e.uiGet(t, "/tokens", token)
	if !strings.Contains(body, "New API token") || !strings.Contains(body, "Tokens") {
		t.Fatalf("tokens page missing form/header: %s", body)
	}

	form := url.Values{"name": {"from ui"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, "/tokens", token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create token code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Copy this token now.") {
		t.Fatalf("body missing created token notice: %s", body)
	}
	tokens, err := e.store.ListAuthTokens(e.ctx, user.ID)
	if err != nil {
		t.Fatalf("ListAuthTokens: %v", err)
	}
	var created *model.AuthToken
	for i := range tokens {
		if tokens[i].Name == "from ui" {
			created = &tokens[i]
			break
		}
	}
	if created == nil {
		t.Fatalf("created token missing: %+v", tokens)
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, "/tokens/"+created.ID.String()+"/revoke", token, strings.NewReader(url.Values{}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("revoke code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	tokens, err = e.store.ListAuthTokens(e.ctx, user.ID)
	if err != nil {
		t.Fatalf("ListAuthTokens after revoke: %v", err)
	}
	for _, tok := range tokens {
		if tok.ID == created.ID && tok.RevokedAt == nil {
			t.Fatalf("token not revoked: %+v", tok)
		}
	}
}

func TestUISettingsPageUpdatesProfileAndPassword(t *testing.T) {
	e := newHTTPEnv(t)
	username := "uisettings" + strings.ToLower(uniqueProjectKey(t))
	oldPassword := "correct-horse-battery"
	newPassword := "new-correct-horse"
	user, err := e.store.CreateAccount(e.ctx, store.CreateAccountParams{
		Username: username,
		Password: oldPassword,
		Name:     "Old UI",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	token, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{
		UserID: user.ID,
		Kind:   model.AuthTokenKindSession,
		Name:   "session",
	})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	body := e.uiGet(t, "/settings", token.RawToken)
	for _, want := range []string{"Settings", "Display name", "Email", "Current password", "New password"} {
		if !strings.Contains(body, want) {
			t.Fatalf("settings body missing %q: %s", want, body)
		}
	}

	form := url.Values{"name": {"New UI"}, "email": {"ui@example.com"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, "/settings/profile", token.RawToken, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("profile code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Profile saved.") || !strings.Contains(body, "New UI") || !strings.Contains(body, "ui@example.com") {
		t.Fatalf("profile body missing saved values: %s", body)
	}

	form = url.Values{"current_password": {"wrong-password"}, "new_password": {newPassword}}
	res = e.uiDoNoRedirect(t, http.MethodPost, "/settings/password", token.RawToken, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Current password not accepted.") {
		t.Fatalf("bad password code = %d body = %s", res.StatusCode, body)
	}

	form = url.Values{"current_password": {oldPassword}, "new_password": {newPassword}}
	res = e.uiDoNoRedirect(t, http.MethodPost, "/settings/password", token.RawToken, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Password changed.") {
		t.Fatalf("password code = %d body = %s", res.StatusCode, body)
	}
	if _, err := e.store.AuthenticatePassword(e.ctx, username, oldPassword); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("old password err = %v, want ErrUnauthorized", err)
	}
	if _, err := e.store.AuthenticatePassword(e.ctx, username, newPassword); err != nil {
		t.Fatalf("new password auth: %v", err)
	}
}

func TestUIRendersPersonalWorkViews(t *testing.T) {
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-work")
	assigned, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "assigned to current user",
		AssigneeID: &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue assigned: %v", err)
	}
	if _, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "not assigned to current user"}); err != nil {
		t.Fatalf("CreateIssue unassigned: %v", err)
	}

	meBody := e.uiGet(t, "/me", token)
	if !strings.Contains(meBody, assigned.Title) {
		t.Fatalf("me body missing assigned issue: %s", meBody)
	}
	if strings.Contains(meBody, "not assigned to current user") {
		t.Fatalf("me body included unassigned issue: %s", meBody)
	}
}

func TestUIRendersProjectSprintBoard(t *testing.T) {
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-board")
	sp, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Board Sprint",
		StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}
	active := model.SprintStatusActive
	if _, err := e.store.UpdateSprint(e.ctx, sp.ID, store.UpdateSprintParams{Status: &active}); err != nil {
		t.Fatalf("UpdateSprint active: %v", err)
	}
	todo, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "board todo issue"})
	if err != nil {
		t.Fatalf("CreateIssue todo: %v", err)
	}
	inProgress, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "board progress issue"})
	if err != nil {
		t.Fatalf("CreateIssue progress: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, todo.ID, store.UpdateIssueParams{SprintID: &sp.ID}); err != nil {
		t.Fatalf("assign todo: %v", err)
	}
	inProgressStatus := model.StatusInProgress
	if _, err := e.store.UpdateIssue(e.ctx, inProgress.ID, store.UpdateIssueParams{SprintID: &sp.ID, Status: &inProgressStatus}); err != nil {
		t.Fatalf("assign progress: %v", err)
	}
	otherProject, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "Other UI Project", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	if _, err := e.store.GrantProjectAccess(e.ctx, otherProject.ID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess other: %v", err)
	}
	otherSprint, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: otherProject.ID,
		Name:      "Other Sprint",
		StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint other: %v", err)
	}
	if _, err := e.store.UpdateSprint(e.ctx, otherSprint.ID, store.UpdateSprintParams{Status: &active}); err != nil {
		t.Fatalf("UpdateSprint other active: %v", err)
	}
	otherIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: otherProject.ID, Title: "other project sprint issue"})
	if err != nil {
		t.Fatalf("CreateIssue other: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, otherIssue.ID, store.UpdateIssueParams{SprintID: &otherSprint.ID}); err != nil {
		t.Fatalf("assign other: %v", err)
	}

	body := e.uiGet(t, "/projects/"+e.projectID.String()+"/sprint", token)
	for _, want := range []string{"Sprint", "To do", "In progress", "Done", "board todo issue", "board progress issue", "Board Sprint"} {
		if !strings.Contains(body, want) {
			t.Fatalf("sprint body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "other project sprint issue") || strings.Contains(body, "Active sprint issues across accessible projects.") {
		t.Fatalf("sprint body included wrong scope/copy: %s", body)
	}
}

func TestUIRendersProjectBacklog(t *testing.T) {
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-backlog")
	backlogIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "issue still in backlog"})
	if err != nil {
		t.Fatalf("CreateIssue backlog: %v", err)
	}
	otherProject, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "Other Backlog Project", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	if _, err := e.store.GrantProjectAccess(e.ctx, otherProject.ID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess other: %v", err)
	}
	if _, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: otherProject.ID, Title: "other project backlog issue"}); err != nil {
		t.Fatalf("CreateIssue other backlog: %v", err)
	}

	body := e.uiGet(t, "/projects/"+e.projectID.String()+"/backlog", token)
	if !strings.Contains(body, "Backlog") || !strings.Contains(body, backlogIssue.Title) {
		t.Fatalf("backlog body missing expected issue/header: %s", body)
	}
	if strings.Contains(body, "other project backlog issue") || strings.Contains(body, "Backlog issues across accessible projects.") {
		t.Fatalf("backlog body included wrong scope/copy: %s", body)
	}
}

func TestUIProjectRoutesRedirectAndRejectOldGlobals(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-routes")

	res := e.uiDoNoRedirect(t, http.MethodGet, "/projects/"+e.projectID.String(), token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("project root code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/projects/"+e.projectID.String()+"/sprint" {
		t.Fatalf("project root Location = %q", loc)
	}

	for _, path := range []string{"/sprint", "/sprint/panel", "/backlog", "/backlog/panel"} {
		res := e.uiDoNoRedirect(t, http.MethodGet, path, token, nil)
		defer res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s code = %d body = %s", path, res.StatusCode, readBody(t, res))
		}
	}
}

func TestUIProjectChildRoutesRequireAccess(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustUserToken(t, "ui-no-project")

	for _, path := range []string{"/projects/" + e.projectID.String() + "/sprint", "/projects/" + e.projectID.String() + "/backlog"} {
		res := e.uiDoNoRedirect(t, http.MethodGet, path, token, nil)
		defer res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("%s code = %d body = %s", path, res.StatusCode, readBody(t, res))
		}
	}
}

func TestUIRendersProjectSprintEmptyState(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-empty")
	body := e.uiGet(t, "/projects/"+e.projectID.String()+"/sprint", token)
	if !strings.Contains(body, "No active sprint.") {
		t.Fatalf("body missing no-active-sprint state: %s", body)
	}
}

func TestUIProjectSprintDoesNotIncludeBacklog(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-sprint")
	sp, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Active UI Sprint",
		StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}
	active := model.SprintStatusActive
	if _, err := e.store.UpdateSprint(e.ctx, sp.ID, store.UpdateSprintParams{Status: &active}); err != nil {
		t.Fatalf("UpdateSprint active: %v", err)
	}
	inSprint, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "issue inside active sprint"})
	if err != nil {
		t.Fatalf("CreateIssue in sprint: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, inSprint.ID, store.UpdateIssueParams{SprintID: &sp.ID}); err != nil {
		t.Fatalf("assign issue: %v", err)
	}
	if _, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "issue still in backlog"}); err != nil {
		t.Fatalf("CreateIssue backlog: %v", err)
	}

	body := e.uiGet(t, "/projects/"+e.projectID.String()+"/sprint", token)
	if !strings.Contains(body, "issue inside active sprint") {
		t.Fatalf("sprint body missing sprint issue: %s", body)
	}
	if strings.Contains(body, "issue still in backlog") {
		t.Fatalf("sprint body included backlog issue: %s", body)
	}
}

func TestUILogoutClearsCookie(t *testing.T) {
	e := newHTTPEnv(t)
	res := e.uiDoNoRedirect(t, http.MethodPost, "/logout", e.authToken, nil)
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("code = %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/login" {
		t.Fatalf("Location = %q", loc)
	}
	setCookie := res.Header.Get("Set-Cookie")
	if !strings.Contains(setCookie, uiCookieNameForTest+"=") || !strings.Contains(setCookie, "Max-Age=0") {
		t.Fatalf("Set-Cookie = %q", setCookie)
	}
}

const uiCookieNameForTest = "track_slash_ui_token"

func (e *httpEnv) uiGet(t *testing.T, path, token string) string {
	t.Helper()
	res := e.uiDoNoRedirect(t, http.MethodGet, path, token, nil)
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s code = %d body = %s", path, res.StatusCode, body)
	}
	return body
}

func (e *httpEnv) uiDoNoRedirect(t *testing.T, method, path, token string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(e.ctx, method, e.ts.URL+path, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: uiCookieNameForTest, Value: token, Path: "/"})
	}
	client := *e.ts.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return res
}

func findUICookie(t *testing.T, cookies []*http.Cookie) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == uiCookieNameForTest {
			return cookie
		}
	}
	t.Fatalf("ui auth cookie not found: %v", cookies)
	return nil
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(data)
}

func TestUIHomeRedirectsToFirstProject(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-home")
	res := e.uiDoNoRedirect(t, http.MethodGet, "/", token, nil)
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("code = %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/projects/"+e.projectID.String()+"/sprint" {
		t.Fatalf("Location = %q", loc)
	}
}

func TestUIHomeRedirectsToProjectsWithoutAccessibleProject(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustUserToken(t, "ui-home-empty")
	res := e.uiDoNoRedirect(t, http.MethodGet, "/", token, nil)
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("code = %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/projects" {
		t.Fatalf("Location = %q", loc)
	}
}

func (e *httpEnv) mustProjectMemberToken(t *testing.T, label string) (model.User, string) {
	t.Helper()
	user, token := e.mustUserToken(t, label)
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	return user, token
}
