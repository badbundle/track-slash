package server_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

var uiCSRFInputPattern = regexp.MustCompile(`(?i)name="csrf_token"\s+value="([^"]*)"`)

// The static template guard proves the field is present; only a real render
// proves it is populated. A forgotten construction site renders value="" and
// fails at submit time, not at build time.
func TestRenderedPagesCarryAPopulatedCSRFToken(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	// The owner token reaches every panel, including member management.
	token := e.authToken
	issue := e.mustIssue(t, "csrf form issue")

	for _, path := range []string{
		"/me",
		"/projects",
		"/projects/new",
		"/issues/new",
		"/settings",
		"/tokens",
		e.projectPath() + "/sprint",
		e.projectPath() + "/about",
		e.projectPath() + "/members",
		e.projectPath() + "/sprints",
		e.projectPath() + "/planned",
		e.projectPath() + "/all",
		e.projectPath() + "/context",
		e.projectPath() + "/tags",
		e.projectPath() + "/deleted",
		e.projectPath() + "/issues/new",
		"/" + e.ownerUsername + "/issues/" + issue.Identifier,
		"/" + e.ownerUsername + "/issues/" + issue.Identifier + "/context",
		"/" + e.ownerUsername + "/issues/" + issue.Identifier + "/tags",
	} {
		t.Run(path, func(t *testing.T) {
			body := e.uiGet(t, path, token)
			assertPopulatedCSRFTokens(t, path, body)
		})
	}
}

// Editing forms are rendered on demand as fragments, so they need the same
// guarantee as the pages that host them.
func TestRenderedIssueEditFragmentsCarryAPopulatedCSRFToken(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	token := e.authToken
	issue := e.mustIssue(t, "csrf fragment issue")
	issuePath := "/" + e.ownerUsername + "/issues/" + issue.Identifier

	for _, path := range []string{
		issuePath + "/title/edit",
		issuePath + "/description/edit",
		issuePath + "/status/edit",
		issuePath + "/priority/edit",
		issuePath + "/due-date/edit",
		issuePath + "/assignee/edit",
		issuePath + "/reporter/edit",
		issuePath + "/sprint/edit",
		issuePath + "/links/new",
		issuePath + "/sub-issues/new",
		e.projectPath() + "/member-candidates?username=nobody",
		e.projectPath() + "/name/edit",
		e.projectPath() + "/description/edit",
	} {
		t.Run(path, func(t *testing.T) {
			body := e.uiGet(t, path, token)
			assertPopulatedCSRFTokens(t, path, body)
		})
	}
}

func assertPopulatedCSRFTokens(t *testing.T, path, body string) {
	t.Helper()
	matches := uiCSRFInputPattern.FindAllStringSubmatch(body, -1)
	for _, match := range matches {
		if strings.TrimSpace(match[1]) == "" {
			t.Fatalf("%s rendered an empty csrf_token; a construction site is missing uiSessionCSRFToken", path)
		}
	}
	// A page with a posting form but no token at all would slip past the loop
	// above, so check the two counts agree.
	forms := strings.Count(strings.ToLower(body), `method="post"`)
	if forms > len(matches) {
		t.Fatalf("%s has %d posting forms but only %d csrf_token fields", path, forms, len(matches))
	}
}

func (e *httpEnv) mustIssue(t *testing.T, title string) issueRef {
	t.Helper()
	code, body := e.do(t, http.MethodPost, e.projectPath()+"/issues", map[string]any{"title": title})
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("create issue code = %d body = %s", code, body)
	}
	return decode[issueRef](t, body)
}

type issueRef struct {
	Identifier string `json:"identifier"`
}
