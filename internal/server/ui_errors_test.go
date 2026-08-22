package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Unmatched portal URLs render inside the app shell rather than falling through
// to Go's plain-text default.
func TestUINotFoundRendersInsideTheShell(t *testing.T) {
	t.Parallel()
	router := New(nil, nil, nil).Router()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/badbundle/projects/TRACK/sprnit", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	assertUIErrorShell(t, rec, "Page not found")
}

// The error page must render even when the session cannot be resolved, so a
// visitor carrying a cookie never trades a 404 for a 500.
func TestUINotFoundRendersWithoutAResolvableSession(t *testing.T) {
	t.Parallel()
	router := New(nil, nil, nil).Router()

	req := httptest.NewRequest(http.MethodGet, "/badbundle/projects/TRACK/sprnit", nil)
	req.AddCookie(&http.Cookie{Name: uiAuthCookieName, Value: "session-value", Path: "/"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	assertUIErrorShell(t, rec, "Page not found")
}

// A known path addressed with the wrong method previously answered 405 with a
// completely empty body.
func TestUIMethodNotAllowedRendersInsideTheShell(t *testing.T) {
	t.Parallel()
	router := New(nil, nil, nil).Router()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/logout", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	assertUIErrorShell(t, rec, "Method not allowed")
}

// chi propagates a root NotFound handler into sub-routers that have none, so
// the JSON API must set its own or its clients start receiving the HTML shell.
func TestAPIErrorsNeverReturnHTML(t *testing.T) {
	t.Parallel()
	router := New(nil, nil, nil).Router()

	for _, tt := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "unknown path", method: http.MethodGet, path: "/api/v1/bogus"},
		{name: "unknown nested path", method: http.MethodGet, path: "/api/v1/me/bogus"},
		{name: "wrong method on a known path", method: http.MethodDelete, path: "/api/v1/healthz"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			var body errorBody
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body %q: %v", rec.Body.String(), err)
			}
			if body.Error == "" {
				t.Fatalf("empty error in body %q", rec.Body.String())
			}
		})
	}
}

func TestAPINotFoundAndMethodNotAllowedBodies(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus int
		wantError  string
	}{
		{name: "not found", handler: apiNotFound, wantStatus: http.StatusNotFound, wantError: "not found"},
		{name: "method not allowed", handler: apiMethodNotAllowed, wantStatus: http.StatusMethodNotAllowed, wantError: "method not allowed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			tt.handler(rec, httptest.NewRequest(http.MethodGet, "/api/v1/bogus", nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var body errorBody
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body %q: %v", rec.Body.String(), err)
			}
			if body.Error != tt.wantError {
				t.Fatalf("error = %q, want %q", body.Error, tt.wantError)
			}
		})
	}
}

func assertUIErrorShell(t *testing.T, rec *httptest.ResponseRecorder, wantTitle string) {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
	body := rec.Body.String()
	for _, want := range []string{wantTitle, `id="main"`, `class="app-shell`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body does not contain %q:\n%s", want, body)
		}
	}
}
