package server_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bradleymackey/track-slash/internal/model"
)

// Date fields travel as YYYY-MM-DD strings, and the tool schemas the SDK
// validates arguments against have to say so.
func TestMCPDueDates(t *testing.T) {
	t.Parallel()
	e := newMCPHTTPEnv(t, nil)
	session := mcpConnect(t, e, e.authToken)

	tools, err := session.ListTools(e.ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	checked := 0
	for _, tool := range tools.Tools {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s schema: %v", tool.Name, err)
		}
		var schema struct {
			Properties map[string]struct {
				Type   any    `json:"type"`
				Format string `json:"format"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal %s schema: %v", tool.Name, err)
		}
		due, ok := schema.Properties["due_date"]
		if !ok {
			continue
		}
		checked++
		types, _ := json.Marshal(due.Type)
		if due.Format != "date" || !strings.Contains(string(types), `"string"`) || strings.Contains(string(types), `"object"`) {
			t.Fatalf("%s due_date schema = %s format %q", tool.Name, types, due.Format)
		}
	}
	if checked != 3 {
		t.Fatalf("tools with a due_date = %d, want 3", checked)
	}

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
	dueIs := func(issue model.Issue, want string) bool {
		return issue.DueDate != nil && issue.DueDate.String() == want
	}

	created := decodeMCPField[model.Issue](t, mcpCall(t, e, session, "track_create_issue", with(project, map[string]any{"title": "dated", "due_date": "2026-11-30"})), "issue")
	if !dueIs(created, "2026-11-30") {
		t.Fatalf("create due_date = %v", created.DueDate)
	}
	issue := map[string]any{"owner": e.ownerUsername, "issue": created.Identifier}
	sub := decodeMCPField[model.Issue](t, mcpCall(t, e, session, "track_create_sub_issue", with(issue, map[string]any{"title": "dated sub-issue", "due_date": "2026-12-01"})), "issue")
	if !dueIs(sub, "2026-12-01") {
		t.Fatalf("sub-issue due_date = %v", sub.DueDate)
	}
	updated := decodeMCPField[model.Issue](t, mcpCall(t, e, session, "track_update_issue", with(issue, map[string]any{"due_date": "2027-01-15"})), "issue")
	if !dueIs(updated, "2027-01-15") {
		t.Fatalf("update due_date = %v", updated.DueDate)
	}
	cleared := decodeMCPField[model.Issue](t, mcpCall(t, e, session, "track_update_issue", with(issue, map[string]any{"clear_due_date": true})), "issue")
	if cleared.DueDate != nil {
		t.Fatalf("clear due_date = %v", cleared.DueDate)
	}
	// Null leaves the date alone, as leaving it out does.
	if _, err := session.CallTool(e.ctx, &mcp.CallToolParams{Name: "track_update_issue", Arguments: with(issue, map[string]any{"due_date": nil, "title": "still undated"})}); err != nil {
		t.Fatalf("update with null due_date: %v", err)
	}

	// A date in another shape is refused before anything changes.
	for _, bad := range []any{"30/11/2026", "2026-02-30", 20261130, map[string]any{}} {
		res, err := session.CallTool(e.ctx, &mcp.CallToolParams{Name: "track_update_issue", Arguments: with(issue, map[string]any{"due_date": bad})})
		if err != nil {
			t.Fatalf("CallTool with due_date %v: %v", bad, err)
		}
		content, _ := json.Marshal(res.Content)
		if !res.IsError {
			t.Fatalf("due_date %v accepted: %s", bad, content)
		}
	}
	got, err := e.store.GetIssue(e.ctx, created.ID)
	if err != nil || got.DueDate != nil || got.Title != "still undated" {
		t.Fatalf("issue after refused dates = due %v title %q, %v", got.DueDate, got.Title, err)
	}
}
