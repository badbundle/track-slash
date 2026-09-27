package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/store"
)

func TestIssueDescriptionLength(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	longest := strings.Repeat("é", store.MaxIssueDescriptionRunes)
	tooLong := longest + "x"

	issue, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{ProjectID: env.projectID, Title: "At the limit", Description: longest})
	if err != nil {
		t.Fatalf("CreateIssue at the limit: %v", err)
	}
	if _, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{ProjectID: env.projectID, Title: "Over", Description: tooLong}); !errors.Is(err, store.ErrIssueDescriptionTooLong) {
		t.Fatalf("CreateIssue over the limit err = %v", err)
	}
	if _, err := env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{ParentIssueID: issue.ID, Title: "Over", Description: tooLong}); !errors.Is(err, store.ErrIssueDescriptionTooLong) {
		t.Fatalf("CreateSubIssue over the limit err = %v", err)
	}
	if _, err := env.store.UpdateIssue(env.ctx, issue.ID, store.UpdateIssueParams{Description: &tooLong}); !errors.Is(err, store.ErrIssueDescriptionTooLong) {
		t.Fatalf("UpdateIssue over the limit err = %v", err)
	}
	if !errors.Is(store.ErrIssueDescriptionTooLong, store.ErrConflict) {
		t.Fatal("the limit error should map like a conflict")
	}
}
