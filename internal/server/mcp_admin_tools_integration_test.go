package server_test

import (
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
)

// The user and token tools take their IDs as UUID strings, the way every other
// tool takes an ID; they once described them as byte arrays no client sends.
func TestMCPUserAndTokenToolsTakeStringIDs(t *testing.T) {
	t.Parallel()
	e := newMCPHTTPEnv(t, nil)
	admin := mcpConnect(t, e, e.authToken)

	username := "mcpadmin" + strings.ToLower(uniqueProjectKey(t))
	user := decodeMCPField[model.User](t, mcpCall(t, e, admin, "track_create_user", map[string]any{
		"username": username, "email": username + "@example.com", "name": "MCP Admin Target",
	}), "user")
	if got := decodeMCPField[model.User](t, mcpCall(t, e, admin, "track_get_user", map[string]any{"id": user.ID.String()}), "user"); got.ID != user.ID {
		t.Fatalf("track_get_user = %+v", got)
	}
	created := decodeMCPField[struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}](t, mcpCall(t, e, admin, "track_create_user_token", map[string]any{"user_id": user.ID.String(), "name": "agent"}), "token")
	if created.ID == "" || created.Token == "" {
		t.Fatalf("track_create_user_token = %+v", created)
	}
	tokens := decodeMCPField[[]model.AuthToken](t, mcpCall(t, e, admin, "track_list_user_tokens", map[string]any{"user_id": user.ID.String()}), "tokens")
	if len(tokens) != 1 || tokens[0].ID.String() != created.ID {
		t.Fatalf("track_list_user_tokens = %+v", tokens)
	}

	// The user revokes a token of their own; the admin revokes one of theirs.
	targetSession := mcpConnect(t, e, created.Token)
	second := decodeMCPField[struct {
		ID string `json:"id"`
	}](t, mcpCall(t, e, targetSession, "track_create_my_token", map[string]any{"name": "spare"}), "token")
	mcpCall(t, e, targetSession, "track_revoke_my_token", map[string]any{"id": second.ID})
	mcpCall(t, e, admin, "track_revoke_token", map[string]any{"id": created.ID})
	for _, token := range decodeMCPField[[]model.AuthToken](t, mcpCall(t, e, admin, "track_list_user_tokens", map[string]any{"user_id": user.ID.String()}), "tokens") {
		if token.RevokedAt == nil {
			t.Fatalf("token %s still live after revoke", token.ID)
		}
	}
	mcpCall(t, e, admin, "track_delete_user", map[string]any{"id": user.ID.String()})
	requireMCPErrorCode(t, mcpCallExpectError(t, e, admin, "track_get_user", map[string]any{"id": user.ID.String()}), "not_found")

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"track_get_user", map[string]any{"id": "not-a-uuid"}},
		{"track_delete_user", map[string]any{"id": "not-a-uuid"}},
		{"track_create_user_token", map[string]any{"user_id": "not-a-uuid", "name": "x"}},
		{"track_list_user_tokens", map[string]any{"user_id": "not-a-uuid"}},
		{"track_revoke_token", map[string]any{"id": "not-a-uuid"}},
		{"track_revoke_my_token", map[string]any{"id": "not-a-uuid"}},
	} {
		requireMCPErrorCode(t, mcpCallExpectError(t, e, admin, tc.tool, tc.args), "validation_error")
	}
}
