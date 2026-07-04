package server_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/server"
	objectstorage "github.com/bradleymackey/track-slash/internal/storage"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/bradleymackey/track-slash/internal/testutil"
)

type staticOAuthHandler struct {
	token string
}

func (h staticOAuthHandler) TokenSource(context.Context) (oauth2.TokenSource, error) {
	if h.token == "" {
		return nil, nil
	}
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: h.token}), nil
}

func (h staticOAuthHandler) Authorize(context.Context, *http.Request, *http.Response) error {
	return errors.New("authorization unavailable in tests")
}

func newMCPHTTPEnv(t *testing.T, storageSvc *objectstorage.Service) *httpEnv {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	db := testutil.NewMigratedDatabase(t)
	st := store.New(db.Pool)
	srv := server.NewWithOptions(st, nil, server.Options{ObjectStorage: storageSvc})
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	key := uniqueProjectKey(t)
	admin, err := st.CreateOrUpdateAdminUser(ctx, "admin-"+key+"@example.com", "Admin")
	if err != nil {
		t.Fatalf("CreateOrUpdateAdminUser: %v", err)
	}
	proj, err := st.CreateProjectForUser(ctx, admin.ID, key, "mcp-test", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser: %v", err)
	}
	token, err := st.CreateAuthToken(ctx, store.CreateAuthTokenParams{
		UserID: admin.ID,
		Kind:   model.AuthTokenKindAPI,
		Name:   "test",
	})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	return &httpEnv{
		ctx: ctx, ts: ts, pool: db.Pool, store: st, projectID: proj.ID, projKey: key, ownerUsername: admin.Username,
		adminID: admin.ID, authToken: token.RawToken,
	}
}

func mcpConnect(t *testing.T, e *httpEnv, token string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "track-slash-test", Version: "v1"}, nil)
	session, err := client.Connect(e.ctx, &mcp.StreamableClientTransport{
		Endpoint:     e.ts.URL + "/mcp",
		OAuthHandler: staticOAuthHandler{token: token},
	}, nil)
	if err != nil {
		t.Fatalf("Connect MCP: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func mcpCall(t *testing.T, e *httpEnv, session *mcp.ClientSession, name string, args map[string]any) map[string]json.RawMessage {
	t.Helper()
	res, err := session.CallTool(e.ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structured content %s: %v raw=%s", name, err, raw)
	}
	if res.IsError {
		content, _ := json.Marshal(res.Content)
		t.Fatalf("tool %s error: structured=%s content=%s", name, raw, content)
	}
	return out
}

func mcpCallExpectError(t *testing.T, e *httpEnv, session *mcp.ClientSession, name string, args map[string]any) map[string]json.RawMessage {
	t.Helper()
	res, err := session.CallTool(e.ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured error content: %v", err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structured error content %s: %v raw=%s", name, err, raw)
	}
	if !res.IsError {
		t.Fatalf("tool %s succeeded, want error: %s", name, raw)
	}
	return out
}

func decodeMCPField[T any](t *testing.T, out map[string]json.RawMessage, field string) T {
	t.Helper()
	raw, ok := out[field]
	if !ok {
		t.Fatalf("missing field %q in %v", field, out)
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal field %q: %v raw=%s", field, err, raw)
	}
	return v
}

func requireMCPErrorCode(t *testing.T, out map[string]json.RawMessage, want string) {
	t.Helper()
	var got struct {
		Code string `json:"code"`
	}
	raw := decodeMCPField[json.RawMessage](t, out, "error")
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal error: %v raw=%s", err, raw)
	}
	if got.Code != want {
		t.Fatalf("error code = %q, want %q", got.Code, want)
	}
}

func TestMCPToolErrorsAreStructured(t *testing.T) {
	t.Parallel()
	storageSvc, _ := newLocalStorageService(t, 1024*1024)
	e := newMCPHTTPEnv(t, storageSvc)
	session := mcpConnect(t, e, e.authToken)

	out := mcpCallExpectError(t, e, session, "track_create_issue", map[string]any{
		"owner": e.ownerUsername,
		"key":   e.projKey,
		"title": "",
	})
	requireMCPErrorCode(t, out, "validation_error")
}

func TestMCPRequiresAPIToken(t *testing.T) {
	t.Parallel()
	storageSvc, _ := newLocalStorageService(t, 1024*1024)
	e := newMCPHTTPEnv(t, storageSvc)

	client := mcp.NewClient(&mcp.Implementation{Name: "track-slash-test", Version: "v1"}, nil)
	if _, err := client.Connect(e.ctx, &mcp.StreamableClientTransport{Endpoint: e.ts.URL + "/mcp"}, nil); err == nil {
		t.Fatalf("unauthenticated MCP connect succeeded")
	}

	sessionToken, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{
		UserID: e.adminID,
		Kind:   model.AuthTokenKindSession,
		Name:   "session",
	})
	if err != nil {
		t.Fatalf("CreateAuthToken session: %v", err)
	}
	if _, err := client.Connect(e.ctx, &mcp.StreamableClientTransport{
		Endpoint:     e.ts.URL + "/mcp",
		OAuthHandler: staticOAuthHandler{token: sessionToken.RawToken},
	}, nil); err == nil {
		t.Fatalf("session token MCP connect succeeded")
	}
}

func TestMCPAcceptsStaleSessionID(t *testing.T) {
	t.Parallel()
	storageSvc, _ := newLocalStorageService(t, 1024*1024)
	e := newMCPHTTPEnv(t, storageSvc)

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	req, err := http.NewRequestWithContext(e.ctx, http.MethodPost, e.ts.URL+"/mcp", body)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.authToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	req.Header.Set("Mcp-Session-Id", "stale-session-after-restart")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST stale session: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, data)
	}
	var got struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v body=%s", err, data)
	}
	if got.Error != nil {
		t.Fatalf("unexpected error: %+v", got.Error)
	}
	for _, tool := range got.Result.Tools {
		if tool.Name == "track_get_me" {
			return
		}
	}
	t.Fatalf("track_get_me not found in tools: %+v", got.Result.Tools)
}

func TestMCPProjectIssueCommentParity(t *testing.T) {
	t.Parallel()
	storageSvc, _ := newLocalStorageService(t, 1024*1024)
	e := newMCPHTTPEnv(t, storageSvc)
	session := mcpConnect(t, e, e.authToken)

	tools, err := session.ListTools(e.ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools.Tools) < 40 {
		t.Fatalf("tool count = %d, want broad parity", len(tools.Tools))
	}

	projectOut := mcpCall(t, e, session, "track_get_project", map[string]any{
		"owner": e.ownerUsername,
		"key":   e.projKey,
	})
	project := decodeMCPField[model.Project](t, projectOut, "project")
	if project.ID != e.projectID {
		t.Fatalf("project id = %s, want %s", project.ID, e.projectID)
	}

	issueOut := mcpCall(t, e, session, "track_create_issue", map[string]any{
		"owner":       e.ownerUsername,
		"key":         e.projKey,
		"title":       "MCP issue",
		"description": "created by MCP",
		"priority":    "P1",
	})
	issue := decodeMCPField[model.Issue](t, issueOut, "issue")
	if issue.Identifier == "" || issue.Title != "MCP issue" || issue.Priority != model.PriorityP1 {
		t.Fatalf("bad issue from MCP: %+v", issue)
	}

	code, body := e.do(t, http.MethodGet, e.issuePath(issue), nil)
	if code != http.StatusOK {
		t.Fatalf("API get issue code = %d body=%s", code, body)
	}
	apiIssue := decode[model.Issue](t, body)
	if apiIssue.ID != issue.ID || apiIssue.Identifier != issue.Identifier {
		t.Fatalf("API/MCP issue mismatch: api=%+v mcp=%+v", apiIssue, issue)
	}

	commentOut := mcpCall(t, e, session, "track_create_comment", map[string]any{
		"owner": e.ownerUsername,
		"issue": issue.Identifier,
		"body":  "MCP comment",
	})
	comment := decodeMCPField[model.Comment](t, commentOut, "comment")
	if comment.Ref != "comment-1" || comment.Body != "MCP comment" {
		t.Fatalf("bad comment from MCP: %+v", comment)
	}

	listOut := mcpCall(t, e, session, "track_list_comments", map[string]any{
		"owner": e.ownerUsername,
		"issue": issue.Identifier,
	})
	comments := decodeMCPField[[]model.Comment](t, listOut, "items")
	if len(comments) != 1 || comments[0].ID != comment.ID {
		t.Fatalf("comments = %+v, want %+v", comments, comment)
	}

	_, memberToken := e.mustProjectMemberToken(t, "mcp-comment-other")
	memberSession := mcpConnect(t, e, memberToken)
	errOut := mcpCallExpectError(t, e, memberSession, "track_delete_comment", map[string]any{
		"owner":   e.ownerUsername,
		"issue":   issue.Identifier,
		"comment": comment.Ref,
	})
	requireMCPErrorCode(t, errOut, "forbidden")
	gotComment, err := e.store.GetComment(e.ctx, comment.ID)
	if err != nil {
		t.Fatalf("GetComment after forbidden delete: %v", err)
	}
	if gotComment.Body != "MCP comment" {
		t.Fatalf("forbidden delete changed comment: %+v", gotComment)
	}
	mcpCall(t, e, session, "track_delete_comment", map[string]any{
		"owner":   e.ownerUsername,
		"issue":   issue.Identifier,
		"comment": comment.Ref,
	})
	if _, err := e.store.GetComment(e.ctx, comment.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetComment after author delete err = %v, want ErrNotFound", err)
	}
}

func TestMCPCreateIssuePeopleRequireProjectMembers(t *testing.T) {
	t.Parallel()
	storageSvc, _ := newLocalStorageService(t, 1024*1024)
	e := newMCPHTTPEnv(t, storageSvc)
	session := mcpConnect(t, e, e.authToken)
	member, _ := e.mustProjectMemberToken(t, "mcp-issue-member")
	nonMember, _ := e.mustUserToken(t, "mcp-issue-outsider")

	out := mcpCall(t, e, session, "track_create_issue", map[string]any{
		"owner":       e.ownerUsername,
		"key":         e.projKey,
		"title":       "MCP member people",
		"assignee_id": member.ID.String(),
		"reporter_id": member.ID.String(),
	})
	issue := decodeMCPField[model.Issue](t, out, "issue")
	if issue.AssigneeID == nil || *issue.AssigneeID != member.ID || issue.ReporterID == nil || *issue.ReporterID != member.ID {
		t.Fatalf("issue people = assignee %v reporter %v, want %s", issue.AssigneeID, issue.ReporterID, member.ID)
	}

	listOut := mcpCall(t, e, session, "track_list_issues", map[string]any{
		"owner":        e.ownerUsername,
		"key":          e.projKey,
		"assignee_ids": []string{member.ID.String()},
	})
	assigned := decodeMCPField[[]model.Issue](t, listOut, "items")
	if len(assigned) != 1 || assigned[0].ID != issue.ID {
		t.Fatalf("assigned issues = %+v, want issue %s", assigned, issue.ID)
	}

	updateOut := mcpCall(t, e, session, "track_update_issue", map[string]any{
		"owner":       e.ownerUsername,
		"issue":       issue.Identifier,
		"assignee_id": member.ID.String(),
	})
	updated := decodeMCPField[model.Issue](t, updateOut, "issue")
	if updated.AssigneeID == nil || *updated.AssigneeID != member.ID {
		t.Fatalf("updated assignee = %v, want %s", updated.AssigneeID, member.ID)
	}

	out = mcpCallExpectError(t, e, session, "track_create_issue", map[string]any{
		"owner":       e.ownerUsername,
		"key":         e.projKey,
		"title":       "MCP invalid assignee",
		"assignee_id": "not-a-uuid",
	})
	requireMCPErrorCode(t, out, "validation_error")

	out = mcpCallExpectError(t, e, session, "track_create_issue", map[string]any{
		"owner":       e.ownerUsername,
		"key":         e.projKey,
		"title":       "MCP nonmember assignee",
		"assignee_id": nonMember.ID.String(),
	})
	requireMCPErrorCode(t, out, "not_found")

	out = mcpCallExpectError(t, e, session, "track_create_sub_issue", map[string]any{
		"owner":       e.ownerUsername,
		"issue":       issue.Identifier,
		"title":       "MCP child nonmember",
		"assignee_id": nonMember.ID.String(),
	})
	requireMCPErrorCode(t, out, "not_found")

	out = mcpCall(t, e, session, "track_create_sub_issue", map[string]any{
		"owner":       e.ownerUsername,
		"issue":       issue.Identifier,
		"title":       "MCP child member",
		"assignee_id": member.ID.String(),
	})
	child := decodeMCPField[model.Issue](t, out, "issue")
	if child.AssigneeID == nil || *child.AssigneeID != member.ID {
		t.Fatalf("child assignee = %v, want %s", child.AssigneeID, member.ID)
	}
}

func TestMCPAttachmentAndResourceRoundTrip(t *testing.T) {
	t.Parallel()
	storageSvc, _ := newLocalStorageService(t, 1024*1024)
	e := newMCPHTTPEnv(t, storageSvc)
	session := mcpConnect(t, e, e.authToken)

	issueOut := mcpCall(t, e, session, "track_create_issue", map[string]any{
		"owner": e.ownerUsername,
		"key":   e.projKey,
		"title": "Attachment issue",
	})
	issue := decodeMCPField[model.Issue](t, issueOut, "issue")
	content := []byte("hello from mcp attachment")
	attachOut := mcpCall(t, e, session, "track_create_attachment", map[string]any{
		"owner":          e.ownerUsername,
		"issue":          issue.Identifier,
		"filename":       "note.txt",
		"content_type":   "text/plain",
		"content_base64": base64.StdEncoding.EncodeToString(content),
	})
	attachment := decodeMCPField[model.IssueAttachment](t, attachOut, "attachment")
	if attachment.Object.Ref != "object-1" || attachment.Object.Filename != "note.txt" {
		t.Fatalf("bad attachment: %+v", attachment)
	}

	readOut := mcpCall(t, e, session, "track_read_attachment_content", map[string]any{
		"owner":  e.ownerUsername,
		"issue":  issue.Identifier,
		"object": attachment.Object.Ref,
	})
	encoded := decodeMCPField[string](t, readOut, "content_base64")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("DecodeString: %v", err)
	}
	if string(decoded) != string(content) {
		t.Fatalf("content = %q, want %q", decoded, content)
	}

	res, err := session.ReadResource(e.ctx, &mcp.ReadResourceParams{
		URI: "track://attachment-content/" + e.ownerUsername + "/" + issue.Identifier + "/" + attachment.Object.Ref,
	})
	if err != nil {
		t.Fatalf("ReadResource attachment content: %v", err)
	}
	if len(res.Contents) != 1 || string(res.Contents[0].Blob) != string(content) {
		t.Fatalf("resource contents = %+v, want %q", res.Contents, content)
	}
}
