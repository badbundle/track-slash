package server_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
		t.Fatalf("tool %s error: %s", name, raw)
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
	var got struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	raw := decodeMCPField[json.RawMessage](t, out, "error")
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal error: %v raw=%s", err, raw)
	}
	if got.Code != "validation_error" {
		t.Fatalf("error code = %q, want validation_error", got.Code)
	}
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
