package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

// lateRepeat makes a weekly repeat whose first repetition was four weeks ago
// today, so completing it now skips the three weeks between and lands on
// today.
func lateRepeat(t *testing.T) (store.IssueRepeatSetting, model.Date) {
	t.Helper()
	rule, err := model.ParseRepeatRule("weekly")
	if err != nil {
		t.Fatalf("ParseRepeatRule: %v", err)
	}
	today := model.TodayIn(time.UTC, time.Now())
	start := today.AddDays(-28)
	return store.IssueRepeatSetting{Rule: rule, TimeZone: "UTC", StartsOn: &start}, today
}

func TestIssueRepeatREST(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	code, body := e.do(t, http.MethodPost, e.projectIssuesPath(), map[string]any{
		"title":            "Team retro",
		"repeat":           "FREQ=MONTHLY;BYDAY=3WE",
		"repeat_time_zone": "Europe/London",
		"repeat_starts_on": "2026-10-01",
	})
	retro := decode[model.Issue](t, body)
	if code != http.StatusCreated || retro.Repeat == nil || retro.Repeat.Rule != "FREQ=MONTHLY;BYDAY=3WE" || retro.Repeat.Summary != "Every month on the third Wednesday" ||
		retro.Repeat.TimeZone != "Europe/London" || retro.Repeat.Occurrence.String() != "2026-10-21" || retro.DueDate == nil || retro.DueDate.String() != "2026-10-21" {
		t.Fatalf("create repeating code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodPost, e.projectIssuesPath(), map[string]any{"title": "plain"})
	if code != http.StatusCreated || !strings.Contains(string(body), `"repeat":null`) {
		t.Fatalf("create plain code = %d body = %s", code, body)
	}
	plain := decode[model.Issue](t, body)
	for name, req := range map[string]map[string]any{
		"bad rule":          {"title": "x", "repeat": "FREQ=HOURLY"},
		"zone without rule": {"title": "x", "repeat_time_zone": "Europe/London"},
		"bad zone":          {"title": "x", "repeat": "daily", "repeat_time_zone": "Mars/Base"},
		"bad start":         {"title": "x", "repeat": "daily", "repeat_starts_on": "next week"},
	} {
		if code, body := e.do(t, http.MethodPost, e.projectIssuesPath(), req); code != http.StatusBadRequest || !strings.Contains(string(body), "repeat") {
			t.Fatalf("create %s code = %d body = %s", name, code, body)
		}
	}

	code, body = e.do(t, http.MethodPost, e.issueSubIssuesPath(retro), map[string]any{"title": "Book the room", "repeat": "weekdays"})
	if sub := decode[model.Issue](t, body); code != http.StatusCreated || sub.Repeat == nil || sub.Repeat.Summary != "Every weekday" {
		t.Fatalf("create repeating sub-issue code = %d body = %s", code, body)
	}
	if code, body := e.do(t, http.MethodPost, e.issueSubIssuesPath(retro), map[string]any{"title": "x", "repeat": "nope"}); code != http.StatusBadRequest {
		t.Fatalf("create sub-issue bad rule code = %d body = %s", code, body)
	}

	// Set, change and stop a repeat on an existing issue.
	code, body = e.do(t, http.MethodPatch, e.issuePath(plain), map[string]any{"repeat": "FREQ=WEEKLY;BYDAY=FR", "repeat_starts_on": "2026-10-05"})
	if updated := decode[model.Issue](t, body); code != http.StatusOK || updated.Repeat == nil || !updated.Repeat.Current || updated.Repeat.Occurrence.String() != "2026-10-09" {
		t.Fatalf("set repeat code = %d body = %s", code, body)
	}
	if code, body := e.do(t, http.MethodPatch, e.issuePath(plain), map[string]any{"repeat": "CRON:61 * * * *"}); code != http.StatusBadRequest || !strings.Contains(string(body), "cron") {
		t.Fatalf("update bad cron code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodPatch, e.issuePath(plain), map[string]any{"clear_repeat": true})
	if stopped := decode[model.Issue](t, body); code != http.StatusOK || stopped.Repeat == nil || stopped.Repeat.Current {
		t.Fatalf("clear repeat code = %d body = %s", code, body)
	}

	// Completing a late repetition creates the next and names what it
	// skipped.
	setting, today := lateRepeat(t)
	late, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "Weekly report", Repeat: &setting})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	code, body = e.do(t, http.MethodPatch, e.issuePath(late), map[string]any{"status": "done"})
	done := decode[model.Issue](t, body)
	if code != http.StatusOK || done.Repeat == nil || done.Repeat.Next == nil || done.Repeat.Next.Occurrence != today ||
		done.Repeat.Next.SkippedCount != 3 || done.Repeat.Next.SkippedDates[0] != today.AddDays(-21) {
		t.Fatalf("complete late code = %d body = %s", code, body)
	}

	// A done issue must be reopened to repeat.
	if code, body := e.do(t, http.MethodPatch, e.issuePath(done), map[string]any{"repeat": "daily"}); code != http.StatusConflict || !strings.Contains(string(body), "reopen") {
		t.Fatalf("repeat a done issue code = %d body = %s", code, body)
	}

	// Making an issue repeat is for members.
	if _, err := e.store.UpdateProjectAccessMode(e.ctx, e.projectID, model.ProjectAccessPublicIssues); err != nil {
		t.Fatalf("UpdateProjectAccessMode: %v", err)
	}
	_, outsiderToken := e.mustUserToken(t, "repeat-outsider")
	if code, body := e.doWithToken(t, outsiderToken, http.MethodPost, e.projectIssuesPath(), map[string]any{"title": "outsider", "repeat": "daily"}); code != http.StatusForbidden {
		t.Fatalf("outsider create with repeat code = %d body = %s", code, body)
	}
}

func TestIssueRepeatMCP(t *testing.T) {
	t.Parallel()
	e := newMCPHTTPEnv(t, nil)
	session := mcpConnect(t, e, e.authToken)
	project := map[string]any{"owner": e.ownerUsername, "key": e.projKey}
	with := func(base map[string]any, extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	created := decodeMCPField[model.Issue](t, mcpCall(t, e, session, "track_create_issue", with(project, map[string]any{
		"title": "Pay rent", "repeat": "FREQ=MONTHLY;BYMONTHDAY=1", "repeat_starts_on": "2026-11-01",
	})), "issue")
	if created.Repeat == nil || created.Repeat.TimeZone != "UTC" || created.Repeat.Occurrence.String() != "2026-11-01" {
		t.Fatalf("create repeat = %+v", created.Repeat)
	}
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_create_issue", with(project, map[string]any{"title": "x", "repeat": "FREQ=SECONDLY"})), "validation_error")

	issue := map[string]any{"owner": e.ownerUsername, "issue": created.Identifier}
	sub := decodeMCPField[model.Issue](t, mcpCall(t, e, session, "track_create_sub_issue", with(issue, map[string]any{"title": "Check the bank", "repeat": "monthly"})), "issue")
	if sub.Repeat == nil || sub.ParentIssueID == nil {
		t.Fatalf("sub-issue repeat = %+v", sub.Repeat)
	}
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_create_sub_issue", with(issue, map[string]any{"title": "x", "repeat_starts_on": "2026-11-01"})), "validation_error")

	changed := decodeMCPField[model.Issue](t, mcpCall(t, e, session, "track_update_issue", with(issue, map[string]any{"repeat": "CRON:0 9 1 * *", "repeat_time_zone": "Asia/Tokyo"})), "issue")
	if changed.Repeat == nil || changed.Repeat.Rule != "CRON:0 9 1 * *" || changed.Repeat.TimeZone != "Asia/Tokyo" || changed.Repeat.ID != created.Repeat.ID {
		t.Fatalf("change repeat = %+v", changed.Repeat)
	}
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_update_issue", with(issue, map[string]any{"repeat": "daily", "repeat_time_zone": "Nowhere/Land"})), "validation_error")
	done := decodeMCPField[model.Issue](t, mcpCall(t, e, session, "track_update_issue", with(issue, map[string]any{"status": "done"})), "issue")
	if done.Repeat == nil || done.Repeat.Next == nil || done.Repeat.Next.Occurrence.String() != "2026-12-01" {
		t.Fatalf("complete = %+v", done.Repeat)
	}
	next := map[string]any{"owner": e.ownerUsername, "issue": done.Repeat.Next.Identifier}
	stopped := decodeMCPField[model.Issue](t, mcpCall(t, e, session, "track_update_issue", with(next, map[string]any{"clear_repeat": true})), "issue")
	if stopped.Repeat == nil || stopped.Repeat.Current {
		t.Fatalf("stop = %+v", stopped.Repeat)
	}

	if _, err := e.store.UpdateProjectAccessMode(e.ctx, e.projectID, model.ProjectAccessPublicIssues); err != nil {
		t.Fatalf("UpdateProjectAccessMode: %v", err)
	}
	_, outsiderToken := e.mustUserToken(t, "mcp-repeat-outsider")
	outsider := mcpConnect(t, e, outsiderToken)
	requireMCPErrorCode(t, mcpCallExpectError(t, e, outsider, "track_create_issue", with(project, map[string]any{"title": "outsider", "repeat": "daily"})), "forbidden")
}

func TestUIIssueRepeat(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-repeat")
	issue := e.mustCreateIssue(t, "repeat target issue")

	panel := e.uiGet(t, e.issuePath(issue)+"/panel", token)
	for _, want := range []string{
		`>Repeat</dt>`,
		`Doesn't repeat`,
		`aria-label="Edit repeat" hx-get="` + e.issuePath(issue) + `/repeat/edit"`,
	} {
		if !strings.Contains(panel, want) {
			t.Fatalf("panel missing %q: %s", want, panel)
		}
	}
	if strings.Contains(panel, "data-issue-repeat-badge") {
		t.Fatalf("non-repeating panel rendered a repeat badge: %s", panel)
	}

	edit := e.uiGet(t, e.issuePath(issue)+"/repeat/edit", token)
	for _, want := range []string{
		`method="post" action="` + e.issuePath(issue) + `/repeat"`,
		`<select id="issue-repeat-frequency" name="repeat_frequency"`,
		`<option value="" selected>Doesn&#39;t repeat</option>`,
		`hx-get="/issues/repeat-fields"`,
		`data-repeat-time-zone`,
		`aria-label="Save repeat"`,
	} {
		if !strings.Contains(edit, want) {
			t.Fatalf("repeat edit missing %q: %s", want, edit)
		}
	}

	if strings.Contains(edit, `type="hidden" name="repeat_starts_on"`) {
		t.Fatalf("an issue without a due date carried a default start date: %s", edit)
	}
	// An issue with a due date starts the schedule there once a frequency
	// is chosen.
	due, err := model.ParseDate("2026-11-30")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	dated, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "dated", DueDate: &due})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if datedEdit := e.uiGet(t, e.issuePath(dated)+"/repeat/edit", token); !strings.Contains(datedEdit, `<input type="hidden" name="repeat_starts_on" value="2026-11-30">`) {
		t.Fatalf("dated issue's editor missing its start date: %s", datedEdit)
	}

	// The picker re-renders as choices change, with a preview.
	fields := e.uiGet(t, "/issues/repeat-fields?"+url.Values{
		"repeat_prefix":          {"issue-repeat"},
		"repeat_frequency":       {"monthly"},
		"repeat_monthly_by":      {"weekday"},
		"repeat_ordinal":         {"3"},
		"repeat_ordinal_weekday": {"WE"},
		"repeat_starts_on":       {"2026-10-01"},
		"repeat_time_zone":       {"Europe/London"},
	}.Encode(), token)
	for _, want := range []string{
		"Every month on the third Wednesday",
		"Wed, Oct 21, 2026 · Wed, Nov 18, 2026 · Wed, Dec 16, 2026",
		"Dates in Europe/London.",
		`<option value="3" selected>Third</option>`,
		`<option value="WE" selected>Wednesday</option>`,
	} {
		if !strings.Contains(fields, want) {
			t.Fatalf("repeat fields missing %q: %s", want, fields)
		}
	}
	if bad := e.uiGet(t, "/issues/repeat-fields?repeat_prefix=issue-repeat&repeat_frequency=cron&repeat_cron=nope", token); !strings.Contains(bad, "data-repeat-error") {
		t.Fatalf("bad cron preview missing error: %s", bad)
	}
	res := e.uiDoNoRedirect(t, http.MethodGet, "/issues/repeat-fields?repeat_prefix=elsewhere", token, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown picker code = %d", res.StatusCode)
	}

	post := func(form url.Values) (int, string) {
		t.Helper()
		res := e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/repeat", token, strings.NewReader(form.Encode()))
		defer res.Body.Close()
		return res.StatusCode, readBody(t, res)
	}
	code, body := post(url.Values{"repeat_frequency": {"weekly"}, "repeat_weekday": {"MO", "TH"}, "repeat_interval": {"2"}, "repeat_starts_on": {"2026-10-05"}, "repeat_time_zone": {"UTC"}})
	for _, want := range []string{
		`data-issue-repeat-badge data-tooltip="Repeats: Every 2 weeks on Monday and Thursday"`,
		`data-issue-repeat-summary>Every 2 weeks on Monday and Thursday<`,
		"Then Oct 8, 2026 · UTC",
	} {
		if code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("set repeat code = %d missing %q: %s", code, want, body)
		}
	}
	// Editing opens on the current repeat.
	edit = e.uiGet(t, e.issuePath(issue)+"/repeat/edit", token)
	for _, want := range []string{`<option value="weekly" selected>Weekly</option>`, `name="repeat_interval" value="2"`, `value="MO" checked`, `value="TH" checked`, `name="repeat_starts_on" value="2026-10-05"`} {
		if !strings.Contains(edit, want) {
			t.Fatalf("repeat edit missing %q: %s", want, edit)
		}
	}
	if list := e.uiGet(t, e.projectPath()+"/all", token); !strings.Contains(list, "data-issue-repeat-badge") {
		t.Fatalf("issue list missing repeat badge: %s", list)
	}

	code, body = post(url.Values{"repeat_frequency": {"daily"}, "repeat_time_zone": {"Mars/Base"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Unknown time zone.") {
		t.Fatalf("bad zone code = %d body = %s", code, body)
	}
	code, body = post(url.Values{"repeat_frequency": {"cron"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Enter a cron expression.") {
		t.Fatalf("empty cron code = %d body = %s", code, body)
	}
	// The cron library looks five years ahead, so this finds no date.
	code, body = post(url.Values{"repeat_frequency": {"cron"}, "repeat_cron": {"0 0 29 2 *"}, "repeat_starts_on": {"2097-03-01"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "The schedule never falls on a date.") {
		t.Fatalf("schedule without dates code = %d body = %s", code, body)
	}
	missing := e.issuePath(issue)[:strings.LastIndex(e.issuePath(issue), "-")] + "-9999"
	for _, req := range []struct{ method, path string }{{http.MethodGet, missing + "/repeat/edit"}, {http.MethodPost, missing + "/repeat"}} {
		res := e.uiDoNoRedirect(t, req.method, req.path, token, strings.NewReader("repeat_frequency=daily"))
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s code = %d, want 404", req.method, req.path, res.StatusCode)
		}
	}
	code, body = post(url.Values{"repeat_frequency": {""}})
	if code != http.StatusOK || !strings.Contains(body, "Doesn't repeat") || strings.Contains(body, "data-issue-repeat-badge") {
		t.Fatalf("stop repeat code = %d body = %s", code, body)
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(issue)+"/repeat", token, strings.NewReader("%zz"))
	defer res.Body.Close()
	if body := readBody(t, res); res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "unable to read form") {
		t.Fatalf("unreadable form code = %d body = %s", res.StatusCode, body)
	}

	// A done issue doesn't offer the editor, and saving says to reopen it.
	done := model.StatusDone
	if _, err := e.store.UpdateIssue(e.ctx, issue.ID, store.UpdateIssueParams{Status: &done}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if donePanel := e.uiGet(t, e.issuePath(issue)+"/panel", token); strings.Contains(donePanel, `aria-label="Edit repeat"`) {
		t.Fatalf("done panel offered the repeat editor: %s", donePanel)
	}
	code, body = post(url.Values{"repeat_frequency": {"daily"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Reopen the issue to make it repeat.") {
		t.Fatalf("repeat a done issue code = %d body = %s", code, body)
	}

	// A read-only member sees the repeat but can't change it.
	readonly, readonlyToken := e.mustUserToken(t, "ui-repeat-readonly")
	if _, err := e.store.SetProjectMemberRole(e.ctx, e.projectID, readonly.ID, model.ProjectMemberRoleReadonly); err != nil {
		t.Fatalf("SetProjectMemberRole: %v", err)
	}
	other := e.mustCreateIssue(t, "readonly target")
	if readonlyPanel := e.uiGet(t, e.issuePath(other)+"/panel", readonlyToken); strings.Contains(readonlyPanel, `aria-label="Edit repeat"`) {
		t.Fatalf("readonly panel offered repeat edit: %s", readonlyPanel)
	}
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issuePath(other)+"/repeat", readonlyToken, strings.NewReader(url.Values{"repeat_frequency": {"daily"}}.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("readonly update code = %d, want 403", res.StatusCode)
	}
}

func TestUIIssueRepeatSkipNotices(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-repeat-notices")
	setting, today := lateRepeat(t)
	late, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "Weekly report", Repeat: &setting})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	done := model.StatusDone
	completed, err := e.store.UpdateIssue(e.ctx, late.ID, store.UpdateIssueParams{Status: &done})
	if err != nil || completed.Repeat.Next == nil {
		t.Fatalf("complete = %+v, %v", completed.Repeat, err)
	}
	format := func(d model.Date) string { return d.Time().Format("Jan 2, 2006") }

	// The completed issue warns that its late completion skipped dates.
	panel := e.uiGet(t, e.issuePath(completed)+"/panel", token)
	for _, want := range []string{
		`data-issue-repeat-notice="skipped"`,
		`data-lucide="triangle-alert"`,
		"Completed after " + format(today.AddDays(-21)) + ", " + format(today.AddDays(-14)) + " and " + format(today.AddDays(-7)) + ", so those repetitions were skipped. Next:",
		completed.Repeat.Next.Identifier,
		"due " + format(today) + ".",
		`data-issue-repeat-next`,
	} {
		if !strings.Contains(panel, want) {
			t.Fatalf("completed panel missing %q: %s", want, panel)
		}
	}

	// The next repetition says why it skipped ahead.
	next, err := e.store.GetIssueByOwnerKeyNumber(e.ctx, completed.OwnerUsername, completed.ProjectKey, completed.Repeat.Next.Number)
	if err != nil {
		t.Fatalf("get next: %v", err)
	}
	panel = e.uiGet(t, e.issuePath(next)+"/panel", token)
	for _, want := range []string{
		`data-issue-repeat-notice="arrived"`,
		"Skipped 3 repetitions (" + format(today.AddDays(-21)) + ", " + format(today.AddDays(-14)) + " and " + format(today.AddDays(-7)) + ") because the previous repetition",
		late.Identifier,
		"was completed on " + time.Now().UTC().Format("Jan 2, 2006") + ".",
		`data-issue-repeat-previous`,
		`data-issue-repeat-badge`,
	} {
		if !strings.Contains(panel, want) {
			t.Fatalf("next panel missing %q: %s", want, panel)
		}
	}
}

func TestUINewIssueRepeat(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustProjectMemberToken(t, "ui-new-repeat")
	submit := func(token string, form url.Values) (int, string) {
		t.Helper()
		form.Set("project_id", e.projectID.String())
		res := e.uiDoNoRedirectWithHeaders(t, http.MethodPost, "/issues", token, strings.NewReader(form.Encode()), map[string]string{"HX-Request": "true"})
		defer res.Body.Close()
		return res.StatusCode, readBody(t, res)
	}

	if form := e.uiGet(t, e.projectPath()+"/issues/new", token); !strings.Contains(form, `<select id="new-issue-repeat-frequency" name="repeat_frequency"`) {
		t.Fatalf("new issue form missing repeat picker: %s", form)
	}
	code, body := submit(token, url.Values{"title": {"Daily stand-up"}, "repeat_frequency": {"daily"}, "repeat_starts_on": {"2026-10-05"}, "repeat_time_zone": {"Europe/Paris"}})
	if code != http.StatusOK || !strings.Contains(body, `data-issue-repeat-summary>Every day<`) || !strings.Contains(body, "Europe/Paris") {
		t.Fatalf("create repeating code = %d body = %s", code, body)
	}
	code, body = submit(token, url.Values{"title": {"Cron without expression"}, "repeat_frequency": {"cron"}})
	if code != http.StatusOK || !strings.Contains(body, "Enter a cron expression.") || !strings.Contains(body, `value="Cron without expression"`) {
		t.Fatalf("create with empty cron code = %d body = %s", code, body)
	}

	if _, err := e.store.UpdateProjectAccessMode(e.ctx, e.projectID, model.ProjectAccessPublicIssues); err != nil {
		t.Fatalf("UpdateProjectAccessMode: %v", err)
	}
	_, outsiderToken := e.mustUserToken(t, "ui-new-repeat-outsider")
	if form := e.uiGet(t, e.projectPath()+"/issues/new", outsiderToken); strings.Contains(form, `name="repeat_frequency"`) {
		t.Fatalf("public new issue form offered the repeat picker: %s", form)
	}
	code, body = submit(outsiderToken, url.Values{"title": {"outsider"}, "repeat_frequency": {"daily"}})
	if code != http.StatusOK || !strings.Contains(body, "Public issue submissions cannot make an issue repeat.") {
		t.Fatalf("outsider create with repeat code = %d body = %s", code, body)
	}
}
