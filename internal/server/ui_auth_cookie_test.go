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
		expires        bool
		wantSecure     bool
	}{
		{name: "localhost http", publicOrigin: "http://localhost:8080"},
		{name: "direct tls", directTLS: true, expires: true, wantSecure: true},
		{name: "tls terminated proxy", publicOrigin: "https://track.example.com", wantSecure: true},
		{name: "untrusted forwarded proto", forwardedProto: "https"},
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
			if tt.expires {
				expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
				expiresAt = &expires
			}
			srv.setUISessionCookie(setRecorder, req, "raw-token", expiresAt)
			setCookies := setRecorder.Result().Cookies()
			assertCookieSecure(t, setCookies, tt.wantSecure)
			if expiresAt != nil && !setCookies[0].Expires.Equal(*expiresAt) {
				t.Fatalf("cookie Expires = %v, want %v", setCookies[0].Expires, *expiresAt)
			}

			clearRecorder := httptest.NewRecorder()
			srv.clearUISessionCookie(clearRecorder, req)
			assertCookieSecure(t, clearRecorder.Result().Cookies(), tt.wantSecure)
		})
	}
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
