package store

import (
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
)

func TestChangelogUserFacingLabels(t *testing.T) {
	duplicate := model.CloseReasonDuplicate
	if got := changelogCloseReasonLabel(&duplicate); got != "Duplicate" {
		t.Fatalf("duplicate close reason label = %q", got)
	}
	if got := changelogCloseReasonLabel(nil); got != "" {
		t.Fatalf("nil close reason label = %q", got)
	}
	if got := changelogSprintStatusLabel(model.SprintStatusActive); got != "Active" {
		t.Fatalf("active sprint status label = %q", got)
	}
	if got := changelogLinkTypeLabel(model.LinkTypeRelatesTo); got != "Relates to" {
		t.Fatalf("relates_to link type label = %q", got)
	}
}

func TestChangelogPreviewCompactsAndTruncates(t *testing.T) {
	raw := strings.Repeat("word ", 60)
	got := changelogPreview(raw)
	if strings.Contains(got, "  ") {
		t.Fatalf("preview did not compact whitespace: %q", got)
	}
	if len([]rune(got)) > 160 {
		t.Fatalf("preview length = %d, want <= 160", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("preview = %q, want ellipsis", got)
	}
}
