package server

import (
	"net/url"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestParseIssueListQueryValuesAPIShape(t *testing.T) {
	t.Parallel()

	values := url.Values{
		"status":    {"todo", "done", "todo"},
		"priority":  {"P0", "P1", "P0"},
		"sort":      {"number"},
		"direction": {"desc"},
	}
	got, err := parseIssueListQueryValues(values, issueListQueryOptions{
		DefaultSort:     store.ListIssuesSortNumber,
		AllowNumberSort: true,
	})
	if err != nil {
		t.Fatalf("parseIssueListQueryValues: %v", err)
	}
	if len(got.Statuses) != 2 || got.Statuses[0] != model.StatusTodo || got.Statuses[1] != model.StatusDone {
		t.Fatalf("statuses = %+v, want todo/done", got.Statuses)
	}
	if len(got.Priorities) != 2 || got.Priorities[0] != model.PriorityP0 || got.Priorities[1] != model.PriorityP1 {
		t.Fatalf("priorities = %+v, want P0/P1", got.Priorities)
	}
	if got.Sort != store.ListIssuesSortNumber {
		t.Fatalf("sort = %q, want number", got.Sort)
	}
	if got.Direction != store.ListIssuesSortDescending {
		t.Fatalf("direction = %q, want desc", got.Direction)
	}
}

func TestParseIssueListQueryValuesRejectsNumberSortWhenDisabled(t *testing.T) {
	t.Parallel()

	if _, err := parseIssueListQueryValues(url.Values{"sort": {"number"}}, issueListQueryOptions{
		DefaultSort:     uiIssueListDefaultSort,
		AllowNumberSort: false,
	}); err == nil {
		t.Fatal("parseIssueListQueryValues sort=number err = nil, want error")
	}
}
