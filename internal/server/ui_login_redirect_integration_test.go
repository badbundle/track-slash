package server_test

import (
	"net/http"
	"strings"
	"testing"
)

// With an expired session, every htmx control on the page hits the auth
// middleware. A plain 303 there renders the login card inside the app shell,
// beside the sidebar it was supposed to replace.
func TestExpiredSessionAnswersHTMXWithHXRedirect(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	res := e.uiDoNoRedirectWithHeaders(t, http.MethodGet, "/me/panel", "expired-session", nil, map[string]string{
		"HX-Request":     "true",
		"HX-Current-URL": "http://localhost:8080/me",
	})
	defer res.Body.Close()
	body := readBody(t, res)

	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("code = %d body = %s", res.StatusCode, body)
	}
	if got := res.Header.Get("HX-Redirect"); got != "/login?next=%2Fme" {
		t.Fatalf("HX-Redirect = %q", got)
	}
	if strings.Contains(body, "<!doctype html>") {
		t.Fatalf("htmx received a document to swap: %s", body)
	}
}

// A plain navigation still gets a normal redirect the browser can follow.
func TestExpiredSessionRedirectsPlainNavigation(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	res := e.uiDoNoRedirect(t, http.MethodGet, "/me", "expired-session", nil)
	defer res.Body.Close()
	body := readBody(t, res)

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("code = %d body = %s", res.StatusCode, body)
	}
	if got := res.Header.Get("Location"); got != "/login?next=%2Fme" {
		t.Fatalf("Location = %q", got)
	}
	if got := res.Header.Get("HX-Redirect"); got != "" {
		t.Fatalf("HX-Redirect = %q, want absent", got)
	}
}
