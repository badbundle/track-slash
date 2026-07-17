package realtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOriginAllowed(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		policy OriginPolicy
		want   bool
	}{
		{"empty policy rejects browser origin", "https://x.com", OriginPolicy{}, false},
		{"empty policy rejects missing origin", "", OriginPolicy{}, false},
		{"non-browser policy allows missing origin", "", OriginPolicy{AllowMissingOrigin: true}, true},
		{"exact match", "https://app.example.com", OriginPolicy{AllowedOrigins: []string{"https://app.example.com"}}, true},
		{"second entry matches", "http://localhost:3000", OriginPolicy{AllowedOrigins: []string{"https://app.example.com", "http://localhost:3000"}}, true},
		{"mismatch rejected", "https://evil.com", OriginPolicy{AllowedOrigins: []string{"https://app.example.com"}}, false},
		{"port mismatch rejected", "http://localhost:3001", OriginPolicy{AllowedOrigins: []string{"http://localhost:3000"}}, false},
		{"scheme mismatch rejected", "http://app.example.com", OriginPolicy{AllowedOrigins: []string{"https://app.example.com"}}, false},
		{"localhost development", "http://localhost:5173", OriginPolicy{AllowLocalhostOrigins: true}, true},
		{"loopback development", "http://127.0.0.1:5173", OriginPolicy{AllowLocalhostOrigins: true}, true},
		{"localhost trailing slash", "http://localhost:5173/", OriginPolicy{AllowLocalhostOrigins: true}, true},
		{"malformed development origin", "http://[::1", OriginPolicy{AllowLocalhostOrigins: true}, false},
		{"development origin requires HTTP", "ftp://localhost:5173", OriginPolicy{AllowLocalhostOrigins: true}, false},
		{"development origin requires host", "http:", OriginPolicy{AllowLocalhostOrigins: true}, false},
		{"development origin rejects userinfo", "http://user@localhost:5173", OriginPolicy{AllowLocalhostOrigins: true}, false},
		{"development origin rejects query", "http://localhost:5173?x=1", OriginPolicy{AllowLocalhostOrigins: true}, false},
		{"development origin rejects fragment", "http://localhost:5173#x", OriginPolicy{AllowLocalhostOrigins: true}, false},
		{"localhost path rejected", "http://localhost:5173/app", OriginPolicy{AllowLocalhostOrigins: true}, false},
		{"non-loopback IP rejected", "http://192.0.2.1:5173", OriginPolicy{AllowLocalhostOrigins: true}, false},
		{"non-local development origin rejected", "https://track.example.com", OriginPolicy{AllowLocalhostOrigins: true}, false},
	}
	for _, c := range cases {
		if got := c.policy.Allows(c.origin); got != c.want {
			t.Errorf("%s: Allows(%q) = %v, want %v", c.name, c.origin, got, c.want)
		}
	}
}

// TestHandlerRejectsDisallowedOrigin pins the 403 path so a regression in the
// origin gate is loud. A real WS upgrade isn't needed: the gate runs before
// Accept.
func TestHandlerRejectsDisallowedOrigin(t *testing.T) {
	hub := NewHub()
	h := hub.Handler(OriginPolicy{AllowedOrigins: []string{"https://app.example.com"}}, nil)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", rec.Code)
	}
}

func TestHandlerAcceptsAllowedOriginPreUpgrade(t *testing.T) {
	// We can't complete a real upgrade against httptest.NewRecorder (no
	// hijacker). We only assert the gate doesn't 403 the request — the
	// downstream Accept will fail to upgrade, which is fine: nothing
	// writes a body and the handler returns.
	hub := NewHub()
	h := hub.Handler(OriginPolicy{AllowedOrigins: []string{"https://app.example.com"}}, nil)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("allowed origin was forbidden")
	}
}
