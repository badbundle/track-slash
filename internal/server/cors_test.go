package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bradleymackey/track-slash/internal/server"
)

// nilStore satisfies the *store.Store positional arg of server.New for tests
// that never hit a DB-backed handler. /healthz pings the DB, so we avoid that
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

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/users", nil)
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

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/users", nil)
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

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/users", nil)
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
