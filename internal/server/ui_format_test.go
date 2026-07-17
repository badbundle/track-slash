package server

import (
	"bytes"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestUIProjectIcon(t *testing.T) {
	t.Parallel()

	thumbID := uuid.MustParse("6a0d51f8-4a4f-46d5-8de1-726a7823d8f4")
	project := model.Project{OwnerUsername: "bradley", Key: "TRACK", Name: "Roadmap", ImageThumbnailObjectID: &thumbID}
	icon := uiProjectIcon(project, "icon-class")
	if icon.Label != "Roadmap" || icon.Initial != "R" || icon.Class != "icon-class" {
		t.Fatalf("project icon = %+v", icon)
	}
	wantURL := "/bradley/projects/TRACK/image/thumbnail/content?v=" + thumbID.String()
	if icon.ThumbnailURL != wantURL {
		t.Fatalf("ThumbnailURL = %q, want %q", icon.ThumbnailURL, wantURL)
	}

	var buf bytes.Buffer
	if err := uiTemplates.ExecuteTemplate(&buf, "project-icon", icon); err != nil {
		t.Fatalf("ExecuteTemplate project icon: %v", err)
	}
	body := buf.String()
	for _, want := range []string{`aria-label="Roadmap"`, `class="icon-class overflow-hidden rounded-md"`, `src="` + wantURL + `"`, `class="h-full w-full rounded-md object-cover"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("image project icon missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "rounded-full") || strings.Contains(body, ">R</span>") {
		t.Fatalf("image project icon should be square without fallback: %s", body)
	}

	for _, tt := range []struct {
		name string
		key  string
		want string
	}{
		{name: " roadmap", key: "TRACK", want: "R"},
		{name: "", key: "TRACK", want: "T"},
		{name: "", key: "", want: "?"},
	} {
		fallback := uiProjectIcon(model.Project{Name: tt.name, Key: tt.key}, "icon-class")
		if fallback.Initial != tt.want || fallback.ThumbnailURL != "" {
			t.Fatalf("uiProjectIcon(%q, %q) = %+v, want initial %q", tt.name, tt.key, fallback, tt.want)
		}
	}
}

func TestUIIssueContextBreadcrumbLinksIssueAndPreservesParent(t *testing.T) {
	t.Parallel()

	project := model.Project{OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"}
	parent := model.Issue{OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-7"}
	issue := model.Issue{OwnerUsername: "bradley", ProjectKey: "TRACK", Identifier: "TRACK-8"}
	breadcrumb := uiIssueContextBreadcrumb(project, issue, &parent)
	if len(breadcrumb.Items) != 5 {
		t.Fatalf("breadcrumb items = %#v, want project, parent, issue, and context hierarchy", breadcrumb.Items)
	}
	parentItem := breadcrumb.Items[2]
	if parentItem.Label != parent.Identifier || parentItem.Href != uiIssuePath(parent) || parentItem.Current {
		t.Fatalf("parent breadcrumb = %#v", parentItem)
	}
	issueItem := breadcrumb.Items[3]
	if issueItem.Label != issue.Identifier || issueItem.Href != uiIssuePath(issue) || issueItem.HXGet != uiIssuePanelPath(issue) || issueItem.Current || !issueItem.IssueKey {
		t.Fatalf("issue breadcrumb = %#v", issueItem)
	}
	contextItem := breadcrumb.Items[4]
	if contextItem.Label != "Context" || !contextItem.Current || contextItem.Href != "" {
		t.Fatalf("context breadcrumb = %#v", contextItem)
	}
}

func TestUIStatusClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status model.Status
		want   string
	}{
		{status: model.StatusTodo, want: "border-slate-300 bg-slate-100 text-slate-700 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200"},
		{status: model.StatusInProgress, want: "border-blue-300 bg-blue-50 text-blue-800 dark:border-blue-500/40 dark:bg-blue-950/40 dark:text-blue-200"},
		{status: model.StatusDone, want: "border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-500/40 dark:bg-emerald-950/40 dark:text-emerald-200"},
		{status: model.StatusClosed, want: "border-zinc-300 bg-zinc-100 text-zinc-700 dark:border-zinc-600 dark:bg-zinc-900 dark:text-zinc-200"},
		{status: model.Status("custom"), want: "border-slate-300 bg-slate-50 text-slate-700 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200"},
	}

	for _, tt := range tests {
		if got := uiStatusClass(tt.status); got != tt.want {
			t.Fatalf("uiStatusClass(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestUIStatusRowClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status model.Status
		want   string
	}{
		{status: model.StatusTodo, want: "bg-slate-50/70 hover:bg-slate-100/80 dark:bg-slate-900/30 dark:hover:bg-slate-800/70"},
		{status: model.StatusInProgress, want: "bg-blue-50/45 hover:bg-blue-50 dark:bg-blue-950/15 dark:hover:bg-blue-950/30"},
		{status: model.StatusDone, want: "bg-emerald-50/45 hover:bg-emerald-50 dark:bg-emerald-950/15 dark:hover:bg-emerald-950/30"},
		{status: model.StatusClosed, want: "bg-zinc-50/70 hover:bg-zinc-100/80 dark:bg-zinc-900/35 dark:hover:bg-zinc-800/70"},
		{status: model.Status("custom"), want: "bg-white hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800/60"},
	}

	for _, tt := range tests {
		if got := uiStatusRowClass(tt.status); got != tt.want {
			t.Fatalf("uiStatusRowClass(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestUIStatusSurfaceClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status model.Status
		want   string
	}{
		{status: model.StatusTodo, want: "bg-slate-50/70 dark:bg-slate-900/30"},
		{status: model.StatusInProgress, want: "bg-blue-50/45 dark:bg-blue-950/15"},
		{status: model.StatusDone, want: "bg-emerald-50/45 dark:bg-emerald-950/15"},
		{status: model.StatusClosed, want: "bg-zinc-50/70 dark:bg-zinc-900/35"},
		{status: model.Status("custom"), want: "bg-white dark:bg-slate-900"},
	}

	for _, tt := range tests {
		if got := uiStatusSurfaceClass(tt.status); got != tt.want {
			t.Fatalf("uiStatusSurfaceClass(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestUICloseReasonLabelAndOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason model.IssueCloseReason
		want   string
	}{
		{reason: model.CloseReasonDuplicate, want: "Duplicate"},
		{reason: model.CloseReasonWontDo, want: "Won't Do"},
		{reason: model.CloseReasonInvalid, want: "Invalid"},
		{reason: model.IssueCloseReason("custom"), want: "custom"},
	}
	for _, tt := range tests {
		if got := uiCloseReasonLabel(tt.reason); got != tt.want {
			t.Fatalf("uiCloseReasonLabel(%q) = %q, want %q", tt.reason, got, tt.want)
		}
		reason := tt.reason
		if got := uiCloseReasonLabel(&reason); got != tt.want {
			t.Fatalf("uiCloseReasonLabel(&%q) = %q, want %q", tt.reason, got, tt.want)
		}
	}

	opts := uiCloseReasonOptions()
	if len(opts) != 3 ||
		opts[0].Reason != model.CloseReasonDuplicate ||
		opts[1].Reason != model.CloseReasonWontDo ||
		opts[2].Reason != model.CloseReasonInvalid {
		t.Fatalf("close reason options = %+v", opts)
	}
}

func TestUISubIssueProgress(t *testing.T) {
	t.Parallel()

	empty := uiSubIssueProgress(nil)
	if empty.Total != 0 || empty.DonePercent != "0%" || empty.InProgressPercent != "0%" || empty.TodoPercent != "0%" || empty.Label != "Sub-issue progress: no sub-issues" {
		t.Fatalf("empty progress = %+v", empty)
	}

	mixed := uiSubIssueProgress([]model.Issue{
		{Status: model.StatusDone},
		{Status: model.StatusDone},
		{Status: model.StatusClosed},
		{Status: model.StatusInProgress},
		{Status: model.StatusTodo},
		{Status: model.Status("custom")},
	})
	if mixed.Total != 6 || mixed.Done != 3 || mixed.InProgress != 1 || mixed.Todo != 2 {
		t.Fatalf("mixed counts = %+v", mixed)
	}
	if mixed.DonePercent != "50.00%" || mixed.InProgressPercent != "16.67%" || mixed.TodoPercent != "33.33%" {
		t.Fatalf("mixed percentages = %+v", mixed)
	}
	if mixed.Label != "Sub-issue progress: 3 done, 1 in progress, 2 to do" {
		t.Fatalf("mixed label = %q", mixed.Label)
	}
}

func TestUIIssueColumnStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status model.Status
		want   model.Status
	}{
		{status: model.StatusTodo, want: model.StatusTodo},
		{status: model.StatusInProgress, want: model.StatusInProgress},
		{status: model.StatusDone, want: model.StatusDone},
		{status: model.StatusClosed, want: model.StatusDone},
		{status: model.Status("custom"), want: model.Status("custom")},
	}
	for _, tt := range tests {
		if got := uiIssueColumnStatus(tt.status); got != tt.want {
			t.Fatalf("uiIssueColumnStatus(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestUIPriorityClassAndLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		priority  model.IssuePriority
		wantClass string
		wantLabel string
	}{
		{priority: model.PriorityP0, wantClass: "bg-red-600", wantLabel: "P0"},
		{priority: model.PriorityP1, wantClass: "bg-orange-500", wantLabel: "P1"},
		{priority: model.PriorityP2, wantClass: "bg-amber-500", wantLabel: "P2"},
		{priority: model.PriorityP3, wantClass: "bg-yellow-500", wantLabel: "P3"},
		{priority: model.PriorityP4, wantClass: "bg-gray-500", wantLabel: "P4"},
		{priority: "", wantClass: "bg-amber-500", wantLabel: "P2"},
		{priority: model.IssuePriority("PX"), wantClass: "bg-gray-500", wantLabel: "PX"},
	}

	for _, tt := range tests {
		if got := uiPriorityClass(tt.priority); got != tt.wantClass {
			t.Fatalf("uiPriorityClass(%q) = %q, want %q", tt.priority, got, tt.wantClass)
		}
		if got := uiPriorityLabel(tt.priority); got != tt.wantLabel {
			t.Fatalf("uiPriorityLabel(%q) = %q, want %q", tt.priority, got, tt.wantLabel)
		}
	}
}

func TestUIDueBadgeClassOverdueOnlyForOpenPastIssues(t *testing.T) {
	t.Parallel()

	past, err := model.ParseDate("2026-06-19")
	if err != nil {
		t.Fatalf("ParseDate past: %v", err)
	}
	future, err := model.ParseDate("2026-06-21")
	if err != nil {
		t.Fatalf("ParseDate future: %v", err)
	}
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)

	if !uiIssueOverdue(model.Issue{Status: model.StatusTodo, DueDate: &past}, now) {
		t.Fatal("open past issue should be overdue")
	}
	if uiIssueOverdue(model.Issue{Status: model.StatusDone, DueDate: &past}, now) {
		t.Fatal("done past issue should not be overdue")
	}
	if uiIssueOverdue(model.Issue{Status: model.StatusClosed, DueDate: &past}, now) {
		t.Fatal("closed past issue should not be overdue")
	}
	if uiIssueOverdue(model.Issue{Status: model.StatusTodo, DueDate: &future}, now) {
		t.Fatal("future issue should not be overdue")
	}
	if uiIssueOverdue(model.Issue{Status: model.StatusTodo}, now) {
		t.Fatal("issue without due date should not be overdue")
	}

	today, err := model.ParseDate("2026-06-20")
	if err != nil {
		t.Fatalf("ParseDate today: %v", err)
	}
	sixDays, err := model.ParseDate("2026-06-26")
	if err != nil {
		t.Fatalf("ParseDate six days: %v", err)
	}
	sevenDays, err := model.ParseDate("2026-06-27")
	if err != nil {
		t.Fatalf("ParseDate seven days: %v", err)
	}
	for _, issue := range []model.Issue{
		{Status: model.StatusTodo, DueDate: &today},
		{Status: model.StatusTodo, DueDate: &sixDays},
	} {
		if !uiIssueDueSoon(issue, now) {
			t.Fatalf("issue should be due soon: %+v", issue)
		}
	}
	if uiIssueDueSoon(model.Issue{Status: model.StatusTodo, DueDate: &sevenDays}, now) {
		t.Fatal("seven days out should not be due soon")
	}
	if days, ok := uiIssueDueDays(model.Issue{Status: model.StatusTodo, DueDate: &sixDays}, now); !ok || days != 6 {
		t.Fatalf("days = %d, ok = %v, want 6 true", days, ok)
	}
	if days, ok := uiIssueDueDays(model.Issue{Status: model.StatusDone, DueDate: &today}, now); ok || days != 0 {
		t.Fatalf("done days = %d, ok = %v, want 0 false", days, ok)
	}
	if days, ok := uiIssueDueDays(model.Issue{Status: model.StatusClosed, DueDate: &today}, now); ok || days != 0 {
		t.Fatalf("closed days = %d, ok = %v, want 0 false", days, ok)
	}
}

func TestUIDueDateFormatHelpers(t *testing.T) {
	t.Parallel()

	if uiDueDateValue(nil) != "" || uiDueDateShort(nil) != "" || uiDueDateFull(nil) != "" {
		t.Fatal("nil due date helpers should return empty strings")
	}
	dueDate, err := model.ParseDate("2026-06-24")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	if got := uiDueDateValue(&dueDate); got != "2026-06-24" {
		t.Fatalf("value = %q", got)
	}
	if got := uiDueDateShort(&dueDate); got != "Jun 24" {
		t.Fatalf("short = %q", got)
	}
	if got := uiDueDateFull(&dueDate); got != "Jun 24, 2026" {
		t.Fatalf("full = %q", got)
	}
	overdueDate, err := model.ParseDate("2020-01-01")
	if err != nil {
		t.Fatalf("ParseDate overdue: %v", err)
	}
	if got := uiDueBadgeClass(model.Issue{Status: model.StatusTodo, DueDate: &overdueDate}); !strings.Contains(got, "border-rose-200") {
		t.Fatalf("overdue class = %q", got)
	}
	today, err := model.ParseDate(time.Now().Format(model.DateLayout))
	if err != nil {
		t.Fatalf("ParseDate today: %v", err)
	}
	if got := uiDueBadgeClass(model.Issue{Status: model.StatusTodo, DueDate: &today}); !strings.Contains(got, "border-amber-200") {
		t.Fatalf("today class = %q", got)
	}
	if got := uiDueBadgeIcon(model.Issue{Status: model.StatusTodo, DueDate: &today}); got != "clock" {
		t.Fatalf("today icon = %q", got)
	}
	if got := uiDueBadgeLabel(model.Issue{Status: model.StatusTodo, DueDate: &today}); got != "Today" {
		t.Fatalf("today label = %q", got)
	}
	tomorrow := model.DateFromTime(time.Now().AddDate(0, 0, 1))
	if got := uiDueBadgeLabel(model.Issue{Status: model.StatusTodo, DueDate: &tomorrow}); got != "1 day" {
		t.Fatalf("tomorrow label = %q", got)
	}
	sixDays := model.DateFromTime(time.Now().AddDate(0, 0, 6))
	if got := uiDueBadgeLabel(model.Issue{Status: model.StatusTodo, DueDate: &sixDays}); got != "6 days" {
		t.Fatalf("six-day label = %q", got)
	}
	if got := uiDueBadgeClass(model.Issue{}); !strings.Contains(got, "border-slate-200") {
		t.Fatalf("neutral class = %q", got)
	}
	if got := uiDueBadgeIcon(model.Issue{}); got != "calendar" {
		t.Fatalf("neutral icon = %q", got)
	}
	if got := uiDueBadgeLabel(model.Issue{}); got != "" {
		t.Fatalf("nil label = %q", got)
	}
}
