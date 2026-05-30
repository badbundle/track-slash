package server

import "testing"

func TestUIProjectIcon(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want string
	}{
		{name: "Roadmap", key: "TRACK", want: "R"},
		{name: " roadmap", key: "TRACK", want: "R"},
		{name: "", key: "TRACK", want: "T"},
		{name: "", key: "", want: "?"},
	}

	for _, tt := range tests {
		if got := uiProjectIcon(tt.name, tt.key); got != tt.want {
			t.Fatalf("uiProjectIcon(%q, %q) = %q, want %q", tt.name, tt.key, got, tt.want)
		}
	}
}

func TestSafeUINextRootPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: "/"},
		{name: "root", raw: "/", want: "/"},
		{name: "work", raw: "/sprint", want: "/sprint"},
		{name: "work panel with query", raw: "/sprint/panel?x=1", want: "/sprint/panel?x=1"},
		{name: "projects", raw: "/projects", want: "/projects"},
		{name: "projects panel", raw: "/projects/panel", want: "/projects/panel"},
		{name: "project", raw: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16", want: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16"},
		{name: "api", raw: "/api/v1/projects", want: "/"},
		{name: "legacy app", raw: "/app/sprint", want: "/"},
		{name: "scheme relative", raw: "//evil.example/sprint", want: "/"},
		{name: "relative", raw: "sprint", want: "/"},
	}

	for _, tt := range tests {
		if got := safeUINext(tt.raw); got != tt.want {
			t.Fatalf("%s: safeUINext(%q) = %q, want %q", tt.name, tt.raw, got, tt.want)
		}
	}
}
