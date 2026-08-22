package server

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// go-chi/cors aborts a preflight whose requested method is absent from
// AllowedMethods, so the browser blocks the real request and the route becomes
// unreachable cross-origin.
func TestCORSPreflightApprovesEveryAllowedMethod(t *testing.T) {
	t.Parallel()
	const origin = "https://app.example.com"
	router := New(nil, nil, []string{origin}).Router()

	for _, method := range corsAllowedMethods {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodOptions, "/api/v1/badbundle/projects/TRACK/favorite", nil)
			req.Header.Set("Origin", origin)
			req.Header.Set("Access-Control-Request-Method", method)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
				t.Fatalf("%s preflight Access-Control-Allow-Origin = %q, want %q", method, got, origin)
			}
			allowed := rec.Header().Get("Access-Control-Allow-Methods")
			if !slices.Contains(strings.Split(allowed, ", "), method) {
				t.Fatalf("%s preflight Access-Control-Allow-Methods = %q", method, allowed)
			}
		})
	}
}

// A future PUT-only route must not silently become unreachable, so every method
// the API registers has to appear in the allow list.
func TestCORSAllowedMethodsCoverEveryAPIRoute(t *testing.T) {
	t.Parallel()
	routes, ok := New(nil, nil, nil).Router().(chi.Routes)
	if !ok {
		t.Fatal("router is not a chi.Routes")
	}

	seen := map[string][]string{}
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/api/v1/") {
			seen[method] = append(seen[method], route)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	if len(seen) == 0 {
		t.Fatal("walked no API routes")
	}
	for method, examples := range seen {
		if !slices.Contains(corsAllowedMethods, method) {
			t.Fatalf("API registers %s (for example %s) but corsAllowedMethods omits it", method, examples[0])
		}
	}
}

// CONNECT and TRACE are never registered deliberately; they only appear on
// catch-all handlers, and advertising them cross-origin would be wrong.
func TestCORSAllowedMethodsExcludeConnectAndTrace(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodConnect, http.MethodTrace} {
		if slices.Contains(corsAllowedMethods, method) {
			t.Fatalf("corsAllowedMethods includes %s", method)
		}
	}
}
