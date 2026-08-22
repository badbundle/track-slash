package server_test

import (
	"net/http"
	"strings"
	"testing"
)

// A real connection must answer HEAD with the GET headers and no body, which is
// what link-preview crawlers and `curl -I` expect.
func TestHEADOnASignedInPageCarriesHeadersAndNoBody(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "head-probe")

	head := e.uiDoNoRedirect(t, http.MethodHead, "/me", token, nil)
	defer head.Body.Close()
	headBody := readBody(t, head)

	if head.StatusCode != http.StatusOK {
		t.Fatalf("HEAD /me code = %d body = %s", head.StatusCode, headBody)
	}
	if headBody != "" {
		t.Fatalf("HEAD /me returned a body: %s", headBody)
	}

	get := e.uiDoNoRedirect(t, http.MethodGet, "/me", token, nil)
	defer get.Body.Close()
	getBody := readBody(t, get)

	if get.StatusCode != head.StatusCode {
		t.Fatalf("GET /me code = %d, HEAD code = %d", get.StatusCode, head.StatusCode)
	}
	if get.Header.Get("Content-Type") != head.Header.Get("Content-Type") {
		t.Fatalf("GET Content-Type = %q, HEAD = %q", get.Header.Get("Content-Type"), head.Header.Get("Content-Type"))
	}
	if getBody == "" {
		t.Fatal("GET /me returned an empty body")
	}
}

// anonymousProjectReadAllowed has always whitelisted HEAD, but the branch was
// unreachable while every route answered a HEAD probe with 405.
func TestHEADOnAPublicProjectPageIsAllowedAnonymously(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	code, body := e.do(t, http.MethodPatch, e.projectPath()+"/access", map[string]any{
		"is_public":             true,
		"public_issue_creation": false,
	})
	if code != http.StatusOK {
		t.Fatalf("enable public access code = %d body = %s", code, body)
	}

	res := e.uiDoNoRedirect(t, http.MethodHead, e.projectPath()+"/all", "", nil)
	defer res.Body.Close()
	resBody := readBody(t, res)

	if res.StatusCode != http.StatusOK {
		t.Fatalf("anonymous HEAD code = %d location = %q body = %s", res.StatusCode, res.Header.Get("Location"), resBody)
	}
	if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
}
