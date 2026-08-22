package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
)

// access and blocks are legal usernames, and chi resolves a static segment
// before a param one. While the collection-level actions lived under /members/,
// a role change for either member ran a project-wide action instead.
func TestUIMemberNamedAfterACollectionActionKeepsItsOwnRoute(t *testing.T) {
	t.Parallel()

	for _, username := range []string{"access", "blocks", "candidates"} {
		t.Run(username, func(t *testing.T) {
			t.Parallel()
			e := newHTTPEnv(t)

			member := e.mustUserNamed(t, username)
			if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, member.ID); err != nil {
				t.Fatalf("GrantProjectAccess: %v", err)
			}

			res := e.uiDoNoRedirect(t, http.MethodPost, e.projectPath()+"/members/"+username, e.authToken,
				strings.NewReader("role=readonly"))
			body := readBody(t, res)
			res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("update member code = %d body = %s", res.StatusCode, body)
			}

			if got := e.mustProjectMemberRole(t, member.ID); got != model.ProjectMemberRoleReadonly {
				t.Fatalf("@%s role = %q, want %q", username, got, model.ProjectMemberRoleReadonly)
			}

			settings, err := e.store.GetProjectAccessSettings(e.ctx, e.projectID)
			if err != nil {
				t.Fatalf("GetProjectAccessSettings: %v", err)
			}
			if settings.IsPublic || settings.PublicIssueCreation {
				t.Fatalf("updating @%s changed project visibility: %+v", username, settings)
			}
			blocks, err := e.store.ListProjectUserBlocks(e.ctx, e.projectID)
			if err != nil {
				t.Fatalf("ListProjectUserBlocks: %v", err)
			}
			if len(blocks) != 0 {
				t.Fatalf("updating @%s blocked users: %+v", username, blocks)
			}
		})
	}
}

// The collection-level actions still work from their new paths beside /members.
func TestUIProjectMemberCollectionActionsMovedOutOfTheUsernameNamespace(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	outsider := e.mustUserNamed(t, "outsider-user")

	res := e.uiDoNoRedirect(t, http.MethodPost, e.projectPath()+"/member-access", e.authToken,
		strings.NewReader("is_public=on&public_issue_creation=on"))
	body := readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("member-access code = %d body = %s", res.StatusCode, body)
	}
	settings, err := e.store.GetProjectAccessSettings(e.ctx, e.projectID)
	if err != nil {
		t.Fatalf("GetProjectAccessSettings: %v", err)
	}
	if !settings.IsPublic || !settings.PublicIssueCreation {
		t.Fatalf("access settings = %+v, want public with public issue creation", settings)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.projectPath()+"/member-blocks", e.authToken,
		strings.NewReader("username="+outsider.Username))
	body = readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("member-blocks code = %d body = %s", res.StatusCode, body)
	}
	blocks, err := e.store.ListProjectUserBlocks(e.ctx, e.projectID)
	if err != nil {
		t.Fatalf("ListProjectUserBlocks: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Username != outsider.Username {
		t.Fatalf("blocks = %+v, want only @%s", blocks, outsider.Username)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.projectPath()+"/member-blocks/"+outsider.Username+"/delete", e.authToken, nil)
	body = readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("unblock code = %d body = %s", res.StatusCode, body)
	}
	blocks, err = e.store.ListProjectUserBlocks(e.ctx, e.projectID)
	if err != nil {
		t.Fatalf("ListProjectUserBlocks: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("blocks after unblock = %+v", blocks)
	}

	candidates := e.uiGet(t, e.projectPath()+"/member-candidates?username="+outsider.Username, e.authToken)
	if !strings.Contains(candidates, outsider.Username) {
		t.Fatalf("member-candidates missing @%s: %s", outsider.Username, candidates)
	}
}

// The member page must link to the relocated actions, not the old paths under
// the username namespace.
func TestUIProjectMemberPageUsesTheRelocatedActionPaths(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	page := e.uiGet(t, e.projectPath()+"/members", e.authToken)
	for _, want := range []string{
		e.projectPath() + "/member-access",
		e.projectPath() + "/member-blocks",
		e.projectPath() + "/member-candidates",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("member page missing %q", want)
		}
	}
	for _, unwanted := range []string{
		e.projectPath() + "/members/access",
		e.projectPath() + "/members/blocks",
		e.projectPath() + "/members/candidates",
	} {
		if strings.Contains(page, unwanted) {
			t.Fatalf("member page still uses the captured path %q", unwanted)
		}
	}
}

func (e *httpEnv) mustUserNamed(t *testing.T, username string) model.User {
	t.Helper()
	user, err := e.store.CreateUser(e.ctx, username+"@example.com", username)
	if err != nil {
		t.Fatalf("CreateUser %s: %v", username, err)
	}
	if user.Username != username {
		t.Fatalf("username = %q, want %q", user.Username, username)
	}
	return user
}

func (e *httpEnv) mustProjectMemberRole(t *testing.T, userID uuid.UUID) model.ProjectMemberRole {
	t.Helper()
	members, err := e.store.ListProjectMembers(e.ctx, e.projectID)
	if err != nil {
		t.Fatalf("ListProjectMembers: %v", err)
	}
	for _, member := range members {
		if member.UserID == userID {
			return member.Role
		}
	}
	t.Fatalf("user %s is not a member: %+v", userID, members)
	return ""
}
