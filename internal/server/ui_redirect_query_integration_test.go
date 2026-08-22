package server_test

import (
	"net/http"
	"net/url"
	"testing"
)

// A bookmarked or shared filtered URL used to land on the unfiltered view,
// because both canonical redirects rebuilt the path and dropped the query.
func TestProjectViewRedirectsKeepTheQueryString(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, session := e.mustProjectMemberToken(t, "redirect-query")
	query := "assignee_id=" + url.QueryEscape(user.ID.String()) + "&sort=priority"

	for _, tt := range []struct {
		name         string
		path         string
		wantLocation string
	}{
		{
			name:         "project root canonicalises to the sprint view",
			path:         e.projectPath(),
			wantLocation: e.projectPath() + "/sprint",
		},
		{
			name:         "legacy backlog canonicalises to the all view",
			path:         e.projectPath() + "/backlog",
			wantLocation: e.projectPath() + "/all",
		},
		{
			name:         "legacy backlog panel canonicalises to the all panel",
			path:         e.projectPath() + "/backlog/panel",
			wantLocation: e.projectPath() + "/all/panel",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := e.uiDoNoRedirect(t, http.MethodGet, tt.path+"?"+query, session, nil)
			res.Body.Close()
			if res.StatusCode != http.StatusSeeOther {
				t.Fatalf("code = %d, want 303", res.StatusCode)
			}
			if got := res.Header.Get("Location"); got != tt.wantLocation+"?"+query {
				t.Fatalf("Location = %q, want %q", got, tt.wantLocation+"?"+query)
			}

			// The carried-over query must be one the target actually accepts,
			// not just an opaque string echoed into Location.
			followed := e.uiDoNoRedirect(t, http.MethodGet, res.Header.Get("Location"), session, nil)
			body := readBody(t, followed)
			followed.Body.Close()
			if followed.StatusCode != http.StatusOK {
				t.Fatalf("following %s gave %d: %s", res.Header.Get("Location"), followed.StatusCode, body)
			}
		})
	}
}

// Without a query the canonical redirect stays clean, with no stray separator.
func TestProjectViewRedirectsWithoutAQueryAreUnchanged(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, session := e.mustProjectMemberToken(t, "redirect-plain")

	res := e.uiDoNoRedirect(t, http.MethodGet, e.projectPath(), session, nil)
	res.Body.Close()
	if got := res.Header.Get("Location"); got != e.projectPath()+"/sprint" {
		t.Fatalf("Location = %q, want %q", got, e.projectPath()+"/sprint")
	}
}
