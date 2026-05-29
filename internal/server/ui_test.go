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
