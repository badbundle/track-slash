package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUIStaticAssetsAreServedWithoutAuthentication(t *testing.T) {
	t.Parallel()

	router := New(nil, nil, nil).Router()
	tests := []struct {
		path        string
		contentType string
	}{
		{path: "/static/app.css", contentType: "text/css"},
		{path: "/static/app.js", contentType: "text/javascript"},
		{path: "/static/auth.js", contentType: "text/javascript"},
		{path: "/static/htmx.min.js", contentType: "text/javascript"},
		{path: "/static/lucide.min.js", contentType: "text/javascript"},
		{path: "/static/preload.js", contentType: "text/javascript"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want 200", tt.path, rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, tt.contentType) {
				t.Fatalf("GET %s Content-Type = %q, want prefix %q", tt.path, got, tt.contentType)
			}
			if rec.Body.Len() == 0 {
				t.Fatalf("GET %s returned an empty body", tt.path)
			}
		})
	}
}

// The asset tree ships no index.html, so Go's file server would otherwise answer
// a directory request with a listing that enumerates every shipped asset.
func TestUIStaticDirectoriesAreNotListed(t *testing.T) {
	t.Parallel()

	router := New(nil, nil, nil).Router()
	for _, path := range []string{"/static/", "/static", "/static/nested/"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET %s status = %d, want 404", path, rec.Code)
			}
			// Go's dirList writes one anchor per entry.
			if strings.Contains(rec.Body.String(), `>app.css</a>`) {
				t.Fatalf("GET %s enumerated the asset tree: %s", path, rec.Body.String())
			}
		})
	}
}

// r.Handle registered every method, so a write verb returned the asset body
// instead of being refused.
func TestUIStaticRejectsWriteMethods(t *testing.T) {
	t.Parallel()

	router := New(nil, nil, nil).Router()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/static/app.js", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s /static/app.js status = %d, want 405", method, rec.Code)
			}
		})
	}
}

// Uptime checks and crawlers ask for assets with HEAD; it must keep working
// alongside the tightened method set.
func TestUIStaticServesHead(t *testing.T) {
	t.Parallel()

	router := New(nil, nil, nil).Router()
	req := httptest.NewRequest(http.MethodHead, "/static/app.js", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD /static/app.js status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/javascript") {
		t.Fatalf("HEAD /static/app.js Content-Type = %q, want text/javascript", got)
	}
}
