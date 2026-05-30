package model

import "testing"

func TestStatusValid(t *testing.T) {
	cases := []struct {
		in   Status
		want bool
	}{
		{StatusTodo, true},
		{StatusInProgress, true},
		{StatusDone, true},
		{"", false},
		{"open", false},
		{"DONE", false},
		{"in progress", false},
	}
	for _, c := range cases {
		t.Run(string(c.in), func(t *testing.T) {
			if got := c.in.Valid(); got != c.want {
				t.Fatalf("Status(%q).Valid() = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestSprintStatusValid(t *testing.T) {
	cases := []struct {
		in   SprintStatus
		want bool
	}{
		{SprintStatusPlanned, true},
		{SprintStatusActive, true},
		{SprintStatusCompleted, true},
		{"", false},
		{"open", false},
		{"ACTIVE", false},
		{"in progress", false},
	}
	for _, c := range cases {
		t.Run(string(c.in), func(t *testing.T) {
			if got := c.in.Valid(); got != c.want {
				t.Fatalf("SprintStatus(%q).Valid() = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestAuthTokenKindValid(t *testing.T) {
	cases := []struct {
		in   AuthTokenKind
		want bool
	}{
		{AuthTokenKindAPI, true},
		{AuthTokenKindSession, true},
		{"", false},
		{"jwt", false},
		{"API", false},
	}
	for _, c := range cases {
		t.Run(string(c.in), func(t *testing.T) {
			if got := c.in.Valid(); got != c.want {
				t.Fatalf("AuthTokenKind(%q).Valid() = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestAuthCredentialKindValid(t *testing.T) {
	cases := []struct {
		in   AuthCredentialKind
		want bool
	}{
		{AuthCredentialKindPassword, true},
		{AuthCredentialKindPasskey, true},
		{"", false},
		{"totp", false},
		{"PASSWORD", false},
	}
	for _, c := range cases {
		t.Run(string(c.in), func(t *testing.T) {
			if got := c.in.Valid(); got != c.want {
				t.Fatalf("AuthCredentialKind(%q).Valid() = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
