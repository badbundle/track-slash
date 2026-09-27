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

// Member search matches emails only for someone who manages members; a
// public reader cannot rebuild addresses from which members a query returns.
func TestMemberSearchMatchesEmailOnlyForManagers(t *testing.T) {
	t.Parallel()
	e := newMCPHTTPEnv(t, nil)
	if _, err := e.store.UpdateProjectAccessMode(e.ctx, e.projectID, model.ProjectAccessPublic); err != nil {
		t.Fatalf("UpdateProjectAccessMode: %v", err)
	}
	key := strings.ToLower(uniqueProjectKey(t))
	member, err := e.store.CreateUserProfile(e.ctx, "findable"+key, "private-"+key+"@secret.example", "Findable")
	if err != nil {
		t.Fatalf("CreateUserProfile: %v", err)
	}
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, member.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	_, outsiderToken := e.mustUserToken(t, "email-prober")
	path := e.projectPath() + "/members/search?q=" + url.QueryEscape("private-"+key)
	for name, do := range map[string]func() (int, []byte){
		"anonymous": func() (int, []byte) { return e.doUnauth(t, http.MethodGet, path, nil) },
		"outsider":  func() (int, []byte) { return e.doWithToken(t, outsiderToken, http.MethodGet, path, nil) },
	} {
		code, body := do()
		if code != http.StatusOK || strings.Contains(string(body), member.ID.String()) {
			t.Fatalf("%s email probe = %d %s", name, code, body)
		}
	}
	if code, body := e.do(t, http.MethodGet, path, nil); code != http.StatusOK || !strings.Contains(string(body), member.ID.String()) || strings.Contains(string(body), "@secret.example") {
		t.Fatalf("manager email search = %d %s", code, body)
	}
	args := map[string]any{"owner": e.ownerUsername, "key": e.projKey, "query": "private-" + key}
	if users := decodeMCPField[[]json.RawMessage](t, mcpCall(t, e, mcpConnect(t, e, outsiderToken), "track_search_project_members", args), "users"); len(users) != 0 {
		t.Fatalf("outsider MCP email probe found %d", len(users))
	}
	if users := decodeMCPField[[]json.RawMessage](t, mcpCall(t, e, mcpConnect(t, e, e.authToken), "track_search_project_members", args), "users"); len(users) != 1 {
		t.Fatalf("manager MCP email search found %d", len(users))
	}
}

// A public project's readers see its live issues, not the ones taken down,
// nor the files only those hold.
func TestDeletedIssuesAreForMembers(t *testing.T) {
	t.Parallel()
	e, _ := newStorageHTTPEnv(t, 1024*1024)
	if _, err := e.store.UpdateProjectAccessMode(e.ctx, e.projectID, model.ProjectAccessPublic); err != nil {
		t.Fatalf("UpdateProjectAccessMode: %v", err)
	}
	readonly, readonlyToken := e.mustUserToken(t, "deleted-readonly")
	if _, err := e.store.SetProjectMemberRole(e.ctx, e.projectID, readonly.ID, model.ProjectMemberRoleReadonly); err != nil {
		t.Fatalf("SetProjectMemberRole: %v", err)
	}
	_, outsiderToken := e.mustUserToken(t, "deleted-outsider")
	issue, err := e.store.CreateIssue(store.WithActor(e.ctx, e.adminID), store.CreateIssueParams{ProjectID: e.projectID, Title: "Pasted a secret"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	code, body := e.doMultipartPath(t, e.authToken, e.issuePath(issue)+"/attachments", "secret.txt", []byte("hunter2"))
	if code != http.StatusCreated {
		t.Fatalf("attach = %d %s", code, body)
	}
	attachment := decode[model.IssueAttachment](t, body)
	if code, body := e.do(t, http.MethodDelete, e.issuePath(issue), nil); code != http.StatusNoContent {
		t.Fatalf("delete issue = %d %s", code, body)
	}
	objectPath := e.projectPath() + "/objects/" + attachment.Object.Ref

	for name, token := range map[string]string{"outsider": outsiderToken, "anonymous": ""} {
		if code, body := e.doWithToken(t, token, http.MethodGet, e.projectIssuesPath()+"/deleted", nil); code == http.StatusOK {
			t.Fatalf("%s listed deleted issues: %s", name, body)
		}
		if code, body := e.doWithToken(t, token, http.MethodGet, objectPath+"/content", nil); code != http.StatusNotFound {
			t.Fatalf("%s deleted issue's file = %d %s", name, code, body)
		}
		code, body := e.doWithToken(t, token, http.MethodGet, e.projectPath()+"/objects", nil)
		if code != http.StatusOK || strings.Contains(string(body), attachment.Object.ID.String()) {
			t.Fatalf("%s object list = %d %s", name, code, body)
		}
		code, body = e.doWithToken(t, token, http.MethodGet, e.projectPath()+"/changelog?limit=200", nil)
		if code != http.StatusOK || strings.Contains(string(body), issue.ID.String()) {
			t.Fatalf("%s changelog shows the deleted issue: %d %s", name, code, body)
		}
	}
	if code, body := e.doWithToken(t, readonlyToken, http.MethodGet, e.projectIssuesPath()+"/deleted", nil); code != http.StatusOK || !strings.Contains(string(body), issue.ID.String()) {
		t.Fatalf("readonly member deleted list = %d %s", code, body)
	}
	if code, body := e.doWithToken(t, readonlyToken, http.MethodGet, objectPath+"/content", nil); code != http.StatusOK {
		t.Fatalf("readonly member deleted issue's file = %d %s", code, body)
	}

	outsider := mcpConnect(t, e, outsiderToken)
	requireMCPErrorCode(t, mcpCallExpectError(t, e, outsider, "track_list_deleted_issues", map[string]any{"owner": e.ownerUsername, "key": e.projKey}), "forbidden")
	requireMCPErrorCode(t, mcpCallExpectError(t, e, outsider, "track_get_object", map[string]any{"owner": e.ownerUsername, "key": e.projKey, "object": attachment.Object.Ref}), "not_found")
	if objects := decodeMCPField[[]model.StorageObject](t, mcpCall(t, e, outsider, "track_list_objects", map[string]any{"owner": e.ownerUsername, "key": e.projKey}), "items"); len(objects) != 0 {
		t.Fatalf("outsider MCP objects = %+v", objects)
	}

	for _, path := range []string{e.projectPath() + "/deleted", "/" + e.ownerUsername + "/issues/" + issue.Identifier} {
		res := e.uiDoNoRedirect(t, http.MethodGet, path, outsiderToken, nil)
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("outsider UI %s = %d", path, res.StatusCode)
		}
	}
	if body := e.uiGet(t, e.projectPath()+"/deleted", readonlyToken); !strings.Contains(body, issue.Title) {
		t.Fatalf("readonly member UI deleted page lacks the issue: %s", body)
	}
}

// Editing or deleting a comment the caller cannot see answers like reading it:
// missing, not forbidden.
func TestHiddenCommentChangesAnswerNotFound(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	f := newCommentVisibilityFixture(t, e)
	path := e.issueCommentsPath(f.issue) + "/" + f.private.Ref
	if code, body := e.doWithToken(t, f.outsiderToken, http.MethodPatch, path, map[string]any{"body": "x"}); code != http.StatusNotFound {
		t.Fatalf("outsider edit hidden = %d %s", code, body)
	}
	if code, body := e.doWithToken(t, f.outsiderToken, http.MethodDelete, path, nil); code != http.StatusNotFound {
		t.Fatalf("outsider delete hidden = %d %s", code, body)
	}
	// A visible comment that is not theirs is still forbidden.
	if code, body := e.doWithToken(t, f.outsiderToken, http.MethodPatch, e.issueCommentsPath(f.issue)+"/"+f.shared.Ref, map[string]any{"body": "x"}); code != http.StatusForbidden {
		t.Fatalf("outsider edit shared = %d %s", code, body)
	}

	// The same holds for a help-desk reporter in the UI.
	h := newHTTPEnv(t)
	hd := newHelpDeskFixture(t, h)
	res := h.uiDoNoRedirect(t, http.MethodGet, "/"+h.ownerUsername+"/issues/"+hd.aliceIssue.Identifier+"/comments/"+hd.internalNote.Ref+"/edit", hd.aliceToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("reporter UI edit of an internal note = %d", res.StatusCode)
	}
}

// A block is for members to see, not every reader of the changelog.
func TestBlockHistoryIsForMembers(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	if _, err := e.store.UpdateProjectAccessMode(e.ctx, e.projectID, model.ProjectAccessPublic); err != nil {
		t.Fatalf("UpdateProjectAccessMode: %v", err)
	}
	target := e.mustUserNamed(t, "blocked-in-history")
	if code, body := e.do(t, http.MethodPut, e.projectPath()+"/blocks/"+target.Username, nil); code != http.StatusOK {
		t.Fatalf("block = %d %s", code, body)
	}
	if code, body := e.doUnauth(t, http.MethodGet, e.projectPath()+"/changelog?limit=200", nil); code != http.StatusOK || strings.Contains(string(body), target.Username) {
		t.Fatalf("anonymous changelog = %d %s", code, body)
	}
	if code, body := e.do(t, http.MethodGet, e.projectPath()+"/changelog?limit=200", nil); code != http.StatusOK || !strings.Contains(string(body), target.Username) {
		t.Fatalf("owner changelog = %d %s", code, body)
	}
}

// Removing a member closes their open realtime socket, as blocking does, so
// it has to reauthorize.
func TestRemovingAMemberDisconnectsTheirSocket(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	hub := realtime.NewHub()
	ts := httptest.NewServer(server.New(e.store, hub, nil).Router())
	t.Cleanup(ts.Close)
	member, memberToken := e.mustProjectMemberToken(t, "socket-member")

	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+apiPath("/ws"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + memberToken}},
	})
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()
	for _, topic := range []string{realtime.ProjectTopic(e.projectID), realtime.ProjectTopic(uuid.New())} {
		msg, _ := json.Marshal(map[string]string{"action": "subscribe", "topic": topic})
		if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
			t.Fatalf("subscribe: %v", err)
		}
	}
	if _, data, err := conn.Read(ctx); err != nil || !strings.Contains(string(data), "forbidden") {
		t.Fatalf("subscribe sync = %s, %v", data, err)
	}

	req, _ := http.NewRequestWithContext(e.ctx, http.MethodDelete, ts.URL+apiPath(e.projectPath()+"/members/"+member.Username), nil)
	req.Header.Set("Authorization", "Bearer "+e.authToken)
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke = %d", res.StatusCode)
	}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break // closed, as intended
		}
		if ctx.Err() != nil {
			t.Fatalf("socket stayed open after removal; last message %s", data)
		}
	}
}

// Member pickers name members by name and username, never their email.
func TestUIMemberPickersOmitEmails(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	key := strings.ToLower(uniqueProjectKey(t))
	member, err := e.store.CreateUserProfile(e.ctx, "picker"+key, "picker-"+key+"@secret.example", "Picker Member")
	if err != nil {
		t.Fatalf("CreateUserProfile: %v", err)
	}
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, member.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	for _, path := range []string{e.projectPath() + "/issues/new", "/issues/new?project_id=" + e.projectID.String()} {
		body := e.uiGet(t, path, e.authToken)
		if !strings.Contains(body, member.Username) || strings.Contains(body, "@secret.example") {
			t.Fatalf("%s picker shows an email or misses the member", path)
		}
	}
}
