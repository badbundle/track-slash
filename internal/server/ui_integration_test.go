package server_test

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-member")

	body := e.uiGet(t, "/me", token)
	for _, want := range []string{">Me<", ">Projects<", "Create issue", `href="/issues/new"`, `hx-get="/issues/new/panel"`, `data-sidebar-action`, `href="/settings"`, `href="/tokens"`, `data-lucide="plus"`, `data-lucide="user"`, `data-lucide="folder"`, "data-nav-loader", "#sidebar-toggle:checked ~ .app-shell > aside", `track-slash.sidebar.collapsed`, `data-member-menu`, `data-close-on-outside`, `closeOpenDropdowns`, `overflow-visible border-r`} {
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
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-projects")
	hidden, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "Hidden UI Project", "")
	if err != nil {
		t.Fatalf("CreateProject hidden: %v", err)
	}

	body := e.uiGet(t, "/projects", token)
	for _, want := range []string{"Projects", "Projects you can access.", `aria-label="New project"`, `href="/projects/new"`, `hx-get="/projects/new/panel"`, e.projKey, "http-test", "inline-flex w-fit justify-self-start", `href="` + e.projectPath() + `/sprint"`, `hx-get="` + e.projectPath() + `/sprint/panel"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("projects body missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{`id="project-key"`, `>Create project<`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("projects body still included project form %q: %s", notWant, body)
		}
	}
	if strings.Contains(body, `href="`+e.projectPath()+`/backlog"`) {
		t.Fatalf("projects body included backlog row action: %s", body)
	}
	if strings.Contains(body, hidden.Name) {
		t.Fatalf("projects body included inaccessible project: %s", body)
	}

	body = e.uiGet(t, "/projects/new", token)
	for _, want := range []string{"New project", "Create project", `action="/projects"`, `id="project-key"`, `id="project-name"`, `id="project-description"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("new project body missing %q: %s", want, body)
		}
	}
	newProjectMain := mainContentBlock(t, body)
	for _, notWant := range []string{`data-lucide="arrow-left"`, `href="/projects"`, `hx-get="/projects/panel"`} {
		if strings.Contains(newProjectMain, notWant) {
			t.Fatalf("new project body should not render back button markup %q: %s", notWant, body)
		}
	}

	form := url.Values{"key": {"bad"}, "name": {"Bad"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, "/projects", token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "Key must match") || !strings.Contains(body, "New project") || !strings.Contains(body, `value="bad"`) {
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
	if res.StatusCode != http.StatusConflict || !strings.Contains(body, "Project key already exists.") || !strings.Contains(body, "New project") {
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

func TestUINewIssueCreatesIssueWithAllFieldsAndDefaultReporter(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	reporter, token := e.mustProjectMemberToken(t, "ui-new-issue")
	assignee, _ := e.mustProjectMemberToken(t, "ui-new-issue-assignee")
	searchProject, err := e.store.CreateProjectForUser(e.ctx, reporter.ID, uniqueProjectKey(t), "Search Target Project", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser search target: %v", err)
	}

	body := e.uiGet(t, "/issues/new", token)
	for _, want := range []string{
		"New issue",
		"Choose a project",
		`method="post" action="/issues"`,
		`hx-post="/issues"`,
		`id="new-issue-project-form" method="get" action="/issues/new/panel"`,
		`id="issue-project" name="project"`,
		`type="hidden" name="project_id" value=""`,
		`hx-get="/issues/new/projects"`,
		`hx-target="#new-issue-project-options"`,
		`hx-swap="outerHTML"`,
		`data-search`,
		`data-search-collapsible`,
		`data-search-clear-target="project_id"`,
		`data-search-input`,
		`hx-trigger="input changed delay:300ms"`,
		`hx-push-url="false"`,
		`hx-include="#new-issue-project-form"`,
		`id="new-issue-project-options"`,
		`data-search-options hidden role="listbox" aria-label="Project suggestions"`,
		`data-search-option`,
		`data-target-name="project_id"`,
		`data-target-value="` + e.projectID.String() + `"`,
		e.projKey + " - http-test",
		searchProject.Key + " - Search Target Project",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("global new issue page missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `@`+assignee.Username) {
		t.Fatalf("global new issue page should wait for project before member suggestions: %s", body)
	}

	body = e.uiGet(t, "/issues/new/projects?project="+url.QueryEscape("search target"), token)
	for _, want := range []string{
		`id="new-issue-project-options"`,
		`data-search-options role="listbox" aria-label="Project suggestions"`,
		`data-target-value="` + searchProject.ID.String() + `"`,
		searchProject.Key + " - Search Target Project",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("project search panel missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, e.projKey+" - http-test") || strings.Contains(body, `data-target-value="`+e.projectID.String()+`"`) {
		t.Fatalf("project search did not filter unrelated project: %s", body)
	}
	body = e.uiGet(t, "/issues/new/projects?project="+url.QueryEscape("missing"), token)
	for _, want := range []string{
		`id="new-issue-project-options"`,
		`data-search-options role="listbox" aria-label="Project suggestions"`,
		`No projects found.`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("empty project search options missing %q: %s", want, body)
		}
	}

	body = e.uiGet(t, e.projectPath()+"/issues/new", token)
	for _, want := range []string{
		"New issue",
		"Create issue",
		`method="post" action="/issues"`,
		`hx-post="/issues"`,
		`hx-push-url="false"`,
		`id="issue-project" name="project" value="` + e.projKey + ` - http-test"`,
		`type="hidden" name="project_id" value="` + e.projectID.String() + `"`,
		`id="issue-title"`,
		`id="issue-description"`,
		`role="listbox" aria-labelledby="issue-priority-label" data-priority-picker`,
		`type="radio" name="priority" value="P2" checked`,
		`aria-label="Priority P2"`,
		`data-checkbox-reveal`,
		`id="issue-due-date-toggle" type="checkbox" data-checkbox-reveal-toggle aria-controls="issue-due-date-field" aria-expanded="false"`,
		`id="issue-due-date-field" data-checkbox-reveal-panel hidden`,
		`id="issue-due-date" type="date" name="due_date" value="" disabled`,
		`name="assignee"`,
		`name="reporter"`,
		`@` + assignee.Username,
		`@` + reporter.Username,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("new issue page missing %q: %s", want, body)
		}
	}

	body = e.uiGet(t, "/issues/new/panel?project_id="+url.QueryEscape(e.projectID.String())+"&title=Kept", token)
	for _, want := range []string{
		`id="issue-project" name="project" value="` + e.projKey + ` - http-test"`,
		`type="hidden" name="project_id" value="` + e.projectID.String() + `"`,
		`name="title" value="Kept"`,
		`@` + assignee.Username,
		`@` + reporter.Username,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("project-selected new issue panel missing %q: %s", want, body)
		}
	}

	form := url.Values{
		"project_id":  {e.projectID.String()},
		"title":       {"all fields issue"},
		"description": {"full body from ui"},
		"priority":    {string(model.PriorityP1)},
		"due_date":    {"2099-06-24"},
		"assignee":    {"@" + assignee.Username},
		"reporter":    {"@" + reporter.Username},
	}
	res := e.uiDoNoRedirectWithHeaders(t, http.MethodPost, "/issues", token, strings.NewReader(form.Encode()), map[string]string{"HX-Request": "true"})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create issue code = %d body = %s", res.StatusCode, body)
	}

	issues, _, err := e.store.ListIssues(e.ctx, store.ListIssuesParams{ProjectID: e.projectID, Limit: 10})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	var created model.Issue
	for _, issue := range issues {
		if issue.Title == "all fields issue" {
			created = issue
			break
		}
	}
	if created.ID == uuid.Nil {
		t.Fatalf("created issue not found: %+v", issues)
	}
	if push := res.Header.Get("HX-Push-Url"); push != e.issuePath(created) {
		t.Fatalf("HX-Push-Url = %q, want %q", push, e.issuePath(created))
	}
	if created.Description != "full body from ui" || created.Priority != model.PriorityP1 || created.DueDate == nil || created.DueDate.String() != "2099-06-24" {
		t.Fatalf("created fields = %+v", created)
	}
	if created.AssigneeID == nil || *created.AssigneeID != assignee.ID || created.ReporterID == nil || *created.ReporterID != reporter.ID {
		t.Fatalf("created people = assignee %v reporter %v", created.AssigneeID, created.ReporterID)
	}
	for _, want := range []string{"all fields issue", "full body from ui", assignee.Name, reporter.Name, "Jun 24"} {
		if !strings.Contains(body, want) {
			t.Fatalf("created issue panel missing %q: %s", want, body)
		}
	}

	defaultForm := url.Values{"project_id": {e.projectID.String()}, "title": {"default reporter issue"}}
	res = e.uiDoNoRedirect(t, http.MethodPost, "/issues", token, strings.NewReader(defaultForm.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("default create code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	issues, _, err = e.store.ListIssues(e.ctx, store.ListIssuesParams{ProjectID: e.projectID, Limit: 10})
	if err != nil {
		t.Fatalf("ListIssues default: %v", err)
	}
	var defaulted model.Issue
	for _, issue := range issues {
		if issue.Title == "default reporter issue" {
			defaulted = issue
			break
		}
	}
	if defaulted.ID == uuid.Nil {
		t.Fatalf("default reporter issue not found: %+v", issues)
	}
	if loc := res.Header.Get("Location"); loc != e.issuePath(defaulted) {
		t.Fatalf("Location = %q, want %q", loc, e.issuePath(defaulted))
	}
	if defaulted.ReporterID == nil || *defaulted.ReporterID != reporter.ID || defaulted.Priority != model.PriorityP2 || defaulted.AssigneeID != nil || defaulted.DueDate != nil {
		t.Fatalf("defaulted issue = %+v", defaulted)
	}
}

func TestUINewIssueValidationPreservesFormValues(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-new-issue-errors")
	other, _ := e.mustUserToken(t, "ui-new-issue-other")

	base := url.Values{
		"project_id":  {e.projectID.String()},
		"title":       {"Draft issue"},
		"description": {"Draft body"},
		"priority":    {string(model.PriorityP1)},
		"due_date":    {"2099-06-24"},
		"assignee":    {"@" + user.Username},
		"reporter":    {"@" + user.Username},
	}
	clone := func(in url.Values) url.Values {
		out := url.Values{}
		for key, values := range in {
			out[key] = append([]string(nil), values...)
		}
		return out
	}
	cases := []struct {
		name      string
		mutate    func(url.Values)
		wantError string
		want      []string
	}{
		{
			name:      "blank project",
			mutate:    func(v url.Values) { v.Del("project_id") },
			wantError: "Choose a project.",
			want:      []string{`id="issue-project" name="project"`, `type="hidden" name="project_id" value=""`},
		},
		{
			name:      "blank title",
			mutate:    func(v url.Values) { v.Set("title", "   ") },
			wantError: "Title required, max 200 chars.",
			want:      []string{`name="title" value="   "`},
		},
		{
			name:      "long title",
			mutate:    func(v url.Values) { v.Set("title", strings.Repeat("x", 201)) },
			wantError: "Title required, max 200 chars.",
		},
		{
			name:      "bad priority",
			mutate:    func(v url.Values) { v.Set("priority", "P5") },
			wantError: "Invalid priority.",
		},
		{
			name:      "bad due date",
			mutate:    func(v url.Values) { v.Set("due_date", "tomorrow") },
			wantError: "Use YYYY-MM-DD.",
			want:      []string{`name="due_date" value="tomorrow"`},
		},
		{
			name:      "unknown assignee",
			mutate:    func(v url.Values) { v.Set("assignee", "@missinguser") },
			wantError: "Choose a user.",
			want:      []string{`name="assignee" value="@missinguser"`},
		},
		{
			name:      "forbidden reporter",
			mutate:    func(v url.Values) { v.Set("reporter", "@"+other.Username) },
			wantError: "Reporter must be you.",
			want:      []string{`name="reporter" value="@` + other.Username + `"`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form := clone(base)
			tc.mutate(form)
			res := e.uiDoNoRedirect(t, http.MethodPost, "/issues", token, strings.NewReader(form.Encode()))
			defer res.Body.Close()
			body := readBody(t, res)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("code = %d body = %s", res.StatusCode, body)
			}
			for _, want := range append([]string{
				"New issue",
				tc.wantError,
				"Draft body",
				`name="assignee"`,
				`name="reporter"`,
			}, tc.want...) {
				if !strings.Contains(body, want) {
					t.Fatalf("%s response missing %q: %s", tc.name, want, body)
				}
			}
		})
	}
	issues, _, err := e.store.ListIssues(e.ctx, store.ListIssuesParams{ProjectID: e.projectID, Limit: 10})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("validation created issues: %+v", issues)
	}
}

func TestUIProjectAndIssueContext(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-context")
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "context target issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	body := e.uiGet(t, e.projectPath()+"/about", token)
	projectContextDetail := issueContextDetailBlock(t, body)
	for _, want := range []string{`aria-label="Manage context"`, `href="` + e.projectPath() + `/context"`, ">0</span>"} {
		if !strings.Contains(projectContextDetail, want) {
			t.Fatalf("about context body missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{`role="dialog" aria-modal="true"`, "No context.", "context items", `placeholder="Context"`, `name="file"`, `aria-label="Create context"`, `aria-label="Upload context"`, `aria-label="Add context"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("about context body should stay compact, found %q: %s", notWant, body)
		}
	}

	body = e.uiGet(t, e.projectPath()+"/context", token)
	for _, want := range []string{"Context", "No context.", `aria-label="Add context"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("project context manager missing %q: %s", want, body)
		}
	}

	body = e.uiGet(t, e.projectPath()+"/context/new", token)
	for _, want := range []string{"New context", "Upload text", `placeholder="Context"`, `name="file"`, `aria-label="Create context"`, `aria-label="Upload context"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("project context create manager missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `role="dialog" aria-modal="true"`) {
		t.Fatalf("project context create should not render a modal: %s", body)
	}

	res := e.uiDoMultipartContext(t, e.projectPath()+"/context", token, nil, "image.png", "nope")
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("bad upload code = %d body = %s", res.StatusCode, body)
	}
	for _, want := range []string{"file must be .txt, .md, or .markdown", `aria-label="Upload context"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("bad upload manager missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `role="dialog" aria-modal="true"`) {
		t.Fatalf("bad upload should stay in manager, not modal: %s", body)
	}

	form := url.Values{"title": {"Architecture"}, "body": {"Use the existing store path."}}
	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.projectPath()+"/context", token, strings.NewReader(form.Encode()), map[string]string{
		"HX-Current-URL": e.ts.URL + e.projectPath() + "/context/new",
		"HX-Request":     "true",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create context code = %d body = %s", res.StatusCode, body)
	}
	if replace := res.Header.Get("HX-Replace-Url"); replace != e.projectPath()+"/context" {
		t.Fatalf("create context HX-Replace-Url = %q", replace)
	}
	if push := res.Header.Get("HX-Push-Url"); push != "" {
		t.Fatalf("create context HX-Push-Url = %q, want empty", push)
	}
	for _, want := range []string{"Architecture", `aria-label="Link issue"`, `aria-label="Edit context"`, `aria-label="Delete context"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("created context body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Use the existing store path.") || strings.Contains(body, `placeholder="`+e.projKey+`-12"`) || strings.Contains(body, `font-mono`) {
		t.Fatalf("created context row should stay compact: %s", body)
	}
	contextPath := e.projectPath() + "/context/context-1"

	body = e.uiGet(t, contextPath+"/issues/new", token)
	for _, want := range []string{"Linked issues", `placeholder="` + e.projKey + `-12"`, "No linked issues.", `aria-label="Link issue"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("project context issue link manager missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `role="dialog" aria-modal="true"`) {
		t.Fatalf("project context issue link should not render a modal: %s", body)
	}

	body = e.uiGet(t, contextPath+"/edit", token)
	for _, want := range []string{`value="Architecture"`, "Use the existing store path.", `aria-label="Save context"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("edit context body missing %q: %s", want, body)
		}
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, contextPath, token, strings.NewReader(url.Values{"title": {""}, "body": {"Still here"}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("invalid update context code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "title required, max 200 chars") || !strings.Contains(body, "Still here") {
		t.Fatalf("invalid update context body missing error/state: %s", body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, contextPath+"/issues", token, strings.NewReader(url.Values{"issue": {"bad"}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("bad project link context code = %d body = %s", res.StatusCode, body)
	}
	if strings.Contains(body, `role="dialog" aria-modal="true"`) || !strings.Contains(body, "Choose an issue in this project.") || !strings.Contains(body, `value="bad"`) {
		t.Fatalf("bad project link context body missing error/state: %s", body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, contextPath+"/issues", token, strings.NewReader(url.Values{"issue": {issue.Identifier}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("project link context code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, `aria-label="Link issue"`) || strings.Contains(body, `placeholder="`+e.projKey+`-12"`) || !strings.Contains(body, `>1</span>`) {
		t.Fatalf("project link context response should return compact context row: %s", body)
	}
	body = e.uiGet(t, contextPath+"/issues/new", token)
	if !strings.Contains(body, issue.Identifier) || !strings.Contains(body, `aria-label="Unlink issue"`) {
		t.Fatalf("project link context manager missing linked issue: %s", body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, contextPath+"/issues", token, strings.NewReader(url.Values{"issue": {issue.Identifier}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("duplicate project link context code = %d body = %s", res.StatusCode, body)
	}
	if strings.Contains(body, `role="dialog" aria-modal="true"`) || !strings.Contains(body, "Issue already linked.") {
		t.Fatalf("duplicate project link context body missing conflict error: %s", body)
	}

	form = url.Values{"title": {"Architecture v2"}, "body": {"Use the updated store path."}}
	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, contextPath, token, strings.NewReader(form.Encode()), map[string]string{
		"HX-Current-URL": e.ts.URL + contextPath + "/edit",
		"HX-Request":     "true",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update context code = %d body = %s", res.StatusCode, body)
	}
	if replace := res.Header.Get("HX-Replace-Url"); replace != e.projectPath()+"/context" {
		t.Fatalf("update context HX-Replace-Url = %q", replace)
	}
	if push := res.Header.Get("HX-Push-Url"); push != "" {
		t.Fatalf("update context HX-Push-Url = %q, want empty", push)
	}
	if !strings.Contains(body, "Architecture v2") {
		t.Fatalf("updated context body missing title: %s", body)
	}
	if strings.Contains(body, "Use the updated store path.") {
		t.Fatalf("updated context row should not show body preview: %s", body)
	}

	issueBody := e.uiGet(t, e.issuePath(issue), token)
	issueMain := mainContentBlock(t, issueBody)
	contextDetail := issueContextDetailBlock(t, issueBody)
	for _, want := range []string{"Context", `aria-label="Manage context"`, ">1</span>", `hx-get="` + e.issuePath(issue) + `/context"`} {
		if !strings.Contains(contextDetail, want) {
			t.Fatalf("issue context detail after project edit missing %q: %s", want, issueBody)
		}
	}
	for _, notWant := range []string{"context-1", "Architecture v2", "Use the updated store path.", `aria-label="Remove context"`, `aria-label="Add context"`, `aria-label="Attach context"`} {
		if strings.Contains(issueMain, notWant) {
			t.Fatalf("issue page should keep context details in manager, found %q: %s", notWant, issueBody)
		}
	}
	issueContextManager := e.uiGet(t, e.issuePath(issue)+"/context", token)
	for _, want := range []string{"Context", "context-1", "Architecture v2", `aria-label="Edit context"`, `aria-label="Remove context"`, `aria-label="Add context"`, `aria-label="Attach context"`} {
		if !strings.Contains(issueContextManager, want) {
			t.Fatalf("issue context manager after project edit missing %q: %s", want, issueContextManager)
		}
	}
	if strings.Contains(issueContextManager, "Use the updated store path.") || strings.Contains(issueContextManager, `role="dialog" aria-modal="true"`) {
		t.Fatalf("issue context manager should not show body preview or modal: %s", issueContextManager)
	}
	issueContextManager = e.uiGet(t, e.issuePath(issue)+"/context/context-1", token)
	if !strings.Contains(issueContextManager, "Use the updated store path.") {
		t.Fatalf("issue context item view missing latest body: %s", issueContextManager)
	}
	issueContextManager = e.uiGet(t, e.issuePath(issue)+"/context/context-1/edit", token)
	for _, want := range []string{`value="Architecture v2"`, "Use the updated store path.", `aria-label="Save context"`} {
		if !strings.Contains(issueContextManager, want) {
			t.Fatalf("issue context edit manager missing project-linked %q: %s", want, issueContextManager)
		}
	}
	form = url.Values{"title": {"Architecture v3"}, "body": {"Use the issue manager edit path."}}
	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.issuePath(issue)+"/context/context-1", token, strings.NewReader(form.Encode()), map[string]string{
		"HX-Current-URL": e.ts.URL + e.issuePath(issue) + "/context/context-1/edit",
		"HX-Request":     "true",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("issue edit project context code = %d body = %s", res.StatusCode, body)
	}
	if replace := res.Header.Get("HX-Replace-Url"); replace != e.issuePath(issue)+"/context" {
		t.Fatalf("issue edit context HX-Replace-Url = %q", replace)
	}
	if push := res.Header.Get("HX-Push-Url"); push != "" {
		t.Fatalf("issue edit context HX-Push-Url = %q, want empty", push)
	}
	if !strings.Contains(body, "Architecture v3") || strings.Contains(body, "Use the issue manager edit path.") {
		t.Fatalf("issue edit project context response should show compact updated row: %s", body)
	}
	projectBody := e.uiGet(t, e.projectPath()+"/context", token)
	if !strings.Contains(projectBody, "Architecture v3") {
		t.Fatalf("project context manager missing issue-edited project context: %s", projectBody)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, contextPath+"/issues/"+issue.Identifier+"/delete", token, nil)
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("project unlink context code = %d body = %s", res.StatusCode, body)
	}
	if strings.Contains(body, `action="`+contextPath+`/issues/`+issue.Identifier+`/delete"`) {
		t.Fatalf("project unlink context body still has linked issue action: %s", body)
	}

	issueBody = e.uiGet(t, e.issuePath(issue), token)
	contextDetail = issueContextDetailBlock(t, issueBody)
	for _, want := range []string{"Context", `aria-label="Manage context"`, ">0</span>"} {
		if !strings.Contains(contextDetail, want) {
			t.Fatalf("empty issue context detail missing %q: %s", want, issueBody)
		}
	}
	for _, notWant := range []string{"No context.", `placeholder="Search context by title"`, `aria-label="Add context"`, `aria-label="Attach context"`} {
		if strings.Contains(issueBody, notWant) {
			t.Fatalf("empty issue page should keep context controls in manager, found %q: %s", notWant, issueBody)
		}
	}
	issueContextManager = e.uiGet(t, e.issuePath(issue)+"/context", token)
	for _, want := range []string{"No context.", `aria-label="Add context"`, `aria-label="Attach context"`} {
		if !strings.Contains(issueContextManager, want) {
			t.Fatalf("empty issue context manager missing %q: %s", want, issueContextManager)
		}
	}
	if strings.Contains(issueContextManager, `role="dialog" aria-modal="true"`) {
		t.Fatalf("empty issue context manager should not render modal: %s", issueContextManager)
	}

	issueBody = e.uiGet(t, e.issuePath(issue)+"/context/new", token)
	for _, want := range []string{"New context", `placeholder="Context"`, `autofocus`, `aria-label="Create context"`, `aria-label="Upload context"`, `name="file"`} {
		if !strings.Contains(issueBody, want) {
			t.Fatalf("adding issue context manager missing %q: %s", want, issueBody)
		}
	}
	for _, notWant := range []string{`placeholder="Search context by title"`} {
		if strings.Contains(issueBody, notWant) {
			t.Fatalf("adding issue context manager should not include %q: %s", notWant, issueBody)
		}
	}

	issueBody = e.uiGet(t, e.issuePath(issue)+"/context/link", token)
	issueMain = mainContentBlock(t, issueBody)
	for _, want := range []string{`placeholder="Search context by title"`, `value="Architecture v3"`, `autofocus`, `aria-label="Attach context"`} {
		if !strings.Contains(issueBody, want) {
			t.Fatalf("attaching issue context body missing %q: %s", want, issueBody)
		}
	}
	if strings.Contains(issueMain, "context-1") {
		t.Fatalf("attaching issue context should not expose context refs: %s", issueBody)
	}
	for _, notWant := range []string{`aria-label="Create issue context"`, `aria-label="Upload issue context"`} {
		if strings.Contains(issueBody, notWant) {
			t.Fatalf("attaching issue context body should not include %q: %s", notWant, issueBody)
		}
	}
	if strings.Contains(issueBody, `role="dialog" aria-modal="true"`) {
		t.Fatalf("attaching issue context should not render modal: %s", issueBody)
	}

	res = e.uiDoMultipartContext(t, e.issuePath(issue)+"/context", token, nil, "image.png", "nope")
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("bad issue upload code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "file must be .txt, .md, or .markdown") || !strings.Contains(body, `aria-label="Upload context"`) {
		t.Fatalf("bad issue upload body missing error/state: %s", body)
	}

	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.issuePath(issue)+"/context", token, strings.NewReader(url.Values{"mode": {"create"}, "title": {"Issue note"}, "body": {"Only needed here."}}.Encode()), map[string]string{
		"HX-Current-URL": e.ts.URL + e.issuePath(issue) + "/context/new",
		"HX-Request":     "true",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create issue-only context code = %d body = %s", res.StatusCode, body)
	}
	if replace := res.Header.Get("HX-Replace-Url"); replace != e.issuePath(issue)+"/context" {
		t.Fatalf("create issue-only context HX-Replace-Url = %q", replace)
	}
	if push := res.Header.Get("HX-Push-Url"); push != "" {
		t.Fatalf("create issue-only context HX-Push-Url = %q, want empty", push)
	}
	for _, want := range []string{"Issue note", "Issue-only", `aria-label="Edit context"`, `aria-label="Remove context"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("issue-only context response missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Only needed here.") || strings.Contains(body, `font-mono`) {
		t.Fatalf("issue-only context row should not show body preview: %s", body)
	}
	issueContextManager = e.uiGet(t, e.issuePath(issue)+"/context/context-2", token)
	if !strings.Contains(issueContextManager, "Only needed here.") {
		t.Fatalf("issue-only context view missing body: %s", issueContextManager)
	}
	issueContextManager = e.uiGet(t, e.issuePath(issue)+"/context/context-2/edit", token)
	for _, want := range []string{`value="Issue note"`, "Only needed here.", `aria-label="Save context"`} {
		if !strings.Contains(issueContextManager, want) {
			t.Fatalf("issue-only context edit manager missing %q: %s", want, issueContextManager)
		}
	}
	projectBody = e.uiGet(t, e.projectPath()+"/context", token)
	if strings.Contains(projectBody, "Issue note") {
		t.Fatalf("project context manager should not show issue-only context: %s", projectBody)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/context/context-2/delete", token, nil)
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete issue-only context code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "No context.") || strings.Contains(body, "Issue note") {
		t.Fatalf("issue-only context remained after delete: %s", body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/context", token, strings.NewReader(url.Values{"context": {"Architecture v3"}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("link context code = %d body = %s", res.StatusCode, body)
	}
	for _, want := range []string{"Architecture v3", `aria-label="Edit context"`, `aria-label="Remove context"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("linked issue context manager missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Use the updated store path.") || strings.Contains(body, `font-mono`) {
		t.Fatalf("linked issue context manager should not show context body preview: %s", body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/context/context-1/delete", token, nil)
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("issue unlink context code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "No context.") || strings.Contains(body, "Use the updated store path.") {
		t.Fatalf("issue unlink context body still shows context: %s", body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, contextPath+"/delete", token, nil)
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete context code = %d body = %s", res.StatusCode, body)
	}
	if strings.Contains(body, "context-1") || strings.Contains(body, "Architecture v3") {
		t.Fatalf("delete context body still shows deleted context: %s", body)
	}
}

func TestUIProjectAboutStats(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-stats")
	todoIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "about stats todo",
		AssigneeID: &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue todo: %v", err)
	}
	_ = todoIssue
	doneIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "about stats done",
		AssigneeID: &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue done: %v", err)
	}
	done := model.StatusDone
	if _, err := e.store.UpdateIssue(e.ctx, doneIssue.ID, store.UpdateIssueParams{Status: &done}); err != nil {
		t.Fatalf("UpdateIssue done: %v", err)
	}

	body := e.uiGet(t, e.projectPath()+"/about", token)
	for _, want := range []string{"Issue stats", "All time", "Last 7 days", "Top assignees", "ui-stats", ">2</td>", ">1</td>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("project about stats missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "No assigned issues.") {
		t.Fatalf("project about stats rendered empty assignee state: %s", body)
	}
}

func TestUITokensPageCreatesAndRevokesToken(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-work")

	activeSprint, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Personal Active Sprint",
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
		Name:      "Personal Planned Sprint",
		StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint planned: %v", err)
	}

	activeTodoP0, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "active assigned todo p0",
		Priority:   model.PriorityP0,
		AssigneeID: &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue active todo: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, activeTodoP0.ID, store.UpdateIssueParams{SprintID: &activeSprint.ID}); err != nil {
		t.Fatalf("assign active todo: %v", err)
	}
	activeDoneP1, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "active assigned done p1",
		Priority:   model.PriorityP1,
		AssigneeID: &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue active done: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, activeDoneP1.ID, store.UpdateIssueParams{SprintID: &activeSprint.ID}); err != nil {
		t.Fatalf("assign active done: %v", err)
	}
	done := model.StatusDone
	if _, err := e.store.UpdateIssue(e.ctx, activeDoneP1.ID, store.UpdateIssueParams{Status: &done}); err != nil {
		t.Fatalf("set active done: %v", err)
	}
	activeUnassigned, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "active unassigned issue",
		Priority:  model.PriorityP0,
	})
	if err != nil {
		t.Fatalf("CreateIssue active unassigned: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, activeUnassigned.ID, store.UpdateIssueParams{SprintID: &activeSprint.ID}); err != nil {
		t.Fatalf("assign active unassigned: %v", err)
	}
	plannedAssigned, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "planned assigned issue",
		AssigneeID: &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue planned assigned: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, plannedAssigned.ID, store.UpdateIssueParams{SprintID: &plannedSprint.ID}); err != nil {
		t.Fatalf("assign planned: %v", err)
	}
	backlogAssigned, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "backlog assigned issue",
		AssigneeID: &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue backlog assigned: %v", err)
	}
	parent, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "parent with child"})
	if err != nil {
		t.Fatalf("CreateIssue parent: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, parent.ID, store.UpdateIssueParams{SprintID: &activeSprint.ID}); err != nil {
		t.Fatalf("assign parent active: %v", err)
	}
	child, err := e.store.CreateSubIssue(e.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "assigned child issue",
		AssigneeID:    &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateSubIssue assigned child: %v", err)
	}
	otherProject, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "Other Personal Project", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	if _, err := e.store.GrantProjectAccess(e.ctx, otherProject.ID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess other: %v", err)
	}
	otherActive, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: otherProject.ID,
		Name:      "Other Active Sprint",
		StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint other active: %v", err)
	}
	if _, err := e.store.UpdateSprint(e.ctx, otherActive.ID, store.UpdateSprintParams{Status: &active}); err != nil {
		t.Fatalf("UpdateSprint other active: %v", err)
	}
	otherP0, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  otherProject.ID,
		Title:      "other project active p0",
		Priority:   model.PriorityP0,
		AssigneeID: &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue other active: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, otherP0.ID, store.UpdateIssueParams{SprintID: &otherActive.ID}); err != nil {
		t.Fatalf("assign other active: %v", err)
	}

	meBody := e.uiGet(t, "/me", token)
	for _, want := range []string{"Active Sprints", "All", "Issue controls", "active assigned todo p0", "active assigned done p1", "other project active p0"} {
		if !strings.Contains(meBody, want) {
			t.Fatalf("me body missing %q: %s", want, meBody)
		}
	}
	for _, notWant := range []string{activeUnassigned.Title, plannedAssigned.Title, backlogAssigned.Title, child.Title} {
		if strings.Contains(meBody, notWant) {
			t.Fatalf("me body included %q: %s", notWant, meBody)
		}
	}

	allBody := e.uiGet(t, "/me/all", token)
	for _, want := range []string{"All assigned issues", activeTodoP0.Title, activeDoneP1.Title, plannedAssigned.Title, backlogAssigned.Title, child.Title, otherP0.Title} {
		if !strings.Contains(allBody, want) {
			t.Fatalf("me all body missing %q: %s", want, allBody)
		}
	}
	if strings.Contains(allBody, activeUnassigned.Title) {
		t.Fatalf("me all body included unassigned issue: %s", allBody)
	}

	filteredActive := e.uiGet(t, "/me?status=done&priority=P1", token)
	if !strings.Contains(filteredActive, "active assigned done p1") {
		t.Fatalf("filtered active missing done p1: %s", filteredActive)
	}
	for _, notWant := range []string{activeTodoP0.Title, otherP0.Title, plannedAssigned.Title} {
		if strings.Contains(filteredActive, notWant) {
			t.Fatalf("filtered active included %q: %s", notWant, filteredActive)
		}
	}

	filteredAll := e.uiGet(t, "/me/all?status=todo&priority=P0", token)
	for _, want := range []string{activeTodoP0.Title, otherP0.Title} {
		if !strings.Contains(filteredAll, want) {
			t.Fatalf("filtered all missing %q: %s", want, filteredAll)
		}
	}
	for _, notWant := range []string{activeDoneP1.Title, plannedAssigned.Title, backlogAssigned.Title, child.Title} {
		if strings.Contains(filteredAll, notWant) {
			t.Fatalf("filtered all included %q: %s", notWant, filteredAll)
		}
	}

	priorityBody := e.uiGet(t, "/me?sort=priority", token)
	otherIdx := strings.Index(priorityBody, "other project active p0")
	doneIdx := strings.Index(priorityBody, "active assigned done p1")
	if otherIdx < 0 || doneIdx < 0 || otherIdx > doneIdx {
		t.Fatalf("priority sort order wrong: other=%d done=%d body=%s", otherIdx, doneIdx, priorityBody)
	}
}

func TestUIRendersProjectSprintBoard(t *testing.T) {
	t.Parallel()
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
	earlyDue, err := model.ParseDate("2099-06-24")
	if err != nil {
		t.Fatalf("ParseDate early: %v", err)
	}
	lateDue, err := model.ParseDate("2099-06-26")
	if err != nil {
		t.Fatalf("ParseDate late: %v", err)
	}
	todo, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "board todo issue", AssigneeID: &user.ID, DueDate: &earlyDue})
	if err != nil {
		t.Fatalf("CreateIssue todo: %v", err)
	}
	laterTodo, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "board later todo issue", Priority: model.PriorityP1, DueDate: &lateDue})
	if err != nil {
		t.Fatalf("CreateIssue later todo: %v", err)
	}
	inProgress, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "board progress issue", Priority: model.PriorityP0})
	if err != nil {
		t.Fatalf("CreateIssue progress: %v", err)
	}
	closedIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "board closed issue"})
	if err != nil {
		t.Fatalf("CreateIssue closed: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, todo.ID, store.UpdateIssueParams{SprintID: &sp.ID}); err != nil {
		t.Fatalf("assign todo: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, laterTodo.ID, store.UpdateIssueParams{SprintID: &sp.ID}); err != nil {
		t.Fatalf("assign later todo: %v", err)
	}
	inProgressStatus := model.StatusInProgress
	if _, err := e.store.UpdateIssue(e.ctx, inProgress.ID, store.UpdateIssueParams{SprintID: &sp.ID, Status: &inProgressStatus}); err != nil {
		t.Fatalf("assign progress: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, closedIssue.ID, store.UpdateIssueParams{SprintID: &sp.ID}); err != nil {
		t.Fatalf("assign closed: %v", err)
	}
	closedStatus := model.StatusClosed
	closedReason := model.CloseReasonWontDo
	if _, err := e.store.UpdateIssue(e.ctx, closedIssue.ID, store.UpdateIssueParams{Status: &closedStatus, CloseReason: &closedReason}); err != nil {
		t.Fatalf("close issue: %v", err)
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
	for _, want := range []string{"Sprint", "To do", "In progress", "Done", "Closed", "board todo issue", "board later todo issue", "board progress issue", "board closed issue", "Board Sprint", "Focus current sprint goals\nShip board clarity", `aria-label="Assigned to ui-board"`, ">U</span>", `aria-label="Issue controls"`, "Status", "Priority", "Sort", "Direction", "Due date", "Asc", "Desc", `data-lucide="arrow-up"`, `data-lucide="arrow-down"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("sprint body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "other project sprint issue") || strings.Contains(body, "Active sprint issues across accessible projects.") {
		t.Fatalf("sprint body included wrong scope/copy: %s", body)
	}

	filteredBody := e.uiGet(t, e.projectPath()+"/sprint?status=in_progress&priority=P0", token)
	if !strings.Contains(filteredBody, "board progress issue") {
		t.Fatalf("filtered sprint missing progress issue: %s", filteredBody)
	}
	for _, notWant := range []string{"board todo issue", "board later todo issue", "board closed issue"} {
		if strings.Contains(filteredBody, notWant) {
			t.Fatalf("filtered sprint included %q: %s", notWant, filteredBody)
		}
	}

	dueDescBody := e.uiGet(t, e.projectPath()+"/sprint?sort=due&direction=desc", token)
	laterIdx := strings.Index(dueDescBody, "board later todo issue")
	earlyIdx := strings.Index(dueDescBody, "board todo issue")
	if laterIdx < 0 || earlyIdx < 0 || laterIdx > earlyIdx {
		t.Fatalf("due desc sprint order wrong: later=%d early=%d body=%s", laterIdx, earlyIdx, dueDescBody)
	}
}

func TestUIProjectAssigneeFilterAppliesAcrossProjectSections(t *testing.T) {
	t.Parallel()
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
	doneStatus := model.StatusDone
	if _, err := e.store.UpdateIssue(e.ctx, bobBacklog.ID, store.UpdateIssueParams{Status: &doneStatus}); err != nil {
		t.Fatalf("set bob backlog done: %v", err)
	}

	aliceQuery := "?assignee_id=" + alice.ID.String()
	body := e.uiGet(t, e.projectPath()+"/sprint"+aliceQuery, token)
	for _, want := range []string{
		`aria-label="Issue controls"`,
		`aria-label="Toggle Alice Filter"`,
		`aria-label="Toggle Bob Filter"`,
		`aria-pressed="true"`,
		"AF",
		"BF",
		"alice sprint issue",
		`href="` + e.projectPath() + `/planned"`,
		`href="` + e.projectPath() + `/all"`,
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

	body = e.uiGet(t, e.projectPath()+"/all"+aliceQuery, token)
	for _, want := range []string{"alice sprint issue", "alice planned issue", "alice backlog issue"} {
		if !strings.Contains(body, want) {
			t.Fatalf("filtered all issues missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{"bob sprint issue", "bob planned issue", "bob backlog issue", "unassigned sprint issue"} {
		if strings.Contains(body, notWant) {
			t.Fatalf("filtered all issues included %q: %s", notWant, body)
		}
	}

	multiAllQuery := "?assignee_id=" + alice.ID.String() + "&assignee_id=" + bob.ID.String() + "&status=todo&status=done"
	body = e.uiGet(t, e.projectPath()+"/all"+multiAllQuery, token)
	for _, want := range []string{
		`aria-label="Issue controls"`,
		`aria-pressed="true"`,
		"alice sprint issue",
		"bob sprint issue",
		"alice planned issue",
		"bob planned issue",
		"alice backlog issue",
		"bob backlog issue",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("multi-filter all issues missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "unassigned sprint issue") {
		t.Fatalf("multi-filter all issues included unassigned issue: %s", body)
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

func TestUIRendersProjectPlannedAndAll(t *testing.T) {
	t.Parallel()
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

	body := e.uiGet(t, e.projectPath()+"/planned", token)
	for _, want := range []string{"Planned", "Second Planned Sprint", "First Planned Sprint", "scheduled second issue", "scheduled first issue"} {
		if !strings.Contains(body, want) {
			t.Fatalf("planned body missing %q: %s", want, body)
		}
	}
	secondIdx := strings.Index(body, "scheduled second issue")
	firstIdx := strings.Index(body, "scheduled first issue")
	if secondIdx < 0 || firstIdx < 0 || secondIdx > firstIdx {
		t.Fatalf("planned order wrong: second=%d first=%d body=%s", secondIdx, firstIdx, body)
	}
	if strings.Contains(body, backlogIssue.Title) {
		t.Fatalf("planned body included unscheduled issue: %s", body)
	}
	if strings.Contains(body, "other project backlog issue") || strings.Contains(body, "other project planned issue") || strings.Contains(body, "Other Planned Sprint") || strings.Contains(body, "Backlog issues across accessible projects.") {
		t.Fatalf("planned body included wrong scope/copy: %s", body)
	}

	body = e.uiGet(t, e.projectPath()+"/all", token)
	for _, want := range []string{"All issues", "Issue controls", "Status", "Priority", "Sort", "Updated", "Any", backlogIssue.Title, "scheduled first issue", "scheduled second issue"} {
		if !strings.Contains(body, want) {
			t.Fatalf("all body missing %q: %s", want, body)
		}
	}
	backlogIdx := strings.Index(body, backlogIssue.Title)
	firstIdx = strings.Index(body, "scheduled first issue")
	secondIdx = strings.Index(body, "scheduled second issue")
	if backlogIdx < 0 || firstIdx < 0 || secondIdx < 0 || secondIdx > firstIdx || firstIdx > backlogIdx {
		t.Fatalf("all issue order wrong: backlog=%d first=%d second=%d body=%s", backlogIdx, firstIdx, secondIdx, body)
	}
	if strings.Contains(body, "Other Planned Sprint") || strings.Contains(body, "other project backlog issue") || strings.Contains(body, "other project planned issue") {
		t.Fatalf("all body included wrong scope: %s", body)
	}
}

func TestUIRendersIssueDetailPage(t *testing.T) {
	t.Parallel()
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
		`aria-label="Issue actions"`,
		`data-lucide="more-horizontal"`,
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
		`method="post" action="` + e.issuePath(issue) + `/delete"`,
		`hx-post="` + e.issuePath(issue) + `/delete"`,
		`hx-push-url="` + e.projectPath() + `/all"`,
		`hx-confirm="Delete this issue? You can undo it from the next screen."`,
		`Delete issue`,
		`data-lucide="trash-2"`,
		`text-rose-600`,
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
		`title="Issue actions"`,
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
	for _, notWant := range []string{`data-lucide="arrow-left"`, `href="` + e.projectPath() + `/all"`, `hx-get="` + e.projectPath() + `/all/panel"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("issue body should not render back button markup %q: %s", notWant, body)
		}
	}
	for _, notWant := range []string{`aria-label="Edit status"`, ">Status</dt>"} {
		if strings.Contains(body, notWant) {
			t.Fatalf("issue body still renders separate status edit affordance %q: %s", notWant, body)
		}
	}
	for _, notWant := range []string{`/archive`, `Archive issue`, `data-lucide="archive"`, `data-lucide="settings"`} {
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
	t.Parallel()
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
		`data-option-dropdown-toggle`,
		`data-option-dropdown-list`,
		`option-dropdown-enter`,
		`role="listbox" aria-label="Issue status"`,
		`method="post" action="` + e.issuePath(issue) + `/status"`,
		`hx-post="` + e.issuePath(issue) + `/status"`,
		`hx-push-url="false"`,
		`name="status" value="todo"`,
		`name="status" value="in_progress"`,
		`name="status" value="done"`,
		`name="status" value="closed"`,
		`hx-get="` + e.issuePath(issue) + `/panel"`,
		"To do",
		"In progress",
		"Done",
		"Closed",
	} {
		if !strings.Contains(edit, want) {
			t.Fatalf("status edit response missing %q: %s", want, edit)
		}
	}
	if strings.Contains(edit, `aria-label="Cancel status change"`) ||
		strings.Contains(edit, `disabled aria-label="Change status"`) ||
		strings.Contains(edit, `cursor-default`) ||
		strings.Contains(edit, `title="Change status"`) ||
		strings.Contains(edit, `title="Cancel status change"`) {
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

	closedStatus := url.Values{"status": {string(model.StatusClosed)}}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/status", token, strings.NewReader(closedStatus.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("closed status code = %d body = %s", res.StatusCode, body)
	}
	for _, want := range []string{
		`role="dialog" aria-modal="true"`,
		"Close issue",
		"Closed",
		"Close reason",
		`data-option-dropdown-toggle`,
		`data-option-dropdown-list`,
		`method="post" action="` + e.issuePath(issue) + `/close-reason"`,
		`name="close_reason"`,
		"Duplicate",
		"Won&#39;t Do",
		"Invalid",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("closed status response missing %q: %s", want, body)
		}
	}
	modalEnd := strings.Index(body, `<section class="grid gap-6`)
	if modalEnd < 0 {
		t.Fatalf("closed status response missing issue detail section: %s", body)
	}
	if strings.Contains(body[:modalEnd], "Missing reason") {
		t.Fatalf("closed status response rendered modal missing reason indicator: %s", body[:modalEnd])
	}
	if !strings.Contains(body[modalEnd:], "Missing reason") {
		t.Fatalf("closed status response missing detail-panel missing reason indicator: %s", body[modalEnd:])
	}
	if strings.Contains(body, ">Reason</option>") || strings.Contains(body, `aria-expanded="false"`) && strings.Contains(body, "To do") {
		t.Fatalf("closed status response kept confusing pending close UI: %s", body)
	}
	updated, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after closed status: %v", err)
	}
	if updated.Status != model.StatusInProgress || updated.CloseReason != nil {
		t.Fatalf("closed status picker changed issue = status %q reason %v, want in_progress/no reason", updated.Status, updated.CloseReason)
	}

	closeReason := url.Values{"close_reason": {string(model.CloseReasonInvalid)}}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/close-reason", token, strings.NewReader(closeReason.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("close reason code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Closed") || !strings.Contains(body, "Invalid") || strings.Contains(body, `name="close_reason"`) {
		t.Fatalf("close reason response did not return read mode with reason: %s", body)
	}
	updated, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after close reason: %v", err)
	}
	if updated.Status != model.StatusClosed || updated.CloseReason == nil || *updated.CloseReason != model.CloseReasonInvalid {
		t.Fatalf("closed issue = status %q reason %v, want closed/invalid", updated.Status, updated.CloseReason)
	}
}

func TestUIEditCloseReasonUpdatesAndReopenHidesReason(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-close-reason")
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "close reason target",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	closed := model.StatusClosed
	initialReason := model.CloseReasonWontDo
	issue, err = e.store.UpdateIssue(e.ctx, issue.ID, store.UpdateIssueParams{
		Status:      &closed,
		CloseReason: &initialReason,
	})
	if err != nil {
		t.Fatalf("close issue: %v", err)
	}

	body := e.uiGet(t, e.issuePath(issue), token)
	for _, want := range []string{
		"close reason target",
		"Close reason",
		"W",
		`hx-get="` + e.issuePath(issue) + `/close-reason/edit"`,
		`aria-label="Edit close reason"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("closed issue detail missing %q: %s", want, body)
		}
	}
	if !strings.Contains(body, "Won&#39;t Do") && !strings.Contains(body, "Won't Do") {
		t.Fatalf("closed issue detail missing Won't Do label: %s", body)
	}

	edit := e.uiGet(t, e.issuePath(issue)+"/close-reason/edit", token)
	for _, want := range []string{
		`method="post" action="` + e.issuePath(issue) + `/close-reason"`,
		`hx-post="` + e.issuePath(issue) + `/close-reason"`,
		`data-option-dropdown-toggle`,
		`data-option-dropdown-list`,
		`name="close_reason"`,
		`value="duplicate"`,
		`value="wont_do" role="option" aria-selected="true"`,
		`value="invalid"`,
		`aria-label="Choose close reason"`,
		`data-lucide="check"`,
	} {
		if !strings.Contains(edit, want) {
			t.Fatalf("close reason edit missing %q: %s", want, edit)
		}
	}

	res := e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/close-reason", token, strings.NewReader(url.Values{"close_reason": {"bogus"}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("bad close reason code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Choose a close reason.") {
		t.Fatalf("bad close reason response missing validation error: %s", body)
	}
	updated, err := e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after bad reason: %v", err)
	}
	if updated.CloseReason == nil || *updated.CloseReason != model.CloseReasonWontDo {
		t.Fatalf("bad reason changed close reason = %v, want wont_do", updated.CloseReason)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/close-reason", token, strings.NewReader(url.Values{"close_reason": {string(model.CloseReasonDuplicate)}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update close reason code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Duplicate") || strings.Contains(body, `name="close_reason"`) {
		t.Fatalf("close reason update response did not return read mode: %s", body)
	}
	updated, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after reason update: %v", err)
	}
	if updated.CloseReason == nil || *updated.CloseReason != model.CloseReasonDuplicate {
		t.Fatalf("updated close reason = %v, want duplicate", updated.CloseReason)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/status", token, strings.NewReader(url.Values{"status": {string(model.StatusInProgress)}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reopen code = %d body = %s", res.StatusCode, body)
	}
	if strings.Contains(body, "Close reason") || strings.Contains(body, "Duplicate") {
		t.Fatalf("reopened response still rendered close reason: %s", body)
	}
	updated, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after reopen: %v", err)
	}
	if updated.Status != model.StatusInProgress || updated.CloseReason != nil {
		t.Fatalf("reopened issue = status %q reason %v, want in_progress/nil", updated.Status, updated.CloseReason)
	}
}

func TestUIDeleteIssueReturnsBackTarget(t *testing.T) {
	t.Parallel()
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
	allNotice := e.projectPath() + "/all?deleted_issue=" + url.QueryEscape(issue.Identifier)
	if loc := res.Header.Get("Location"); loc != allNotice {
		t.Fatalf("delete Location = %q", loc)
	}
	if _, err := e.store.GetIssue(e.ctx, issue.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetIssue deleted err = %v, want ErrNotFound", err)
	}
	body := e.uiGet(t, allNotice, token)
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
	t.Parallel()
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
	for _, notWant := range []string{`data-lucide="arrow-left"`, `href="` + e.projectPath() + `/deleted"`, `hx-get="` + e.projectPath() + `/deleted/panel"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("deleted issue page should not render back button markup %q: %s", notWant, body)
		}
	}
	for _, notWant := range []string{"not found", "deleted description should stay hidden", "Comments", "Sub-issues", `aria-label="Issue actions"`} {
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
	t.Parallel()
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

	projectBody := e.uiGet(t, e.projectPath()+"/planned", token)
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
	for _, notWant := range []string{`data-lucide="arrow-left"`, `href="` + e.projectPath() + `/sprint"`, `hx-get="` + e.projectPath() + `/sprint/panel"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("deleted body should not render back button markup %q: %s", notWant, body)
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
	if strings.Contains(body, "<!doctype html>") || !strings.Contains(body, child.Title) || !strings.Contains(body, "Sub-issue of") || !strings.Contains(body, parent.Title) || !strings.Contains(body, `aria-label="Issue actions"`) {
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
	t.Parallel()
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
		`hx-push-url="false"`,
		`name="priority" value="P0"`,
		`name="priority" value="P1"`,
		`name="priority" value="P2"`,
		`name="priority" value="P3"`,
		`name="priority" value="P4"`,
		`hx-get="` + e.issuePath(issue) + `/panel"`,
		`aria-label="Priority P3"`,
		`bg-yellow-500`,
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
		strings.Contains(edit, `aria-label="Cancel priority change"`) ||
		strings.Contains(edit, `data-lucide="x"`) ||
		strings.Contains(edit, `data-lucide="arrow-left"`) ||
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
	t.Parallel()
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
		`hx-push-url="false"`,
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
	res := e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.issuePath(issue)+"/description", token, strings.NewReader(form.Encode()), map[string]string{
		"HX-Current-URL": e.ts.URL + e.issuePath(issue),
		"HX-Request":     "true",
	})
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update description code = %d body = %s", res.StatusCode, body)
	}
	if push := res.Header.Get("HX-Push-Url"); push != "" {
		t.Fatalf("description update HX-Push-Url = %q, want empty", push)
	}
	if replace := res.Header.Get("HX-Replace-Url"); replace != "" {
		t.Fatalf("description update HX-Replace-Url = %q, want empty", replace)
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
	t.Parallel()
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
		`hx-push-url="false"`,
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
		`hx-push-url="false"`,
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
	t.Parallel()
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
		`hx-push-url="false"`,
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
	if !strings.Contains(body, `class="min-w-0 truncate text-slate-900 dark:text-slate-100">None</span>`) {
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
	t.Parallel()
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

	closedIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "closed sprint issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue closed: %v", err)
	}
	closed := model.StatusClosed
	closedReason := model.CloseReasonWontDo
	if _, err := e.store.UpdateIssue(e.ctx, closedIssue.ID, store.UpdateIssueParams{SprintID: &current.ID}); err != nil {
		t.Fatalf("assign closed current: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, closedIssue.ID, store.UpdateIssueParams{Status: &closed, CloseReason: &closedReason}); err != nil {
		t.Fatalf("mark closed: %v", err)
	}
	body = e.uiGet(t, e.issuePath(closedIssue), token)
	if !strings.Contains(body, "Current Sprint") || !strings.Contains(body, "cursor-not-allowed") || strings.Contains(body, `/sprint/edit`) {
		t.Fatalf("closed detail did not render read-only sprint: %s", body)
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(closedIssue)+"/sprint", token, strings.NewReader(url.Values{"sprint": {next.Ref}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("closed sprint post code = %d body = %s", res.StatusCode, body)
	}
	updated, err = e.store.GetIssue(e.ctx, closedIssue.ID)
	if err != nil {
		t.Fatalf("GetIssue closed after rejected post: %v", err)
	}
	if updated.SprintID == nil || *updated.SprintID != current.ID {
		t.Fatalf("closed SprintID = %v, want %s", updated.SprintID, current.ID)
	}
}

func TestUIAddEditAndRemoveIssueLinks(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-links")
	targetDue, err := model.ParseDate("2099-06-24")
	if err != nil {
		t.Fatalf("ParseDate target: %v", err)
	}
	replacementDue, err := model.ParseDate("2099-06-26")
	if err != nil {
		t.Fatalf("ParseDate replacement: %v", err)
	}
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "link ui source"})
	if err != nil {
		t.Fatalf("CreateIssue source: %v", err)
	}
	target, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "link ui target", DueDate: &targetDue})
	if err != nil {
		t.Fatalf("CreateIssue target: %v", err)
	}
	replacement, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "link ui replacement", DueDate: &replacementDue})
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
	if !strings.Contains(body, "Blocked by") || !strings.Contains(body, "link ui target") || !strings.Contains(body, "Jun 24") || strings.Contains(body, `name="target_issue"`) {
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
	if !strings.Contains(body, "Clones") || !strings.Contains(body, "link ui replacement") || !strings.Contains(body, "Jun 26") || strings.Contains(body, "link ui target") {
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
	for _, want := range []string{
		">Linked issues</h2>",
		`aria-label="Linked issue progress: no linked issues"`,
		`aria-label="Add link"`,
		"w-full sm:w-1/3",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("delete link response missing empty link state %q: %s", want, body)
		}
	}
	for _, notWant := range []string{"No linked issues.", "link ui replacement"} {
		if strings.Contains(body, notWant) {
			t.Fatalf("delete link response included stale linked issue state %q: %s", notWant, body)
		}
	}
	if _, err := e.store.GetIssueLink(e.ctx, link.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetIssueLink after delete err = %v, want ErrNotFound", err)
	}
}

func TestUICreateCommentPostsAndRerendersIssuePanel(t *testing.T) {
	t.Parallel()
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
	composerStart := strings.Index(body, `placeholder="Add a comment"`)
	firstCommentStart := strings.Index(body, "new ui comment")
	if composerStart < 0 || firstCommentStart < 0 || composerStart > firstCommentStart {
		t.Fatalf("comment composer should render above comments: %s", body)
	}
	comments, _, err := e.store.ListCommentsForIssue(e.ctx, store.ListCommentsForIssueParams{IssueID: issue.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListCommentsForIssue: %v", err)
	}
	if len(comments) != 1 || comments[0].Body != "new ui comment" || comments[0].AuthorID != user.ID {
		t.Fatalf("comments = %+v, want one new comment by %s", comments, user.ID)
	}

	second := url.Values{"body": {"second ui comment"}}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueCommentsPath(issue), token, strings.NewReader(second.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("second code = %d body = %s", res.StatusCode, body)
	}
	secondCommentStart := strings.Index(body, "second ui comment")
	firstCommentStart = strings.Index(body, "new ui comment")
	composerStart = strings.Index(body, `placeholder="Add a comment"`)
	if composerStart < 0 || secondCommentStart < 0 || firstCommentStart < 0 || composerStart > secondCommentStart || secondCommentStart > firstCommentStart {
		t.Fatalf("comments should render newest-first below the composer: %s", body)
	}
	comments, _, err = e.store.ListCommentsForIssue(e.ctx, store.ListCommentsForIssueParams{IssueID: issue.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListCommentsForIssue after second comment: %v", err)
	}
	if len(comments) != 2 || comments[0].Body != "new ui comment" || comments[1].Body != "second ui comment" {
		t.Fatalf("store comments = %+v, want API/store default oldest-first", comments)
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
	if len(comments) != 2 {
		t.Fatalf("empty comment should not create a row, comments = %+v", comments)
	}
}

func TestUIEditCommentAuthorOnly(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	author, authorToken := e.mustProjectMemberToken(t, "ui-comment-author")
	_, otherToken := e.mustProjectMemberToken(t, "ui-comment-other")
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "editable comment issue"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	comment, err := e.store.CreateComment(e.ctx, store.CreateCommentParams{
		IssueID:  issue.ID,
		AuthorID: author.ID,
		Body:     "original ui comment",
	})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	commentPath := e.issueCommentsPath(issue) + "/" + comment.Ref
	editPath := commentPath + "/edit"

	body := e.uiGet(t, e.issuePath(issue), authorToken)
	for _, want := range []string{
		"original ui comment",
		`aria-label="Edit comment"`,
		`hx-get="` + editPath + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("author issue body missing %q: %s", want, body)
		}
	}
	otherBody := e.uiGet(t, e.issuePath(issue), otherToken)
	for _, notWant := range []string{`aria-label="Edit comment"`, `hx-get="` + editPath + `"`} {
		if strings.Contains(otherBody, notWant) {
			t.Fatalf("non-author issue body included edit control %q: %s", notWant, otherBody)
		}
	}

	edit := e.uiGet(t, editPath, authorToken)
	for _, want := range []string{
		`method="post" action="` + commentPath + `"`,
		`hx-post="` + commentPath + `"`,
		`<textarea name="body" rows="2"`,
		"original ui comment",
		`aria-label="Save comment"`,
		`aria-label="Cancel editing comment"`,
		"⌘ + Enter to save",
	} {
		if !strings.Contains(edit, want) {
			t.Fatalf("edit response missing %q: %s", want, edit)
		}
	}

	empty := url.Values{"body": {"   "}}
	res := e.uiDoNoRedirect(t, http.MethodPost, commentPath, authorToken, strings.NewReader(empty.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("empty edit code = %d body = %s", res.StatusCode, body)
	}
	for _, want := range []string{"Comment required, max 10000 chars.", `aria-label="Save comment"`, ">   </textarea>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("empty edit response missing %q: %s", want, body)
		}
	}

	form := url.Values{"body": {"edited ui comment"}}
	res = e.uiDoNoRedirect(t, http.MethodPost, commentPath, authorToken, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("edit code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "edited ui comment") || !strings.Contains(body, ">•</span>") || !strings.Contains(body, "Edited ") || strings.Contains(body, "original ui comment") || strings.Contains(body, `aria-label="Save comment"`) {
		t.Fatalf("edit response did not return edited read mode: %s", body)
	}
	updated, err := e.store.GetComment(e.ctx, comment.ID)
	if err != nil {
		t.Fatalf("GetComment after edit: %v", err)
	}
	if updated.Body != "edited ui comment" || updated.EditedAt == nil {
		t.Fatalf("updated comment = %+v, want edited body and edited_at", updated)
	}

	res = e.uiDoNoRedirect(t, http.MethodGet, editPath, otherToken, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("non-author edit GET code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, commentPath, otherToken, strings.NewReader(url.Values{"body": {"not yours"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("non-author edit POST code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	got, err := e.store.GetComment(e.ctx, comment.ID)
	if err != nil {
		t.Fatalf("GetComment after non-author edit: %v", err)
	}
	if got.Body != "edited ui comment" {
		t.Fatalf("non-author changed body to %q", got.Body)
	}
}

func TestUICreateSubIssuePostsTitleOnlyAndRerendersIssuePanel(t *testing.T) {
	t.Parallel()
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
	childDue, err := model.ParseDate("2099-06-25")
	if err != nil {
		t.Fatalf("ParseDate child due: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, children[0].ID, store.UpdateIssueParams{DueDate: &childDue}); err != nil {
		t.Fatalf("UpdateIssue child due date: %v", err)
	}
	body = e.uiGet(t, e.issuePath(parent), token)
	if !strings.Contains(body, "new child from ui") || !strings.Contains(body, "Jun 25") {
		t.Fatalf("parent issue panel missing sub-issue due date: %s", body)
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
	t.Parallel()
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
	t.Parallel()
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
	comment, err := e.store.CreateComment(e.ctx, store.CreateCommentParams{
		IssueID:  issue.ID,
		AuthorID: e.adminID,
		Body:     "protected comment",
	})
	if err != nil {
		t.Fatalf("CreateComment protected: %v", err)
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
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issueCommentsPath(issue)+"/"+comment.Ref+"/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue comment edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueCommentsPath(issue)+"/"+comment.Ref, token, strings.NewReader(url.Values{"body": {"denied edit"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue comment update code = %d body = %s", res.StatusCode, readBody(t, res))
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
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/close-reason/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue close reason edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/close-reason", token, strings.NewReader(url.Values{"close_reason": {string(model.CloseReasonInvalid)}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue close reason update code = %d body = %s", res.StatusCode, readBody(t, res))
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
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-issue-links")
	dueDate, err := model.ParseDate("2099-06-24")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "linked from lists",
		AssigneeID: &user.ID,
		DueDate:    &dueDate,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	sp, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Linked Active Sprint",
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
	if _, err := e.store.UpdateIssue(e.ctx, issue.ID, store.UpdateIssueParams{SprintID: &sp.ID}); err != nil {
		t.Fatalf("assign sprint: %v", err)
	}
	wantHref := `href="` + e.issuePath(issue) + `"`
	wantHXGet := `hx-get="` + e.issuePath(issue) + `/panel"`
	wantHXPush := `hx-push-url="` + e.issuePath(issue) + `"`

	for _, path := range []string{e.projectPath() + "/all", "/me", "/me/all"} {
		body := e.uiGet(t, path, token)
		for _, want := range []string{wantHref, wantHXGet, wantHXPush, `data-main-view="projects"`} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing issue link %q: %s", path, want, body)
			}
		}
		if !strings.Contains(body, "Jun 24") {
			t.Fatalf("%s missing due date badge: %s", path, body)
		}
	}
}

func TestUIIssueDueDateEditorRoundtrip(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-due-date")
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "due ui issue"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	path := e.issuePath(issue) + "/due-date"

	edit := e.uiGet(t, path+"/edit", token)
	for _, want := range []string{
		"due ui issue",
		`method="post" action="` + path + `"`,
		`hx-post="` + path + `"`,
		`type="date" name="due_date" value=""`,
		`aria-label="Save due date"`,
	} {
		if !strings.Contains(edit, want) {
			t.Fatalf("due date edit missing %q: %s", want, edit)
		}
	}

	form := url.Values{"due_date": {"2099-06-24"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, path, token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("set due date code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Jun 24") || !strings.Contains(body, `aria-label="Edit due date"`) {
		t.Fatalf("set due date response missing read mode: %s", body)
	}
	got, err := e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after set due date: %v", err)
	}
	if got.DueDate == nil || got.DueDate.String() != "2099-06-24" {
		t.Fatalf("stored DueDate = %v", got.DueDate)
	}

	bad := url.Values{"due_date": {"tomorrow"}}
	res = e.uiDoNoRedirect(t, http.MethodPost, path, token, strings.NewReader(bad.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Use YYYY-MM-DD.") || !strings.Contains(body, `value="tomorrow"`) {
		t.Fatalf("bad due date response = %d %s", res.StatusCode, body)
	}

	clear := url.Values{"due_date": {""}}
	res = e.uiDoNoRedirect(t, http.MethodPost, path, token, strings.NewReader(clear.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("clear due date code = %d body = %s", res.StatusCode, body)
	}
	dueIndex := strings.Index(body, ">Due date</dt>")
	if dueIndex < 0 {
		t.Fatalf("clear due date response missing due date row: %s", body)
	}
	dueEnd := dueIndex + 500
	if dueEnd > len(body) {
		dueEnd = len(body)
	}
	dueBlock := body[dueIndex:dueEnd]
	if !strings.Contains(dueBlock, "None") || strings.Contains(dueBlock, "Jun 24") {
		t.Fatalf("clear due date response missing empty state: %s", body)
	}
	got, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after clear due date: %v", err)
	}
	if got.DueDate != nil {
		t.Fatalf("cleared DueDate = %v, want nil", got.DueDate)
	}
}

func TestUIProjectRoutesRedirectAndRejectOldGlobals(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustUserToken(t, "ui-no-project")

	for _, path := range []string{
		e.projectPath() + "/about",
		e.projectPath() + "/about/panel",
		e.projectPath() + "/sprint",
		e.projectPath() + "/sprint/panel",
		e.projectPath() + "/planned",
		e.projectPath() + "/planned/panel",
		e.projectPath() + "/all",
		e.projectPath() + "/all/panel",
		e.projectPath() + "/all/page",
		e.projectPath() + "/backlog",
		e.projectPath() + "/backlog/panel",
		e.projectPath() + "/issues/new",
		e.projectPath() + "/issues/new/panel",
		e.projectPath() + "/deleted",
		e.projectPath() + "/deleted/panel",
	} {
		res := e.uiDoNoRedirect(t, http.MethodGet, path, token, nil)
		defer res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("%s code = %d body = %s", path, res.StatusCode, readBody(t, res))
		}
	}
	res := e.uiDoNoRedirect(t, http.MethodPost, e.projectPath()+"/issues", token, strings.NewReader(url.Values{"title": {"denied"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("create issue denied code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, "/issues", token, strings.NewReader(url.Values{"project_id": {e.projectID.String()}, "title": {"denied"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("global create issue denied code = %d body = %s", res.StatusCode, readBody(t, res))
	}

	res = e.uiDoNoRedirect(t, http.MethodGet, e.projectPath()+"/issues/new", "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauth new issue code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/login?next="+url.QueryEscape(e.projectPath()+"/issues/new") {
		t.Fatalf("new issue Location = %q", loc)
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, "/issues/new", "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauth global new issue code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/login?next="+url.QueryEscape("/issues/new") {
		t.Fatalf("global new issue Location = %q", loc)
	}
}

func TestUIRendersProjectSprintEmptyState(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-empty")
	body := e.uiGet(t, e.projectPath()+"/sprint", token)
	if !strings.Contains(body, "No active sprint.") {
		t.Fatalf("body missing no-active-sprint state: %s", body)
	}
}

func TestUIProjectSprintDoesNotIncludeBacklog(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-sprint")
	dueDate, err := model.ParseDate("2099-06-24")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
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
	inSprint, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "issue inside active sprint", DueDate: &dueDate})
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
	if !strings.Contains(body, "Jun 24") {
		t.Fatalf("sprint body missing issue due date: %s", body)
	}
	if strings.Contains(body, "issue still in backlog") {
		t.Fatalf("sprint body included backlog issue: %s", body)
	}
}

func TestUILogoutClearsCookie(t *testing.T) {
	t.Parallel()
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

func (e *httpEnv) uiDoMultipartContext(t *testing.T, path, token string, fields map[string]string, filename, content string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return e.uiDoNoRedirectWithHeaders(t, http.MethodPost, path, token, &buf, map[string]string{
		"Content-Type": writer.FormDataContentType(),
	})
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

func issueContextDetailBlock(t *testing.T, body string) string {
	t.Helper()
	contextLabel := strings.Index(body, ">Context</dt>")
	if contextLabel < 0 {
		t.Fatalf("missing issue context detail row: %s", body)
	}
	blockEnd := contextLabel + 1100
	if blockEnd > len(body) {
		blockEnd = len(body)
	}
	return body[contextLabel:blockEnd]
}

func mainContentBlock(t *testing.T, body string) string {
	t.Helper()
	mainStart := strings.Index(body, `<main id="main"`)
	if mainStart < 0 {
		t.Fatalf("missing main content: %s", body)
	}
	contentStart := strings.Index(body[mainStart:], ">")
	if contentStart < 0 {
		t.Fatalf("malformed main content: %s", body)
	}
	contentStart += mainStart + 1
	contentEnd := strings.Index(body[contentStart:], "</main>")
	if contentEnd < 0 {
		t.Fatalf("missing main content end: %s", body)
	}
	return body[contentStart : contentStart+contentEnd]
}

func TestUIHomeRedirectsToFirstProject(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
