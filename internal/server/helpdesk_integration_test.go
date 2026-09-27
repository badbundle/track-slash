package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// helpDeskFixture is a help-desk project with two reporters, each with an
// issue, and one reporter blocked after filing.
type helpDeskFixture struct {
	alice        model.User
	aliceToken   string
	aliceIssue   model.Issue
	bobIssue     model.Issue
	blockedToken string
	blockedIssue model.Issue
	sharedReply  model.Comment
	internalNote model.Comment
}

func newHelpDeskFixture(t *testing.T, e *httpEnv) helpDeskFixture {
	t.Helper()
	code, body := e.do(t, http.MethodPatch, e.projectPath()+"/access", map[string]any{"access_mode": "helpdesk"})
	if settings := decode[model.ProjectAccessSettings](t, body); code != http.StatusOK || settings.AccessMode != model.ProjectAccessHelpDesk || settings.IsPublic || !settings.PublicIssueCreation {
		t.Fatalf("enable help desk code = %d body = %s", code, body)
	}
	alice, aliceToken := e.mustUserToken(t, "helpdesk-alice")
	bob, _ := e.mustUserToken(t, "helpdesk-bob")
	blocked, blockedToken := e.mustUserToken(t, "helpdesk-blocked")
	mustIssue := func(reporter model.User, title string) model.Issue {
		t.Helper()
		issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: title, Description: "Steps: see object-1 and " + e.projKey + "-1", ReporterID: &reporter.ID})
		if err != nil {
			t.Fatalf("CreateIssue: %v", err)
		}
		return issue
	}
	f := helpDeskFixture{
		alice: alice, aliceToken: aliceToken, blockedToken: blockedToken,
		aliceIssue:   mustIssue(alice, "Alice cannot sign in"),
		bobIssue:     mustIssue(bob, "Bob's secret report"),
		blockedIssue: mustIssue(blocked, "Spam"),
	}
	var err error
	f.sharedReply, err = e.store.CreateComment(store.WithActor(e.ctx, e.adminID), store.CreateCommentParams{
		IssueID: f.aliceIssue.ID, AuthorID: e.adminID, Body: "Thanks, we can reproduce it.", Visibility: model.CommentVisibilityShared,
	})
	if err != nil {
		t.Fatalf("CreateComment shared: %v", err)
	}
	// A help desk keeps a comment to members unless it is shared.
	f.internalNote, err = e.store.CreateComment(store.WithActor(e.ctx, e.adminID), store.CreateCommentParams{
		IssueID: f.aliceIssue.ID, AuthorID: e.adminID, Body: "Internal: caused by the session cache.",
	})
	if err != nil || f.internalNote.Visibility != model.CommentVisibilityMembers {
		t.Fatalf("CreateComment default = %+v, %v", f.internalNote, err)
	}
	if _, err := e.store.BlockProjectUser(e.ctx, e.projectID, blocked.ID, e.adminID); err != nil {
		t.Fatalf("BlockProjectUser: %v", err)
	}
	return f
}

// requireReporterIssueJSON checks an issue body carries only what a reporter
// may see.
func requireReporterIssueJSON(t *testing.T, raw []byte) model.ReporterIssue {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal issue: %v body=%s", err, raw)
	}
	for _, hidden := range []string{"priority", "assignee_id", "close_reason", "due_date", "sprint_id", "parent_issue_id", "tags"} {
		if _, ok := fields[hidden]; ok {
			t.Fatalf("reporter issue carries %s: %s", hidden, raw)
		}
	}
	var issue model.ReporterIssue
	if err := json.Unmarshal(raw, &issue); err != nil {
		t.Fatalf("unmarshal reporter issue: %v", err)
	}
	return issue
}

func TestHTTPHelpDeskReporter(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	f := newHelpDeskFixture(t, e)
	as := func(method, path string, body any) (int, []byte) {
		return e.doWithToken(t, f.aliceToken, method, path, body)
	}

	// Filing: a title and description only.
	code, body := as(http.MethodPost, e.projectIssuesPath(), map[string]any{"title": "Export fails", "description": "It times out."})
	if code != http.StatusCreated {
		t.Fatalf("reporter create code = %d body = %s", code, body)
	}
	created := requireReporterIssueJSON(t, body)
	if created.Title != "Export fails" || created.Status != model.ReporterStatusOpen || created.ReporterID == nil || *created.ReporterID != f.alice.ID {
		t.Fatalf("reporter created = %+v", created)
	}
	for name, req := range map[string]map[string]any{
		"priority": {"title": "x", "priority": "P0"},
		"due date": {"title": "x", "due_date": "2030-01-01"},
	} {
		if code, body := as(http.MethodPost, e.projectIssuesPath(), req); code != http.StatusBadRequest {
			t.Fatalf("reporter create with %s code = %d body = %s", name, code, body)
		}
	}
	if code, body := as(http.MethodPost, e.projectIssuesPath(), map[string]any{"title": "x", "assignee_id": e.adminID}); code != http.StatusForbidden {
		t.Fatalf("reporter create with assignee code = %d body = %s", code, body)
	}

	// Reading: their own issues, reduced; another reporter's is missing.
	code, body = as(http.MethodGet, e.issuePath(f.aliceIssue), nil)
	if code != http.StatusOK || requireReporterIssueJSON(t, body).ID != f.aliceIssue.ID {
		t.Fatalf("reporter get own code = %d body = %s", code, body)
	}
	missingCode, missingBody := as(http.MethodGet, "/"+e.ownerUsername+"/issues/"+e.projKey+"-9999", nil)
	otherCode, otherBody := as(http.MethodGet, e.issuePath(f.bobIssue), nil)
	if otherCode != http.StatusNotFound || otherCode != missingCode || string(otherBody) != string(missingBody) {
		t.Fatalf("other reporter's issue = %d %s, missing = %d %s", otherCode, otherBody, missingCode, missingBody)
	}
	code, body = as(http.MethodGet, e.projectIssuesPath()+"?status=done&limit=1", nil)
	page := decodePage[json.RawMessage](t, body)
	if code != http.StatusOK || len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatalf("reporter list page code = %d body = %s", code, body)
	}
	code, body = as(http.MethodGet, e.projectIssuesPath()+"?cursor="+url.QueryEscape(*page.NextCursor), nil)
	rest := decodePage[json.RawMessage](t, body)
	if code != http.StatusOK || len(rest.Items) != 1 || rest.NextCursor != nil {
		t.Fatalf("reporter list second page code = %d body = %s", code, body)
	}
	listed := map[uuid.UUID]bool{}
	for _, raw := range append(page.Items, rest.Items...) {
		listed[requireReporterIssueJSON(t, raw).ID] = true
	}
	if !listed[created.ID] || !listed[f.aliceIssue.ID] || listed[f.bobIssue.ID] {
		t.Fatalf("reporter listed = %v", listed)
	}
	if code, body := as(http.MethodGet, e.projectIssuesPath()+"?cursor=bad", nil); code != http.StatusBadRequest {
		t.Fatalf("reporter list bad cursor code = %d body = %s", code, body)
	}
	if code, body := as(http.MethodGet, e.projectIssuesPath()+"?limit=0", nil); code != http.StatusBadRequest {
		t.Fatalf("reporter list bad limit code = %d body = %s", code, body)
	}
	status := model.StatusDone
	if _, err := e.store.UpdateIssue(e.ctx, f.aliceIssue.ID, store.UpdateIssueParams{Status: &status}); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if code, body := as(http.MethodGet, e.issuePath(f.aliceIssue), nil); code != http.StatusOK || requireReporterIssueJSON(t, body).Status != model.ReporterStatusClosed {
		t.Fatalf("reporter status after done code = %d body = %s", code, body)
	}

	// Batch reads skip other reporters' issues as missing and refuse their own.
	code, body = as(http.MethodGet, "/"+e.ownerUsername+"/issues?refs="+f.bobIssue.Identifier+","+e.projKey+"-9999", nil)
	if code != http.StatusOK || strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("reporter batch others code = %d body = %s", code, body)
	}
	if code, body := as(http.MethodGet, "/"+e.ownerUsername+"/issues?refs="+f.aliceIssue.Identifier, nil); code != http.StatusForbidden {
		t.Fatalf("reporter batch own code = %d body = %s", code, body)
	}

	// Comments: shared ones only, and replies that stay shared.
	commentsPath := e.issueCommentsPath(f.aliceIssue)
	code, body = as(http.MethodGet, commentsPath, nil)
	if ids := commentIDs(decodePage[model.Comment](t, body).Items); code != http.StatusOK || !ids[f.sharedReply.ID] || ids[f.internalNote.ID] {
		t.Fatalf("reporter comments code = %d ids = %v", code, ids)
	}
	if code, body := as(http.MethodGet, commentsPath+"/"+f.internalNote.Ref, nil); code != http.StatusNotFound {
		t.Fatalf("reporter get internal note code = %d body = %s", code, body)
	}
	if code, body := as(http.MethodGet, commentsPath+"/"+f.sharedReply.Ref, nil); code != http.StatusOK {
		t.Fatalf("reporter get shared reply code = %d body = %s", code, body)
	}
	code, body = as(http.MethodPost, commentsPath, map[string]any{"body": "It happens on Safari too."})
	reply := decode[model.Comment](t, body)
	if code != http.StatusCreated || reply.Visibility != model.CommentVisibilityShared || reply.AuthorID != f.alice.ID {
		t.Fatalf("reporter reply code = %d body = %s", code, body)
	}
	if code, body := as(http.MethodPost, commentsPath, map[string]any{"body": "Hidden?", "visibility": "members"}); code != http.StatusBadRequest {
		t.Fatalf("reporter members-only reply code = %d body = %s", code, body)
	}
	replyPath := commentsPath + "/" + reply.Ref
	if code, body := as(http.MethodPatch, replyPath, map[string]any{"body": "It happens on Safari and Firefox."}); code != http.StatusOK || decode[model.Comment](t, body).Body != "It happens on Safari and Firefox." {
		t.Fatalf("reporter edit reply code = %d body = %s", code, body)
	}
	if code, body := as(http.MethodPatch, replyPath, map[string]any{"visibility": "members"}); code != http.StatusBadRequest {
		t.Fatalf("reporter hide reply code = %d body = %s", code, body)
	}
	if code, body := as(http.MethodPatch, commentsPath+"/"+f.sharedReply.Ref, map[string]any{"body": "Rewritten"}); code != http.StatusForbidden {
		t.Fatalf("reporter edit member reply code = %d body = %s", code, body)
	}
	if code, body := as(http.MethodDelete, commentsPath+"/"+f.sharedReply.Ref, nil); code != http.StatusForbidden {
		t.Fatalf("reporter delete member reply code = %d body = %s", code, body)
	}
	if code, body := as(http.MethodDelete, replyPath, nil); code != http.StatusNoContent {
		t.Fatalf("reporter delete reply code = %d body = %s", code, body)
	}
	for name, path := range map[string]string{"list": e.issueCommentsPath(f.bobIssue), "get": e.issueCommentsPath(f.bobIssue) + "/comment-1"} {
		if code, body := as(http.MethodGet, path, nil); code != http.StatusNotFound {
			t.Fatalf("reporter %s other reporter's comments code = %d body = %s", name, code, body)
		}
	}
	if code, body := as(http.MethodPost, e.issueCommentsPath(f.bobIssue), map[string]any{"body": "Hi Bob"}); code != http.StatusNotFound {
		t.Fatalf("reporter comment on other reporter's issue code = %d body = %s", code, body)
	}

	// Everything else stays closed: forbidden on their own issue, missing on
	// another reporter's, forbidden at the project level.
	for _, suffix := range []string{"/sub-issues", "/attachments", "/context", "/tags", "/links", "/github-links"} {
		if code, body := as(http.MethodGet, e.issuePath(f.aliceIssue)+suffix, nil); code != http.StatusForbidden {
			t.Fatalf("reporter GET own %s code = %d body = %s", suffix, code, body)
		}
		if code, body := as(http.MethodGet, e.issuePath(f.bobIssue)+suffix, nil); code != http.StatusNotFound {
			t.Fatalf("reporter GET other %s code = %d body = %s", suffix, code, body)
		}
	}
	if code, body := as(http.MethodPatch, e.issuePath(f.aliceIssue), map[string]any{"title": "Mine now"}); code != http.StatusForbidden {
		t.Fatalf("reporter update own issue code = %d body = %s", code, body)
	}
	if code, body := as(http.MethodDelete, e.issuePath(f.aliceIssue), nil); code != http.StatusForbidden {
		t.Fatalf("reporter delete own issue code = %d body = %s", code, body)
	}
	for _, path := range []string{e.projectPath(), e.projectPath() + "/changelog", e.projectPath() + "/members", e.projectPath() + "/access", e.projectSprintsPath(), e.projectPath() + "/tags", e.projectPath() + "/stats"} {
		if code, body := as(http.MethodGet, path, nil); code != http.StatusForbidden {
			t.Fatalf("reporter GET %s code = %d body = %s", path, code, body)
		}
	}
	code, body = as(http.MethodGet, "/projects", nil)
	if code != http.StatusOK || projectResponseInPage(decodePage[projectResponseDecoded](t, body).Items, e.projectID.String()) {
		t.Fatalf("reporter project list code = %d body = %s", code, body)
	}

	// A blocked user is refused everywhere, their own issue included.
	blocked := func(method, path string, body any) {
		t.Helper()
		if code, resp := e.doWithToken(t, f.blockedToken, method, path, body); code != http.StatusForbidden {
			t.Fatalf("blocked %s %s code = %d body = %s", method, path, code, resp)
		}
	}
	blocked(http.MethodGet, e.issuePath(f.blockedIssue), nil)
	blocked(http.MethodGet, e.issuePath(f.bobIssue), nil)
	blocked(http.MethodGet, e.issueCommentsPath(f.blockedIssue), nil)
	blocked(http.MethodPost, e.issueCommentsPath(f.blockedIssue), map[string]any{"body": "Let me in"})
	blocked(http.MethodGet, e.projectIssuesPath(), nil)
	blocked(http.MethodPost, e.projectIssuesPath(), map[string]any{"title": "More spam"})
	blocked(http.MethodGet, "/"+e.ownerUsername+"/issues?refs="+f.blockedIssue.Identifier, nil)

	// Members still get the full issue.
	code, body = e.do(t, http.MethodGet, e.issuePath(f.aliceIssue), nil)
	if code != http.StatusOK || decode[model.Issue](t, body).Priority == "" {
		t.Fatalf("member get code = %d body = %s", code, body)
	}
}

func TestMCPHelpDeskReporter(t *testing.T) {
	t.Parallel()
	e := newMCPHTTPEnv(t, nil)
	f := newHelpDeskFixture(t, e)
	session := mcpConnect(t, e, f.aliceToken)
	project := map[string]any{"owner": e.ownerUsername, "key": e.projKey}
	with := func(base map[string]any, extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	issueArgs := func(issue model.Issue, extra map[string]any) map[string]any {
		return with(map[string]any{"owner": e.ownerUsername, "issue": issue.Identifier}, extra)
	}

	createdOut := mcpCall(t, e, session, "track_create_issue", with(project, map[string]any{"title": "From my agent", "description": "Details"}))
	created := requireReporterIssueJSON(t, createdOut["issue"])
	if created.Title != "From my agent" || created.Status != model.ReporterStatusOpen {
		t.Fatalf("MCP reporter created = %+v", created)
	}
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_create_issue", with(project, map[string]any{"title": "x", "priority": "P0"})), "validation_error")

	if got := requireReporterIssueJSON(t, mcpCall(t, e, session, "track_get_issue", issueArgs(f.aliceIssue, nil))["issue"]); got.ID != f.aliceIssue.ID {
		t.Fatalf("MCP reporter get own = %+v", got)
	}
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_get_issue", issueArgs(f.bobIssue, nil)), "not_found")
	listed := decodeMCPField[[]json.RawMessage](t, mcpCall(t, e, session, "track_list_issues", with(project, map[string]any{"status": "done"})), "items")
	ids := map[uuid.UUID]bool{}
	for _, raw := range listed {
		ids[requireReporterIssueJSON(t, raw).ID] = true
	}
	if len(ids) != 2 || !ids[created.ID] || !ids[f.aliceIssue.ID] {
		t.Fatalf("MCP reporter listed = %v", ids)
	}
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_list_issues", with(project, map[string]any{"limit": -1})), "validation_error")
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_list_issues", with(project, map[string]any{"cursor": "bad"})), "validation_error")

	comments := decodeMCPField[[]model.Comment](t, mcpCall(t, e, session, "track_list_comments", issueArgs(f.aliceIssue, nil)), "items")
	if got := commentIDs(comments); !got[f.sharedReply.ID] || got[f.internalNote.ID] {
		t.Fatalf("MCP reporter comments = %v", got)
	}
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_get_comment", issueArgs(f.aliceIssue, map[string]any{"comment": f.internalNote.Ref})), "not_found")
	reply := decodeMCPField[model.Comment](t, mcpCall(t, e, session, "track_create_comment", issueArgs(f.aliceIssue, map[string]any{"body": "More detail"})), "comment")
	if reply.Visibility != model.CommentVisibilityShared {
		t.Fatalf("MCP reporter reply = %+v", reply)
	}
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_create_comment", issueArgs(f.aliceIssue, map[string]any{"body": "x", "visibility": "members"})), "validation_error")
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_update_comment", issueArgs(f.aliceIssue, map[string]any{"comment": reply.Ref, "visibility": "members"})), "validation_error")
	if updated := decodeMCPField[model.Comment](t, mcpCall(t, e, session, "track_update_comment", issueArgs(f.aliceIssue, map[string]any{"comment": reply.Ref, "body": "Even more detail"})), "comment"); updated.Body != "Even more detail" {
		t.Fatalf("MCP reporter updated reply = %+v", updated)
	}
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_update_comment", issueArgs(f.aliceIssue, map[string]any{"comment": f.sharedReply.Ref, "body": "x"})), "forbidden")
	mcpCall(t, e, session, "track_delete_comment", issueArgs(f.aliceIssue, map[string]any{"comment": reply.Ref}))
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_create_comment", issueArgs(f.bobIssue, map[string]any{"body": "Hi"})), "not_found")

	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_update_issue", issueArgs(f.aliceIssue, map[string]any{"title": "x"})), "forbidden")
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_list_issue_links", issueArgs(f.bobIssue, nil)), "not_found")
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_get_project", project), "forbidden")
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_list_project_changelog", project), "forbidden")
	if got := decodeMCPField[[]model.Issue](t, mcpCall(t, e, session, "track_batch_get_issues", map[string]any{"owner": e.ownerUsername, "refs": []string{f.bobIssue.Identifier}}), "issues"); len(got) != 0 {
		t.Fatalf("MCP reporter batch = %+v", got)
	}
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_batch_get_issues", map[string]any{"owner": e.ownerUsername, "refs": []string{f.aliceIssue.Identifier}}), "forbidden")
	for _, listedProject := range decodeMCPField[[]model.Project](t, mcpCall(t, e, session, "track_list_projects", map[string]any{}), "items") {
		if listedProject.ID == e.projectID {
			t.Fatal("MCP reporter project list includes the help desk")
		}
	}

	blockedSession := mcpConnect(t, e, f.blockedToken)
	requireMCPErrorCode(t, mcpCallExpectError(t, e, blockedSession, "track_get_issue", issueArgs(f.blockedIssue, nil)), "forbidden")
	requireMCPErrorCode(t, mcpCallExpectError(t, e, blockedSession, "track_list_issues", project), "forbidden")
	requireMCPErrorCode(t, mcpCallExpectError(t, e, blockedSession, "track_create_issue", with(project, map[string]any{"title": "Spam"})), "forbidden")
	requireMCPErrorCode(t, mcpCallExpectError(t, e, blockedSession, "track_list_comments", issueArgs(f.blockedIssue, nil)), "forbidden")
}

func TestUIHelpDeskReporter(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	f := newHelpDeskFixture(t, e)
	newPath := e.projectPath() + "/issues/new"
	minePath := e.projectPath() + "/issues/mine"
	aliceIssuePath := "/" + e.ownerUsername + "/issues/" + f.aliceIssue.Identifier
	get := func(token, path string) (int, string, string) {
		t.Helper()
		res := e.uiDoNoRedirect(t, http.MethodGet, path, token, nil)
		defer res.Body.Close()
		return res.StatusCode, readBody(t, res), res.Header.Get("Location")
	}
	post := func(token, path string, form url.Values) (int, string, string) {
		t.Helper()
		res := e.uiDoNoRedirect(t, http.MethodPost, path, token, strings.NewReader(form.Encode()))
		defer res.Body.Close()
		return res.StatusCode, readBody(t, res), res.Header.Get("Location")
	}

	// Signed out, the form sends visitors to sign in and back.
	if code, _, location := get("", newPath); code != http.StatusSeeOther || !strings.HasPrefix(location, "/login?next=") {
		t.Fatalf("anonymous new code = %d location = %q", code, location)
	}

	code, body, _ := get(f.aliceToken, newPath)
	for _, want := range []string{`data-helpdesk-view="new"`, e.projKey, "Submit issue", "data-helpdesk-promo", `href="https://trackslash.com"`, "the open issue tracker your coding agents can actually use", minePath} {
		if code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("help desk form missing %q (code %d): %s", want, code, body)
		}
	}
	for _, notWant := range []string{"Priority", "Assignee", f.bobIssue.Title} {
		if strings.Contains(body, notWant) {
			t.Fatalf("help desk form shows %q: %s", notWant, body)
		}
	}
	if code, body, _ := get(f.aliceToken, newPath+"/panel"); code != http.StatusOK || !strings.Contains(body, `data-helpdesk-view="new"`) {
		t.Fatalf("help desk form panel code = %d body = %s", code, body)
	}
	if code, body, _ := post(f.aliceToken, e.projectPath()+"/issues", url.Values{"title": {" "}, "description": {"kept"}}); code != http.StatusOK || !strings.Contains(body, "Title required") || !strings.Contains(body, "kept") {
		t.Fatalf("help desk blank title code = %d body = %s", code, body)
	}
	code, _, location := post(f.aliceToken, e.projectPath()+"/issues", url.Values{"title": {"Invoices are blank"}, "description": {"Since Monday."}, "priority": {"P0"}})
	if code != http.StatusSeeOther || !strings.HasPrefix(location, "/"+e.ownerUsername+"/issues/"+e.projKey+"-") {
		t.Fatalf("help desk submit code = %d location = %q", code, location)
	}
	// A priority sent anyway is ignored, as the global form would too.
	res := e.uiDoNoRedirectWithHeaders(t, http.MethodPost, "/issues", f.aliceToken, strings.NewReader(url.Values{"project_id": {e.projectID.String()}, "title": {"Via the global form"}, "priority": {"P0"}}.Encode()), map[string]string{"HX-Request": "true"})
	globalBody := readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(globalBody, `data-helpdesk-view="issue"`) || !strings.Contains(res.Header.Get("HX-Push-Url"), "/issues/") {
		t.Fatalf("global form as reporter code = %d body = %s", res.StatusCode, globalBody)
	}
	issues, _, err := e.store.ListIssues(e.ctx, store.ListIssuesParams{ProjectID: e.projectID, ReporterID: &f.alice.ID, Limit: 10})
	if err != nil || len(issues) != 3 {
		t.Fatalf("reporter issues = %+v, %v", issues, err)
	}
	for _, issue := range issues {
		if issue.Priority != model.PriorityP2 {
			t.Fatalf("reporter set priority: %+v", issue)
		}
	}

	code, body, _ = get(f.aliceToken, location)
	if code != http.StatusOK || !strings.Contains(body, "Invoices are blank") || !strings.Contains(body, `data-helpdesk-status="open"`) {
		t.Fatalf("help desk issue page code = %d body = %s", code, body)
	}
	code, body, _ = get(f.aliceToken, aliceIssuePath)
	for _, want := range []string{`data-helpdesk-view="issue"`, f.aliceIssue.Title, f.sharedReply.Body, "Send reply", "Steps: see object-1"} {
		if code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("help desk issue page missing %q (code %d): %s", want, code, body)
		}
	}
	// The reporter's own email is in their account menu; a member's never is.
	memberEmail := strings.ToLower("admin-" + e.projKey + "@example.com")
	for _, notWant := range []string{f.internalNote.Body, "P2", "Assignee", "Sub-issues", "Members only", memberEmail, "/attachments/"} {
		if strings.Contains(body, notWant) {
			t.Fatalf("help desk issue page shows %q: %s", notWant, body)
		}
	}
	if code, body, _ := get(f.aliceToken, aliceIssuePath+"/panel"); code != http.StatusOK || !strings.Contains(body, `data-helpdesk-view="issue"`) {
		t.Fatalf("help desk issue panel code = %d body = %s", code, body)
	}
	code, body, _ = get(f.aliceToken, minePath)
	if code != http.StatusOK || !strings.Contains(body, f.aliceIssue.Title) || !strings.Contains(body, "Invoices are blank") || strings.Contains(body, f.bobIssue.Title) || strings.Contains(body, f.blockedIssue.Title) {
		t.Fatalf("help desk list code = %d body = %s", code, body)
	}

	// Replies, and editing them.
	if code, body, _ := post(f.aliceToken, aliceIssuePath+"/comments", url.Values{"body": {" "}}); code != http.StatusOK || !strings.Contains(body, "Reply required") {
		t.Fatalf("blank reply code = %d body = %s", code, body)
	}
	if code, body, _ := post(f.aliceToken, aliceIssuePath+"/comments", url.Values{"body": {"Screenshots attached below."}, "visibility": {"members"}}); code != http.StatusOK || !strings.Contains(body, "Screenshots attached below.") {
		t.Fatalf("reply code = %d body = %s", code, body)
	}
	comments, _, err := e.store.ListCommentsForIssue(e.ctx, store.ListCommentsForIssueParams{IssueID: f.aliceIssue.ID, Limit: 10, NewestFirst: true, IncludeMembersOnly: true})
	if err != nil || comments[0].AuthorID != f.alice.ID || comments[0].Visibility != model.CommentVisibilityShared {
		t.Fatalf("reporter reply = %+v, %v", comments, err)
	}
	reply := comments[0]
	replyPath := aliceIssuePath + "/comments/" + reply.Ref
	if code, body, _ := get(f.aliceToken, replyPath+"/edit"); code != http.StatusOK || !strings.Contains(body, "Save reply") {
		t.Fatalf("edit reply code = %d body = %s", code, body)
	}
	if code, body, _ := post(f.aliceToken, replyPath, url.Values{"body": {" "}}); code != http.StatusOK || !strings.Contains(body, "Reply required") {
		t.Fatalf("blank reply edit code = %d body = %s", code, body)
	}
	if code, body, _ := post(f.aliceToken, replyPath, url.Values{"body": {"Screenshots attached."}}); code != http.StatusOK || !strings.Contains(body, "Screenshots attached.") {
		t.Fatalf("reply edit code = %d body = %s", code, body)
	}
	if code, _, _ := get(f.aliceToken, aliceIssuePath+"/comments/"+f.sharedReply.Ref+"/edit"); code != http.StatusForbidden {
		t.Fatalf("edit member reply code = %d", code)
	}
	if code, _, _ := post(f.aliceToken, aliceIssuePath+"/comments/"+f.sharedReply.Ref, url.Values{"body": {"x"}}); code != http.StatusForbidden {
		t.Fatalf("update member reply code = %d", code)
	}

	// Another reporter's issue is missing on every issue route; the project's
	// own pages are forbidden.
	bobPath := "/" + e.ownerUsername + "/issues/" + f.bobIssue.Identifier
	missingPath := "/" + e.ownerUsername + "/issues/" + e.projKey + "-9999"
	for _, suffix := range []string{"", "/panel", "/context", "/comments/comment-1/edit"} {
		code, _, _ := get(f.aliceToken, bobPath+suffix)
		missing, _, _ := get(f.aliceToken, missingPath+suffix)
		if code != http.StatusNotFound || missing != http.StatusNotFound {
			t.Fatalf("GET other%s = %d, missing = %d", suffix, code, missing)
		}
	}
	if code, _, _ := post(f.aliceToken, bobPath+"/comments", url.Values{"body": {"Hi"}}); code != http.StatusNotFound {
		t.Fatalf("reply on other reporter's issue code = %d", code)
	}
	for _, path := range []string{e.projectPath(), e.projectPath() + "/all", e.projectPath() + "/changelog", aliceIssuePath + "/context", aliceIssuePath + "/title/edit"} {
		if code, _, _ := get(f.aliceToken, path); code != http.StatusForbidden {
			t.Fatalf("reporter GET %s code = %d", path, code)
		}
	}

	// Blocked: forbidden everywhere, their own issue included.
	blockedPath := "/" + e.ownerUsername + "/issues/" + f.blockedIssue.Identifier
	for _, path := range []string{newPath, minePath, blockedPath, blockedPath + "/panel"} {
		if code, _, _ := get(f.blockedToken, path); code != http.StatusForbidden {
			t.Fatalf("blocked GET %s code = %d", path, code)
		}
	}
	if code, _, _ := post(f.blockedToken, blockedPath+"/comments", url.Values{"body": {"Let me in"}}); code != http.StatusForbidden {
		t.Fatalf("blocked reply code = %d", code)
	}
	if code, _, _ := post(f.blockedToken, e.projectPath()+"/issues", url.Values{"title": {"Spam"}}); code != http.StatusForbidden {
		t.Fatalf("blocked submit code = %d", code)
	}

	// Members: the list sends them to the project, the issue page is theirs,
	// shared comments name the reporter, and About shares the link.
	if code, _, location := get(e.authToken, minePath); code != http.StatusSeeOther || location != e.projectPath()+"/all" {
		t.Fatalf("member list code = %d location = %q", code, location)
	}
	code, body, _ = get(e.authToken, aliceIssuePath)
	for _, want := range []string{f.internalNote.Body, "Shared with the reporter", `<option value="members" selected>`, "data-comment-members-only"} {
		if code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("member issue page missing %q: %s", want, body)
		}
	}
	code, body, _ = get(e.authToken, e.projectPath()+"/about")
	if code != http.StatusOK || !strings.Contains(body, "data-helpdesk-link") || !strings.Contains(body, `href="`+newPath+`"`) {
		t.Fatalf("member about page code = %d body = %s", code, body)
	}
	code, body, _ = get(e.authToken, e.projectPath()+"/members")
	if code != http.StatusOK || !strings.Contains(body, `value="helpdesk" checked`) {
		t.Fatalf("member access card code = %d body = %s", code, body)
	}
	if code, body, _ := get(e.authToken, newPath); code != http.StatusOK || strings.Contains(body, `data-helpdesk-view`) {
		t.Fatalf("member new issue page code = %d body = %s", code, body)
	}

	// Outsiders to a private project are refused the list outright.
	_, outsiderToken := e.mustUserToken(t, "helpdesk-outsider")
	if _, err := e.store.UpdateProjectAccessMode(e.ctx, e.projectID, model.ProjectAccessPrivate); err != nil {
		t.Fatalf("UpdateProjectAccessMode: %v", err)
	}
	if code, _, _ := get(outsiderToken, minePath); code != http.StatusForbidden {
		t.Fatalf("private project list code = %d", code)
	}
	if code, _, location := get("", minePath); code != http.StatusSeeOther || !strings.HasPrefix(location, "/login?next=") {
		t.Fatalf("anonymous list code = %d location = %q", code, location)
	}
}

// TestWebSocketHelpDeskReporterGetsNoTopics checks a help-desk reporter cannot
// subscribe to anything in the project, their own issue included, so realtime
// never carries more than they may see.
func TestWebSocketHelpDeskReporterGetsNoTopics(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	f := newHelpDeskFixture(t, e)
	hub := realtime.NewHub()
	ts := httptest.NewServer(server.New(e.store, hub, nil).Router())
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+apiPath("/ws"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + f.aliceToken}},
	})
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()
	for _, topic := range []string{realtime.IssueTopic(f.aliceIssue.ID), realtime.CommentTopic(f.sharedReply.ID), realtime.ProjectTopic(e.projectID)} {
		msg, _ := json.Marshal(map[string]string{"action": "subscribe", "topic": topic})
		if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
			t.Fatalf("subscribe %s: %v", topic, err)
		}
		if _, data, err := conn.Read(ctx); err != nil || !strings.Contains(string(data), "forbidden") {
			t.Fatalf("subscribe %s = %s, %v", topic, data, err)
		}
	}
	if hub.TopicCount() != 0 {
		t.Fatalf("reporter subscribed to %d topics", hub.TopicCount())
	}
}
