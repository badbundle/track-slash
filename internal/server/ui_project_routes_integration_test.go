package server_test

import (
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestUIIssueListsLinkToIssueDetail(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-issue-links")
	dueDate, err := model.ParseDate("2099-06-24")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "linked from lists",
		AssigneeID: &user.ID,
		DueDate:    &dueDate,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	sp, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Linked Active Sprint",
		StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}
	active := model.SprintStatusActive
	if _, err := e.store.UpdateSprint(e.ctx, sp.ID, store.UpdateSprintParams{Status: &active}); err != nil {
		t.Fatalf("UpdateSprint active: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, issue.ID, store.UpdateIssueParams{SprintID: &sp.ID}); err != nil {
		t.Fatalf("assign sprint: %v", err)
	}
	wantHref := `href="` + e.issuePath(issue) + `"`
	wantHXGet := `hx-get="` + e.issuePath(issue) + `/panel"`
	wantHXPush := `hx-push-url="` + e.issuePath(issue) + `"`

	for _, path := range []string{e.projectPath() + "/all", "/me", "/me/all"} {
		body := e.uiGet(t, path, token)
		for _, want := range []string{wantHref, wantHXGet, wantHXPush, `data-main-view="projects"`} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing issue link %q: %s", path, want, body)
			}
		}
		if !strings.Contains(body, "Jun 24") {
			t.Fatalf("%s missing due date badge: %s", path, body)
		}
	}
}

func TestUIProjectRoutesRedirectAndRejectOldGlobals(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-routes")

	res := e.uiDoNoRedirect(t, http.MethodGet, e.projectPath(), token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("project root code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != e.projectPath()+"/sprint" {
		t.Fatalf("project root Location = %q", loc)
	}

	res = e.uiDoNoRedirect(t, http.MethodGet, "/projects/"+e.projectID.String(), token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("old project UUID route code = %d body = %s", res.StatusCode, readBody(t, res))
	}

	for _, path := range []string{"/sprint", "/sprint/panel", "/backlog", "/backlog/panel"} {
		res := e.uiDoNoRedirect(t, http.MethodGet, path, token, nil)
		defer res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s code = %d body = %s", path, res.StatusCode, readBody(t, res))
		}
	}
}

func TestUIProjectChildRoutesRequireAccess(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustUserToken(t, "ui-no-project")
	sp, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Denied Sprint",
		StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "denied sprint issue"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	for _, path := range []string{
		e.projectPath() + "/about",
		e.projectPath() + "/about/panel",
		e.projectPath() + "/name/edit?view=about",
		e.projectPath() + "/description/edit",
		e.projectPath() + "/sprint",
		e.projectPath() + "/sprint/panel",
		e.projectPath() + "/planned",
		e.projectPath() + "/planned/panel",
		e.projectPath() + "/sprints/new",
		e.projectPath() + "/sprints/" + sp.Ref + "/edit",
		e.projectPath() + "/sprints/" + sp.Ref + "/issues/new",
		e.projectPath() + "/all",
		e.projectPath() + "/all/panel",
		e.projectPath() + "/all/page",
		e.projectPath() + "/backlog",
		e.projectPath() + "/backlog/panel",
		e.projectPath() + "/issues/new",
		e.projectPath() + "/issues/new/panel",
		e.projectPath() + "/deleted",
		e.projectPath() + "/deleted/panel",
	} {
		res := e.uiDoNoRedirect(t, http.MethodGet, path, token, nil)
		defer res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("%s code = %d body = %s", path, res.StatusCode, readBody(t, res))
		}
	}
	res := e.uiDoNoRedirect(t, http.MethodPost, e.projectPath()+"/issues", token, strings.NewReader(url.Values{"title": {"denied"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("create issue denied code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	for _, item := range []struct {
		path string
		form url.Values
	}{
		{path: e.projectPath() + "/name", form: url.Values{"view": {"about"}, "name": {"Denied"}}},
		{path: e.projectPath() + "/description", form: url.Values{"description": {"denied"}}},
		{path: e.projectPath() + "/sprints", form: url.Values{"name": {"Denied"}, "start_date": {"2026-06-01"}, "end_date": {"2026-06-14"}}},
		{path: e.projectPath() + "/sprints/" + sp.Ref, form: url.Values{"name": {"Denied"}, "start_date": {"2026-06-01"}, "end_date": {"2026-06-14"}}},
		{path: e.projectPath() + "/sprints/" + sp.Ref + "/activate", form: url.Values{}},
		{path: e.projectPath() + "/sprints/" + sp.Ref + "/complete", form: url.Values{}},
		{path: e.projectPath() + "/sprints/" + sp.Ref + "/delete", form: url.Values{}},
		{path: e.projectPath() + "/sprints/" + sp.Ref + "/move-up", form: url.Values{}},
		{path: e.projectPath() + "/sprints/" + sp.Ref + "/move-down", form: url.Values{}},
		{path: e.projectPath() + "/sprints/" + sp.Ref + "/issues", form: url.Values{"issue": {issue.Identifier}}},
	} {
		res := e.uiDoNoRedirect(t, http.MethodPost, item.path, token, strings.NewReader(item.form.Encode()))
		defer res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("%s denied code = %d body = %s", item.path, res.StatusCode, readBody(t, res))
		}
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, "/issues", token, strings.NewReader(url.Values{"project_id": {e.projectID.String()}, "title": {"denied"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("global create issue denied code = %d body = %s", res.StatusCode, readBody(t, res))
	}

	res = e.uiDoNoRedirect(t, http.MethodGet, e.projectPath()+"/issues/new", "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauth new issue code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/login?next="+url.QueryEscape(e.projectPath()+"/issues/new") {
		t.Fatalf("new issue Location = %q", loc)
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, "/issues/new", "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauth global new issue code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/login?next="+url.QueryEscape("/issues/new") {
		t.Fatalf("global new issue Location = %q", loc)
	}
}

func TestUIRendersProjectSprintEmptyState(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-empty")
	body := e.uiGet(t, e.projectPath()+"/sprint", token)
	if !strings.Contains(body, "No active sprint.") {
		t.Fatalf("body missing no-active-sprint state: %s", body)
	}
}

func TestUIProjectSprintDoesNotIncludeBacklog(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-sprint")
	dueDate, err := model.ParseDate("2099-06-24")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	sp, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Active UI Sprint",
		StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}
	active := model.SprintStatusActive
	if _, err := e.store.UpdateSprint(e.ctx, sp.ID, store.UpdateSprintParams{Status: &active}); err != nil {
		t.Fatalf("UpdateSprint active: %v", err)
	}
	inSprint, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "issue inside active sprint", DueDate: &dueDate})
	if err != nil {
		t.Fatalf("CreateIssue in sprint: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, inSprint.ID, store.UpdateIssueParams{SprintID: &sp.ID}); err != nil {
		t.Fatalf("assign issue: %v", err)
	}
	if _, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "issue still in backlog"}); err != nil {
		t.Fatalf("CreateIssue backlog: %v", err)
	}

	body := e.uiGet(t, e.projectPath()+"/sprint", token)
	if !strings.Contains(body, "issue inside active sprint") {
		t.Fatalf("sprint body missing sprint issue: %s", body)
	}
	if !strings.Contains(body, "Jun 24") {
		t.Fatalf("sprint body missing issue due date: %s", body)
	}
	if strings.Contains(body, "issue still in backlog") {
		t.Fatalf("sprint body included backlog issue: %s", body)
	}
}
