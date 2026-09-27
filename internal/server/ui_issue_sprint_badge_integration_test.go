package server_test

import (
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

// issueSummaryRow returns the issue-summary-row markup for the issue titled
// title, so assertions about one row cannot match another.
func issueSummaryRow(t *testing.T, body, title string) string {
	t.Helper()
	for _, row := range strings.Split(body, "data-issue-summary-row")[1:] {
		if end := strings.Index(row, "</a>"); end >= 0 {
			row = row[:end]
		}
		if strings.Contains(row, ">"+title+"<") {
			return row
		}
	}
	t.Fatalf("no issue row titled %q: %s", title, body)
	return ""
}

func (e *httpEnv) mustAssignIssueSprint(t *testing.T, issue model.Issue, sprint model.Sprint) {
	t.Helper()
	if _, err := e.store.UpdateIssue(e.ctx, issue.ID, store.UpdateIssueParams{SprintID: &sprint.ID}); err != nil {
		t.Fatalf("assign %s to %s: %v", issue.Identifier, sprint.Ref, err)
	}
}

func TestUIProjectAllMarksSprintedIssues(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	active := e.mustPlannedSprint(t, "UI polish")
	planned := e.mustPlannedSprint(t, "Later work")
	inActive := e.mustCreateIssue(t, "Sprinted now")
	inPlanned := e.mustCreateIssue(t, "Sprinted later")
	e.mustCreateIssue(t, "Unsprinted")
	e.mustAssignIssueSprint(t, inActive, active)
	e.mustAssignIssueSprint(t, inPlanned, planned)
	activeStatus := model.SprintStatusActive
	if _, err := e.store.UpdateSprint(e.ctx, active.ID, store.UpdateSprintParams{Status: &activeStatus}); err != nil {
		t.Fatalf("activate sprint: %v", err)
	}

	// The All page and the infinite-scroll page share one builder; check both
	// entry points render the badge.
	for _, path := range []string{e.projectPath() + "/all", e.projectPath() + "/all/page"} {
		body := e.uiGet(t, path, e.authToken)
		for _, tc := range []struct {
			title  string
			sprint model.Sprint
			status string
		}{
			{title: "Sprinted now", sprint: active, status: "active"},
			{title: "Sprinted later", sprint: planned, status: "planned"},
		} {
			row := issueSummaryRow(t, body, tc.title)
			label := "In " + tc.sprint.Ref + " · " + tc.sprint.Name + " (" + tc.status + ")"
			for _, want := range []string{
				`data-issue-sprint-badge="` + tc.sprint.Ref + `"`,
				`data-tooltip="` + label + `"`,
				`<span aria-hidden="true">S</span>`,
				`<span class="sr-only">` + label + `</span>`,
			} {
				if !strings.Contains(row, want) {
					t.Fatalf("%s: %q row missing %q: %s", path, tc.title, want, row)
				}
			}
		}
		if row := issueSummaryRow(t, body, "Unsprinted"); strings.Contains(row, "data-issue-sprint-badge") {
			t.Fatalf("%s: unsprinted issue shows a sprint badge: %s", path, row)
		}
	}

	// Views already grouped by sprint don't repeat it on every row.
	for _, tc := range []struct{ view, title string }{
		{view: "sprint", title: "Sprinted now"},
		{view: "planned", title: "Later work"},
	} {
		body := e.uiGet(t, e.projectPath()+"/"+tc.view, e.authToken)
		if !strings.Contains(body, tc.title) {
			t.Fatalf("%s view missing %q: %s", tc.view, tc.title, body)
		}
		if strings.Contains(body, "data-issue-sprint-badge") {
			t.Fatalf("%s view shows sprint badges: %s", tc.view, body)
		}
	}
}

func TestUIProjectAllHidesSprintBadgesWhenSprintsAreOff(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	// Disabling sprints keeps planned sprints and their issues.
	planned := e.mustPlannedSprint(t, "Kept for later")
	issue := e.mustCreateIssue(t, "Planned but sprints are off")
	e.mustAssignIssueSprint(t, issue, planned)
	e.mustSetFixtureSprintsEnabled(t, false)

	body := e.uiGet(t, e.projectPath()+"/all", e.authToken)
	if row := issueSummaryRow(t, body, "Planned but sprints are off"); strings.Contains(row, "data-issue-sprint-badge") {
		t.Fatalf("sprint badge shown with sprints disabled: %s", row)
	}
}
