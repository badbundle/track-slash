package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
)

// Public projects are link-only: anyone with the link can open one, and file
// into it when it takes public issues, but lists name it only to its owner and
// members. Site admins get the same lists as everyone else.
func TestLinkOnlyPublicProjects(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	owner, ownerToken, project := e.mustOwnedProject(t, "link-only-owner", "Link Only")
	projectPath := uiProjectPathFor(project)
	code, body := e.doWithToken(t, ownerToken, http.MethodPatch, projectPath+"/access", map[string]any{"access_mode": "public_issues"})
	if code != http.StatusOK {
		t.Fatalf("enable public issues code = %d body = %s", code, body)
	}
	readonly, readonlyToken := e.mustUserToken(t, "link-only-readonly")
	if _, err := e.store.SetProjectMemberRole(e.ctx, project.ID, readonly.ID, model.ProjectMemberRoleReadonly); err != nil {
		t.Fatalf("SetProjectMemberRole readonly: %v", err)
	}
	_, outsiderToken := e.mustUserToken(t, "link-only-outsider")
	adminToken := e.authToken

	// REST and MCP lists.
	for name, tc := range map[string]struct {
		token string
		want  bool
	}{
		"owner":     {token: ownerToken, want: true},
		"read-only": {token: readonlyToken, want: true},
		"outsider":  {token: outsiderToken},
		"admin":     {token: adminToken},
		"anonymous": {},
	} {
		code, body := e.doWithToken(t, tc.token, http.MethodGet, "/projects", nil)
		if code != http.StatusOK {
			t.Fatalf("%s list code = %d body = %s", name, code, body)
		}
		if got := projectResponseInPage(decodePage[projectResponseDecoded](t, body).Items, project.ID.String()); got != tc.want {
			t.Fatalf("%s REST list includes public project = %v, want %v", name, got, tc.want)
		}
		if tc.token == "" {
			continue
		}
		session := mcpConnect(t, e, tc.token)
		listed := decodeMCPField[[]model.Project](t, mcpCall(t, e, session, "track_list_projects", map[string]any{}), "items")
		got := false
		for _, item := range listed {
			got = got || item.ID == project.ID
		}
		if got != tc.want {
			t.Fatalf("%s MCP list includes public project = %v, want %v", name, got, tc.want)
		}
	}

	// The link still opens it, and an outsider can still file into it.
	for name, token := range map[string]string{"outsider": outsiderToken, "admin": adminToken, "anonymous": ""} {
		if code, body := e.doWithToken(t, token, http.MethodGet, projectPath, nil); code != http.StatusOK {
			t.Fatalf("%s REST get code = %d body = %s", name, code, body)
		}
	}
	outsiderSession := mcpConnect(t, e, outsiderToken)
	got := decodeMCPField[model.Project](t, mcpCall(t, e, outsiderSession, "track_get_project", map[string]any{"owner": owner.Username, "key": project.Key}), "project")
	if got.ID != project.ID {
		t.Fatalf("outsider MCP get project = %+v", got)
	}
	code, body = e.doWithToken(t, outsiderToken, http.MethodPost, projectPath+"/issues", map[string]any{"title": "Filed by link"})
	if code != http.StatusCreated {
		t.Fatalf("outsider REST issue code = %d body = %s", code, body)
	}
	for _, path := range []string{projectPath + "/all", projectPath + "/issues/new"} {
		if body := e.uiGet(t, path, outsiderToken); !strings.Contains(body, project.Key) {
			t.Fatalf("outsider UI %s missing project: %s", path, body)
		}
	}
	if body := e.uiGet(t, projectPath+"/all", adminToken); !strings.Contains(body, project.Key) {
		t.Fatalf("admin UI project page missing project: %s", body)
	}

	// UI lists and pickers.
	ownerPage := "/" + owner.Username + "/projects"
	listPaths := []string{"/projects", "/projects/panel", ownerPage, "/me", "/issues/new", "/issues/new/projects?project=" + project.Key}
	for name, tc := range map[string]struct {
		token string
		lists []string
	}{
		"owner":     {token: ownerToken, lists: listPaths},
		"read-only": {token: readonlyToken, lists: []string{"/projects", "/projects/panel", ownerPage}},
		"outsider":  {token: outsiderToken},
		"admin":     {token: adminToken},
	} {
		for _, path := range listPaths {
			want := false
			for _, listed := range tc.lists {
				want = want || listed == path
			}
			body := e.uiGet(t, path, tc.token)
			// "/me" names the projects only by count; "/issues/new" lists the
			// picker's options only once a search is typed.
			if path == "/me" || path == "/issues/new" {
				continue
			}
			if got := strings.Contains(body, project.Key); got != want {
				t.Fatalf("%s UI %s includes public project = %v, want %v: %s", name, path, got, want, body)
			}
		}
	}
	if body := e.uiGet(t, "/me", outsiderToken); !strings.Contains(body, ">0 projects<") {
		t.Fatalf("outsider Me counts link-only projects: %s", body)
	}
	if body := e.uiGet(t, "/issues/new/projects?project=", readonlyToken); strings.Contains(body, project.Key) {
		t.Fatalf("read-only member offered the project to file into: %s", body)
	}

	// "/" sends a user to their own first project, never to someone else's
	// public one.
	for name, tc := range map[string]struct {
		token string
		want  string
	}{
		"owner":    {token: ownerToken, want: projectPath + "/"},
		"outsider": {token: outsiderToken, want: "/projects"},
	} {
		res := e.uiDoNoRedirect(t, http.MethodGet, "/", tc.token, nil)
		res.Body.Close()
		if location := res.Header.Get("Location"); res.StatusCode != http.StatusSeeOther || !strings.HasPrefix(location, tc.want) {
			t.Fatalf("%s home redirect = %d %q, want %q", name, res.StatusCode, location, tc.want)
		}
	}
}
