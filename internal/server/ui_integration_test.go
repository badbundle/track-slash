package server_test

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestUIRedirectsUnauthenticatedApp(t *testing.T) {
	e := newHTTPEnv(t)
	res := e.uiDoNoRedirect(t, http.MethodGet, "/app", "", nil)
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("code = %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); !strings.HasPrefix(loc, "/app/login?next=") {
		t.Fatalf("Location = %q", loc)
	}
}

func TestUILoginRejectsBadToken(t *testing.T) {
	e := newHTTPEnv(t)
	form := url.Values{"token": {"not-a-token"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, "/app/login", "", strings.NewReader(form.Encode()))
	defer res.Body.Close()

	body := readBody(t, res)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("code = %d body = %s", res.StatusCode, body)
	}
	if strings.Contains(res.Header.Get("Set-Cookie"), uiCookieNameForTest) {
		t.Fatalf("unexpected auth cookie: %s", res.Header.Get("Set-Cookie"))
	}
	if !strings.Contains(body, "Token not accepted.") {
		t.Fatalf("body missing login error: %s", body)
	}
}

func TestUILoginSetsCookie(t *testing.T) {
	e := newHTTPEnv(t)
	form := url.Values{"token": {e.authToken}, "next": {"/app/projects/" + e.projectID.String()}}
	res := e.uiDoNoRedirect(t, http.MethodPost, "/app/login", "", strings.NewReader(form.Encode()))
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("code = %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/app/projects/"+e.projectID.String() {
		t.Fatalf("Location = %q", loc)
	}
	cookie := findUICookie(t, res.Cookies())
	if !cookie.HttpOnly {
		t.Fatal("ui auth cookie is not HttpOnly")
	}
	if cookie.Path != "/app" {
		t.Fatalf("cookie Path = %q", cookie.Path)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie SameSite = %v", cookie.SameSite)
	}
}

func TestUIRendersProjectSidebarForVisibleProjects(t *testing.T) {
	e := newHTTPEnv(t)
	user, token := e.mustUserToken(t, "ui-member")
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	otherName := "ui-secret-" + uuid.NewString()
	if _, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), otherName, ""); err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}

	body := e.uiGet(t, "/app/projects/"+e.projectID.String(), token)
	if !strings.Contains(body, "http-test") {
		t.Fatalf("body missing visible project: %s", body)
	}
	if strings.Contains(body, otherName) {
		t.Fatalf("body leaked inaccessible project: %s", body)
	}
	if !strings.Contains(body, user.Name) {
		t.Fatalf("body missing current user: %s", body)
	}
}

func TestUIRendersCurrentSprintAndBacklogSeparately(t *testing.T) {
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

	body := e.uiGet(t, "/app/projects/"+e.projectID.String(), token)
	currentIdx := strings.Index(body, "Current sprint")
	inSprintIdx := strings.Index(body, "issue inside active sprint")
	backlogIdx := strings.Index(body, "Backlog")
	backlogIssueIdx := strings.Index(body, "issue still in backlog")
	if currentIdx < 0 || inSprintIdx < 0 || backlogIdx < 0 || backlogIssueIdx < 0 {
		t.Fatalf("body missing expected sections/issues: %s", body)
	}
	if !(currentIdx < inSprintIdx && inSprintIdx < backlogIdx && backlogIdx < backlogIssueIdx) {
		t.Fatalf("unexpected section ordering: current=%d inSprint=%d backlog=%d backlogIssue=%d", currentIdx, inSprintIdx, backlogIdx, backlogIssueIdx)
	}
}

func TestUIRendersNoActiveSprintState(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-empty")
	body := e.uiGet(t, "/app/projects/"+e.projectID.String(), token)
	if !strings.Contains(body, "No active sprint.") {
		t.Fatalf("body missing no-active-sprint state: %s", body)
	}
}

func TestUILogoutClearsCookie(t *testing.T) {
	e := newHTTPEnv(t)
	res := e.uiDoNoRedirect(t, http.MethodPost, "/app/logout", e.authToken, nil)
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("code = %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/app/login" {
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
		req.AddCookie(&http.Cookie{Name: uiCookieNameForTest, Value: token, Path: "/app"})
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
	res := e.uiDoNoRedirect(t, http.MethodGet, "/app", token, nil)
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("code = %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != fmt.Sprintf("/app/projects/%s", e.projectID) {
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
