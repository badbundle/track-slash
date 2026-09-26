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

// A browser navigation that fails with one of the portal's plain-text errors
// gets the branded error page with the same status, and none of the raw body.
func TestUIErrorPagesBrandFailedPageLoads(t *testing.T) {
	t.Parallel()
	s := New(nil, nil, nil)

	for _, tt := range []struct {
		status int
		title  string
	}{
		{status: http.StatusNotFound, title: "Page not found"},
		{status: http.StatusForbidden, title: "No access"},
		{status: http.StatusConflict, title: "Page changed"},
		{status: http.StatusBadRequest, title: "Bad request"},
		{status: http.StatusTooManyRequests, title: "Page could not load"},
		{status: http.StatusInternalServerError, title: "Something went wrong"},
	} {
		handler := s.uiErrorPages(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "raw detail", tt.status)
		}))
		req := httptest.NewRequest(http.MethodGet, "/badbundle/projects/NOPE", nil)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != tt.status {
			t.Fatalf("%d: status = %d", tt.status, rec.Code)
		}
		assertUIErrorShell(t, rec, tt.title)
		if strings.Contains(rec.Body.String(), "raw detail") {
			t.Fatalf("%d: error page leaked the plain-text body: %s", tt.status, rec.Body.String())
		}
	}
}

// Everything that is not a browser page navigation, and every response that
// is not a plain-text error, passes through untouched.
func TestUIErrorPagesLeaveOtherResponsesAlone(t *testing.T) {
	t.Parallel()
	s := New(nil, nil, nil)
	const html = "text/html,application/xhtml+xml"
	plainError := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "raw detail", http.StatusNotFound) }

	for _, tt := range []struct {
		name     string
		method   string
		headers  map[string]string
		handler  http.HandlerFunc
		wantCode int
		wantBody string
	}{
		{name: "htmx request", method: http.MethodGet, headers: map[string]string{"Accept": html, "HX-Request": "true"}, handler: plainError, wantCode: http.StatusNotFound, wantBody: "raw detail\n"},
		{name: "fetch request", method: http.MethodGet, headers: map[string]string{"Accept": "*/*"}, handler: plainError, wantCode: http.StatusNotFound, wantBody: "raw detail\n"},
		{name: "form post", method: http.MethodPost, headers: map[string]string{"Accept": html}, handler: plainError, wantCode: http.StatusNotFound, wantBody: "raw detail\n"},
		{name: "page", method: http.MethodGet, headers: map[string]string{"Accept": html}, handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<p>ok</p>"))
		}, wantCode: http.StatusOK, wantBody: "<p>ok</p>"},
		{name: "html error", method: http.MethodGet, headers: map[string]string{"Accept": html}, handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("<p>custom</p>"))
		}, wantCode: http.StatusNotFound, wantBody: "<p>custom</p>"},
		{name: "repeated header", method: http.MethodGet, headers: map[string]string{"Accept": html}, handler: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("accepted"))
		}, wantCode: http.StatusAccepted, wantBody: "accepted"},
	} {
		req := httptest.NewRequest(tt.method, "/badbundle/projects/NOPE", nil)
		for key, value := range tt.headers {
			req.Header.Set(key, value)
		}
		rec := httptest.NewRecorder()
		s.uiErrorPages(tt.handler).ServeHTTP(rec, req)

		if rec.Code != tt.wantCode || rec.Body.String() != tt.wantBody {
			t.Fatalf("%s: got %d %q, want %d %q", tt.name, rec.Code, rec.Body.String(), tt.wantCode, tt.wantBody)
		}
	}
}

// Wrapping the writer must not hide Flush or Hijack from handlers that reach
// them through http.ResponseController.
func TestUIErrorPageWriterUnwrapsForResponseController(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()

	if err := http.NewResponseController(&uiErrorPageWriter{ResponseWriter: rec}).Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if !rec.Flushed {
		t.Fatal("Flush did not reach the underlying writer")
	}
}
