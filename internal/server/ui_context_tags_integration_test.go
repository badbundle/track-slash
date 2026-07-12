package server_test

import (
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestUIProjectAndIssueContext(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-context")
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "context target issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	otherProject, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "other context ui", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}

	body := e.uiGet(t, e.projectPath()+"/about", token)
	if strings.Contains(body, `aria-label="Manage context"`) || strings.Contains(body, ">Context</dt>") {
		t.Fatalf("about should not render project context details: %s", body)
	}
	for _, notWant := range []string{"No context.", "context items", `placeholder="Context"`, `accept=".txt,.md,.markdown`, `aria-label="Create context"`, `aria-label="Upload context"`, `aria-label="Add context"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("about context body should stay compact, found %q: %s", notWant, body)
		}
	}

	body = e.uiGet(t, e.projectPath()+"/context", token)
	for _, want := range []string{"Context", "No context pages yet.", `aria-label="New context page"`, "Create the first context page"} {
		if !strings.Contains(body, want) {
			t.Fatalf("project context manager missing %q: %s", want, body)
		}
	}

	body = e.uiGet(t, e.projectPath()+"/context/new", token)
	for _, want := range []string{"New Markdown page", "Import document", `placeholder="Markdown (optional)"`, `name="file"`, `aria-label="Create context page"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("project context create manager missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `role="dialog" aria-modal="true"`) {
		t.Fatalf("project context create should not render a modal: %s", body)
	}

	res := e.uiDoMultipartContext(t, e.projectPath()+"/context", token, nil, "image.png", "nope")
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("bad upload code = %d body = %s", res.StatusCode, body)
	}
	for _, want := range []string{"file must be .txt, .md, or .markdown", "Import document"} {
		if !strings.Contains(body, want) {
			t.Fatalf("bad upload manager missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `role="dialog" aria-modal="true"`) {
		t.Fatalf("bad upload should stay in manager, not modal: %s", body)
	}

	form := url.Values{"title": {"Architecture"}, "body": {"Use the existing store path."}}
	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.projectPath()+"/context", token, strings.NewReader(form.Encode()), map[string]string{
		"HX-Current-URL": e.ts.URL + e.projectPath() + "/context/new",
		"HX-Request":     "true",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create context code = %d body = %s", res.StatusCode, body)
	}
	if replace := res.Header.Get("HX-Replace-Url"); replace != e.projectPath()+"/context/context-1" {
		t.Fatalf("create context HX-Replace-Url = %q", replace)
	}
	if push := res.Header.Get("HX-Push-Url"); push != "" {
		t.Fatalf("create context HX-Push-Url = %q, want empty", push)
	}
	for _, want := range []string{"Architecture", "Use the existing store path.", `aria-label="Manage linked issues"`, `aria-label="Edit context page"`, `aria-label="Delete context page"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("created context body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "text/markdown; charset=utf-8") {
		t.Fatalf("created context body should not display MIME metadata: %s", body)
	}
	contextPath := e.projectPath() + "/context/context-1"

	body = e.uiGet(t, contextPath+"/issues/new", token)
	for _, want := range []string{"Linked issues", `placeholder="` + e.projKey + `-12"`, "No linked issues.", `aria-label="Link issue"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("project context issue link manager missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `role="dialog" aria-modal="true"`) {
		t.Fatalf("project context issue link should not render a modal: %s", body)
	}

	body = e.uiGet(t, contextPath+"/edit", token)
	for _, want := range []string{`value="Architecture"`, "Use the existing store path.", `aria-label="Save context"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("edit context body missing %q: %s", want, body)
		}
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, contextPath, token, strings.NewReader(url.Values{"title": {""}, "body": {"Still here"}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("invalid update context code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "title required, max 200 chars") || !strings.Contains(body, "Still here") {
		t.Fatalf("invalid update context body missing error/state: %s", body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, contextPath+"/issues", token, strings.NewReader(url.Values{"issue": {"bad"}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("bad project link context code = %d body = %s", res.StatusCode, body)
	}
	if strings.Contains(body, `role="dialog" aria-modal="true"`) || !strings.Contains(body, "Choose an issue in this project.") || !strings.Contains(body, `value="bad"`) {
		t.Fatalf("bad project link context body missing error/state: %s", body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, contextPath+"/issues", token, strings.NewReader(url.Values{"issue": {otherProject.Key + "-999999"}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("cross-project link context code = %d body = %s", res.StatusCode, body)
	}
	if strings.Contains(body, `role="dialog" aria-modal="true"`) || !strings.Contains(body, "Issue must be in this project.") || strings.Contains(body, "Issue not found.") {
		t.Fatalf("cross-project link context body missing project validation: %s", body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, contextPath+"/issues", token, strings.NewReader(url.Values{"issue": {issue.Identifier}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("project link context code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, `placeholder="`+e.projKey+`-12"`) || !strings.Contains(body, `>1</span>`) {
		t.Fatalf("project link context response should keep the selected page and link manager: %s", body)
	}
	body = e.uiGet(t, contextPath+"/issues/new", token)
	if !strings.Contains(body, issue.Identifier) || !strings.Contains(body, `aria-label="Unlink issue"`) {
		t.Fatalf("project link context manager missing linked issue: %s", body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, contextPath+"/issues", token, strings.NewReader(url.Values{"issue": {issue.Identifier}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("duplicate project link context code = %d body = %s", res.StatusCode, body)
	}
	if strings.Contains(body, `role="dialog" aria-modal="true"`) || !strings.Contains(body, "Issue already linked.") {
		t.Fatalf("duplicate project link context body missing conflict error: %s", body)
	}

	form = url.Values{"title": {"Architecture v2"}, "body": {"Use the updated store path."}}
	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, contextPath, token, strings.NewReader(form.Encode()), map[string]string{
		"HX-Current-URL": e.ts.URL + contextPath + "/edit",
		"HX-Request":     "true",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update context code = %d body = %s", res.StatusCode, body)
	}
	if replace := res.Header.Get("HX-Replace-Url"); replace != contextPath {
		t.Fatalf("update context HX-Replace-Url = %q", replace)
	}
	if push := res.Header.Get("HX-Push-Url"); push != "" {
		t.Fatalf("update context HX-Push-Url = %q, want empty", push)
	}
	if !strings.Contains(body, "Architecture v2") {
		t.Fatalf("updated context body missing title: %s", body)
	}
	if !strings.Contains(body, "Use the updated store path.") {
		t.Fatalf("updated selected context should show the page body: %s", body)
	}

	issueBody := e.uiGet(t, e.issuePath(issue), token)
	issueMain := mainContentBlock(t, issueBody)
	contextDetail := issueContextDetailBlock(t, issueBody)
	for _, want := range []string{"Context", `aria-label="Manage context"`, ">1</span>", `hx-get="` + e.issuePath(issue) + `/context"`, `hx-push-url="` + e.issuePath(issue) + `/context"`} {
		if !strings.Contains(contextDetail, want) {
			t.Fatalf("issue context detail after project edit missing %q: %s", want, issueBody)
		}
	}
	for _, notWant := range []string{"context-1", "Architecture v2", "Use the updated store path.", `aria-label="Remove context"`, `aria-label="Add context"`, `aria-label="Attach context"`} {
		if strings.Contains(issueMain, notWant) {
			t.Fatalf("issue page should keep context details in manager, found %q: %s", notWant, issueBody)
		}
	}
	issueContextManager := e.uiGet(t, e.issuePath(issue)+"/context", token)
	for _, want := range []string{"Context", "Architecture v2", "Use the updated store path.", `aria-label="Edit context"`, `aria-label="Remove context"`, `aria-label="Add issue context"`, `aria-label="Attach project context"`, `aria-label="Back to issue"`} {
		if !strings.Contains(issueContextManager, want) {
			t.Fatalf("issue context manager after project edit missing %q: %s", want, issueContextManager)
		}
	}
	if strings.Contains(issueContextManager, `role="dialog" aria-modal="true"`) {
		t.Fatalf("issue context manager should not render as a modal: %s", issueContextManager)
	}
	issueContextManager = e.uiGet(t, e.issuePath(issue)+"/context/context-1", token)
	if !strings.Contains(issueContextManager, "Use the updated store path.") || !strings.Contains(issueContextManager, `aria-current="page"`) {
		t.Fatalf("issue context item view missing latest body: %s", issueContextManager)
	}
	issueContextManager = e.uiGet(t, e.issuePath(issue)+"/context/context-1/edit", token)
	for _, want := range []string{`value="Architecture v2"`, "Use the updated store path.", `aria-label="Save context"`, `aria-label="Cancel editing context"`} {
		if !strings.Contains(issueContextManager, want) {
			t.Fatalf("issue context edit manager missing project-linked %q: %s", want, issueContextManager)
		}
	}
	form = url.Values{"title": {"Architecture v3"}, "body": {"Use the issue manager edit path."}}
	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.issuePath(issue)+"/context/context-1", token, strings.NewReader(form.Encode()), map[string]string{
		"HX-Current-URL": e.ts.URL + e.issuePath(issue) + "/context/context-1/edit",
		"HX-Request":     "true",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("issue edit project context code = %d body = %s", res.StatusCode, body)
	}
	if replace := res.Header.Get("HX-Replace-Url"); replace != e.issuePath(issue)+"/context/context-1" {
		t.Fatalf("issue edit context HX-Replace-Url = %q", replace)
	}
	if push := res.Header.Get("HX-Push-Url"); push != "" {
		t.Fatalf("issue edit context HX-Push-Url = %q, want empty", push)
	}
	if !strings.Contains(body, "Architecture v3") || !strings.Contains(body, `aria-current="page"`) || !strings.Contains(body, "Use the issue manager edit path.") {
		t.Fatalf("issue edit project context response should keep the selected document visible: %s", body)
	}
	projectBody := e.uiGet(t, e.projectPath()+"/context", token)
	if !strings.Contains(projectBody, "Architecture v3") {
		t.Fatalf("project context manager missing issue-edited project context: %s", projectBody)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, contextPath+"/issues/"+issue.Identifier+"/delete", token, nil)
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("project unlink context code = %d body = %s", res.StatusCode, body)
	}
	if strings.Contains(body, `action="`+contextPath+`/issues/`+issue.Identifier+`/delete"`) {
		t.Fatalf("project unlink context body still has linked issue action: %s", body)
	}

	issueBody = e.uiGet(t, e.issuePath(issue), token)
	contextDetail = issueContextDetailBlock(t, issueBody)
	for _, want := range []string{"Context", `aria-label="Manage context"`, ">0</span>"} {
		if !strings.Contains(contextDetail, want) {
			t.Fatalf("empty issue context detail missing %q: %s", want, issueBody)
		}
	}
	for _, notWant := range []string{"No context.", `placeholder="Search context by title"`, `aria-label="Add context"`, `aria-label="Attach context"`} {
		if strings.Contains(issueBody, notWant) {
			t.Fatalf("empty issue page should keep context controls in manager, found %q: %s", notWant, issueBody)
		}
	}
	issueContextManager = e.uiGet(t, e.issuePath(issue)+"/context", token)
	for _, want := range []string{"Context", "No context attached.", "Add context to this issue", "New issue context", "Attach project context", `aria-label="Back to issue"`} {
		if !strings.Contains(issueContextManager, want) {
			t.Fatalf("empty issue context manager missing %q: %s", want, issueContextManager)
		}
	}
	if strings.Contains(issueContextManager, `role="dialog" aria-modal="true"`) {
		t.Fatalf("empty issue context route should not render a modal: %s", issueContextManager)
	}

	issueBody = e.uiGet(t, e.issuePath(issue)+"/context/new", token)
	for _, want := range []string{"New issue context", "Import text", `placeholder="Context"`, `autofocus`, `aria-label="Create context"`, `aria-label="Upload context"`, `name="file"`} {
		if !strings.Contains(issueBody, want) {
			t.Fatalf("adding issue context manager missing %q: %s", want, issueBody)
		}
	}
	for _, notWant := range []string{`placeholder="Search context by title"`} {
		if strings.Contains(issueBody, notWant) {
			t.Fatalf("adding issue context manager should not include %q: %s", notWant, issueBody)
		}
	}

	issueBody = e.uiGet(t, e.issuePath(issue)+"/context/link", token)
	issueMain = mainContentBlock(t, issueBody)
	for _, want := range []string{`placeholder="Search context by title"`, `<option value="Architecture v3">`, `autofocus`, `aria-label="Attach context"`, "Manage project context"} {
		if !strings.Contains(issueBody, want) {
			t.Fatalf("attaching issue context body missing %q: %s", want, issueBody)
		}
	}
	if strings.Contains(issueMain, "context-1") {
		t.Fatalf("attaching issue context should not expose context refs: %s", issueBody)
	}
	for _, notWant := range []string{`aria-label="Create issue context"`, `aria-label="Upload issue context"`} {
		if strings.Contains(issueBody, notWant) {
			t.Fatalf("attaching issue context body should not include %q: %s", notWant, issueBody)
		}
	}
	if strings.Contains(issueBody, `role="dialog" aria-modal="true"`) {
		t.Fatalf("attaching issue context should not render a modal: %s", issueBody)
	}

	res = e.uiDoMultipartContext(t, e.issuePath(issue)+"/context", token, nil, "image.png", "nope")
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("bad issue upload code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "file must be .txt, .md, or .markdown") || !strings.Contains(body, `aria-label="Upload context"`) || !strings.Contains(body, "Import text") {
		t.Fatalf("bad issue upload body missing error/state: %s", body)
	}

	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.issuePath(issue)+"/context", token, strings.NewReader(url.Values{"mode": {"create"}, "title": {"Issue note"}, "body": {"Only needed here."}}.Encode()), map[string]string{
		"HX-Current-URL": e.ts.URL + e.issuePath(issue) + "/context/new",
		"HX-Request":     "true",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create issue-only context code = %d body = %s", res.StatusCode, body)
	}
	if replace := res.Header.Get("HX-Replace-Url"); replace != e.issuePath(issue)+"/context/context-2" {
		t.Fatalf("create issue-only context HX-Replace-Url = %q", replace)
	}
	if push := res.Header.Get("HX-Push-Url"); push != "" {
		t.Fatalf("create issue-only context HX-Push-Url = %q, want empty", push)
	}
	for _, want := range []string{"Issue note", "Issue-only", "Only needed here.", `aria-label="Edit context"`, `aria-label="Remove context"`, `aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("issue-only context response missing %q: %s", want, body)
		}
	}
	issueContextManager = e.uiGet(t, e.issuePath(issue)+"/context/context-2", token)
	if !strings.Contains(issueContextManager, "Only needed here.") || !strings.Contains(issueContextManager, `aria-current="page"`) {
		t.Fatalf("issue-only context view missing body: %s", issueContextManager)
	}
	issueContextManager = e.uiGet(t, e.issuePath(issue)+"/context/context-2/edit", token)
	for _, want := range []string{`value="Issue note"`, "Only needed here.", `aria-label="Save context"`, `aria-label="Cancel editing context"`} {
		if !strings.Contains(issueContextManager, want) {
			t.Fatalf("issue-only context edit manager missing %q: %s", want, issueContextManager)
		}
	}
	projectBody = e.uiGet(t, e.projectPath()+"/context", token)
	if strings.Contains(projectBody, "Issue note") {
		t.Fatalf("project context manager should not show issue-only context: %s", projectBody)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/context/context-2/delete", token, nil)
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete issue-only context code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "No context attached.") || strings.Contains(body, "Issue note") {
		t.Fatalf("issue-only context remained after delete: %s", body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/context", token, strings.NewReader(url.Values{"context": {"Architecture v3"}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("link context code = %d body = %s", res.StatusCode, body)
	}
	for _, want := range []string{"Architecture v3", "Use the issue manager edit path.", `aria-label="Edit context"`, `aria-label="Remove context"`, `aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("linked issue context manager missing %q: %s", want, body)
		}
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/context", token, strings.NewReader(url.Values{"context": {""}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Context required.") || !strings.Contains(body, "Attach project context") {
		t.Fatalf("blank issue context attach should keep the manager error state, code = %d body = %s", res.StatusCode, body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/context", token, strings.NewReader(url.Values{"context": {"Architecture v3"}}.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Context already linked.") || !strings.Contains(body, "Attach project context") {
		t.Fatalf("duplicate issue context attach should keep the manager error state, code = %d body = %s", res.StatusCode, body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/context/context-1/delete", token, nil)
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("issue unlink context code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "No context attached.") || strings.Contains(body, "Use the updated store path.") {
		t.Fatalf("issue unlink context body still shows context: %s", body)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, contextPath+"/delete", token, nil)
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete context code = %d body = %s", res.StatusCode, body)
	}
	if strings.Contains(body, contextPath) || strings.Contains(body, "Architecture v3") {
		t.Fatalf("delete context body still shows deleted context: %s", body)
	}
}

func TestUIIssueTagModalAttachesAndDetachesTags(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-issue-tags")

	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "tag modal issue"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	attached, err := e.store.CreateIssueTag(e.ctx, store.CreateIssueTagParams{ProjectID: e.projectID, Name: "UI", Color: model.TagColorBlue})
	if err != nil {
		t.Fatalf("CreateIssueTag attached: %v", err)
	}
	available, err := e.store.CreateIssueTag(e.ctx, store.CreateIssueTagParams{ProjectID: e.projectID, Name: "Customer Beta", Color: model.TagColorGreen})
	if err != nil {
		t.Fatalf("CreateIssueTag available: %v", err)
	}
	if _, err := e.store.CreateIssueTagLink(e.ctx, store.CreateIssueTagLinkParams{IssueID: issue.ID, TagID: attached.ID}); err != nil {
		t.Fatalf("CreateIssueTagLink: %v", err)
	}

	res := e.uiDoNoRedirectWithHeaders(t, http.MethodGet, e.issuePath(issue)+"/tags", token, nil, map[string]string{"HX-Request": "true"})
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("tag modal GET code = %d body = %s", res.StatusCode, body)
	}
	for _, want := range []string{
		`role="dialog" aria-modal="true" aria-labelledby="issue-tags-title"`,
		`id="issue-tags-title"`,
		`Manage tags`,
		`Search project tags for ` + issue.Identifier + `.`,
		`#UI`,
		`#Customer Beta`,
		`data-search-input`,
		`data-search-option data-value="Customer Beta"`,
		`hx-post="` + e.issuePath(issue) + `/tags"`,
		`hx-post="` + e.issuePath(issue) + `/tags/` + attached.Ref + `/delete"`,
		`href="` + e.projectPath() + `/tags"`,
		`Manage project tags`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("tag modal GET missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{`>Issue tags</h1>`, `hx-push-url="` + e.issuePath(issue) + `/tags"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("tag modal GET rendered stale fullscreen/push markup %q: %s", notWant, body)
		}
	}

	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.issuePath(issue)+"/tags", token, strings.NewReader(url.Values{"tag": {available.Name}}.Encode()), map[string]string{"HX-Request": "true"})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("attach tag code = %d body = %s", res.StatusCode, body)
	}
	if got := res.Header.Get("HX-Replace-Url"); got != e.issuePath(issue) {
		t.Fatalf("attach tag HX-Replace-Url = %q, want %q", got, e.issuePath(issue))
	}
	for _, want := range []string{`id="issue-tags-title"`, `#UI`, `#Customer Beta`, `No available tags.`} {
		if !strings.Contains(body, want) {
			t.Fatalf("attach tag response missing %q: %s", want, body)
		}
	}
	updated, err := e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after attach: %v", err)
	}
	if len(updated.Tags) != 2 {
		t.Fatalf("issue tags after attach = %+v, want 2", updated.Tags)
	}

	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.issuePath(issue)+"/tags", token, strings.NewReader(url.Values{"tag": {available.Name}}.Encode()), map[string]string{"HX-Request": "true"})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Tag already attached.") || !strings.Contains(body, `id="issue-tags-title"`) {
		t.Fatalf("duplicate attach should keep modal with error, code = %d body = %s", res.StatusCode, body)
	}

	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.issuePath(issue)+"/tags", token, strings.NewReader(url.Values{"tag": {""}}.Encode()), map[string]string{"HX-Request": "true"})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Choose a tag.") || !strings.Contains(body, `id="issue-tags-title"`) {
		t.Fatalf("empty attach should keep modal with error, code = %d body = %s", res.StatusCode, body)
	}

	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.issuePath(issue)+"/tags/"+attached.Ref+"/delete", token, nil, map[string]string{"HX-Request": "true"})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("detach tag code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, `id="issue-tags-title"`) || !strings.Contains(body, `data-search-option data-value="UI"`) {
		t.Fatalf("detach response should keep modal and make removed tag available: %s", body)
	}
	updated, err = e.store.GetIssue(e.ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after detach: %v", err)
	}
	if len(updated.Tags) != 1 || updated.Tags[0].ID != available.ID {
		t.Fatalf("issue tags after detach = %+v, want only %s", updated.Tags, available.DisplayName)
	}
}
