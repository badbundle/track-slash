package server_test

import (
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestUIIssueRoutesRequireAccessAndPreserveLoginNext(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "protected issue"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	linked, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "protected linked issue"})
	if err != nil {
		t.Fatalf("CreateIssue linked: %v", err)
	}
	link, err := e.store.CreateIssueLink(e.ctx, store.CreateIssueLinkParams{
		SourceID: issue.ID,
		TargetID: linked.ID,
		LinkType: model.LinkTypeRelatesTo,
	})
	if err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}
	comment, err := e.store.CreateComment(e.ctx, store.CreateCommentParams{
		IssueID:  issue.ID,
		AuthorID: e.adminID,
		Body:     "protected comment",
	})
	if err != nil {
		t.Fatalf("CreateComment protected: %v", err)
	}
	deletedIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "protected deleted issue"})
	if err != nil {
		t.Fatalf("CreateIssue deleted: %v", err)
	}
	if err := e.store.DeleteIssue(e.ctx, deletedIssue.ID); err != nil {
		t.Fatalf("DeleteIssue protected: %v", err)
	}
	_, token := e.mustUserToken(t, "ui-issue-denied")
	res := e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue), token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue detail code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	form := url.Values{"body": {"denied comment"}}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueCommentsPath(issue), token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue comment code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issueCommentsPath(issue)+"/"+comment.Ref+"/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue comment edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueCommentsPath(issue)+"/"+comment.Ref, token, strings.NewReader(url.Values{"body": {"denied edit"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue comment update code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/description/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue description edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/title/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue title edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/title", token, strings.NewReader(url.Values{"title": {"denied"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue title update code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/description", token, strings.NewReader(url.Values{"description": {"denied"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue description update code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/status/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue status edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/status", token, strings.NewReader(url.Values{"status": {string(model.StatusDone)}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue status update code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/close-reason/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue close reason edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/close-reason", token, strings.NewReader(url.Values{"close_reason": {string(model.CloseReasonInvalid)}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue close reason update code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/delete", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue delete code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(deletedIssue)+"/restore", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue restore code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(deletedIssue), token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("deleted issue detail code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(deletedIssue)+"/panel", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("deleted issue panel code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/assignee/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue assignee edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/assignee", token, strings.NewReader(url.Values{"assignee": {"denied"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue assignee update code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/reporter/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue reporter edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/reporter", token, strings.NewReader(url.Values{"reporter": {"denied"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue reporter update code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/sprint/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue sprint edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/sprint", token, strings.NewReader(url.Values{"sprint": {"denied"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue sprint update code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issueLinksPath(issue)+"/new", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue link new code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issueSubIssuesPath(issue)+"/new", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue sub-issue new code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueLinksPath(issue), token, strings.NewReader(url.Values{"relation": {"relates_to"}, "target_issue": {linked.Identifier}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue link create code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issueLinksPath(issue)+"/"+link.Ref+"/edit", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue link edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueLinksPath(issue)+"/"+link.Ref, token, strings.NewReader(url.Values{"relation": {"blocks"}, "target_issue": {linked.Identifier}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue link update code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueLinksPath(issue)+"/"+link.Ref+"/delete", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("issue link delete code = %d body = %s", res.StatusCode, readBody(t, res))
	}

	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue), "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauth issue code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/login?next="+url.QueryEscape(e.issuePath(issue)) {
		t.Fatalf("Location = %q", loc)
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issueLinksPath(issue)+"/new", "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauth issue link new code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/login?next="+url.QueryEscape(e.issueLinksPath(issue)+"/new") {
		t.Fatalf("link new Location = %q", loc)
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issueSubIssuesPath(issue)+"/new", "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauth issue sub-issue new code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/login?next="+url.QueryEscape(e.issueSubIssuesPath(issue)+"/new") {
		t.Fatalf("sub-issue new Location = %q", loc)
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/assignee/edit", "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauth assignee edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/login?next="+url.QueryEscape(e.issuePath(issue)+"/assignee/edit") {
		t.Fatalf("assignee edit Location = %q", loc)
	}
	res = e.uiDoNoRedirect(t, http.MethodGet, e.issuePath(issue)+"/sprint/edit", "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("unauth sprint edit code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/login?next="+url.QueryEscape(e.issuePath(issue)+"/sprint/edit") {
		t.Fatalf("sprint edit Location = %q", loc)
	}

	_, memberToken := e.mustProjectMemberToken(t, "ui-issue-bad-id")
	for _, tc := range []struct {
		method string
		path   string
		body   io.Reader
	}{
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref"},
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref/panel"},
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref/title/edit"},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/title", body: strings.NewReader(url.Values{"title": {"hello"}}.Encode())},
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref/description/edit"},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/description", body: strings.NewReader(url.Values{"description": {"hello"}}.Encode())},
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref/status/edit"},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/status", body: strings.NewReader(url.Values{"status": {string(model.StatusDone)}}.Encode())},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/delete"},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/restore"},
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref/assignee/edit"},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/assignee", body: strings.NewReader(url.Values{"assignee": {"hello"}}.Encode())},
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref/reporter/edit"},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/reporter", body: strings.NewReader(url.Values{"reporter": {"hello"}}.Encode())},
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref/sprint/edit"},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/sprint", body: strings.NewReader(url.Values{"sprint": {"sprint-1"}}.Encode())},
		{method: http.MethodGet, path: "/" + e.ownerUsername + "/issues/not-a-ref/links/new"},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/links", body: strings.NewReader(url.Values{"relation": {"relates_to"}, "target_issue": {linked.Identifier}}.Encode())},
		{method: http.MethodPost, path: "/" + e.ownerUsername + "/issues/not-a-ref/comments", body: strings.NewReader(url.Values{"body": {"hello"}}.Encode())},
	} {
		res := e.uiDoNoRedirect(t, tc.method, tc.path, memberToken, tc.body)
		defer res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s %s code = %d body = %s", tc.method, tc.path, res.StatusCode, readBody(t, res))
		}
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/archive", memberToken, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("removed issue archive route code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	unknownIssuePath := "/" + e.ownerUsername + "/issues/" + e.projKey + "-999999"
	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: unknownIssuePath},
		{method: http.MethodGet, path: unknownIssuePath + "/panel"},
		{method: http.MethodPost, path: unknownIssuePath + "/delete"},
		{method: http.MethodPost, path: unknownIssuePath + "/restore"},
		{method: http.MethodGet, path: unknownIssuePath + "/sprint/edit"},
		{method: http.MethodPost, path: unknownIssuePath + "/sprint"},
	} {
		res := e.uiDoNoRedirect(t, tc.method, tc.path, memberToken, nil)
		defer res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s code = %d body = %s", tc.method, tc.path, res.StatusCode, readBody(t, res))
		}
	}
	for _, tc := range []struct {
		method string
		path   string
		body   io.Reader
	}{
		{method: http.MethodGet, path: e.issueLinksPath(issue) + "/not-a-link/edit"},
		{method: http.MethodPost, path: e.issueLinksPath(issue) + "/not-a-link", body: strings.NewReader(url.Values{"relation": {"blocks"}, "target_issue": {linked.Identifier}}.Encode())},
		{method: http.MethodPost, path: e.issueLinksPath(issue) + "/not-a-link/delete"},
	} {
		res := e.uiDoNoRedirect(t, tc.method, tc.path, memberToken, tc.body)
		defer res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s %s code = %d body = %s", tc.method, tc.path, res.StatusCode, readBody(t, res))
		}
	}
}

func TestUIIssueDueDateEditorRoundtrip(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-due-date")
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "due ui issue"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	path := e.issuePath(issue) + "/due-date"

	edit := e.uiGet(t, path+"/edit", token)
	for _, want := range []string{
		"due ui issue",
		`method="post" action="` + path + `"`,
		`hx-post="` + path + `"`,
		`type="date" name="due_date" value=""`,
		`aria-label="Save due date"`,
	} {
		if !strings.Contains(edit, want) {
			t.Fatalf("due date edit missing %q: %s", want, edit)
		}
	}

	form := url.Values{"due_date": {"2099-06-24"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, path, token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("set due date code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Jun 24") || !strings.Contains(body, `aria-label="Edit due date"`) {
		t.Fatalf("set due date response missing read mode: %s", body)
	}
	got, err := e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after set due date: %v", err)
	}
	if got.DueDate == nil || got.DueDate.String() != "2099-06-24" {
		t.Fatalf("stored DueDate = %v", got.DueDate)
	}

	bad := url.Values{"due_date": {"tomorrow"}}
	res = e.uiDoNoRedirect(t, http.MethodPost, path, token, strings.NewReader(bad.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Use YYYY-MM-DD.") || !strings.Contains(body, `value="tomorrow"`) {
		t.Fatalf("bad due date response = %d %s", res.StatusCode, body)
	}

	clear := url.Values{"due_date": {""}}
	res = e.uiDoNoRedirect(t, http.MethodPost, path, token, strings.NewReader(clear.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("clear due date code = %d body = %s", res.StatusCode, body)
	}
	dueIndex := strings.Index(body, ">Due date</dt>")
	if dueIndex < 0 {
		t.Fatalf("clear due date response missing due date row: %s", body)
	}
	dueEnd := dueIndex + 500
	if dueEnd > len(body) {
		dueEnd = len(body)
	}
	dueBlock := body[dueIndex:dueEnd]
	if !strings.Contains(dueBlock, "None") || strings.Contains(dueBlock, "Jun 24") {
		t.Fatalf("clear due date response missing empty state: %s", body)
	}
	got, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after clear due date: %v", err)
	}
	if got.DueDate != nil {
		t.Fatalf("cleared DueDate = %v, want nil", got.DueDate)
	}
}
