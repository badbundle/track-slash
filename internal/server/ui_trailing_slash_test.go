package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRedirectUITrailingSlash(t *testing.T) {
	t.Parallel()
	router := New(nil, nil, nil).Router()

	for _, tt := range []struct {
		name         string
		method       string
		target       string
		wantStatus   int
		wantLocation string
	}{
		{
			name:         "portal page redirects to the canonical path",
			method:       http.MethodGet,
			target:       "/badbundle/projects/",
			wantStatus:   http.StatusMovedPermanently,
			wantLocation: "/badbundle/projects",
		},
		{
			name:         "query string survives the redirect",
			method:       http.MethodGet,
			target:       "/badbundle/projects/?sort=priority&assignee_id=7",
			wantStatus:   http.StatusMovedPermanently,
			wantLocation: "/badbundle/projects?sort=priority&assignee_id=7",
		},
		{
			name:         "repeated slashes collapse in one hop",
			method:       http.MethodGet,
			target:       "/login///",
			wantStatus:   http.StatusMovedPermanently,
			wantLocation: "/login",
		},
		{
			name:         "a path of only slashes canonicalises to root",
			method:       http.MethodGet,
			target:       "//",
			wantStatus:   http.StatusMovedPermanently,
			wantLocation: "/",
		},
		{
			name:         "escaped path segments are preserved",
			method:       http.MethodGet,
			target:       "/bad%20bundle/projects/",
			wantStatus:   http.StatusMovedPermanently,
			wantLocation: "/bad%20bundle/projects",
		},
		{
			name:         "HEAD redirects like GET",
			method:       http.MethodHead,
			target:       "/login/",
			wantStatus:   http.StatusMovedPermanently,
			wantLocation: "/login",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.target, nil))
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Location"); got != tt.wantLocation {
				t.Fatalf("Location = %q, want %q", got, tt.wantLocation)
			}
		})
	}
}

// Paths that are already canonical, and the API, MCP, and static subtrees that
// must reach their handlers exactly as addressed, are all served rather than
// rewritten.
func TestRedirectUITrailingSlashLeavesExemptPathsAlone(t *testing.T) {
	t.Parallel()
	router := New(nil, nil, nil).Router()

	for _, target := range []string{
		"/",
		"/login",
		"/api/",
		"/api/v1/",
		"/api/v1/healthz/",
		"/mcp/",
		"/static/",
	} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
			if rec.Code == http.StatusMovedPermanently {
				t.Fatalf("%s was redirected to %q, want the request served as addressed", target, rec.Header().Get("Location"))
			}
		})
	}
}

// A 301 on an unsafe method invites clients to replay it as a GET, dropping the
// body, so only GET and HEAD may be canonicalised.
func TestRedirectUITrailingSlashIgnoresUnsafeMethods(t *testing.T) {
	t.Parallel()
	router := New(nil, nil, nil).Router()

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(method, "/login/", strings.NewReader("")))
			if rec.Code == http.StatusMovedPermanently {
				t.Fatalf("%s /login/ was redirected, want the request served as addressed", method)
			}
		})
	}
}
