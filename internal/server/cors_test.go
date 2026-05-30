package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bradleymackey/track-slash/internal/server"
)

// nilStore satisfies the *store.Store positional arg of server.New for tests
// that never hit a DB-backed handler. /api/v1/healthz pings the DB, so we avoid that
// path; OPTIONS preflights are handled by the cors middleware before any
// route handler runs, and X-Request-ID is emitted by middleware regardless.

func newCORSServer(t *testing.T, origins []string) *httptest.Server {
	t.Helper()
	// Passing nil store + nil hub: none of these CORS tests call a route
	// that dereferences the store. Server constructor doesn't deref either.
	srv := server.New(nil, nil, origins)
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	return ts
}

func TestCORSDisabledWhenAllowListEmpty(t *testing.T) {
	ts := newCORSServer(t, nil)

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+apiPath("/users"), nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()

	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ACAO header present (%q); want empty (cors disabled)", got)
	}
}

func TestCORSPreflightFromAllowedOrigin(t *testing.T) {
	ts := newCORSServer(t, []string{"https://app.example.com"})

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+apiPath("/users"), nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK {
		t.Fatalf("preflight status = %d, want 204 or 200", res.StatusCode)
	}
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("ACAO = %q, want %q", got, "https://app.example.com")
	}
	if got := res.Header.Get("Access-Control-Allow-Methods"); got == "" {
		t.Errorf("ACAM missing")
	}
}

func TestCORSPreflightFromDisallowedOrigin(t *testing.T) {
	ts := newCORSServer(t, []string{"https://app.example.com"})

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+apiPath("/users"), nil)
	req.Header.Set("Origin", "https://evil.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()

	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ACAO = %q, want empty (disallowed origin)", got)
	}
}

func TestRequestIDExposedOnResponse(t *testing.T) {
	ts := newCORSServer(t, nil)

	// Hit an endpoint that doesn't touch the DB. Routes that need the store
	// would panic; X-Request-ID is set by middleware regardless of handler
	// outcome, so a 404 path works.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/does-not-exist", nil)
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()

	if got := res.Header.Get("X-Request-ID"); got == "" {
		t.Errorf("X-Request-ID missing")
	}
}

func TestRouteNamespaceBoundaries(t *testing.T) {
	ts := newCORSServer(t, nil)

	for _, path := range []string{"/app", "/app/login", "/users", "/ws", "/healthz"} {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", path, res.StatusCode)
		}
	}

	client := *ts.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/projects", nil)
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /projects: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("GET /projects status = %d, want 303", res.StatusCode)
	}

	for _, path := range []string{"/users", "/projects"} {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+apiPath(path), nil)
		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", apiPath(path), err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("GET %s status = %d, want 401", apiPath(path), res.StatusCode)
		}
	}
}
