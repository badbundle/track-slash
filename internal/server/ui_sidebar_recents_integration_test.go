package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
)

// sidebarRecents returns the sidebar's Recents markup from a page or panel.
func sidebarRecents(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `<div id="sidebar-recents"`)
	if start < 0 {
		t.Fatalf("missing sidebar recents: %s", body)
	}
	end := strings.Index(body[start:], "\n</div>")
	if end < 0 {
		t.Fatalf("unterminated sidebar recents: %s", body)
	}
	return body[start : start+end]
}

// recentOrder lists the issue identifiers in a Recents section, top first.
func recentOrder(recents string) []string {
	var out []string
	for _, part := range strings.Split(recents, "data-sidebar-recent ")[1:] {
		start := strings.Index(part, `data-tooltip="`) + len(`data-tooltip="`)
		out = append(out, part[start:start+strings.Index(part[start:], ":")])
	}
	return out
}

func (e *httpEnv) issuePanelHTMX(t *testing.T, issue model.Issue, token string) string {
	t.Helper()
	res := e.uiDoNoRedirectWithHeaders(t, http.MethodGet, "/"+e.ownerUsername+"/issues/"+issue.Identifier+"/panel", token, nil, map[string]string{"HX-Request": "true"})
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s panel code = %d body = %s", issue.Identifier, res.StatusCode, body)
	}
	return body
}

func TestUISidebarRecentsFollowIssueViews(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	member, token := e.mustUserToken(t, "ui-recents-member")
	if _, err := e.store.SetProjectMemberRole(e.ctx, e.projectID, member.ID, model.ProjectMemberRoleMember); err != nil {
		t.Fatalf("SetProjectMemberRole: %v", err)
	}
	if err := e.store.FavoriteProject(e.ctx, member.ID, e.projectID); err != nil {
		t.Fatalf("FavoriteProject: %v", err)
	}
	a := e.mustCreateIssue(t, "Alpha work")
	b := e.mustCreateIssue(t, "Beta work")
	issuePage := func(issue model.Issue) string { return "/" + e.ownerUsername + "/issues/" + issue.Identifier }

	// Nothing viewed yet: Favorites has its heading, Recents is hidden.
	me := e.uiGet(t, "/me", token)
	if !strings.Contains(me, ">Favorites</h2>") {
		t.Fatalf("favorites heading missing: %s", me)
	}
	if recents := sidebarRecents(t, me); !strings.Contains(recents, `class="hidden"`) || strings.Contains(recents, "Recents") {
		t.Fatalf("empty Recents should be hidden: %s", recents)
	}

	// A full page load records the view, lists it, and makes its Recents entry
	// the one active destination instead of the favorited project.
	page := e.uiGet(t, issuePage(a), token)
	recents := sidebarRecents(t, page)
	if got := recentOrder(recents); fmt.Sprint(got) != fmt.Sprint([]string{a.Identifier}) {
		t.Fatalf("recents after viewing A = %v: %s", got, recents)
	}
	if !strings.Contains(recents, `data-sidebar-issue-id="`+a.ID.String()+`" href="`+issuePage(a)+`"`) || !strings.Contains(recents, `aria-current="page"`) {
		t.Fatalf("viewed issue is not the active Recents entry: %s", recents)
	}
	if got := strings.Count(sidebarNavMarkup(t, page), `aria-current="page"`); got != 1 {
		t.Fatalf("issue page sidebar marks %d active destinations, want 1: %s", got, page)
	}
	if !strings.Contains(page, `data-sidebar-view="project" data-sidebar-project-id="`+e.projectID.String()+`" data-sidebar-issue-id="`+a.ID.String()+`"`) {
		t.Fatalf("issue panel does not name its issue for the sidebar: %s", page)
	}

	// An htmx navigation leaves the sidebar in place, so the panel swaps the
	// refreshed Recents in out of band.
	panel := e.issuePanelHTMX(t, b, token)
	recents = sidebarRecents(t, panel)
	if !strings.HasPrefix(recents, `<div id="sidebar-recents" hx-swap-oob="true"`) {
		t.Fatalf("htmx panel Recents are not out of band: %s", recents)
	}
	if got := recentOrder(recents); fmt.Sprint(got) != fmt.Sprint([]string{b.Identifier, a.Identifier}) {
		t.Fatalf("recents after viewing B = %v: %s", got, recents)
	}
	if strings.Count(recents, `aria-current="page"`) != 1 || !strings.Contains(recents, `data-sidebar-issue-id="`+b.ID.String()+`" href="`+issuePage(b)+`" hx-get="`+issuePage(b)+`/panel" hx-target="#main" hx-push-url="`+issuePage(b)+`" data-tooltip="`+b.Identifier+`: Beta work" data-tooltip-when-truncated aria-current="page"`) {
		t.Fatalf("B should be the one active Recents entry: %s", recents)
	}

	// A plain panel request (no htmx) is a fragment for a caller that owns the
	// page, so it records the view without touching the sidebar.
	res := e.uiDoNoRedirect(t, http.MethodGet, issuePage(a)+"/panel", token, nil)
	plain := readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || strings.Contains(plain, `id="sidebar-recents"`) {
		t.Fatalf("plain panel code = %d, should not carry Recents: %s", res.StatusCode, plain)
	}

	// Viewing A again moves it back to the top without duplicating it.
	if got := recentOrder(sidebarRecents(t, e.uiGet(t, "/me", token))); fmt.Sprint(got) != fmt.Sprint([]string{a.Identifier, b.Identifier}) {
		t.Fatalf("recents after viewing A again = %v", got)
	}

	// The sidebar shows at most ten issues.
	var last model.Issue
	for i := range 11 {
		last = e.mustCreateIssue(t, fmt.Sprintf("Filler %d", i))
		panel = e.issuePanelHTMX(t, last, token)
	}
	if got := recentOrder(sidebarRecents(t, panel)); len(got) != 10 || got[0] != last.Identifier {
		t.Fatalf("recents after many views = %v, want the 10 newest with %s first", got, last.Identifier)
	}

	// Deleted issues drop out.
	if err := e.store.DeleteIssue(e.ctx, last.ID); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	if recents := sidebarRecents(t, e.uiGet(t, "/me", token)); strings.Contains(recents, last.Identifier+":") {
		t.Fatalf("deleted issue still in Recents: %s", recents)
	}
}

func TestUISidebarRecentsSkipSignedOutVisitors(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	issue := e.mustCreateIssue(t, "Public work")
	if _, err := e.store.UpdateProjectAccessMode(e.ctx, e.projectID, model.ProjectAccessPublic); err != nil {
		t.Fatalf("UpdateProjectAccessSettings: %v", err)
	}

	for _, headers := range []map[string]string{nil, {"HX-Request": "true"}} {
		path := "/" + e.ownerUsername + "/issues/" + issue.Identifier
		if headers != nil {
			path += "/panel"
		}
		res := e.uiDoNoRedirectWithHeaders(t, http.MethodGet, path, "", nil, headers)
		body := readBody(t, res)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("anonymous GET %s code = %d body = %s", path, res.StatusCode, body)
		}
		if strings.Contains(body, `id="sidebar-recents"`) {
			t.Fatalf("anonymous GET %s rendered Recents: %s", path, body)
		}
	}
	var views int
	if err := e.pool.QueryRow(e.ctx, `SELECT count(*) FROM recent_issue_views`).Scan(&views); err != nil {
		t.Fatalf("count recent views: %v", err)
	}
	if views != 0 {
		t.Fatalf("signed-out visits recorded %d views", views)
	}
}
