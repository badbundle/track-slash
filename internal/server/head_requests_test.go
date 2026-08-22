package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// chi's Get() registers GET alone, so before the rewrite every route but the
// root and the static tree answered a HEAD probe with 405 and an empty body.
func TestHEADReachesTheGETHandler(t *testing.T) {
	t.Parallel()
	router := New(nil, nil, nil).Router()

	for _, tt := range []struct {
		path        string
		contentType string
	}{
		{path: "/login", contentType: "text/html"},
		{path: "/signup", contentType: "text/html"},
		{path: "/terms", contentType: "text/html"},
		{path: "/privacy", contentType: "text/html"},
		{path: "/security", contentType: "text/html"},
		{path: "/service-worker.js", contentType: "text/javascript"},
		{path: "/static/app.js", contentType: "text/javascript"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, tt.path, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("HEAD %s status = %d, want 200", tt.path, rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, tt.contentType) {
				t.Fatalf("HEAD %s Content-Type = %q, want prefix %q", tt.path, got, tt.contentType)
			}
		})
	}
}

// A HEAD probe answers with the same status a GET would, including redirects
// and errors, so monitors see the truth about a route.
func TestHEADMirrorsGETStatuses(t *testing.T) {
	t.Parallel()
	router := New(nil, nil, nil).Router()

	for _, tt := range []struct {
		name       string
		path       string
		wantStatus int
	}{
		{name: "authenticated page redirects to login", path: "/me", wantStatus: http.StatusSeeOther},
		{name: "unknown page is not found", path: "/nope", wantStatus: http.StatusNotFound},
		{name: "post-only route is still refused", path: "/logout", wantStatus: http.StatusMethodNotAllowed},
		{name: "static directory is not found", path: "/static/", wantStatus: http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("HEAD %s status = %d, want %d", tt.path, rec.Code, tt.wantStatus)
			}
		})
	}
}

// Uptime monitors probe the root with HEAD and must keep getting an
// unconditional 200 rather than the signed-out redirect a GET returns.
func TestHEADRootStaysAnUnconditional200(t *testing.T) {
	t.Parallel()
	router := New(nil, nil, nil).Router()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD / status = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET / status = %d, want 303", rec.Code)
	}
}

// Rewriting a HEAD probe on a long-lived stream would open a connection nobody
// reads, so those paths keep whatever their own handler does with HEAD.
func TestHEADExemptPathsAreNotRewritten(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/", "/mcp", "/api/v1/ws", devReloadPath} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			var seen string
			handler := serveHEADAsGET(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = r.Method
			}))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodHead, path, nil))
			if seen != http.MethodHead {
				t.Fatalf("%s reached the handler as %q, want HEAD", path, seen)
			}
		})
	}
}

func TestServeHEADAsGETLeavesOtherMethodsAlone(t *testing.T) {
	t.Parallel()

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			var seen string
			handler := serveHEADAsGET(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = r.Method
			}))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, "/login", nil))
			if seen != method {
				t.Fatalf("handler saw %q, want %q", seen, method)
			}
		})
	}
}
