package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
)

func TestHTTPPublicProjectAccessIssueCreationAndBlocks(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	outsider, outsiderToken := e.mustUserToken(t, "public-api-outsider")

	code, body := e.doUnauth(t, http.MethodGet, e.projectPath(), nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("private anonymous get code = %d body = %s", code, body)
	}
	privateUI := e.uiDoNoRedirect(t, http.MethodGet, e.projectPath()+"/all", "", nil)
	privateUIBody := readBody(t, privateUI)
	privateUI.Body.Close()
	if privateUI.StatusCode != http.StatusSeeOther || !strings.HasPrefix(privateUI.Header.Get("Location"), "/login?next=") {
		t.Fatalf("private anonymous UI code = %d location = %q body = %s", privateUI.StatusCode, privateUI.Header.Get("Location"), privateUIBody)
	}

	accessPath := e.projectPath() + "/access"
	code, body = e.do(t, http.MethodPatch, accessPath, map[string]any{
		"is_public":             true,
		"public_issue_creation": false,
	})
	if code != http.StatusOK {
		t.Fatalf("enable public access code = %d body = %s", code, body)
	}
	settings := decode[model.ProjectAccessSettings](t, body)
	if !settings.IsPublic || settings.PublicIssueCreation {
		t.Fatalf("public read settings = %+v", settings)
	}
	code, body = e.doUnauth(t, http.MethodGet, accessPath, nil)
	if code != http.StatusOK || decode[model.ProjectAccessSettings](t, body) != settings {
		t.Fatalf("anonymous get access settings code = %d body = %s", code, body)
	}

	code, body = e.doUnauth(t, http.MethodGet, e.projectPath(), nil)
	if code != http.StatusOK || decode[projectResponseDecoded](t, body).ID != e.projectID {
		t.Fatalf("public anonymous get code = %d body = %s", code, body)
	}
	code, body = e.doUnauth(t, http.MethodGet, "/projects", nil)
	if code != http.StatusOK || !projectResponseInPage(decodePage[projectResponseDecoded](t, body).Items, e.projectID.String()) {
		t.Fatalf("public anonymous list code = %d body = %s", code, body)
	}
	code, body = e.doUnauth(t, http.MethodPatch, accessPath, map[string]any{"is_public": false})
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous access update code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, outsiderToken, http.MethodPatch, e.projectPath(), map[string]any{"description": "denied"})
	if code != http.StatusForbidden {
		t.Fatalf("public outsider project update code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, outsiderToken, http.MethodPost, e.projectIssuesPath(), map[string]any{"title": "disabled"})
	if code != http.StatusForbidden {
		t.Fatalf("disabled public issue creation code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPatch, accessPath, map[string]any{
		"is_public":             true,
		"public_issue_creation": true,
	})
	if code != http.StatusOK {
		t.Fatalf("enable public issue creation code = %d body = %s", code, body)
	}
	code, body = e.doUnauth(t, http.MethodPost, e.projectIssuesPath(), map[string]any{"title": "anonymous"})
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous public issue creation code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, outsiderToken, http.MethodPost, e.projectIssuesPath(), map[string]any{
		"title":       "Public API submission",
		"assignee_id": e.adminID,
	})
	if code != http.StatusForbidden {
		t.Fatalf("public issue with assignee code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, outsiderToken, http.MethodPost, e.projectIssuesPath(), map[string]any{
		"title":       "Public API submission",
		"description": "Created without project write access.",
	})
	if code != http.StatusCreated {
		t.Fatalf("public issue creation code = %d body = %s", code, body)
	}
	issue := decode[model.Issue](t, body)
	if issue.ReporterID == nil || *issue.ReporterID != outsider.ID || issue.AssigneeID != nil {
		t.Fatalf("public issue attribution = %+v", issue)
	}
	code, body = e.doUnauth(t, http.MethodGet, e.issuePath(issue), nil)
	if code != http.StatusOK {
		t.Fatalf("public anonymous issue get code = %d body = %s", code, body)
	}

	publicUI := e.uiDoNoRedirect(t, http.MethodGet, e.projectPath()+"/all", "", nil)
	publicUIBody := readBody(t, publicUI)
	publicUI.Body.Close()
	if publicUI.StatusCode != http.StatusOK {
		t.Fatalf("public anonymous UI code = %d body = %s", publicUI.StatusCode, publicUIBody)
	}
	for _, want := range []string{"Sign in", "Create account", `aria-label="Sign in to create an issue"`} {
		if !strings.Contains(publicUIBody, want) {
			t.Fatalf("public anonymous UI missing %q: %s", want, publicUIBody)
		}
	}
	for _, forbidden := range []string{`aria-label="Favorite project"`, `aria-label="Edit project name"`, `aria-label="New issue"`, ">Members</span>"} {
		if strings.Contains(publicUIBody, forbidden) {
			t.Fatalf("public anonymous UI rendered mutation control %q: %s", forbidden, publicUIBody)
		}
	}

	blockPath := e.projectPath() + "/blocks/" + outsider.Username
	code, body = e.do(t, http.MethodPut, blockPath, nil)
	if code != http.StatusOK {
		t.Fatalf("block user code = %d body = %s", code, body)
	}
	block := decode[model.ProjectUserBlock](t, body)
	if block.UserID != outsider.ID || block.ProjectID != e.projectID {
		t.Fatalf("block = %+v", block)
	}
	code, body = e.do(t, http.MethodGet, e.projectPath()+"/blocks", nil)
	if code != http.StatusOK || len(decode[[]model.ProjectUserBlock](t, body)) != 1 {
		t.Fatalf("list blocks code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, outsiderToken, http.MethodGet, e.projectPath(), nil)
	if code != http.StatusForbidden {
		t.Fatalf("blocked public read code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, outsiderToken, http.MethodPost, e.projectIssuesPath(), map[string]any{"title": "blocked"})
	if code != http.StatusForbidden {
		t.Fatalf("blocked public issue creation code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, outsiderToken, http.MethodGet, "/projects", nil)
	if code != http.StatusOK || projectResponseInPage(decodePage[projectResponseDecoded](t, body).Items, e.projectID.String()) {
		t.Fatalf("blocked project list code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodPut, e.projectPath()+"/blocks/"+e.ownerUsername, nil)
	if code != http.StatusConflict {
		t.Fatalf("block owner code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodDelete, blockPath, nil)
	if code != http.StatusNoContent {
		t.Fatalf("unblock user code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, outsiderToken, http.MethodGet, e.projectPath(), nil)
	if code != http.StatusOK {
		t.Fatalf("unblocked public read code = %d body = %s", code, body)
	}
}

func projectResponseInPage(projects []projectResponseDecoded, id string) bool {
	for _, project := range projects {
		if project.ID.String() == id {
			return true
		}
	}
	return false
}
