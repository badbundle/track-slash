package server_test

import (
	"errors"
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
	next := e.projectPath() + "/about"
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
	for _, want := range []string{">Me<", ">Projects<", `href="/settings"`, `href="/tokens"`, `data-lucide="user"`, `data-lucide="folder"`, "data-nav-loader", "#sidebar-toggle:checked ~ .app-shell > aside", `track-slash.sidebar.collapsed`, `data-member-menu`, `overflow-visible border-r`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "#sidebar-toggle:checked ~ .app-shell aside { width") {
		t.Fatalf("sidebar collapse selector targets nested asides: %s", body)
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
	user, token := e.mustProjectMemberToken(t, "ui-projects")
	hidden, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "Hidden UI Project", "")
	if err != nil {
		t.Fatalf("CreateProject hidden: %v", err)
	}

	body := e.uiGet(t, "/projects", token)
	for _, want := range []string{"Projects", "Projects you can access.", "Create project", e.projKey, "http-test", "inline-flex w-fit justify-self-start", `href="` + e.projectPath() + `/about"`, `hx-get="` + e.projectPath() + `/about/panel"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("projects body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `href="`+e.projectPath()+`/sprint"`) {
		t.Fatalf("projects body included sprint row action: %s", body)
	}
	if strings.Contains(body, `href="`+e.projectPath()+`/backlog"`) {
		t.Fatalf("projects body included backlog row action: %s", body)
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

	dupKey := uniqueProjectKey(t)
	if _, err := e.store.CreateProjectForUser(e.ctx, user.ID, dupKey, "Duplicate Source", ""); err != nil {
		t.Fatalf("CreateProjectForUser duplicate source: %v", err)
	}
	form = url.Values{"key": {dupKey}, "name": {"Duplicate"}}
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
	if loc != "/"+user.Username+"/projects/"+key+"/about" {
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
	parent, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "parent with child"})
	if err != nil {
		t.Fatalf("CreateIssue parent: %v", err)
	}
	child, err := e.store.CreateSubIssue(e.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "assigned child issue",
		AssigneeID:    &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateSubIssue assigned child: %v", err)
	}

	meBody := e.uiGet(t, "/me", token)
	if !strings.Contains(meBody, assigned.Title) {
		t.Fatalf("me body missing assigned issue: %s", meBody)
	}
	if !strings.Contains(meBody, child.Title) {
		t.Fatalf("me body missing assigned sub-issue: %s", meBody)
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

	body := e.uiGet(t, e.projectPath()+"/sprint", token)
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
	firstPlanned, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "First Planned Sprint",
		StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint first planned: %v", err)
	}
	secondPlanned, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Second Planned Sprint",
		StartDate: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint second planned: %v", err)
	}
	if _, err := e.store.ReorderPlannedSprints(e.ctx, store.ReorderPlannedSprintsParams{
		ProjectID: e.projectID,
		SprintIDs: []uuid.UUID{secondPlanned.ID, firstPlanned.ID},
	}); err != nil {
		t.Fatalf("ReorderPlannedSprints: %v", err)
	}
	firstPlannedIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "scheduled first issue"})
	if err != nil {
		t.Fatalf("CreateIssue first planned: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, firstPlannedIssue.ID, store.UpdateIssueParams{SprintID: &firstPlanned.ID}); err != nil {
		t.Fatalf("assign first planned: %v", err)
	}
	secondPlannedIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "scheduled second issue"})
	if err != nil {
		t.Fatalf("CreateIssue second planned: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, secondPlannedIssue.ID, store.UpdateIssueParams{SprintID: &secondPlanned.ID}); err != nil {
		t.Fatalf("assign second planned: %v", err)
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
	otherPlanned, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: otherProject.ID,
		Name:      "Other Planned Sprint",
		StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint other planned: %v", err)
	}
	otherPlannedIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: otherProject.ID, Title: "other project planned issue"})
	if err != nil {
		t.Fatalf("CreateIssue other planned: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, otherPlannedIssue.ID, store.UpdateIssueParams{SprintID: &otherPlanned.ID}); err != nil {
		t.Fatalf("assign other planned: %v", err)
	}

	body := e.uiGet(t, e.projectPath()+"/backlog", token)
	for _, want := range []string{"Planned sprints", "Second Planned Sprint", "First Planned Sprint", "scheduled second issue", "scheduled first issue", "Backlog", backlogIssue.Title} {
		if !strings.Contains(body, want) {
			t.Fatalf("backlog body missing %q: %s", want, body)
		}
	}
	secondIdx := strings.Index(body, "scheduled second issue")
	firstIdx := strings.Index(body, "scheduled first issue")
	backlogIdx := strings.Index(body, backlogIssue.Title)
	if secondIdx < 0 || firstIdx < 0 || backlogIdx < 0 || secondIdx > firstIdx || firstIdx > backlogIdx {
		t.Fatalf("planned/backlog order wrong: second=%d first=%d backlog=%d body=%s", secondIdx, firstIdx, backlogIdx, body)
	}
	if !strings.Contains(body, "Backlog") || !strings.Contains(body, backlogIssue.Title) {
		t.Fatalf("backlog body missing expected issue/header: %s", body)
	}
	if strings.Contains(body, "other project backlog issue") || strings.Contains(body, "other project planned issue") || strings.Contains(body, "Other Planned Sprint") || strings.Contains(body, "Backlog issues across accessible projects.") {
		t.Fatalf("backlog body included wrong scope/copy: %s", body)
	}
}

func TestUIRendersIssueDetailPage(t *testing.T) {
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-issue")
	reporterID := user.ID
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:   e.projectID,
		Title:       "detail page issue",
		Description: "read only body",
		AssigneeID:  &user.ID,
		ReporterID:  &reporterID,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	sp, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Detail Planned Sprint",
		StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, issue.ID, store.UpdateIssueParams{SprintID: &sp.ID}); err != nil {
		t.Fatalf("assign sprint: %v", err)
	}
	linked, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "linked detail issue"})
	if err != nil {
		t.Fatalf("CreateIssue linked: %v", err)
	}
	if _, err := e.store.CreateIssueLink(e.ctx, store.CreateIssueLinkParams{
		SourceID: issue.ID,
		TargetID: linked.ID,
		LinkType: model.LinkTypeBlocks,
	}); err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}
	if _, err := e.store.CreateComment(e.ctx, store.CreateCommentParams{
		IssueID:  issue.ID,
		AuthorID: user.ID,
		Body:     "detail comment body",
	}); err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	subIssue, err := e.store.CreateSubIssue(e.ctx, store.CreateSubIssueParams{
		ParentIssueID: issue.ID,
		Title:         "detail child issue",
		AssigneeID:    &user.ID,
		ReporterID:    &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateSubIssue: %v", err)
	}
	otherProject, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "Other Detail Project", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	if _, err := e.store.GrantProjectAccess(e.ctx, otherProject.ID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess other: %v", err)
	}
	otherIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: otherProject.ID, Title: "unrelated detail issue"})
	if err != nil {
		t.Fatalf("CreateIssue other: %v", err)
	}
	if _, err := e.store.CreateComment(e.ctx, store.CreateCommentParams{
		IssueID:  otherIssue.ID,
		AuthorID: user.ID,
		Body:     "unrelated comment body",
	}); err != nil {
		t.Fatalf("CreateComment other: %v", err)
	}

	body := e.uiGet(t, e.issuePath(issue), token)
	for _, want := range []string{
		"detail page issue",
		"read only body",
		"Detail Planned Sprint",
		user.Name,
		"Blocks",
		"linked detail issue",
		"Sub-issues",
		"detail child issue",
		"detail comment body",
		`aria-label="Issue settings"`,
		`aria-label="Edit description"`,
		`hx-get="` + e.issuePath(issue) + `/description/edit"`,
		`aria-label="Edit link"`,
		`aria-label="Edit comment"`,
		`aria-label="Change status"`,
		`aria-expanded="false"`,
		`hx-get="` + e.issuePath(issue) + `/status/edit"`,
		`aria-label="Edit assignee"`,
		`aria-label="Edit reporter"`,
		`aria-label="Edit sprint"`,
		`aria-label="Add link"`,
		`aria-label="Create sub-issue"`,
		`aria-label="Post comment"`,
		`aria-haspopup="listbox"`,
		`data-lucide="chevron-down"`,
		`placeholder="Add a comment"`,
		`method="post" action="` + e.issueCommentsPath(issue) + `"`,
		`hx-post="` + e.issueCommentsPath(issue) + `"`,
		`data-submit-shortcut="meta-enter"`,
		"disabled",
		`href="` + e.projectPath() + `/backlog"`,
		`hx-get="` + e.projectPath() + `/backlog/panel"`,
		`href="` + e.issuePath(linked) + `"`,
		`hx-get="` + e.issuePath(linked) + `/panel"`,
		`href="` + e.issuePath(subIssue) + `"`,
		`hx-get="` + e.issuePath(subIssue) + `/panel"`,
		`method="post" action="` + e.issueSubIssuesPath(issue) + `"`,
		`hx-post="` + e.issueSubIssuesPath(issue) + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("issue body missing %q: %s", want, body)
		}
	}
	titleHeaderEnd := strings.Index(body, "</header>")
	if titleHeaderEnd < 0 {
		t.Fatalf("issue body missing title header: %s", body)
	}
	titleHeader := body[:titleHeaderEnd]
	for _, notWant := range []string{"Edit issue", "Change status", "Edit description", "Edit status", "To do", "In progress", "Done"} {
		if strings.Contains(titleHeader, notWant) {
			t.Fatalf("title card still contains section action/status %q: %s", notWant, body)
		}
	}
	for _, notWant := range []string{
		`title="Issue settings"`,
		`title="Edit description"`,
		`title="Add link"`,
		`title="Edit link"`,
		`title="Edit comment"`,
		`title="Change status"`,
		`title="Edit status"`,
		`title="Edit assignee"`,
		`title="Edit reporter"`,
		`title="Edit sprint"`,
	} {
		if strings.Contains(body, notWant) {
			t.Fatalf("issue body still renders native title tooltip %q: %s", notWant, body)
		}
	}
	for _, notWant := range []string{`aria-label="Edit status"`, ">Status</dt>"} {
		if strings.Contains(body, notWant) {
			t.Fatalf("issue body still renders separate status edit affordance %q: %s", notWant, body)
		}
	}
	for _, notWant := range []string{`<textarea disabled`, `aria-label="Post comment" class="grid h-9 w-9 shrink-0 cursor-not-allowed`, "\n            Comment\n"} {
		if strings.Contains(body, notWant) {
			t.Fatalf("issue body renders disabled or text-labeled composer %q: %s", notWant, body)
		}
	}
	if strings.Contains(body, `name="description"`) || strings.Contains(body, `placeholder="Description"`) {
		t.Fatalf("sub-issue composer should be title-only: %s", body)
	}
	if strings.Contains(body, `aria-label="Save description"`) || strings.Contains(body, `<textarea name="description"`) {
		t.Fatalf("issue detail should not start in description edit mode: %s", body)
	}
	if strings.Contains(body, `role="listbox" aria-label="Issue status"`) || strings.Contains(body, `aria-label="Cancel status change"`) {
		t.Fatalf("issue detail should not start in status edit mode: %s", body)
	}
	for _, notWant := range []string{"unrelated detail issue", "unrelated comment body", "Other Detail Project"} {
		if strings.Contains(body, notWant) {
			t.Fatalf("issue body included unrelated content %q: %s", notWant, body)
		}
	}

	panel := e.uiGet(t, e.issuePath(issue)+"/panel", token)
	if strings.Contains(panel, "<!doctype html>") {
		t.Fatalf("panel returned shell: %s", panel)
	}
	if !strings.Contains(panel, "detail page issue") || !strings.Contains(panel, "detail comment body") {
		t.Fatalf("panel missing issue context: %s", panel)
	}
}

func TestUIEditStatusUpdatesIssuePanel(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-status")
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "status target issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	edit := e.uiGet(t, e.issuePath(issue)+"/status/edit", token)
	for _, want := range []string{
		"status target issue",
		`aria-label="Change status"`,
		`aria-expanded="true"`,
		`role="listbox" aria-label="Issue status"`,
		`method="post" action="` + e.issuePath(issue) + `/status"`,
		`hx-post="` + e.issuePath(issue) + `/status"`,
		`hx-push-url="` + e.issuePath(issue) + `"`,
		`name="status" value="todo"`,
		`name="status" value="in_progress"`,
		`name="status" value="done"`,
		`aria-label="Cancel status change"`,
		`hx-get="` + e.issuePath(issue) + `/panel"`,
		"To do",
		"In progress",
		"Done",
	} {
		if !strings.Contains(edit, want) {
			t.Fatalf("status edit response missing %q: %s", want, edit)
		}
	}
	if strings.Contains(edit, `title="Change status"`) || strings.Contains(edit, `title="Cancel status change"`) {
		t.Fatalf("status edit response has native tooltip state: %s", edit)
	}

	form := url.Values{"status": {string(model.StatusInProgress)}}
	res := e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/status", token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update status code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "In progress") || strings.Contains(body, `role="option"`) {
		t.Fatalf("update status response did not return read mode with new status: %s", body)
	}
	updated, err := e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after status update: %v", err)
	}
	if updated.Status != model.StatusInProgress {
		t.Fatalf("Status = %q, want %q", updated.Status, model.StatusInProgress)
	}

	badStatus := url.Values{"status": {"blocked"}}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/status", token, strings.NewReader(badStatus.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad status code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "invalid status") {
		t.Fatalf("bad status response missing validation error: %s", body)
	}
	updated, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after bad status: %v", err)
	}
	if updated.Status != model.StatusInProgress {
		t.Fatalf("bad status changed Status = %q, want %q", updated.Status, model.StatusInProgress)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/status", token, strings.NewReader("%zz"))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad form status code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "unable to read form") {
		t.Fatalf("bad form status response missing parse error: %s", body)
	}
	updated, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after bad form status: %v", err)
	}
	if updated.Status != model.StatusInProgress {
		t.Fatalf("bad form changed Status = %q, want %q", updated.Status, model.StatusInProgress)
	}
}

func TestUIEditDescriptionUpdatesAndClearsIssuePanel(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-description")
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:   e.projectID,
		Title:       "description target issue",
		Description: "old description",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	edit := e.uiGet(t, e.issuePath(issue)+"/description/edit", token)
	for _, want := range []string{
		"description target issue",
		`method="post" action="` + e.issuePath(issue) + `/description"`,
		`hx-post="` + e.issuePath(issue) + `/description"`,
		`hx-push-url="` + e.issuePath(issue) + `"`,
		`name="description"`,
		`placeholder="Description"`,
		`aria-label="Save description"`,
		`aria-label="Cancel editing description"`,
		`hx-get="` + e.issuePath(issue) + `/panel"`,
		"old description",
	} {
		if !strings.Contains(edit, want) {
			t.Fatalf("description edit response missing %q: %s", want, edit)
		}
	}
	if strings.Contains(edit, `<textarea disabled`) || strings.Contains(edit, `title="Save description"`) {
		t.Fatalf("description edit response has disabled/editor tooltip state: %s", edit)
	}

	form := url.Values{"description": {"new description"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/description", token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update description code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "new description") || strings.Contains(body, "old description") || strings.Contains(body, `name="description"`) {
		t.Fatalf("update description response did not return read mode with new body: %s", body)
	}
	updated, err := e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after update: %v", err)
	}
	if updated.Description != "new description" {
		t.Fatalf("Description = %q, want new description", updated.Description)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/description", token, strings.NewReader("%zz"))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad form description code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "unable to read form") {
		t.Fatalf("bad form description response missing parse error: %s", body)
	}
	updated, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after bad form: %v", err)
	}
	if updated.Description != "new description" {
		t.Fatalf("bad form changed Description = %q, want new description", updated.Description)
	}

	blank := url.Values{"description": {" \n\t "}}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/description", token, strings.NewReader(blank.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("blank description code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "No description.") || strings.Contains(body, "new description") {
		t.Fatalf("blank description response missing empty state: %s", body)
	}
	updated, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after blank update: %v", err)
	}
	if updated.Description != "" {
		t.Fatalf("blank Description = %q, want empty string", updated.Description)
	}
}

func TestUICreateCommentPostsAndRerendersIssuePanel(t *testing.T) {
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-comment")
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "comment target issue"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	form := url.Values{"body": {"new ui comment"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, e.issueCommentsPath(issue), token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("code = %d body = %s", res.StatusCode, body)
	}
	for _, want := range []string{"comment target issue", "new ui comment", `placeholder="Add a comment"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("comment post response missing %q: %s", want, body)
		}
	}
	comments, _, err := e.store.ListCommentsForIssue(e.ctx, store.ListCommentsForIssueParams{IssueID: issue.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListCommentsForIssue: %v", err)
	}
	if len(comments) != 1 || comments[0].Body != "new ui comment" || comments[0].AuthorID != user.ID {
		t.Fatalf("comments = %+v, want one new comment by %s", comments, user.ID)
	}

	empty := url.Values{"body": {"   "}}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueCommentsPath(issue), token, strings.NewReader(empty.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("empty code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Comment required, max 10000 chars.") {
		t.Fatalf("empty comment response missing validation error: %s", body)
	}
	comments, _, err = e.store.ListCommentsForIssue(e.ctx, store.ListCommentsForIssueParams{IssueID: issue.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListCommentsForIssue after validation: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("empty comment should not create a row, comments = %+v", comments)
	}
}

func TestUICreateSubIssuePostsTitleOnlyAndRerendersIssuePanel(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-sub-issue")
	parent, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "parent target issue"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	form := url.Values{"title": {"new child from ui"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, e.issueSubIssuesPath(parent), token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("code = %d body = %s", res.StatusCode, body)
	}
	for _, want := range []string{"parent target issue", "Sub-issues", "new child from ui", `aria-label="Create sub-issue"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("sub-issue post response missing %q: %s", want, body)
		}
	}
	children, _, err := e.store.ListSubIssuesForIssue(e.ctx, store.ListSubIssuesForIssueParams{
		ParentIssueID: parent.ID,
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("ListSubIssuesForIssue: %v", err)
	}
	if len(children) != 1 || children[0].Title != "new child from ui" || children[0].Description != "" {
		t.Fatalf("children = %+v, want one title-only child", children)
	}

	empty := url.Values{"title": {"   "}}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueSubIssuesPath(parent), token, strings.NewReader(empty.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("empty code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Title required, max 200 chars.") {
		t.Fatalf("empty sub-issue response missing validation error: %s", body)
	}
}

func TestUIRendersSubIssueDetailWithParentBacklinkAndNoSprintControls(t *testing.T) {
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-sub-detail")
	parent, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "parent detail issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue parent: %v", err)
	}
	child, err := e.store.CreateSubIssue(e.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "child detail issue",
		Description:   "child detail body",
		AssigneeID:    &user.ID,
		ReporterID:    &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateSubIssue: %v", err)
	}

	body := e.uiGet(t, e.issuePath(child), token)
	for _, want := range []string{
		"child detail issue",
		"child detail body",
		"Sub-issue of",
		"Parent",
		"parent detail issue",
		`href="` + e.issuePath(parent) + `"`,
		`hx-get="` + e.issuePath(parent) + `/panel"`,
		`data-lucide="corner-up-left"`,
		user.Name,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("sub-issue detail missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{
		"Sub-issues",
		`action="` + e.issueSubIssuesPath(child) + `"`,
		`aria-label="Create sub-issue"`,
		`aria-label="Edit sprint"`,
		">Sprint</dt>",
	} {
		if strings.Contains(body, notWant) {
			t.Fatalf("sub-issue detail included %q: %s", notWant, body)
		}
	}
}

func TestUIIssueRoutesRequireAccessAndPreserveLoginNext(t *testing.T) {
	e := newHTTPEnv(t)
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "protected issue"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	_, token := e.mustUserToken(t, "ui-issue-denied")
	res := e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue), token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue detail code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	form := url.Values{"body": {"denied comment"}}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueCommentsPath(issue), token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue comment code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/description/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue description edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/description", token, strings.NewReader(url.Values{"description": {"denied"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue description update code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/status/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue status edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/status", token, strings.NewReader(url.Values{"status": {string(model.StatusDone)}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue status update code = %d body = %s", res.StatusCode, readBody(t, res))
	}

	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue), "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauth issue code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/login?next="+url.QueryEscape(e.issuePath(issue)) {
		t.Fatalf("Location = %q", loc)
	}

	_, memberToken := e.mustProjectMemberToken(t, "ui-issue-bad-id")
	for _, tc := range []struct {
		method string
		path   string
		body   io.Reader
	}{
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref"},
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref/panel"},
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref/description/edit"},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/description", body: strings.NewReader(url.Values{"description": {"hello"}}.Encode())},
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref/status/edit"},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/status", body: strings.NewReader(url.Values{"status": {string(model.StatusDone)}}.Encode())},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/comments", body: strings.NewReader(url.Values{"body": {"hello"}}.Encode())},
	} {
		res := e.uiDoNoRedirect(t, tc.method, tc.path, memberToken, tc.body)
		defer res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s %s code = %d body = %s", tc.method, tc.path, res.StatusCode, readBody(t, res))
		}
	}
}

func TestUIIssueListsLinkToIssueDetail(t *testing.T) {
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-issue-links")
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "linked from lists",
		AssigneeID: &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	wantHref := `href="` + e.issuePath(issue) + `"`
	wantHXGet := `hx-get="` + e.issuePath(issue) + `/panel"`
	wantHXPush := `hx-push-url="` + e.issuePath(issue) + `"`

	for _, path := range []string{e.projectPath() + "/backlog", "/me"} {
		body := e.uiGet(t, path, token)
		for _, want := range []string{wantHref, wantHXGet, wantHXPush, `data-main-view="projects"`} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing issue link %q: %s", path, want, body)
			}
		}
	}
}

func TestUIProjectRoutesRedirectAndRejectOldGlobals(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-routes")

	res := e.uiDoNoRedirect(t, http.MethodGet, e.projectPath(), token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("project root code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != e.projectPath()+"/about" {
		t.Fatalf("project root Location = %q", loc)
	}

	res = e.uiDoNoRedirect(t, http.MethodGet, "/projects/"+e.projectID.String(), token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("old project UUID route code = %d body = %s", res.StatusCode, readBody(t, res))
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

	for _, path := range []string{e.projectPath() + "/about", e.projectPath() + "/sprint", e.projectPath() + "/backlog"} {
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
	body := e.uiGet(t, e.projectPath()+"/sprint", token)
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

	body := e.uiGet(t, e.projectPath()+"/sprint", token)
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
	if loc := res.Header.Get("Location"); loc != e.projectPath()+"/about" {
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
