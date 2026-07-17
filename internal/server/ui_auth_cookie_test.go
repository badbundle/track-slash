package server

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUISessionCookieSecurityPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		publicOrigin   string
		directTLS      bool
		forwardedProto string
		expiresOffset  time.Duration
		wantSecure     bool
	}{
		{name: "localhost http", publicOrigin: "http://localhost:8080"},
		{name: "direct tls", directTLS: true, expiresOffset: time.Hour, wantSecure: true},
		{name: "tls terminated proxy", publicOrigin: "https://track.example.com", wantSecure: true},
		{name: "untrusted forwarded proto", forwardedProto: "https"},
		{name: "expired session", expiresOffset: -time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := NewWithOptions(nil, nil, Options{PublicOrigin: tt.publicOrigin})
			req := httptest.NewRequest(http.MethodGet, "http://track.example.com/", nil)
			if tt.directTLS {
				req.TLS = &tls.ConnectionState{}
			}
			if tt.forwardedProto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.forwardedProto)
			}

			setRecorder := httptest.NewRecorder()
			var expiresAt *time.Time
			if tt.expiresOffset != 0 {
				expires := time.Now().Add(tt.expiresOffset).UTC().Truncate(time.Second)
				expiresAt = &expires
			}
			srv.setUISessionCookie(setRecorder, req, "raw-token", expiresAt)
			setCookies := setRecorder.Result().Cookies()
			if len(setCookies) != 2 {
				t.Fatalf("set cookies = %v, want session plus cleared pre-auth CSRF cookie", setCookies)
			}
			for _, cookie := range setCookies {
				if cookie.Secure != tt.wantSecure {
					t.Fatalf("cookie %s Secure = %v, want %v", cookie.Name, cookie.Secure, tt.wantSecure)
				}
			}
			sessionCookie := cookieNamed(t, setCookies, uiAuthCookieName)
			preAuthCookie := cookieNamed(t, setCookies, uiPreAuthCSRFCookieName)
			if preAuthCookie.MaxAge != -1 {
				t.Fatalf("pre-auth CSRF cookie MaxAge = %d, want -1", preAuthCookie.MaxAge)
			}
			if expiresAt != nil && !sessionCookie.Expires.Equal(*expiresAt) {
				t.Fatalf("cookie Expires = %v, want %v", sessionCookie.Expires, *expiresAt)
			}
			switch {
			case tt.expiresOffset > 0 && (sessionCookie.MaxAge < 3599 || sessionCookie.MaxAge > 3600):
				t.Fatalf("future cookie MaxAge = %d, want 3599-3600", sessionCookie.MaxAge)
			case tt.expiresOffset < 0 && sessionCookie.MaxAge != -1:
				t.Fatalf("expired cookie MaxAge = %d, want -1", sessionCookie.MaxAge)
			case tt.expiresOffset == 0 && sessionCookie.MaxAge != 0:
				t.Fatalf("session cookie MaxAge = %d, want 0", sessionCookie.MaxAge)
			}

			clearRecorder := httptest.NewRecorder()
			srv.clearUISessionCookie(clearRecorder, req)
			assertCookieSecure(t, clearRecorder.Result().Cookies(), tt.wantSecure)
		})
	}
}

func cookieNamed(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %q not found in %v", name, cookies)
	return nil
}

func assertCookieSecure(t *testing.T, cookies []*http.Cookie, want bool) {
	t.Helper()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %v, want one", cookies)
	}
	if cookies[0].Secure != want {
		t.Fatalf("cookie Secure = %v, want %v", cookies[0].Secure, want)
	}
}
