package model

import (
	"encoding/json"
	"testing"
)

func TestStatusValid(t *testing.T) {
	cases := []struct {
		in   Status
		want bool
	}{
		{StatusTodo, true},
		{StatusInProgress, true},
		{StatusDone, true},
		{StatusClosed, true},
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

func TestStatusCountsAsDone(t *testing.T) {
	cases := []struct {
		in   Status
		want bool
	}{
		{StatusTodo, false},
		{StatusInProgress, false},
		{StatusDone, true},
		{StatusClosed, true},
		{"custom", false},
	}
	for _, c := range cases {
		t.Run(string(c.in), func(t *testing.T) {
			if got := c.in.CountsAsDone(); got != c.want {
				t.Fatalf("Status(%q).CountsAsDone() = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestIssueCloseReasonValid(t *testing.T) {
	cases := []struct {
		in   IssueCloseReason
		want bool
	}{
		{CloseReasonDuplicate, true},
		{CloseReasonWontDo, true},
		{CloseReasonInvalid, true},
		{"", false},
		{"wontdo", false},
		{"won't_do", false},
		{"DUPLICATE", false},
	}
	for _, c := range cases {
		t.Run(string(c.in), func(t *testing.T) {
			if got := c.in.Valid(); got != c.want {
				t.Fatalf("IssueCloseReason(%q).Valid() = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestDateJSONRoundTrip(t *testing.T) {
	d, err := ParseDate("2026-06-24")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(b) != `"2026-06-24"` {
		t.Fatalf("json = %s", b)
	}
	var got Date
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.String() != "2026-06-24" {
		t.Fatalf("got = %s", got.String())
	}
	if err := json.Unmarshal([]byte(`"2026/06/24"`), &got); err == nil {
		t.Fatal("Unmarshal invalid date succeeded")
	}
	if err := json.Unmarshal([]byte(`123`), &got); err == nil {
		t.Fatal("Unmarshal non-string date succeeded")
	}
}

func TestIssuePriorityValid(t *testing.T) {
	cases := []struct {
		in   IssuePriority
		want bool
	}{
		{PriorityP0, true},
		{PriorityP1, true},
		{PriorityP2, true},
		{PriorityP3, true},
		{PriorityP4, true},
		{"", false},
		{"p0", false},
		{"P5", false},
		{"urgent", false},
	}
	for _, c := range cases {
		t.Run(string(c.in), func(t *testing.T) {
			if got := c.in.Valid(); got != c.want {
				t.Fatalf("IssuePriority(%q).Valid() = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestProjectContextKindValid(t *testing.T) {
	cases := []struct {
		in   ProjectContextKind
		want bool
	}{
		{ProjectContextKindText, true},
		{"image", false},
		{"", false},
	}
	for _, c := range cases {
		if got := c.in.Valid(); got != c.want {
			t.Fatalf("ProjectContextKind(%q).Valid() = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestProjectContextScopeValid(t *testing.T) {
	cases := []struct {
		in   ProjectContextScope
		want bool
	}{
		{ProjectContextScopeProject, true},
		{ProjectContextScopeIssue, true},
		{"workspace", false},
		{"", false},
	}
	for _, c := range cases {
		if got := c.in.Valid(); got != c.want {
			t.Fatalf("ProjectContextScope(%q).Valid() = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestProjectContextRef(t *testing.T) {
	if got := ProjectContextRef(12); got != "context-12" {
		t.Fatalf("ProjectContextRef(12) = %q, want context-12", got)
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
