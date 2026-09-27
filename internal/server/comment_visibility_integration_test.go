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

// commentVisibilityFixture is a public project, open to issues, with a report
// from an outsider carrying one shared and one members-only comment.
type commentVisibilityFixture struct {
	issue         model.Issue
	shared        model.Comment
	private       model.Comment
	outsiderToken string
	readonlyToken string
}

func newCommentVisibilityFixture(t *testing.T, e *httpEnv) commentVisibilityFixture {
	t.Helper()
	if _, err := e.store.UpdateProjectAccessMode(e.ctx, e.projectID, model.ProjectAccessPublicIssues); err != nil {
		t.Fatalf("UpdateProjectAccessMode: %v", err)
	}
	outsider, outsiderToken := e.mustUserToken(t, "visibility-outsider")
	readonly, readonlyToken := e.mustUserToken(t, "visibility-readonly")
	if _, err := e.store.SetProjectMemberRole(e.ctx, e.projectID, readonly.ID, model.ProjectMemberRoleReadonly); err != nil {
		t.Fatalf("SetProjectMemberRole: %v", err)
	}
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "Public report", ReporterID: &outsider.ID})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	shared, err := e.store.CreateComment(store.WithActor(e.ctx, e.adminID), store.CreateCommentParams{IssueID: issue.ID, AuthorID: e.adminID, Body: "Shared reply"})
	if err != nil {
		t.Fatalf("CreateComment shared: %v", err)
	}
	private, err := e.store.CreateComment(store.WithActor(e.ctx, e.adminID), store.CreateCommentParams{
		IssueID: issue.ID, AuthorID: e.adminID, Body: "Internal triage note", Visibility: model.CommentVisibilityMembers,
	})
	if err != nil {
		t.Fatalf("CreateComment members: %v", err)
	}
	return commentVisibilityFixture{issue: issue, shared: shared, private: private, outsiderToken: outsiderToken, readonlyToken: readonlyToken}
}

func commentIDs(comments []model.Comment) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for _, c := range comments {
		out[c.ID] = true
	}
	return out
}

func TestHTTPCommentVisibility(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	f := newCommentVisibilityFixture(t, e)
	commentsPath := e.issueCommentsPath(f.issue)

	// A reader who is not a member sees only shared comments, and a
	// members-only comment is missing rather than forbidden.
	for name, do := range map[string]func(method, path string) (int, []byte){
		"outsider":  func(method, path string) (int, []byte) { return e.doWithToken(t, f.outsiderToken, method, path, nil) },
		"anonymous": func(method, path string) (int, []byte) { return e.doUnauth(t, method, path, nil) },
	} {
		code, body := do(http.MethodGet, commentsPath)
		if code != http.StatusOK {
			t.Fatalf("%s list code = %d body = %s", name, code, body)
		}
		if ids := commentIDs(decodePage[model.Comment](t, body).Items); !ids[f.shared.ID] || ids[f.private.ID] {
			t.Fatalf("%s listed = %v", name, ids)
		}
		if code, body := do(http.MethodGet, commentsPath+"/"+f.private.Ref); code != http.StatusNotFound {
			t.Fatalf("%s get members-only code = %d body = %s", name, code, body)
		}
		if code, body := do(http.MethodGet, commentsPath+"/"+f.shared.Ref); code != http.StatusOK {
			t.Fatalf("%s get shared code = %d body = %s", name, code, body)
		}
		code, body = do(http.MethodGet, e.projectPath()+"/changelog?limit=200")
		if code != http.StatusOK {
			t.Fatalf("%s changelog code = %d body = %s", name, code, body)
		}
		for _, entry := range decodePage[model.ProjectChangelogEntry](t, body).Items {
			if entry.EntityID == f.private.ID || entry.MembersOnly {
				t.Fatalf("%s changelog shows members-only entry: %+v", name, entry)
			}
		}
	}

	// Every member, read-only ones included, sees both.
	code, body := e.doWithToken(t, f.readonlyToken, http.MethodGet, commentsPath, nil)
	if ids := commentIDs(decodePage[model.Comment](t, body).Items); code != http.StatusOK || !ids[f.shared.ID] || !ids[f.private.ID] {
		t.Fatalf("readonly list code = %d ids = %v", code, ids)
	}
	code, body = e.do(t, http.MethodGet, commentsPath+"/"+f.private.Ref, nil)
	if code != http.StatusOK || decode[model.Comment](t, body).Visibility != model.CommentVisibilityMembers {
		t.Fatalf("owner get members-only code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, e.projectPath()+"/changelog?limit=200", nil)
	if code != http.StatusOK {
		t.Fatalf("owner changelog code = %d body = %s", code, body)
	}
	found := false
	for _, entry := range decodePage[model.ProjectChangelogEntry](t, body).Items {
		found = found || (entry.EntityID == f.private.ID && entry.MembersOnly)
	}
	if !found {
		t.Fatalf("owner changelog missing members-only entry: %s", body)
	}

	// Creating: visibility is optional and validated.
	code, body = e.do(t, http.MethodPost, commentsPath, map[string]any{"body": "Also internal", "visibility": "members"})
	if code != http.StatusCreated || decode[model.Comment](t, body).Visibility != model.CommentVisibilityMembers {
		t.Fatalf("create members-only code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodPost, commentsPath, map[string]any{"body": "Default"})
	if code != http.StatusCreated || decode[model.Comment](t, body).Visibility != model.CommentVisibilityShared {
		t.Fatalf("create default code = %d body = %s", code, body)
	}
	if code, body := e.do(t, http.MethodPost, commentsPath, map[string]any{"body": "Bad", "visibility": "public"}); code != http.StatusBadRequest {
		t.Fatalf("create invalid visibility code = %d body = %s", code, body)
	}

	// Updating: the visibility alone, the body alone, or neither.
	privatePath := commentsPath + "/" + f.private.Ref
	code, body = e.do(t, http.MethodPatch, privatePath, map[string]any{"visibility": "shared"})
	if updated := decode[model.Comment](t, body); code != http.StatusOK || updated.Visibility != model.CommentVisibilityShared || updated.Body != f.private.Body || updated.EditedAt != nil {
		t.Fatalf("share code = %d body = %s", code, body)
	}
	if code, body := e.doWithToken(t, f.outsiderToken, http.MethodGet, privatePath, nil); code != http.StatusOK {
		t.Fatalf("outsider get shared-now comment code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodPatch, privatePath, map[string]any{"body": "Reworded"})
	if updated := decode[model.Comment](t, body); code != http.StatusOK || updated.Visibility != model.CommentVisibilityShared || updated.Body != "Reworded" {
		t.Fatalf("reword code = %d body = %s", code, body)
	}
	for name, req := range map[string]map[string]any{
		"neither":            {},
		"invalid visibility": {"visibility": "public"},
		"blank body":         {"body": "  "},
	} {
		if code, body := e.do(t, http.MethodPatch, privatePath, req); code != http.StatusBadRequest {
			t.Fatalf("update %s code = %d body = %s", name, code, body)
		}
	}
}

func TestMCPCommentVisibility(t *testing.T) {
	t.Parallel()
	e := newMCPHTTPEnv(t, nil)
	f := newCommentVisibilityFixture(t, e)
	owner := mcpConnect(t, e, e.authToken)
	outsider := mcpConnect(t, e, f.outsiderToken)
	issueArgs := func(extra map[string]any) map[string]any {
		args := map[string]any{"owner": e.ownerUsername, "issue": f.issue.Identifier}
		for key, value := range extra {
			args[key] = value
		}
		return args
	}

	listOut := mcpCall(t, e, outsider, "track_list_comments", issueArgs(nil))
	if ids := commentIDs(decodeMCPField[[]model.Comment](t, listOut, "items")); !ids[f.shared.ID] || ids[f.private.ID] {
		t.Fatalf("outsider listed = %v", ids)
	}
	requireMCPErrorCode(t, mcpCallExpectError(t, e, outsider, "track_get_comment", issueArgs(map[string]any{"comment": f.private.Ref})), "not_found")
	changelogOut := mcpCall(t, e, outsider, "track_list_project_changelog", map[string]any{"owner": e.ownerUsername, "key": e.projKey, "limit": 200})
	for _, entry := range decodeMCPField[[]model.ProjectChangelogEntry](t, changelogOut, "items") {
		if entry.EntityID == f.private.ID {
			t.Fatalf("outsider changelog shows members-only entry: %+v", entry)
		}
	}

	listOut = mcpCall(t, e, owner, "track_list_comments", issueArgs(nil))
	if ids := commentIDs(decodeMCPField[[]model.Comment](t, listOut, "items")); !ids[f.shared.ID] || !ids[f.private.ID] {
		t.Fatalf("owner listed = %v", ids)
	}
	created := decodeMCPField[model.Comment](t, mcpCall(t, e, owner, "track_create_comment", issueArgs(map[string]any{"body": "Internal", "visibility": "members"})), "comment")
	if created.Visibility != model.CommentVisibilityMembers {
		t.Fatalf("MCP created visibility = %q", created.Visibility)
	}
	requireMCPErrorCode(t, mcpCallExpectError(t, e, owner, "track_create_comment", issueArgs(map[string]any{"body": "Bad", "visibility": "public"})), "validation_error")

	shared := decodeMCPField[model.Comment](t, mcpCall(t, e, owner, "track_update_comment", issueArgs(map[string]any{"comment": created.Ref, "visibility": "shared"})), "comment")
	if shared.Visibility != model.CommentVisibilityShared || shared.Body != "Internal" || shared.EditedAt != nil {
		t.Fatalf("MCP shared comment = %+v", shared)
	}
	reworded := decodeMCPField[model.Comment](t, mcpCall(t, e, owner, "track_update_comment", issueArgs(map[string]any{"comment": created.Ref, "body": "Reworded"})), "comment")
	if reworded.Visibility != model.CommentVisibilityShared || reworded.Body != "Reworded" {
		t.Fatalf("MCP reworded comment = %+v", reworded)
	}
	requireMCPErrorCode(t, mcpCallExpectError(t, e, owner, "track_update_comment", issueArgs(map[string]any{"comment": created.Ref})), "validation_error")
	requireMCPErrorCode(t, mcpCallExpectError(t, e, owner, "track_update_comment", issueArgs(map[string]any{"comment": created.Ref, "visibility": "public"})), "validation_error")
	requireMCPErrorCode(t, mcpCallExpectError(t, e, owner, "track_update_comment", issueArgs(map[string]any{"comment": created.Ref, "body": strings.Repeat("x", 10001)})), "validation_error")
}

func TestUICommentVisibility(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	// A private project has no one to hide a comment from, so no choice.
	privateIssue := mustHTTPIssue(t, e)
	privatePath := "/" + e.ownerUsername + "/issues/" + privateIssue.Identifier
	if body := e.uiGet(t, privatePath, e.authToken); strings.Contains(body, `name="visibility"`) {
		t.Fatalf("private project offers a visibility choice: %s", body)
	}

	f := newCommentVisibilityFixture(t, e)
	issuePath := "/" + e.ownerUsername + "/issues/" + f.issue.Identifier
	ownerBody := e.uiGet(t, issuePath, e.authToken)
	for _, want := range []string{f.shared.Body, f.private.Body, "data-comment-members-only", `name="visibility"`, `<option value="shared" selected>`} {
		if !strings.Contains(ownerBody, want) {
			t.Fatalf("owner issue page missing %q: %s", want, ownerBody)
		}
	}
	outsiderBody := e.uiGet(t, issuePath, f.outsiderToken)
	if !strings.Contains(outsiderBody, f.shared.Body) || strings.Contains(outsiderBody, f.private.Body) || strings.Contains(outsiderBody, "data-comment-members-only") {
		t.Fatalf("outsider issue page shows members-only comment: %s", outsiderBody)
	}
	changelogBody := e.uiGet(t, e.uiProjectViewPathForTest("changelog"), f.outsiderToken)
	if strings.Contains(changelogBody, f.private.Body) {
		t.Fatalf("outsider changelog shows members-only preview: %s", changelogBody)
	}
	if body := e.uiGet(t, e.uiProjectViewPathForTest("changelog"), e.authToken); !strings.Contains(body, f.private.Body) {
		t.Fatalf("owner changelog missing members-only preview: %s", body)
	}

	post := func(path string, form url.Values) (int, string) {
		t.Helper()
		res := e.uiDoNoRedirect(t, http.MethodPost, path, e.authToken, strings.NewReader(form.Encode()))
		defer res.Body.Close()
		return res.StatusCode, readBody(t, res)
	}
	code, body := post(issuePath+"/comments", url.Values{"body": {"Posted as members only"}, "visibility": {"members"}})
	if code != http.StatusOK || !strings.Contains(body, "Posted as members only") {
		t.Fatalf("UI create members-only code = %d body = %s", code, body)
	}
	comments, _, err := e.store.ListCommentsForIssue(e.ctx, store.ListCommentsForIssueParams{IssueID: f.issue.ID, Limit: 10, NewestFirst: true, IncludeMembersOnly: true})
	if err != nil || len(comments) != 3 || comments[0].Visibility != model.CommentVisibilityMembers {
		t.Fatalf("UI-created comment = %+v, %v", comments, err)
	}
	posted := comments[0]
	if code, body := post(issuePath+"/comments", url.Values{"body": {"Bad"}, "visibility": {"public"}}); code != http.StatusBadRequest {
		t.Fatalf("UI create invalid visibility code = %d body = %s", code, body)
	}
	// A failed post keeps the chosen visibility.
	if code, body := post(issuePath+"/comments", url.Values{"body": {" "}, "visibility": {"members"}}); code != http.StatusOK || !strings.Contains(body, `<option value="members" selected>`) {
		t.Fatalf("UI create error code = %d body = %s", code, body)
	}

	editBody := e.uiGet(t, issuePath+"/comments/"+posted.Ref+"/edit", e.authToken)
	if strings.Count(editBody, `<option value="members" selected>`) != 1 {
		t.Fatalf("edit form visibility: %s", editBody)
	}
	if code, body := post(issuePath+"/comments/"+posted.Ref, url.Values{"body": {"Now shared"}, "visibility": {"shared"}}); code != http.StatusOK {
		t.Fatalf("UI update code = %d body = %s", code, body)
	}
	if got, err := e.store.GetComment(e.ctx, posted.ID); err != nil || got.Visibility != model.CommentVisibilityShared || got.Body != "Now shared" {
		t.Fatalf("UI-updated comment = %+v, %v", got, err)
	}
	if code, body := post(issuePath+"/comments/"+posted.Ref, url.Values{"body": {"Kept"}}); code != http.StatusOK {
		t.Fatalf("UI update without visibility code = %d body = %s", code, body)
	}
	if got, err := e.store.GetComment(e.ctx, posted.ID); err != nil || got.Visibility != model.CommentVisibilityShared {
		t.Fatalf("UI update without visibility changed it: %+v, %v", got, err)
	}
	if code, body := post(issuePath+"/comments/"+posted.Ref, url.Values{"body": {"Bad"}, "visibility": {"public"}}); code != http.StatusBadRequest {
		t.Fatalf("UI update invalid visibility code = %d body = %s", code, body)
	}
	if code, body := post(issuePath+"/comments/"+posted.Ref, url.Values{"body": {" "}, "visibility": {"members"}}); code != http.StatusOK || !strings.Contains(body, `<option value="members" selected>`) {
		t.Fatalf("UI update error code = %d body = %s", code, body)
	}
}

func (e *httpEnv) uiProjectViewPathForTest(view string) string {
	return "/" + e.ownerUsername + "/projects/" + e.projKey + "/" + view
}

// TestWebSocketMembersOnlyAccess checks a public viewer's subscription never
// carries a members-only event while a member's does.
func TestWebSocketMembersOnlyAccess(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	f := newCommentVisibilityFixture(t, e)
	hub := realtime.NewHub()
	ts := httptest.NewServer(server.New(e.store, hub, nil).Router())
	t.Cleanup(ts.Close)

	subscribe := func(token string) *websocket.Conn {
		t.Helper()
		ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
		defer cancel()
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+apiPath("/ws"), &websocket.DialOptions{
			HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
		})
		if err != nil {
			t.Fatalf("websocket dial: %v", err)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		// The forbidden reply to the second subscription proves the first,
		// read before it, is registered.
		for _, topic := range []string{realtime.IssueTopic(f.issue.ID), realtime.ProjectTopic(uuid.New())} {
			msg, _ := json.Marshal(map[string]string{"action": "subscribe", "topic": topic})
			if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
				t.Fatalf("subscribe: %v", err)
			}
		}
		if _, data, err := conn.Read(ctx); err != nil || !strings.Contains(string(data), "forbidden") {
			t.Fatalf("subscribe sync = %s, %v", data, err)
		}
		return conn
	}
	member := subscribe(f.readonlyToken)
	viewer := subscribe(f.outsiderToken)

	issueID := f.issue.ID
	hub.Publish(realtime.Event{Op: realtime.OpInsert, Entity: realtime.EntityComment, ID: f.private.ID, IssueID: &issueID, MembersOnly: true})
	hub.Publish(realtime.Event{Op: realtime.OpInsert, Entity: realtime.EntityComment, ID: f.shared.ID, IssueID: &issueID})

	first := func(conn *websocket.Conn) uuid.UUID {
		t.Helper()
		ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
		defer cancel()
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read event: %v", err)
		}
		var ev realtime.Event
		if err := json.Unmarshal(data, &ev); err != nil {
			t.Fatalf("unmarshal event: %v data=%s", err, data)
		}
		return ev.ID
	}
	if got := first(member); got != f.private.ID {
		t.Fatalf("member first event = %s, want members-only %s", got, f.private.ID)
	}
	if got := first(viewer); got != f.shared.ID {
		t.Fatalf("viewer first event = %s, want shared %s", got, f.shared.ID)
	}
}
