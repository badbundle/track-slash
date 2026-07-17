package server

import "testing"

func TestWebSocketOriginPoliciesForDeployedServer(t *testing.T) {
	t.Parallel()

	srv := NewWithOptions(nil, nil, Options{PublicOrigin: "https://track.example.com"})
	for _, tt := range []struct {
		name   string
		origin string
		api    bool
		ui     bool
	}{
		{name: "same origin", origin: "https://track.example.com", api: true, ui: true},
		{name: "foreign origin", origin: "https://evil.example.com"},
		{name: "same-site sibling", origin: "https://docs.example.com"},
		{name: "missing non-browser origin", origin: "", api: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := srv.apiWebSocketOrigins.Allows(tt.origin); got != tt.api {
				t.Fatalf("API policy Allows(%q) = %v, want %v", tt.origin, got, tt.api)
			}
			if got := srv.uiWebSocketOrigins.Allows(tt.origin); got != tt.ui {
				t.Fatalf("UI policy Allows(%q) = %v, want %v", tt.origin, got, tt.ui)
			}
		})
	}
}

func TestWebSocketAPIOriginPolicyIncludesExplicitCORSOriginsOnly(t *testing.T) {
	t.Parallel()

	srv := NewWithOptions(nil, nil, Options{
		PublicOrigin:       "https://track.example.com",
		CORSAllowedOrigins: []string{"https://client.example.net", "https://track.example.com"},
	})
	if !srv.apiWebSocketOrigins.Allows("https://client.example.net") {
		t.Fatal("API WebSocket policy rejected explicit CORS origin")
	}
	if srv.uiWebSocketOrigins.Allows("https://client.example.net") {
		t.Fatal("UI WebSocket policy accepted cross-origin API client")
	}
	if len(srv.apiWebSocketOrigins.AllowedOrigins) != 2 {
		t.Fatalf("API allowed origins = %v, want de-duplicated public and CORS origins", srv.apiWebSocketOrigins.AllowedOrigins)
	}
}

func TestWebSocketOriginPoliciesForLocalDevelopment(t *testing.T) {
	t.Parallel()

	srv := NewWithOptions(nil, nil, Options{})
	for _, origin := range []string{"http://localhost:8080", "http://127.0.0.1:5173", "https://[::1]:8443"} {
		if !srv.apiWebSocketOrigins.Allows(origin) || !srv.uiWebSocketOrigins.Allows(origin) {
			t.Fatalf("localhost development origin %q was rejected", origin)
		}
	}
	if !srv.apiWebSocketOrigins.Allows("") {
		t.Fatal("API WebSocket policy rejected missing Origin from a non-browser client")
	}
	if srv.uiWebSocketOrigins.Allows("") {
		t.Fatal("UI WebSocket policy accepted missing Origin")
	}
	for _, origin := range []string{"https://track.example.com", "http://localhost.example.com:8080"} {
		if srv.apiWebSocketOrigins.Allows(origin) || srv.uiWebSocketOrigins.Allows(origin) {
			t.Fatalf("non-local development origin %q was accepted", origin)
		}
	}
}
