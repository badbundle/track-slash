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
