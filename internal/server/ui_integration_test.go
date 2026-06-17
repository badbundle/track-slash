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
	for _, want := range []string{"Projects", "Projects you can access.", "Create project", e.projKey, "http-test", "inline-flex w-fit justify-self-start", `href="` + e.projectPath() + `/sprint"`, `hx-get="` + e.projectPath() + `/sprint/panel"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("projects body missing %q: %s", want, body)
		}
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
	if loc != "/"+user.Username+"/projects/"+key+"/sprint" {
		t.Fatalf("Location = %q", loc)
	}
	body = e.uiGet(t, loc, token)
	if !strings.Contains(body, "Created UI Project") {
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
		Goal:      "Focus current sprint goals\nShip board clarity",
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
	for _, want := range []string{"Sprint", "To do", "In progress", "Done", "board todo issue", "board progress issue", "Board Sprint", "Focus current sprint goals\nShip board clarity", `aria-label="Assignee filter"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("sprint body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "other project sprint issue") || strings.Contains(body, "Active sprint issues across accessible projects.") {
		t.Fatalf("sprint body included wrong scope/copy: %s", body)
	}
}

func TestUIProjectAssigneeFilterAppliesAcrossProjectSections(t *testing.T) {
	e := newHTTPEnv(t)
	alice, token := e.mustProjectMemberToken(t, "ui-filter-alice")
	bob, _ := e.mustProjectMemberToken(t, "ui-filter-bob")
	var err error
	alice, err = e.store.UpdateUserProfile(e.ctx, alice.ID, "Alice Filter", alice.Email)
	if err != nil {
		t.Fatalf("UpdateUserProfile alice: %v", err)
	}
	bob, err = e.store.UpdateUserProfile(e.ctx, bob.ID, "Bob Filter", bob.Email)
	if err != nil {
		t.Fatalf("UpdateUserProfile bob: %v", err)
	}
	activeSprint, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Filtered Active Sprint",
		StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint active: %v", err)
	}
	active := model.SprintStatusActive
	if _, err := e.store.UpdateSprint(e.ctx, activeSprint.ID, store.UpdateSprintParams{Status: &active}); err != nil {
		t.Fatalf("UpdateSprint active: %v", err)
	}
	plannedSprint, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Filtered Planned Sprint",
		StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint planned: %v", err)
	}
	aliceSprint := createAssignedIssueForUI(t, e, "alice sprint issue", alice.ID)
	bobSprint := createAssignedIssueForUI(t, e, "bob sprint issue", bob.ID)
	unassignedSprint, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "unassigned sprint issue"})
	if err != nil {
		t.Fatalf("CreateIssue unassigned sprint: %v", err)
	}
	for _, issue := range []model.Issue{aliceSprint, bobSprint, unassignedSprint} {
		if _, err := e.store.UpdateIssue(e.ctx, issue.ID, store.UpdateIssueParams{SprintID: &activeSprint.ID}); err != nil {
			t.Fatalf("assign active %s: %v", issue.Identifier, err)
		}
	}
	alicePlanned := createAssignedIssueForUI(t, e, "alice planned issue", alice.ID)
	bobPlanned := createAssignedIssueForUI(t, e, "bob planned issue", bob.ID)
	for _, issue := range []model.Issue{alicePlanned, bobPlanned} {
		if _, err := e.store.UpdateIssue(e.ctx, issue.ID, store.UpdateIssueParams{SprintID: &plannedSprint.ID}); err != nil {
			t.Fatalf("assign planned %s: %v", issue.Identifier, err)
		}
	}
	aliceBacklog := createAssignedIssueForUI(t, e, "alice backlog issue", alice.ID)
	bobBacklog := createAssignedIssueForUI(t, e, "bob backlog issue", bob.ID)

	aliceQuery := "?assignee_id=" + alice.ID.String()
	body := e.uiGet(t, e.projectPath()+"/sprint"+aliceQuery, token)
	for _, want := range []string{
		`aria-label="Assignee filter"`,
		`aria-label="Toggle Alice Filter"`,
		`aria-label="Toggle Bob Filter"`,
		`aria-pressed="true"`,
		"AF",
		"BF",
		"alice sprint issue",
		`href="` + e.projectPath() + `/backlog?assignee_id=` + alice.ID.String() + `"`,
		`assignee_id=` + bob.ID.String(),
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("filtered sprint missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{"bob sprint issue", "unassigned sprint issue"} {
		if strings.Contains(body, notWant) {
			t.Fatalf("filtered sprint included %q: %s", notWant, body)
		}
	}

	body = e.uiGet(t, e.projectPath()+"/backlog"+aliceQuery, token)
	for _, want := range []string{"alice planned issue", "alice backlog issue"} {
		if !strings.Contains(body, want) {
			t.Fatalf("filtered backlog missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{"bob planned issue", "bob backlog issue"} {
		if strings.Contains(body, notWant) {
			t.Fatalf("filtered backlog included %q: %s", notWant, body)
		}
	}

	body = e.uiGet(t, e.projectPath()+"/sprint"+aliceQuery+"&assignee_id="+bob.ID.String(), token)
	for _, want := range []string{"alice sprint issue", "bob sprint issue"} {
		if !strings.Contains(body, want) {
			t.Fatalf("multi-filter sprint missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "unassigned sprint issue") || aliceBacklog.ID == uuid.Nil || bobBacklog.ID == uuid.Nil {
		t.Fatalf("multi-filter sprint included wrong issue or setup failed: %s", body)
	}
}

func createAssignedIssueForUI(t *testing.T, e *httpEnv, title string, assigneeID uuid.UUID) model.Issue {
	t.Helper()
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      title,
		AssigneeID: &assigneeID,
	})
	if err != nil {
		t.Fatalf("CreateIssue %s: %v", title, err)
	}
	return issue
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
	link, err := e.store.CreateIssueLink(e.ctx, store.CreateIssueLinkParams{
		SourceID: issue.ID,
		TargetID: linked.ID,
		LinkType: model.LinkTypeBlocks,
	})
	if err != nil {
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
		`hx-get="` + e.issuePath(issue) + `/assignee/edit"`,
		`aria-label="Edit reporter"`,
		`hx-get="` + e.issuePath(issue) + `/reporter/edit"`,
		`aria-label="Edit sprint"`,
		`hx-get="` + e.issuePath(issue) + `/sprint/edit"`,
		`aria-label="Add link"`,
		`hx-get="` + e.issueLinksPath(issue) + `/new"`,
		`aria-label="Add sub-issue"`,
		`hx-get="` + e.issueSubIssuesPath(issue) + `/new"`,
		`hx-get="` + e.issueLinksPath(issue) + `/` + link.Ref + `/edit"`,
		`aria-label="Post comment"`,
		`aria-haspopup="listbox"`,
		`data-lucide="chevron-down"`,
		`placeholder="Add a comment"`,
		`method="post" action="` + e.issueCommentsPath(issue) + `"`,
		`hx-post="` + e.issueCommentsPath(issue) + `"`,
		`data-submit-shortcut="meta-enter"`,
		"disabled",
		`method="post" action="` + e.issuePath(issue) + `/delete"`,
		`hx-post="` + e.issuePath(issue) + `/delete"`,
		`hx-push-url="` + e.projectPath() + `/backlog"`,
		`hx-confirm="Delete this issue? You can undo it from the next screen."`,
		`Delete issue`,
		`data-lucide="trash-2"`,
		`text-rose-600`,
		`href="` + e.projectPath() + `/backlog"`,
		`hx-get="` + e.projectPath() + `/backlog/panel"`,
		`href="` + e.issuePath(linked) + `"`,
		`hx-get="` + e.issuePath(linked) + `/panel"`,
		`href="` + e.issuePath(subIssue) + `"`,
		`hx-get="` + e.issuePath(subIssue) + `/panel"`,
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
	for _, notWant := range []string{`/archive`, `Archive issue`, `data-lucide="archive"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("issue body included removed archive control %q: %s", notWant, body)
		}
	}
	for _, notWant := range []string{`<textarea disabled`, `aria-label="Post comment" class="grid h-9 w-9 shrink-0 cursor-not-allowed`, "\n            Comment\n"} {
		if strings.Contains(body, notWant) {
			t.Fatalf("issue body renders disabled or text-labeled composer %q: %s", notWant, body)
		}
	}
	for _, notWant := range []string{
		`method="post" action="` + e.issueSubIssuesPath(issue) + `"`,
		`hx-post="` + e.issueSubIssuesPath(issue) + `"`,
		`aria-label="Create sub-issue"`,
		`aria-label="Cancel adding sub-issue"`,
	} {
		if strings.Contains(body, notWant) {
			t.Fatalf("issue body should not render sub-issue composer by default %q: %s", notWant, body)
		}
	}
	if strings.Contains(body, `name="description"`) ||
		strings.Contains(body, `placeholder="Description"`) ||
		strings.Contains(body, `name="priority"`) ||
		strings.Contains(body, `aria-label="Sub-issue priority"`) {
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

func TestUIDeleteIssueReturnsBackTarget(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-delete")
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "delete target issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	res := e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/delete", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	backlogNotice := e.projectPath() + "/backlog?deleted_issue=" + url.QueryEscape(issue.Identifier)
	if loc := res.Header.Get("Location"); loc != backlogNotice {
		t.Fatalf("delete Location = %q", loc)
	}
	if _, err := e.store.GetIssue(e.ctx, issue.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetIssue deleted err = %v, want ErrNotFound", err)
	}
	body := e.uiGet(t, backlogNotice, token)
	for _, want := range []string{
		"Issue deleted",
		"delete target issue",
		"Undo delete",
		`hx-post="` + e.issuePath(issue) + `/restore"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("delete notice missing %q: %s", want, body)
		}
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/restore", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("restore code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != e.issuePath(issue) {
		t.Fatalf("restore Location = %q", loc)
	}
	if _, err := e.store.GetIssue(e.ctx, issue.ID); err != nil {
		t.Fatalf("GetIssue restored: %v", err)
	}

	parent, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "delete child parent",
	})
	if err != nil {
		t.Fatalf("CreateIssue parent: %v", err)
	}
	child, err := e.store.CreateSubIssue(e.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "delete child target",
	})
	if err != nil {
		t.Fatalf("CreateSubIssue child: %v", err)
	}
	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.issuePath(child)+"/delete", token, nil, map[string]string{
		"HX-Request": "true",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("htmx delete code = %d body = %s", res.StatusCode, body)
	}
	parentNotice := e.issuePath(parent) + "?deleted_issue=" + url.QueryEscape(child.Identifier)
	if push := res.Header.Get("HX-Push-Url"); push != parentNotice {
		t.Fatalf("HX-Push-Url = %q", push)
	}
	for _, want := range []string{
		"delete child parent",
		"Issue deleted",
		"delete child target",
		"Undo delete",
		`hx-post="` + e.issuePath(child) + `/restore"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("htmx delete response missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "<!doctype html>") {
		t.Fatalf("htmx delete response should render parent issue panel: %s", body)
	}
	if _, err := e.store.GetIssue(e.ctx, child.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetIssue child err = %v, want ErrNotFound", err)
	}
	if _, err := e.store.GetIssue(e.ctx, parent.ID); err != nil {
		t.Fatalf("GetIssue parent after child delete: %v", err)
	}
	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.issuePath(child)+"/restore", token, nil, map[string]string{
		"HX-Request": "true",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("htmx restore code = %d body = %s", res.StatusCode, body)
	}
	if push := res.Header.Get("HX-Push-Url"); push != e.issuePath(child) {
		t.Fatalf("restore HX-Push-Url = %q", push)
	}
	if strings.Contains(body, "<!doctype html>") || strings.Contains(body, "Issue deleted") || !strings.Contains(body, "delete child target") {
		t.Fatalf("htmx restore response should render child issue panel: %s", body)
	}
	if _, err := e.store.GetIssue(e.ctx, child.ID); err != nil {
		t.Fatalf("GetIssue child restored: %v", err)
	}
}

func TestUIOpenDeletedIssueShowsRestorePanel(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-deleted-open")
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:   e.projectID,
		Title:       "deleted open target",
		Description: "deleted description should stay hidden",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := e.store.DeleteIssue(e.ctx, issue.ID); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}

	res := e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue), token, nil)
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("deleted issue page code = %d body = %s", res.StatusCode, body)
	}
	for _, want := range []string{
		"<!doctype html>",
		"This issue has been deleted",
		"deleted open target",
		`href="` + e.projectPath() + `/deleted"`,
		`hx-get="` + e.projectPath() + `/deleted/panel"`,
		`method="post" action="` + e.issuePath(issue) + `/restore"`,
		`hx-post="` + e.issuePath(issue) + `/restore"`,
		`hx-push-url="` + e.issuePath(issue) + `"`,
		`data-lucide="rotate-ccw"`,
		"Restore issue",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("deleted issue page missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{"not found", "deleted description should stay hidden", "Comments", "Sub-issues", `aria-label="Issue settings"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("deleted issue page leaked full/error UI %q: %s", notWant, body)
		}
	}

	res = e.uiDoNoRedirectWithHeaders(t, http.MethodGet, e.issuePath(issue)+"/panel", token, nil, map[string]string{
		"HX-Request": "true",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("deleted issue panel code = %d body = %s", res.StatusCode, body)
	}
	if strings.Contains(body, "<!doctype html>") || !strings.Contains(body, "This issue has been deleted") || !strings.Contains(body, "deleted open target") {
		t.Fatalf("deleted issue panel should render partial tombstone: %s", body)
	}

	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.issuePath(issue)+"/restore", token, nil, map[string]string{
		"HX-Request": "true",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("restore from deleted issue code = %d body = %s", res.StatusCode, body)
	}
	if push := res.Header.Get("HX-Push-Url"); push != e.issuePath(issue) {
		t.Fatalf("restore HX-Push-Url = %q", push)
	}
	if strings.Contains(body, "This issue has been deleted") || !strings.Contains(body, "deleted open target") || !strings.Contains(body, "deleted description should stay hidden") {
		t.Fatalf("restore should render live issue panel: %s", body)
	}
}

func TestUIProjectDeletedPageListsAndRestoresIssues(t *testing.T) {
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-deleted")
	live, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "live issue outside deleted tab",
	})
	if err != nil {
		t.Fatalf("CreateIssue live: %v", err)
	}
	deleted, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "deleted tab target issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue deleted: %v", err)
	}
	parent, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "deleted tab parent issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue parent: %v", err)
	}
	child, err := e.store.CreateSubIssue(e.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "deleted tab child issue",
	})
	if err != nil {
		t.Fatalf("CreateSubIssue child: %v", err)
	}
	otherProject, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "Other Deleted Project", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	if _, err := e.store.GrantProjectAccess(e.ctx, otherProject.ID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess other: %v", err)
	}
	otherDeleted, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: otherProject.ID,
		Title:     "other project deleted issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue other deleted: %v", err)
	}
	if err := e.store.DeleteIssue(e.ctx, deleted.ID); err != nil {
		t.Fatalf("DeleteIssue deleted: %v", err)
	}
	if err := e.store.DeleteIssue(e.ctx, parent.ID); err != nil {
		t.Fatalf("DeleteIssue parent: %v", err)
	}
	if err := e.store.DeleteIssue(e.ctx, otherDeleted.ID); err != nil {
		t.Fatalf("DeleteIssue other: %v", err)
	}

	projectBody := e.uiGet(t, e.projectPath()+"/backlog", token)
	for _, want := range []string{
		`aria-label="Project actions"`,
		`data-lucide="more-horizontal"`,
		`href="` + e.projectPath() + `/deleted"`,
		`hx-get="` + e.projectPath() + `/deleted/panel"`,
		"Deleted issues",
	} {
		if !strings.Contains(projectBody, want) {
			t.Fatalf("project body missing deleted menu affordance %q: %s", want, projectBody)
		}
	}
	tabStart := strings.Index(projectBody, `aria-label="Project views"`)
	if tabStart < 0 {
		t.Fatalf("project body missing tab nav: %s", projectBody)
	}
	tabEnd := strings.Index(projectBody[tabStart:], "</nav>")
	if tabEnd < 0 {
		t.Fatalf("project body missing tab nav close: %s", projectBody)
	}
	tabMarkup := projectBody[tabStart : tabStart+tabEnd]
	if strings.Contains(tabMarkup, "Deleted") || strings.Contains(tabMarkup, `/deleted`) {
		t.Fatalf("deleted rendered as project tab: %s", projectBody)
	}

	body := e.uiGet(t, e.projectPath()+"/deleted", token)
	for _, want := range []string{
		"Deleted issues",
		e.projKey,
		e.projectPath() + "/sprint",
		e.projectPath() + "/sprint/panel",
		deleted.Identifier,
		deleted.Title,
		`href="` + e.issuePath(deleted) + `"`,
		`hx-get="` + e.issuePath(deleted) + `/panel"`,
		parent.Title,
		child.Title,
		"Sub-issue",
		`method="post" action="` + e.issuePath(deleted) + `/restore"`,
		`hx-post="` + e.issuePath(deleted) + `/restore"`,
		`method="post" action="` + e.issuePath(child) + `/restore"`,
		`hx-post="` + e.issuePath(child) + `/restore"`,
		`hx-push-url="` + e.issuePath(child) + `"`,
		`data-lucide="rotate-ccw"`,
		"Restore",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("deleted body missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{live.Title, otherDeleted.Title, "Issue deleted", `aria-label="Project views"`, `aria-label="Project actions"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("deleted body included %q: %s", notWant, body)
		}
	}

	res := e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.issuePath(child)+"/restore", token, nil, map[string]string{
		"HX-Request": "true",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("deleted restore code = %d body = %s", res.StatusCode, body)
	}
	if push := res.Header.Get("HX-Push-Url"); push != e.issuePath(child) {
		t.Fatalf("restore HX-Push-Url = %q", push)
	}
	if strings.Contains(body, "<!doctype html>") || !strings.Contains(body, child.Title) || !strings.Contains(body, "Sub-issue of") || !strings.Contains(body, parent.Title) || !strings.Contains(body, `aria-label="Issue settings"`) {
		t.Fatalf("deleted restore should render restored issue panel: %s", body)
	}
	if _, err := e.store.GetIssue(e.ctx, child.ID); err != nil {
		t.Fatalf("GetIssue child restored from deleted tab: %v", err)
	}
	if _, err := e.store.GetIssue(e.ctx, parent.ID); err != nil {
		t.Fatalf("GetIssue parent restored with child: %v", err)
	}
	body = e.uiGet(t, e.projectPath()+"/deleted", token)
	if strings.Contains(body, parent.Title) || strings.Contains(body, child.Title) {
		t.Fatalf("deleted body kept restored parent/child: %s", body)
	}
	if !strings.Contains(body, deleted.Title) {
		t.Fatalf("deleted body lost remaining deleted issue: %s", body)
	}
}

func TestUIEditPriorityUpdatesIssuePanel(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-priority")
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "priority target issue",
		Priority:  model.PriorityP3,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	edit := e.uiGet(t, e.issuePath(issue)+"/priority/edit", token)
	for _, want := range []string{
		"priority target issue",
		`role="listbox" aria-label="Issue priority"`,
		`method="post" action="` + e.issuePath(issue) + `/priority"`,
		`hx-post="` + e.issuePath(issue) + `/priority"`,
		`hx-push-url="` + e.issuePath(issue) + `"`,
		`name="priority" value="P0"`,
		`name="priority" value="P1"`,
		`name="priority" value="P2"`,
		`name="priority" value="P3"`,
		`name="priority" value="P4"`,
		`aria-label="Cancel priority change"`,
		`hx-get="` + e.issuePath(issue) + `/panel"`,
		`aria-label="Priority P3"`,
		`bg-yellow-500`,
		`flex items-center gap-2`,
		`flex flex-wrap items-center gap-2`,
		`opacity-100`,
		`opacity-40 hover:opacity-80`,
	} {
		if !strings.Contains(edit, want) {
			t.Fatalf("priority edit response missing %q: %s", want, edit)
		}
	}
	if strings.Contains(edit, `title="Change priority"`) ||
		strings.Contains(edit, `title="Cancel priority change"`) ||
		strings.Contains(edit, `aria-expanded="true"`) ||
		strings.Contains(edit, `data-lucide="chevron-up"`) ||
		strings.Contains(edit, `opacity-100 ring-2 ring-indigo-500`) {
		t.Fatalf("priority edit response has native tooltip state: %s", edit)
	}

	form := url.Values{"priority": {string(model.PriorityP0)}}
	res := e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/priority", token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update priority code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, `aria-label="Priority P0"`) || strings.Contains(body, `role="listbox"`) {
		t.Fatalf("update priority response did not return read mode with new priority: %s", body)
	}
	updated, err := e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after priority update: %v", err)
	}
	if updated.Priority != model.PriorityP0 {
		t.Fatalf("Priority = %q, want %q", updated.Priority, model.PriorityP0)
	}

	badPriority := url.Values{"priority": {"p0"}}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/priority", token, strings.NewReader(badPriority.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad priority code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "invalid priority") {
		t.Fatalf("bad priority response missing validation error: %s", body)
	}
	updated, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after bad priority: %v", err)
	}
	if updated.Priority != model.PriorityP0 {
		t.Fatalf("bad priority changed Priority = %q, want %q", updated.Priority, model.PriorityP0)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/priority", token, strings.NewReader("%zz"))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad form priority code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "unable to read form") {
		t.Fatalf("bad form priority response missing parse error: %s", body)
	}
	updated, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after bad form priority: %v", err)
	}
	if updated.Priority != model.PriorityP0 {
		t.Fatalf("bad form changed Priority = %q, want %q", updated.Priority, model.PriorityP0)
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

func TestUIEditIssuePeopleUpdatesAndClearsIssuePanel(t *testing.T) {
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-people")
	target, err := e.store.CreateUser(e.ctx, "ui-person-"+uniqueProjectKey(t)+"@example.com", "UI Person")
	if err != nil {
		t.Fatalf("CreateUser target: %v", err)
	}
	nonMember, err := e.store.CreateUser(e.ctx, "ui-person-outsider-"+uniqueProjectKey(t)+"@example.com", "UI Person Outsider")
	if err != nil {
		t.Fatalf("CreateUser nonmember: %v", err)
	}
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, target.ID); err != nil {
		t.Fatalf("GrantProjectAccess target: %v", err)
	}
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "people target issue",
		ReporterID: &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	editAssignee := e.uiGet(t, e.issuePath(issue)+"/assignee/edit", token)
	for _, want := range []string{
		`method="post" action="` + e.issuePath(issue) + `/assignee"`,
		`hx-post="` + e.issuePath(issue) + `/assignee"`,
		`hx-push-url="` + e.issuePath(issue) + `"`,
		`name="assignee"`,
		`data-search`,
		`data-search-input`,
		`role="listbox" aria-label="Assignee suggestions"`,
		`data-search-option`,
		`aria-label="Save assignee"`,
		`aria-label="Cancel editing assignee"`,
		`hx-get="` + e.issuePath(issue) + `/panel"`,
		`data-value="@` + target.Username + `"`,
		`data-search-text="@` + target.Username,
		target.Name,
	} {
		if !strings.Contains(editAssignee, want) {
			t.Fatalf("assignee edit missing %q: %s", want, editAssignee)
		}
	}
	if strings.Contains(editAssignee, `aria-label="Edit assignee" class="grid h-7 w-7 shrink-0 cursor-not-allowed`) || strings.Contains(editAssignee, `title="Save assignee"`) {
		t.Fatalf("assignee edit has disabled or tooltip state: %s", editAssignee)
	}

	form := url.Values{"assignee": {"@" + target.Username}}
	res := e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/assignee", token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update assignee code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, target.Name) || strings.Contains(body, `name="assignee"`) {
		t.Fatalf("assignee update did not return read mode with target: %s", body)
	}
	updated, err := e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after assignee update: %v", err)
	}
	if updated.AssigneeID == nil || *updated.AssigneeID != target.ID {
		t.Fatalf("AssigneeID = %v, want %s", updated.AssigneeID, target.ID)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/assignee", token, strings.NewReader(url.Values{"assignee": {" "}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("clear assignee code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Unassigned") || strings.Contains(body, target.Name) {
		t.Fatalf("clear assignee response missing empty state: %s", body)
	}
	updated, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after assignee clear: %v", err)
	}
	if updated.AssigneeID != nil {
		t.Fatalf("AssigneeID after clear = %v, want nil", updated.AssigneeID)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/assignee", token, strings.NewReader(url.Values{"assignee": {"@" + nonMember.Username}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("invalid assignee code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Choose a project member.") || !strings.Contains(body, `name="assignee"`) {
		t.Fatalf("invalid assignee did not rerender edit mode: %s", body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/assignee", token, strings.NewReader("%zz"))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "unable to read form") {
		t.Fatalf("bad assignee form code = %d body = %s", res.StatusCode, body)
	}

	editReporter := e.uiGet(t, e.issuePath(issue)+"/reporter/edit", token)
	for _, want := range []string{
		`method="post" action="` + e.issuePath(issue) + `/reporter"`,
		`hx-post="` + e.issuePath(issue) + `/reporter"`,
		`name="reporter"`,
		`value="@` + user.Username + `"`,
		`value="@` + target.Username + `"`,
		`aria-label="Save reporter"`,
		`aria-label="Cancel editing reporter"`,
	} {
		if !strings.Contains(editReporter, want) {
			t.Fatalf("reporter edit missing %q: %s", want, editReporter)
		}
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/reporter", token, strings.NewReader(url.Values{"reporter": {"@" + target.Username}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update reporter code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, target.Name) || strings.Contains(body, `name="reporter"`) {
		t.Fatalf("reporter update did not return read mode with target: %s", body)
	}
	updated, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after reporter update: %v", err)
	}
	if updated.ReporterID == nil || *updated.ReporterID != target.ID {
		t.Fatalf("ReporterID = %v, want %s", updated.ReporterID, target.ID)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/reporter", token, strings.NewReader(url.Values{"reporter": {""}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("clear reporter code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "No reporter") || strings.Contains(body, target.Name) {
		t.Fatalf("clear reporter response missing empty state: %s", body)
	}
	updated, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after reporter clear: %v", err)
	}
	if updated.ReporterID != nil {
		t.Fatalf("ReporterID after clear = %v, want nil", updated.ReporterID)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/reporter", token, strings.NewReader(url.Values{"reporter": {"@" + nonMember.Username}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("invalid reporter code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Choose a project member.") || !strings.Contains(body, `name="reporter"`) {
		t.Fatalf("invalid reporter did not rerender edit mode: %s", body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/reporter", token, strings.NewReader("%zz"))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "unable to read form") {
		t.Fatalf("bad reporter form code = %d body = %s", res.StatusCode, body)
	}
}

func TestUIEditIssueSprintUpdatesClearsAndValidates(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-sprint-edit")
	past, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Past Sprint",
		StartDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint past: %v", err)
	}
	activeStatus := model.SprintStatusActive
	if _, err := e.store.UpdateSprint(e.ctx, past.ID, store.UpdateSprintParams{Status: &activeStatus}); err != nil {
		t.Fatalf("activate past: %v", err)
	}
	if _, err := e.store.CompleteSprint(e.ctx, past.ID); err != nil {
		t.Fatalf("complete past: %v", err)
	}
	active, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Active Sprint",
		StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint active: %v", err)
	}
	if _, err := e.store.UpdateSprint(e.ctx, active.ID, store.UpdateSprintParams{Status: &activeStatus}); err != nil {
		t.Fatalf("activate current: %v", err)
	}
	planned, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Future Sprint",
		StartDate: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint planned: %v", err)
	}
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "sprint target issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	edit := e.uiGet(t, e.issuePath(issue)+"/sprint/edit", token)
	for _, want := range []string{
		`method="post" action="` + e.issuePath(issue) + `/sprint"`,
		`hx-post="` + e.issuePath(issue) + `/sprint"`,
		`hx-push-url="` + e.issuePath(issue) + `"`,
		`name="sprint"`,
		`data-search`,
		`data-search-input`,
		`role="listbox" aria-label="Sprint suggestions"`,
		`data-search-option`,
		`aria-label="Save sprint"`,
		`aria-label="Cancel editing sprint"`,
		`data-value="` + active.Ref + `"`,
		`data-search-text="` + active.Ref,
		"Active Sprint",
		`data-value="` + planned.Ref + `"`,
		"Future Sprint",
	} {
		if !strings.Contains(edit, want) {
			t.Fatalf("sprint edit missing %q: %s", want, edit)
		}
	}
	activeIndex := strings.Index(edit, `value="`+active.Ref+`"`)
	plannedIndex := strings.Index(edit, `value="`+planned.Ref+`"`)
	if activeIndex < 0 || plannedIndex < 0 || activeIndex > plannedIndex {
		t.Fatalf("active option should render before planned option: %s", edit)
	}
	if strings.Contains(edit, "Past Sprint") || strings.Contains(edit, past.Ref) {
		t.Fatalf("sprint edit included completed sprint: %s", edit)
	}

	res := e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/sprint", token, strings.NewReader(url.Values{"sprint": {planned.Ref}}.Encode()))
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("set sprint code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Future Sprint") || strings.Contains(body, `name="sprint"`) {
		t.Fatalf("set sprint did not return read mode with planned sprint: %s", body)
	}
	updated, err := e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after set: %v", err)
	}
	if updated.SprintID == nil || *updated.SprintID != planned.ID {
		t.Fatalf("SprintID = %v, want %s", updated.SprintID, planned.ID)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/sprint", token, strings.NewReader(url.Values{"sprint": {" "}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("clear sprint code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, ">None</dd>") {
		t.Fatalf("clear sprint did not show none: %s", body)
	}
	updated, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after clear: %v", err)
	}
	if updated.SprintID != nil {
		t.Fatalf("SprintID = %v, want nil", updated.SprintID)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/sprint", token, strings.NewReader(url.Values{"sprint": {"not-a-sprint"}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("invalid sprint code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Choose an active or planned sprint.") || !strings.Contains(body, `value="not-a-sprint"`) {
		t.Fatalf("invalid sprint response missing inline error/input: %s", body)
	}
}

func TestUIIssueSprintDoneReadOnlyAndPostRejected(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-sprint-done")
	current, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Current Sprint",
		StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint current: %v", err)
	}
	next, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Next Sprint",
		StartDate: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint next: %v", err)
	}
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "done sprint issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	done := model.StatusDone
	if _, err := e.store.UpdateIssue(e.ctx, issue.ID, store.UpdateIssueParams{SprintID: &current.ID}); err != nil {
		t.Fatalf("assign current: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, issue.ID, store.UpdateIssueParams{Status: &done}); err != nil {
		t.Fatalf("mark done: %v", err)
	}

	body := e.uiGet(t, e.issuePath(issue), token)
	for _, want := range []string{
		"Current Sprint",
		`aria-label="Edit sprint"`,
		"disabled",
		"cursor-not-allowed",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("done detail missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `/sprint/edit`) || strings.Contains(body, `name="sprint"`) {
		t.Fatalf("done detail included editable sprint controls: %s", body)
	}

	res := e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/sprint", token, strings.NewReader(url.Values{"sprint": {next.Ref}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("done sprint post code = %d body = %s", res.StatusCode, body)
	}
	updated, err := e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after rejected post: %v", err)
	}
	if updated.SprintID == nil || *updated.SprintID != current.ID {
		t.Fatalf("SprintID = %v, want %s", updated.SprintID, current.ID)
	}
}

func TestUIAddEditAndRemoveIssueLinks(t *testing.T) {
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-links")
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "link ui source"})
	if err != nil {
		t.Fatalf("CreateIssue source: %v", err)
	}
	target, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "link ui target"})
	if err != nil {
		t.Fatalf("CreateIssue target: %v", err)
	}
	replacement, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "link ui replacement"})
	if err != nil {
		t.Fatalf("CreateIssue replacement: %v", err)
	}

	edit := e.uiGet(t, e.issueLinksPath(issue)+"/new", token)
	for _, want := range []string{
		"link ui source",
		`method="post" action="` + e.issueLinksPath(issue) + `"`,
		`hx-post="` + e.issueLinksPath(issue) + `"`,
		`name="relation" aria-label="Link relationship"`,
		`value="relates_to" selected`,
		`value="blocked_by"`,
		`name="target_issue" value="" placeholder="` + e.projKey + `-12"`,
		`aria-label="Save link"`,
		`aria-label="Cancel adding link"`,
	} {
		if !strings.Contains(edit, want) {
			t.Fatalf("add link response missing %q: %s", want, edit)
		}
	}
	if strings.Contains(edit, "No linked issues.") {
		t.Fatalf("add link response should replace empty state with form: %s", edit)
	}

	empty := url.Values{"relation": {"relates_to"}, "target_issue": {"   "}}
	res := e.uiDoNoRedirect(t, http.MethodPost, e.issueLinksPath(issue), token, strings.NewReader(empty.Encode()))
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("empty target code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Linked issue required.") {
		t.Fatalf("empty target response missing validation error: %s", body)
	}

	badRelation := url.Values{"relation": {"blocks_by_magic"}, "target_issue": {target.Identifier}}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueLinksPath(issue), token, strings.NewReader(badRelation.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("bad relation code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Choose a valid relationship.") || !strings.Contains(body, `value="`+target.Identifier+`"`) {
		t.Fatalf("bad relation response missing preserved form state: %s", body)
	}

	form := url.Values{"relation": {"blocked_by"}, "target_issue": {target.Identifier}}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueLinksPath(issue), token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create link code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Blocked by") || !strings.Contains(body, "link ui target") || strings.Contains(body, `name="target_issue"`) {
		t.Fatalf("create link response did not return read mode: %s", body)
	}
	links, _, err := e.store.ListIssueLinksForIssue(e.ctx, store.ListIssueLinksForIssueParams{IssueID: issue.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListIssueLinksForIssue: %v", err)
	}
	if len(links) != 1 || links[0].SourceID != target.ID || links[0].TargetID != issue.ID || links[0].LinkType != model.LinkTypeBlocks {
		t.Fatalf("links = %+v, want target blocks issue", links)
	}
	link := links[0]

	edit = e.uiGet(t, e.issueLinksPath(issue)+"/"+link.Ref+"/edit", token)
	for _, want := range []string{
		`method="post" action="` + e.issueLinksPath(issue) + `/` + link.Ref + `"`,
		`hx-post="` + e.issueLinksPath(issue) + `/` + link.Ref + `"`,
		`value="blocked_by" selected`,
		`name="target_issue" value="` + target.Identifier + `"`,
		`aria-label="Cancel editing link"`,
		`aria-label="Remove link"`,
		`hx-post="` + e.issueLinksPath(issue) + `/` + link.Ref + `/delete"`,
	} {
		if !strings.Contains(edit, want) {
			t.Fatalf("edit link response missing %q: %s", want, edit)
		}
	}

	update := url.Values{"relation": {"clones"}, "target_issue": {replacement.Identifier}}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueLinksPath(issue)+"/"+link.Ref, token, strings.NewReader(update.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update link code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Clones") || !strings.Contains(body, "link ui replacement") || strings.Contains(body, "link ui target") {
		t.Fatalf("update link response missing updated read mode: %s", body)
	}
	updated, err := e.store.GetIssueLink(e.ctx, link.ID)
	if err != nil {
		t.Fatalf("GetIssueLink after update: %v", err)
	}
	if updated.SourceID != issue.ID || updated.TargetID != replacement.ID || updated.LinkType != model.LinkTypeClones || updated.Ref != link.Ref {
		t.Fatalf("updated link = %+v, want issue clones replacement with same ref %s", updated, link.Ref)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueLinksPath(issue)+"/"+link.Ref+"/delete", token, nil)
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete link code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "No linked issues.") || strings.Contains(body, "link ui replacement") {
		t.Fatalf("delete link response did not return empty link state: %s", body)
	}
	if _, err := e.store.GetIssueLink(e.ctx, link.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetIssueLink after delete err = %v, want ErrNotFound", err)
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

	edit := e.uiGet(t, e.issueSubIssuesPath(parent)+"/new", token)
	for _, want := range []string{
		"parent target issue",
		`aria-label="Cancel adding sub-issue"`,
		`method="post" action="` + e.issueSubIssuesPath(parent) + `"`,
		`hx-post="` + e.issueSubIssuesPath(parent) + `"`,
		`name="title" value="" autofocus placeholder="Title"`,
		`aria-label="Create sub-issue"`,
		`data-lucide="check"`,
	} {
		if !strings.Contains(edit, want) {
			t.Fatalf("sub-issue add response missing %q: %s", want, edit)
		}
	}
	if strings.Contains(edit, `hx-get="`+e.issueSubIssuesPath(parent)+`/new"`) {
		t.Fatalf("sub-issue add response should replace the add button with a cancel button: %s", edit)
	}

	form := url.Values{"title": {"new child from ui"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, e.issueSubIssuesPath(parent), token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("code = %d body = %s", res.StatusCode, body)
	}
	for _, want := range []string{"parent target issue", "Sub-issues", "new child from ui", `aria-label="Add sub-issue"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("sub-issue post response missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `aria-label="Create sub-issue"`) || strings.Contains(body, `name="title"`) {
		t.Fatalf("successful sub-issue post should close the composer: %s", body)
	}
	children, _, err := e.store.ListSubIssuesForIssue(e.ctx, store.ListSubIssuesForIssueParams{
		ParentIssueID: parent.ID,
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("ListSubIssuesForIssue: %v", err)
	}
	if len(children) != 1 || children[0].Title != "new child from ui" || children[0].Description != "" || children[0].Priority != model.PriorityP2 {
		t.Fatalf("children = %+v, want one title-only P2 child", children)
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
	for _, want := range []string{
		`aria-label="Cancel adding sub-issue"`,
		`method="post" action="` + e.issueSubIssuesPath(parent) + `"`,
		`name="title" value="   " autofocus placeholder="Title"`,
		`aria-label="Create sub-issue"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("empty sub-issue response should keep composer open with %q: %s", want, body)
		}
	}
	formIndex := strings.Index(body, `name="title" value="   "`)
	childIndex := strings.Index(body, "new child from ui")
	if formIndex < 0 || childIndex < 0 || formIndex > childIndex {
		t.Fatalf("empty sub-issue response should render composer before rows: %s", body)
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
		`aria-label="Add sub-issue"`,
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
	linked, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "protected linked issue"})
	if err != nil {
		t.Fatalf("CreateIssue linked: %v", err)
	}
	link, err := e.store.CreateIssueLink(e.ctx, store.CreateIssueLinkParams{
		SourceID: issue.ID,
		TargetID: linked.ID,
		LinkType: model.LinkTypeRelatesTo,
	})
	if err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}
	deletedIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "protected deleted issue"})
	if err != nil {
		t.Fatalf("CreateIssue deleted: %v", err)
	}
	if err := e.store.DeleteIssue(e.ctx, deletedIssue.ID); err != nil {
		t.Fatalf("DeleteIssue protected: %v", err)
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
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/delete", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue delete code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(deletedIssue)+"/restore", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue restore code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(deletedIssue), token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("deleted issue detail code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(deletedIssue)+"/panel", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("deleted issue panel code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/assignee/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue assignee edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/assignee", token, strings.NewReader(url.Values{"assignee": {"denied"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue assignee update code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/reporter/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue reporter edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/reporter", token, strings.NewReader(url.Values{"reporter": {"denied"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue reporter update code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/sprint/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue sprint edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/sprint", token, strings.NewReader(url.Values{"sprint": {"denied"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue sprint update code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issueLinksPath(issue)+"/new", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue link new code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issueSubIssuesPath(issue)+"/new", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue sub-issue new code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueLinksPath(issue), token, strings.NewReader(url.Values{"relation": {"relates_to"}, "target_issue": {linked.Identifier}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue link create code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issueLinksPath(issue)+"/"+link.Ref+"/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue link edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueLinksPath(issue)+"/"+link.Ref, token, strings.NewReader(url.Values{"relation": {"blocks"}, "target_issue": {linked.Identifier}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue link update code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueLinksPath(issue)+"/"+link.Ref+"/delete", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue link delete code = %d body = %s", res.StatusCode, readBody(t, res))
	}

	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue), "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauth issue code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/login?next="+url.QueryEscape(e.issuePath(issue)) {
		t.Fatalf("Location = %q", loc)
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issueLinksPath(issue)+"/new", "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauth issue link new code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/login?next="+url.QueryEscape(e.issueLinksPath(issue)+"/new") {
		t.Fatalf("link new Location = %q", loc)
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issueSubIssuesPath(issue)+"/new", "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauth issue sub-issue new code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/login?next="+url.QueryEscape(e.issueSubIssuesPath(issue)+"/new") {
		t.Fatalf("sub-issue new Location = %q", loc)
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/assignee/edit", "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauth assignee edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/login?next="+url.QueryEscape(e.issuePath(issue)+"/assignee/edit") {
		t.Fatalf("assignee edit Location = %q", loc)
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/sprint/edit", "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauth sprint edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/login?next="+url.QueryEscape(e.issuePath(issue)+"/sprint/edit") {
		t.Fatalf("sprint edit Location = %q", loc)
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
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/delete"},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/restore"},
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref/assignee/edit"},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/assignee", body: strings.NewReader(url.Values{"assignee": {"hello"}}.Encode())},
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref/reporter/edit"},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/reporter", body: strings.NewReader(url.Values{"reporter": {"hello"}}.Encode())},
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref/sprint/edit"},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/sprint", body: strings.NewReader(url.Values{"sprint": {"sprint-1"}}.Encode())},
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref/links/new"},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/links", body: strings.NewReader(url.Values{"relation": {"relates_to"}, "target_issue": {linked.Identifier}}.Encode())},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/comments", body: strings.NewReader(url.Values{"body": {"hello"}}.Encode())},
	} {
		res := e.uiDoNoRedirect(t, tc.method, tc.path, memberToken, tc.body)
		defer res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s %s code = %d body = %s", tc.method, tc.path, res.StatusCode, readBody(t, res))
		}
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/archive", memberToken, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("removed issue archive route code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	unknownIssuePath := "/" + e.ownerUsername + "/issues/" + e.projKey + "-999999"
	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: unknownIssuePath},
		{method: http.MethodGet, path: unknownIssuePath + "/panel"},
		{method: http.MethodPost, path: unknownIssuePath + "/delete"},
		{method: http.MethodPost, path: unknownIssuePath + "/restore"},
		{method: http.MethodGet, path: unknownIssuePath + "/sprint/edit"},
		{method: http.MethodPost, path: unknownIssuePath + "/sprint"},
	} {
		res := e.uiDoNoRedirect(t, tc.method, tc.path, memberToken, nil)
		defer res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s code = %d body = %s", tc.method, tc.path, res.StatusCode, readBody(t, res))
		}
	}
	for _, tc := range []struct {
		method string
		path   string
		body   io.Reader
	}{
		{method: http.MethodGet, path: e.issueLinksPath(issue) + "/not-a-link/edit"},
		{method: http.MethodPost, path: e.issueLinksPath(issue) + "/not-a-link", body: strings.NewReader(url.Values{"relation": {"blocks"}, "target_issue": {linked.Identifier}}.Encode())},
		{method: http.MethodPost, path: e.issueLinksPath(issue) + "/not-a-link/delete"},
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
	if loc := res.Header.Get("Location"); loc != e.projectPath()+"/sprint" {
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

	for _, path := range []string{
		e.projectPath() + "/about",
		e.projectPath() + "/about/panel",
		e.projectPath() + "/sprint",
		e.projectPath() + "/sprint/panel",
		e.projectPath() + "/backlog",
		e.projectPath() + "/backlog/panel",
		e.projectPath() + "/deleted",
		e.projectPath() + "/deleted/panel",
	} {
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
	return e.uiDoNoRedirectWithHeaders(t, method, path, token, body, nil)
}

func (e *httpEnv) uiDoNoRedirectWithHeaders(t *testing.T, method, path, token string, body io.Reader, headers map[string]string) *http.Response {
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
	for key, value := range headers {
		req.Header.Set(key, value)
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
	if loc := res.Header.Get("Location"); loc != e.projectPath()+"/sprint" {
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
