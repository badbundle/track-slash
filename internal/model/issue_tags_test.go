package model

import (
	"strings"
	"testing"
)

func TestIssueTagColorValidationAndDefault(t *testing.T) {
	t.Parallel()

	for _, color := range []IssueTagColor{
		TagColorSlate,
		TagColorRed,
		TagColorOrange,
		TagColorAmber,
		TagColorYellow,
		TagColorGreen,
		TagColorTeal,
		TagColorCyan,
		TagColorBlue,
		TagColorViolet,
		TagColorPink,
	} {
		if !color.Valid() {
			t.Fatalf("%q should be valid", color)
		}
	}
	if IssueTagColor("mauve").Valid() {
		t.Fatalf("mauve should be invalid")
	}
	if IssueTagColorOrDefault("") != TagColorBlue {
		t.Fatalf("empty color did not default to blue")
	}
	if IssueTagColorOrDefault(TagColorPink) != TagColorPink {
		t.Fatalf("non-empty color should be preserved")
	}
}

func TestNormalizeIssueTagName(t *testing.T) {
	t.Parallel()

	got, err := NormalizeIssueTagName("  #Customer   Beta  ")
	if err != nil {
		t.Fatalf("NormalizeIssueTagName: %v", err)
	}
	if got != "Customer Beta" {
		t.Fatalf("normalized = %q, want Customer Beta", got)
	}

	got, err = NormalizeIssueTagName("##Double")
	if err != nil {
		t.Fatalf("NormalizeIssueTagName double hash: %v", err)
	}
	if got != "#Double" {
		t.Fatalf("double hash normalized = %q, want #Double", got)
	}

	cases := []string{"", "#", "ok\nbad", strings.Repeat("x", MaxIssueTagNameLength+1)}
	for _, raw := range cases {
		if _, err := NormalizeIssueTagName(raw); err == nil {
			t.Fatalf("NormalizeIssueTagName(%q) err = nil, want error", raw)
		}
	}
}

func TestIssueTagDisplayNameAndRef(t *testing.T) {
	t.Parallel()

	if IssueTagDisplayName("Customer Beta") != "#Customer Beta" {
		t.Fatalf("display name missing hash")
	}
	if IssueTagDisplayName("#Already") != "#Already" {
		t.Fatalf("display name should not double hash")
	}
	if IssueTagRef(42) != "tag-42" {
		t.Fatalf("IssueTagRef = %q, want tag-42", IssueTagRef(42))
	}
}
