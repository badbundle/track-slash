package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/realtime"
	"github.com/bradleymackey/track-slash/internal/server"
	"github.com/bradleymackey/track-slash/internal/store"
)

type privateIssueFixture struct {
	public, secret, privateChild model.Issue
	link                         model.IssueLink
	missingRef                   string
	readonlyToken, outsiderToken string
}

// newPrivateIssueFixture puts a private issue on a public project next to a
// public one that links to it and has a private sub-issue.
func newPrivateIssueFixture(t *testing.T, e *httpEnv, mode model.ProjectAccessMode) privateIssueFixture {
	t.Helper()
	if _, err := e.store.UpdateProjectAccessMode(e.ctx, e.projectID, mode); err != nil {
		t.Fatalf("UpdateProjectAccessMode: %v", err)
	}
	readonly, readonlyToken := e.mustUserToken(t, "private-readonly")
	if _, err := e.store.SetProjectMemberRole(e.ctx, e.projectID, readonly.ID, model.ProjectMemberRoleReadonly); err != nil {
		t.Fatalf("SetProjectMemberRole: %v", err)
	}
	_, outsiderToken := e.mustUserToken(t, "private-outsider")
	f := privateIssueFixture{readonlyToken: readonlyToken, outsiderToken: outsiderToken, missingRef: e.projKey + "-9999"}
	f.public = e.mustCreateIssue(t, "Public issue")
	code, body := e.do(t, http.MethodPost, e.projectIssuesPath(), map[string]any{"title": "Private report with a secret", "private": true})
	if code != http.StatusCreated {
		t.Fatalf("create private issue code = %d body = %s", code, body)
	}
	f.secret = decode[model.Issue](t, body)
	if !f.secret.Private {
		t.Fatalf("created issue is not private: %+v", f.secret)
	}
	code, body = e.do(t, http.MethodPost, e.issueSubIssuesPath(f.public), map[string]any{"title": "Private child task", "private": true})
	if code != http.StatusCreated {
		t.Fatalf("create private sub-issue code = %d body = %s", code, body)
	}
	f.privateChild = decode[model.Issue](t, body)
	code, body = e.do(t, http.MethodPost, e.issueLinksPath(f.public), map[string]any{"target_issue": f.secret.Identifier, "link_type": "relates_to"})
	if code != http.StatusCreated {
		t.Fatalf("create link code = %d body = %s", code, body)
	}
	f.link = decode[model.IssueLink](t, body)
	if code, body := e.do(t, http.MethodPost, e.issueCommentsPath(f.secret), map[string]any{"body": "Confirmed and fixing"}); code != http.StatusCreated {
		t.Fatalf("comment code = %d body = %s", code, body)
	}
	status := model.StatusInProgress
	if _, err := e.store.UpdateIssue(e.ctx, f.secret.ID, store.UpdateIssueParams{Status: &status}); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	return f
}

// Outsiders, signed in or not, get exactly what a missing issue gets for a
// private issue on a public project, and never see it named anywhere.
func TestPrivateIssuesLookMissingToOutsiders(t *testing.T) {
	t.Parallel()
	for _, mode := range []model.ProjectAccessMode{model.ProjectAccessPublic, model.ProjectAccessPublicIssues} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			e := newHTTPEnv(t)
			f := newPrivateIssueFixture(t, e, mode)
			missing := model.Issue{OwnerUsername: e.ownerUsername, Identifier: f.missingRef}
			names := []string{f.secret.Identifier, f.secret.Title, f.privateChild.Identifier, f.privateChild.Title}

			for name, token := range map[string]string{"outsider": f.outsiderToken, "anonymous": ""} {
				// Every route that names the issue answers as for a missing one.
				for _, suffix := range []string{"", "/comments", "/links", "/sub-issues", "/tags", "/context", "/attachments", "/github-links"} {
					missingCode, missingBody := e.doWithToken(t, token, http.MethodGet, e.issuePath(missing)+suffix, nil)
					code, body := e.doWithToken(t, token, http.MethodGet, e.issuePath(f.secret)+suffix, nil)
					if code != http.StatusNotFound || code != missingCode || string(body) != string(missingBody) {
						t.Fatalf("%s GET private%s = %d %s, missing = %d %s", name, suffix, code, body, missingCode, missingBody)
					}
				}
				code, body := e.doWithToken(t, token, http.MethodGet, e.projectPath()+"/links/"+f.link.Ref, nil)
				if code != http.StatusNotFound {
					t.Fatalf("%s GET link to private issue = %d %s", name, code, body)
				}
				// Nothing that lists, counts or names issues mentions it.
				for _, path := range []string{
					e.projectIssuesPath(),
					e.projectIssuesPath() + "?private=true",
					e.issueSubIssuesPath(f.public),
					e.issueLinksPath(f.public),
					e.projectPath() + "/changelog?limit=200",
					e.projectPath() + "/progress",
					e.projectPath() + "/insights",
					"/" + e.ownerUsername + "/issues?refs=" + f.public.Identifier + "," + f.secret.Identifier,
				} {
					if token == "" && strings.Contains(path, "?refs=") {
						continue // batch get needs a signed-in caller
					}
					code, body := e.doWithToken(t, token, http.MethodGet, path, nil)
					if code != http.StatusOK {
						t.Fatalf("%s GET %s = %d %s", name, path, code, body)
					}
					for _, secret := range append(names, f.secret.ID.String(), f.privateChild.ID.String()) {
						if strings.Contains(string(body), secret) {
							t.Fatalf("%s GET %s names the private issue (%q): %s", name, path, secret, body)
						}
					}
				}
				code, body = e.doWithToken(t, token, http.MethodGet, e.projectPath()+"/stats", nil)
				if stats := decode[model.ProjectStats](t, body); code != http.StatusOK || stats.AllTime.Total != 1 {
					t.Fatalf("%s stats = %d %+v", name, code, stats.AllTime)
				}

				// The UI's issue page is the same 404 as a missing issue's, with
				// no sign-in redirect for a signed-out visitor.
				missingPage := e.uiDoNoRedirectWithHeaders(t, http.MethodGet, e.issuePath(missing), token, nil, map[string]string{"Accept": "text/html"})
				missingPageBody := readBody(t, missingPage)
				missingPage.Body.Close()
				page := e.uiDoNoRedirectWithHeaders(t, http.MethodGet, e.issuePath(f.secret), token, nil, map[string]string{"Accept": "text/html"})
				pageBody := readBody(t, page)
				page.Body.Close()
				if page.StatusCode != http.StatusNotFound || page.StatusCode != missingPage.StatusCode || page.Header.Get("Location") != "" {
					t.Fatalf("%s UI private issue = %d %q, missing = %d", name, page.StatusCode, page.Header.Get("Location"), missingPage.StatusCode)
				}
				if strings.ReplaceAll(pageBody, f.secret.Identifier, f.missingRef) != missingPageBody {
					t.Fatalf("%s UI private issue page differs from a missing issue's:\n%s\n---\n%s", name, pageBody, missingPageBody)
				}
				for _, path := range []string{e.uiProjectViewPathForTest("all"), e.uiProjectViewPathForTest("changelog"), e.issuePath(f.public)} {
					res := e.uiDoNoRedirect(t, http.MethodGet, path, token, nil)
					body := readBody(t, res)
					res.Body.Close()
					if res.StatusCode != http.StatusOK {
						t.Fatalf("%s UI %s = %d", name, path, res.StatusCode)
					}
					for _, secret := range names {
						if strings.Contains(body, secret) {
							t.Fatalf("%s UI %s names the private issue (%q): %s", name, path, secret, body)
						}
					}
				}
			}

			// MCP answers not_found, as for a missing issue.
			session := mcpConnect(t, e, f.outsiderToken)
			for _, tool := range []string{"track_get_issue", "track_list_comments", "track_list_issue_links", "track_list_sub_issues"} {
				missingCode := mcpErrorCode(t, mcpCallExpectError(t, e, session, tool, map[string]any{"owner": e.ownerUsername, "issue": f.missingRef}))
				code := mcpErrorCode(t, mcpCallExpectError(t, e, session, tool, map[string]any{"owner": e.ownerUsername, "issue": f.secret.Identifier}))
				if code != "not_found" || code != missingCode {
					t.Fatalf("MCP %s private = %q, missing = %q", tool, code, missingCode)
				}
			}
			requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_get_link", map[string]any{"owner": e.ownerUsername, "key": e.projKey, "link": f.link.Ref}), "not_found")
			for tool, args := range map[string]map[string]any{
				"track_list_issues":       {"owner": e.ownerUsername, "key": e.projKey},
				"track_list_issue_links":  {"owner": e.ownerUsername, "issue": f.public.Identifier},
				"track_list_sub_issues":   {"owner": e.ownerUsername, "issue": f.public.Identifier},
				"track_batch_get_issues":  {"owner": e.ownerUsername, "refs": []string{f.public.Identifier, f.secret.Identifier}},
				"track_get_project_stats": {"owner": e.ownerUsername, "key": e.projKey},
			} {
				raw, err := json.Marshal(mcpCall(t, e, session, tool, args))
				if err != nil {
					t.Fatalf("marshal %s: %v", tool, err)
				}
				for _, secret := range append(names, f.secret.ID.String(), f.privateChild.ID.String()) {
					if strings.Contains(string(raw), secret) {
						t.Fatalf("MCP %s names the private issue (%q): %s", tool, secret, raw)
					}
				}
			}
		})
	}
}

// Members, read-only ones included, see private issues everywhere, marked.
func TestPrivateIssuesForMembers(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	f := newPrivateIssueFixture(t, e, model.ProjectAccessPublicIssues)

	code, body := e.doWithToken(t, f.readonlyToken, http.MethodGet, e.issuePath(f.secret), nil)
	if got := decode[model.Issue](t, body); code != http.StatusOK || !got.Private {
		t.Fatalf("member GET private = %d %s", code, body)
	}
	for path, want := range map[string]string{
		e.projectIssuesPath() + "?private=true":  f.secret.Identifier,
		e.issueSubIssuesPath(f.public):           f.privateChild.Identifier,
		e.issueLinksPath(f.public):               f.secret.ID.String(),
		e.projectPath() + "/changelog?limit=200": f.secret.Identifier,
	} {
		code, body := e.doWithToken(t, f.readonlyToken, http.MethodGet, path, nil)
		if code != http.StatusOK || !strings.Contains(string(body), want) {
			t.Fatalf("member GET %s = %d missing %q: %s", path, code, want, body)
		}
	}
	code, body = e.doWithToken(t, f.readonlyToken, http.MethodGet, e.projectIssuesPath()+"?private=false", nil)
	if code != http.StatusOK || strings.Contains(string(body), f.secret.Identifier) || !strings.Contains(string(body), f.public.Identifier) {
		t.Fatalf("member GET private=false = %d %s", code, body)
	}
	if code, body := e.doWithToken(t, f.readonlyToken, http.MethodGet, e.projectIssuesPath()+"?private=maybe", nil); code != http.StatusBadRequest {
		t.Fatalf("private=maybe = %d %s", code, body)
	}
	code, body = e.doWithToken(t, f.readonlyToken, http.MethodGet, e.projectPath()+"/stats", nil)
	if stats := decode[model.ProjectStats](t, body); code != http.StatusOK || stats.AllTime.Total != 3 {
		t.Fatalf("member stats = %d %+v", code, stats.AllTime)
	}
	session := mcpConnect(t, e, f.readonlyToken)
	listed := decodeMCPField[[]model.Issue](t, mcpCall(t, e, session, "track_list_issues", map[string]any{"owner": e.ownerUsername, "key": e.projKey, "private": true}), "items")
	if len(listed) != 1 || listed[0].ID != f.secret.ID {
		t.Fatalf("member MCP private list = %+v", listed)
	}

	page := e.uiGet(t, e.issuePath(f.secret), f.readonlyToken)
	for _, want := range []string{"data-issue-private-badge", "Yes, members and reporter only", f.secret.Title} {
		if !strings.Contains(page, want) {
			t.Fatalf("member issue page missing %q: %s", want, page)
		}
	}
	if strings.Contains(page, `aria-label="Make issue public"`) {
		t.Fatalf("read-only member offered the private toggle: %s", page)
	}
	if all := e.uiGet(t, e.uiProjectViewPathForTest("all"), f.readonlyToken); !strings.Contains(all, f.secret.Title) || !strings.Contains(all, "data-issue-private-badge") {
		t.Fatalf("member All list missing the private issue: %s", all)
	}
}

// Marking an issue private or public, and who may.
func TestPrivateIssueToggle(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	f := newPrivateIssueFixture(t, e, model.ProjectAccessPublicIssues)

	// Writers toggle it from the details panel.
	adminPage := e.uiGet(t, e.issuePath(f.public), e.authToken)
	if !strings.Contains(adminPage, `aria-label="Make issue private"`) || !strings.Contains(adminPage, `action="`+e.issuePath(f.public)+`/private"`) {
		t.Fatalf("writer missing the private toggle: %s", adminPage)
	}
	res := e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(f.public)+"/private", e.authToken, strings.NewReader("private=true"))
	body := readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Yes, members and reporter only") {
		t.Fatalf("make private = %d %s", res.StatusCode, body)
	}
	if code, _ := e.doWithToken(t, f.outsiderToken, http.MethodGet, e.issuePath(f.public), nil); code != http.StatusNotFound {
		t.Fatalf("outsider after making private = %d", code)
	}
	for _, bad := range []string{"private=maybe", "private="} {
		res := e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(f.public)+"/private", e.authToken, strings.NewReader(bad))
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("toggle %q = %d", bad, res.StatusCode)
		}
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(f.public)+"/private", f.readonlyToken, strings.NewReader("private=false"))
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only toggle = %d", res.StatusCode)
	}

	// A sub-issue of a private issue stays private.
	childPage := e.uiGet(t, e.issuePath(f.privateChild), e.authToken)
	if !strings.Contains(childPage, "Its parent is private") || strings.Contains(childPage, `aria-label="Make issue public"`) {
		t.Fatalf("private child of a private parent offered the toggle: %s", childPage)
	}
	code, body2 := e.do(t, http.MethodPatch, e.issuePath(f.privateChild), map[string]any{"private": false})
	if code != http.StatusConflict {
		t.Fatalf("REST make child public = %d %s", code, body2)
	}
	session := mcpConnect(t, e, e.authToken)
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_update_issue", map[string]any{"owner": e.ownerUsername, "issue": f.privateChild.Identifier, "private": false}), "conflict")

	// Making it public again shows it to everyone, recorded in the changelog.
	code, body2 = e.do(t, http.MethodPatch, e.issuePath(f.secret), map[string]any{"private": false})
	if updated := decode[model.Issue](t, body2); code != http.StatusOK || updated.Private {
		t.Fatalf("REST make public = %d %s", code, body2)
	}
	if code, body := e.doWithToken(t, f.outsiderToken, http.MethodGet, e.issuePath(f.secret), nil); code != http.StatusOK {
		t.Fatalf("outsider after making public = %d %s", code, body)
	}
	mcpCall(t, e, session, "track_update_issue", map[string]any{"owner": e.ownerUsername, "issue": f.secret.Identifier, "private": true})
	if code, _ := e.doWithToken(t, f.outsiderToken, http.MethodGet, e.issuePath(f.secret), nil); code != http.StatusNotFound {
		t.Fatalf("outsider after MCP made it private = %d", code)
	}
	code, changelog := e.do(t, http.MethodGet, e.projectPath()+"/changelog?limit=200", nil)
	if code != http.StatusOK || !strings.Contains(string(changelog), `"field":"private"`) {
		t.Fatalf("changelog missing the private change: %s", changelog)
	}
}

// Someone outside the project files a private issue, and follows it as its
// reporter, the way a help-desk reporter does.
func TestPrivateIssueReporter(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	f := newPrivateIssueFixture(t, e, model.ProjectAccessPublicIssues)
	_, reporterToken := e.mustUserToken(t, "private-reporter")

	newIssue := e.uiGet(t, e.projectPath()+"/issues/new", reporterToken)
	for _, want := range []string{"data-issue-private-field", `name="private" value="true"`, "Keep this issue private", "members will see it. Use this for security vulnerabilities"} {
		if !strings.Contains(newIssue, want) {
			t.Fatalf("new issue form missing %q: %s", want, newIssue)
		}
	}

	code, body := e.doWithToken(t, reporterToken, http.MethodPost, e.projectIssuesPath(), map[string]any{"title": "Account takeover", "description": "Steps inside", "private": true})
	if code != http.StatusCreated || strings.Contains(string(body), `"priority"`) {
		t.Fatalf("reporter REST create = %d %s", code, body)
	}
	reported := decode[model.ReporterIssue](t, body)
	own := model.Issue{OwnerUsername: e.ownerUsername, Identifier: reported.Identifier}

	code, body = e.doWithToken(t, reporterToken, http.MethodGet, e.issuePath(own), nil)
	if code != http.StatusOK || strings.Contains(string(body), `"priority"`) || !strings.Contains(string(body), "Account takeover") {
		t.Fatalf("reporter GET own private issue = %d %s", code, body)
	}
	if code, body := e.doWithToken(t, reporterToken, http.MethodPost, e.issueCommentsPath(own), map[string]any{"body": "More detail"}); code != http.StatusCreated {
		t.Fatalf("reporter reply = %d %s", code, body)
	}
	for _, suffix := range []string{"/sub-issues", "/links", "/tags"} {
		if code, body := e.doWithToken(t, reporterToken, http.MethodGet, e.issuePath(own)+suffix, nil); code != http.StatusForbidden {
			t.Fatalf("reporter GET %s = %d %s", suffix, code, body)
		}
	}
	code, body = e.doWithToken(t, reporterToken, http.MethodGet, "/"+e.ownerUsername+"/issues?refs="+reported.Identifier+","+f.public.Identifier, nil)
	if code != http.StatusOK || strings.Contains(string(body), reported.Identifier) || !strings.Contains(string(body), f.public.Identifier) {
		t.Fatalf("reporter batch = %d %s", code, body)
	}
	// Other people's private issues stay missing to the reporter.
	if code, _ := e.doWithToken(t, reporterToken, http.MethodGet, e.issuePath(f.secret), nil); code != http.StatusNotFound {
		t.Fatalf("reporter GET someone else's private issue = %d", code)
	}

	session := mcpConnect(t, e, reporterToken)
	got := mcpCall(t, e, session, "track_get_issue", map[string]any{"owner": e.ownerUsername, "issue": reported.Identifier})
	if raw, _ := json.Marshal(got); strings.Contains(string(raw), `"priority"`) || !strings.Contains(string(raw), "Account takeover") {
		t.Fatalf("reporter MCP get = %s", raw)
	}
	created := mcpCall(t, e, session, "track_create_issue", map[string]any{"owner": e.ownerUsername, "key": e.projKey, "title": "Second report", "private": true})
	if raw, _ := json.Marshal(created); strings.Contains(string(raw), `"priority"`) {
		t.Fatalf("reporter MCP create = %s", raw)
	}

	page := e.uiGet(t, e.issuePath(own), reporterToken)
	for _, want := range []string{`data-helpdesk-view="issue"`, "Account takeover", "data-issue-private-badge", `aria-label="Project"`, ">Project</a>", "More detail"} {
		if !strings.Contains(page, want) {
			t.Fatalf("reporter page missing %q: %s", want, page)
		}
	}
	if strings.Contains(page, "Your issues") {
		t.Fatalf("reporter page on a public project links to a help desk list: %s", page)
	}

	// Filing through the UI lands on the reporter's view.
	res := e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.projectIssuesPath(), reporterToken, strings.NewReader("title=Through+the+form&private=true"), map[string]string{"HX-Request": "true"})
	formBody := readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(formBody, `data-helpdesk-view="issue"`) || !strings.Contains(formBody, "Through the form") {
		t.Fatalf("UI private filing = %d %s", res.StatusCode, formBody)
	}
}

// The help desk form offers the flag with its own hint, and a sub-issue under
// a private issue says it is private rather than offering the box.
func TestPrivateIssueForms(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	f := newPrivateIssueFixture(t, e, model.ProjectAccessHelpDesk)
	_, reporterToken := e.mustUserToken(t, "helpdesk-private-reporter")
	form := e.uiGet(t, e.projectPath()+"/issues/new", reporterToken)
	if !strings.Contains(form, "data-issue-private-field") || !strings.Contains(form, "It stays private even if this project is made public.") {
		t.Fatalf("help desk form missing the private field: %s", form)
	}
	res := e.uiDoNoRedirect(t, http.MethodPost, e.projectIssuesPath(), reporterToken, strings.NewReader("title=Leaked+data&private=true"))
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("help desk private filing = %d", res.StatusCode)
	}
	issues, _, err := e.store.ListIssues(e.ctx, store.ListIssuesParams{ProjectID: e.projectID, Limit: 50, IncludePrivate: true, IncludeSubIssues: true})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	filed := false
	for _, issue := range issues {
		filed = filed || (issue.Title == "Leaked data" && issue.Private)
	}
	if !filed {
		t.Fatalf("help desk issue not filed private: %+v", issues)
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.projectIssuesPath(), reporterToken, strings.NewReader("title=&private=true"))
	errBody := readBody(t, res)
	res.Body.Close()
	if !strings.Contains(errBody, "Title required") || !strings.Contains(errBody, `id="helpdesk-private" type="checkbox" name="private" value="true" checked`) {
		t.Fatalf("help desk error lost the private box: %s", errBody)
	}

	subIssueForm := e.uiGet(t, e.issuePath(f.secret)+"/sub-issues/new", e.authToken)
	if !strings.Contains(subIssueForm, "Private, like the issue it belongs to.") || strings.Contains(subIssueForm, `id="sub-issue-private"`) {
		t.Fatalf("sub-issue form under a private issue: %s", subIssueForm)
	}
	subIssueForm = e.uiGet(t, e.issuePath(f.public)+"/sub-issues/new", e.authToken)
	if !strings.Contains(subIssueForm, `id="sub-issue-private"`) {
		t.Fatalf("sub-issue form missing the private box: %s", subIssueForm)
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueSubIssuesPath(f.public), e.authToken, strings.NewReader("title=Quiet+child&private=true"))
	res.Body.Close()
	children, _, err := e.store.ListSubIssuesForIssue(e.ctx, store.ListSubIssuesForIssueParams{ParentIssueID: f.public.ID, Limit: 50, IncludePrivate: true})
	if err != nil {
		t.Fatalf("ListSubIssuesForIssue: %v", err)
	}
	quiet := false
	for _, child := range children {
		quiet = quiet || (child.Title == "Quiet child" && child.Private)
	}
	if res.StatusCode != http.StatusOK || !quiet {
		t.Fatalf("UI private sub-issue = %d %+v", res.StatusCode, children)
	}
}

// A realtime subscription to a private issue fails as it would for a missing
// one, and the project topic keeps its events from outsiders.
func TestPrivateIssueRealtimeSubscription(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	f := newPrivateIssueFixture(t, e, model.ProjectAccessPublic)
	hub := realtime.NewHub()
	ts := httptest.NewServer(server.New(e.store, hub, nil).Router())
	t.Cleanup(ts.Close)

	// Only a refused subscription gets a reply, so each connection
	// subscribes to the private issue and then to a missing one, and reads
	// the replies that come back.
	missingID := uuid.New()
	replies := func(token string, want int) []string {
		t.Helper()
		ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
		defer cancel()
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+apiPath("/ws"), &websocket.DialOptions{
			HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
		})
		if err != nil {
			t.Fatalf("websocket dial: %v", err)
		}
		defer conn.CloseNow()
		for _, topic := range []string{realtime.IssueTopic(f.secret.ID), realtime.IssueTopic(missingID)} {
			msg, _ := json.Marshal(map[string]string{"action": "subscribe", "topic": topic})
			if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
				t.Fatalf("subscribe: %v", err)
			}
		}
		out := make([]string, 0, want)
		for len(out) < want {
			_, data, err := conn.Read(ctx)
			if err != nil {
				t.Fatalf("read reply: %v", err)
			}
			out = append(out, string(data))
		}
		return out
	}
	outsider := replies(f.outsiderToken, 2)
	if outsider[0] != outsider[1] {
		t.Fatalf("private topic reply %q differs from missing topic reply %q", outsider[0], outsider[1])
	}
	// A member's subscription to the private issue succeeds, so the only
	// reply is the missing topic's and no second one follows.
	replies(f.readonlyToken, 1)
	if extra := replies(f.readonlyToken, 1); len(extra) != 1 {
		t.Fatalf("member replies = %q", extra)
	}
	ctx, cancel := context.WithTimeout(e.ctx, 300*time.Millisecond)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+apiPath("/ws"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + f.readonlyToken}},
	})
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.CloseNow()
	msg, _ := json.Marshal(map[string]string{"action": "subscribe", "topic": realtime.IssueTopic(f.secret.ID)})
	if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, data, err := conn.Read(ctx); err == nil {
		t.Fatalf("member subscription to the private issue was refused: %s", data)
	}
}

func mcpErrorCode(t *testing.T, out map[string]json.RawMessage) string {
	t.Helper()
	var got struct {
		Code string `json:"code"`
	}
	if raw, ok := out["error"]; ok {
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal MCP error: %v", err)
		}
	}
	return got.Code
}
