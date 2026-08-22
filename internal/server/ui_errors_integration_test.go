package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Everything under /api/v1 is reached through the authenticated {owner} subtree,
// so the JSON 404 and 405 only become observable once a request authenticates.
func TestAPIErrorsAreJSONForAuthenticatedClients(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	for _, tt := range []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantError  string
	}{
		{
			name:       "unknown path under an owner",
			method:     http.MethodGet,
			path:       "/" + e.ownerUsername + "/bogus",
			wantStatus: http.StatusNotFound,
			wantError:  "not found",
		},
		{
			name:       "unknown path under a project",
			method:     http.MethodGet,
			path:       e.projectPath() + "/bogus",
			wantStatus: http.StatusNotFound,
			wantError:  "not found",
		},
		{
			name:       "wrong method on a known project path",
			method:     http.MethodDelete,
			path:       e.projectPath() + "/stats",
			wantStatus: http.StatusMethodNotAllowed,
			wantError:  "method not allowed",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, body := e.do(t, tt.method, tt.path, nil)
			if code != tt.wantStatus {
				t.Fatalf("%s %s code = %d body = %s", tt.method, tt.path, code, body)
			}
			var decoded struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatalf("decode %s: %v", body, err)
			}
			if decoded.Error != tt.wantError {
				t.Fatalf("error = %q, want %q", decoded.Error, tt.wantError)
			}
		})
	}
}

// A signed-in visitor who mistypes a URL keeps their own shell, sidebar and all,
// rather than dropping to an anonymous or plain-text page.
func TestUINotFoundKeepsTheSignedInShell(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-not-found")

	res := e.uiDoNoRedirect(t, http.MethodGet, e.projectPath()+"/sprnit", token, nil)
	defer res.Body.Close()
	body := readBody(t, res)

	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("code = %d body = %s", res.StatusCode, body)
	}
	if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
	for _, want := range []string{"Page not found", `id="main"`, `>@` + user.Username + `<`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

// An expired session must still render the 404 rather than bouncing to /login
// for a page that does not exist.
func TestUINotFoundRendersAnonymouslyWithoutASession(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	res := e.uiDoNoRedirect(t, http.MethodGet, "/nobody/projects/NOPE/sprnit", "not-a-real-session", nil)
	defer res.Body.Close()
	body := readBody(t, res)

	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Page not found") {
		t.Fatalf("body missing the error panel: %s", body)
	}
}
