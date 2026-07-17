package server_test

import (
	"bytes"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bradleymackey/track-slash/internal/server"
)

func TestAuthenticationRateLimits(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	t.Run("identifier across IPs", func(t *testing.T) {
		router := server.NewWithOptions(e.store, nil, server.Options{AuthRateLimit: server.AuthRateLimitOptions{
			IPAttempts:         10,
			IPWindow:           time.Minute,
			IdentifierAttempts: 2,
			IdentifierWindow:   5 * time.Minute,
		}}).Router()
		for attempt, remote := range []string{"192.0.2.1:1001", "192.0.2.2:1002", "192.0.2.3:1003"} {
			response := serveUILogin(router, remote, "", "same-user")
			want := http.StatusUnauthorized
			if attempt == 2 {
				want = http.StatusTooManyRequests
			}
			if response.Code != want {
				t.Fatalf("attempt %d code = %d, want %d", attempt+1, response.Code, want)
			}
			if want == http.StatusTooManyRequests && response.Header().Get("Retry-After") != "300" {
				t.Fatalf("Retry-After = %q, want 300", response.Header().Get("Retry-After"))
			}
		}
	})

	t.Run("IP across identifiers", func(t *testing.T) {
		router := server.NewWithOptions(e.store, nil, server.Options{AuthRateLimit: server.AuthRateLimitOptions{
			IPAttempts:         2,
			IPWindow:           time.Minute,
			IdentifierAttempts: 10,
			IdentifierWindow:   5 * time.Minute,
		}}).Router()
		for attempt, username := range []string{"first-user", "second-user", "third-user"} {
			response := serveUILogin(router, "192.0.2.20:2000", "", username)
			want := http.StatusUnauthorized
			if attempt == 2 {
				want = http.StatusTooManyRequests
			}
			if response.Code != want {
				t.Fatalf("attempt %d code = %d, want %d", attempt+1, response.Code, want)
			}
		}
	})

	t.Run("forwarded IP ignored", func(t *testing.T) {
		router := server.NewWithOptions(e.store, nil, server.Options{AuthRateLimit: server.AuthRateLimitOptions{
			IPAttempts:         2,
			IPWindow:           time.Minute,
			IdentifierAttempts: 10,
			IdentifierWindow:   5 * time.Minute,
		}}).Router()
		for attempt, forwarded := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"} {
			response := serveUILogin(router, "192.0.2.30:3000", forwarded, "xff-user-"+forwarded)
			want := http.StatusUnauthorized
			if attempt == 2 {
				want = http.StatusTooManyRequests
			}
			if response.Code != want {
				t.Fatalf("attempt %d code = %d, want %d", attempt+1, response.Code, want)
			}
		}
	})

	t.Run("trusted proxy separates clients", func(t *testing.T) {
		_, proxyNetwork, err := net.ParseCIDR("192.0.2.0/24")
		if err != nil {
			t.Fatalf("ParseCIDR: %v", err)
		}
		router := server.NewWithOptions(e.store, nil, server.Options{
			TrustedProxyCIDRs: []net.IPNet{*proxyNetwork},
			AuthRateLimit: server.AuthRateLimitOptions{
				IPAttempts:         2,
				IPWindow:           time.Minute,
				IdentifierAttempts: 10,
				IdentifierWindow:   5 * time.Minute,
			},
		}).Router()
		for attempt, forwarded := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"} {
			response := serveUILogin(router, "192.0.2.30:3000", forwarded, "proxy-user-"+forwarded)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("attempt %d code = %d, want %d", attempt+1, response.Code, http.StatusUnauthorized)
			}
		}
	})

	t.Run("authenticated reauth account", func(t *testing.T) {
		router := server.NewWithOptions(e.store, nil, server.Options{AuthRateLimit: server.AuthRateLimitOptions{
			IPAttempts:         10,
			IPWindow:           time.Minute,
			IdentifierAttempts: 2,
			IdentifierWindow:   5 * time.Minute,
		}}).Router()
		for attempt, remote := range []string{"192.0.2.41:4001", "192.0.2.42:4002", "192.0.2.43:4003"} {
			req := httptest.NewRequest(http.MethodPost, apiPath("/me/reauth/password"), strings.NewReader(`{"current_password":"wrong-password"}`))
			req.RemoteAddr = remote
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+e.authToken)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			want := http.StatusUnauthorized
			if attempt == 2 {
				want = http.StatusTooManyRequests
			}
			if response.Code != want {
				t.Fatalf("attempt %d code = %d body = %s, want %d", attempt+1, response.Code, response.Body.String(), want)
			}
		}
	})

	t.Run("passkey options IP", func(t *testing.T) {
		router := server.NewWithOptions(e.store, nil, server.Options{AuthRateLimit: server.AuthRateLimitOptions{
			IPAttempts:         1,
			IPWindow:           time.Minute,
			IdentifierAttempts: 10,
			IdentifierWindow:   5 * time.Minute,
		}}).Router()
		for attempt := 0; attempt < 2; attempt++ {
			req := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/session/passkey/options", nil)
			req.RemoteAddr = "192.0.2.50:5000"
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			want := http.StatusOK
			if attempt == 1 {
				want = http.StatusTooManyRequests
			}
			if response.Code != want {
				t.Fatalf("attempt %d code = %d body = %s, want %d", attempt+1, response.Code, response.Body.String(), want)
			}
		}
	})
}

func serveUILogin(handler http.Handler, remote, forwarded, username string) *httptest.ResponseRecorder {
	secret := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	form := url.Values{
		"username":   {username},
		"password":   {"wrong-password"},
		"csrf_token": {uiCSRFTokenForTest("pre-auth", secret)},
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.RemoteAddr = remote
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: uiPreAuthCookieNameForTest, Value: secret, Path: "/"})
	if forwarded != "" {
		req.Header.Set("X-Forwarded-For", forwarded)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}
