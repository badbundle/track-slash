package server

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
)

var uiRepeatTestNow = time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)

func uiRepeatTestDate(t *testing.T, raw string) model.Date {
	t.Helper()
	d, err := model.ParseDate(raw)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", raw, err)
	}
	return d
}

func TestUIRepeatFieldsFromValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		values   url.Values
		summary  string
		upcoming string
		err      string
		check    func(t *testing.T, f uiRepeatFields)
	}{
		{name: "doesn't repeat", values: url.Values{}, check: func(t *testing.T, f uiRepeatFields) {
			if f.StartsOn != "2026-10-05" || f.StartsOnGiven || f.TimeZone != "" || f.rule != nil || f.MonthlyBy != "day" || f.OrdinalWeekday != "MO" || f.Ordinal != 1 {
				t.Fatalf("defaults = %+v", f)
			}
		}},
		{name: "every 3 days", values: url.Values{"repeat_frequency": {"daily"}, "repeat_interval": {"3"}}, summary: "Every 3 days", upcoming: "Mon, Oct 5, 2026|Thu, Oct 8, 2026|Sun, Oct 11, 2026", check: func(t *testing.T, f uiRepeatFields) {
			if f.IntervalUnit() != "days" || f.TimeZone != "UTC" {
				t.Fatalf("unit %q zone %q", f.IntervalUnit(), f.TimeZone)
			}
		}},
		{name: "weekdays", values: url.Values{"repeat_frequency": {"weekdays"}}, summary: "Every weekday", check: func(t *testing.T, f uiRepeatFields) {
			if f.IntervalUnit() != "" {
				t.Fatalf("weekdays has an interval unit %q", f.IntervalUnit())
			}
		}},
		{name: "weekly without days takes the start's", values: url.Values{"repeat_frequency": {"weekly"}, "repeat_weekday": {"XX"}, "repeat_starts_on": {"2026-10-07"}}, summary: "Every week on Wednesday", check: func(t *testing.T, f uiRepeatFields) {
			if strings.Join(f.Weekdays, ",") != "WE" || f.IntervalUnit() != "week" {
				t.Fatalf("weekdays %v unit %q", f.Weekdays, f.IntervalUnit())
			}
		}},
		{name: "weekly on chosen days", values: url.Values{"repeat_frequency": {"weekly"}, "repeat_weekday": {"FR", "MO"}}, summary: "Every week on Monday and Friday"},
		{name: "monthly on a day", values: url.Values{"repeat_frequency": {"monthly"}, "repeat_month_day": {"31"}, "repeat_monthly_by": {"sideways"}}, summary: "Every month on the 31st, or the last day of shorter months"},
		{name: "monthly on a weekday", values: url.Values{"repeat_frequency": {"monthly"}, "repeat_monthly_by": {"weekday"}, "repeat_ordinal": {"-1"}, "repeat_ordinal_weekday": {"FR"}}, summary: "Every month on the last Friday", upcoming: "Fri, Oct 30, 2026|Fri, Nov 27, 2026|Fri, Dec 25, 2026"},
		{name: "yearly", values: url.Values{"repeat_frequency": {"yearly"}, "repeat_year_month": {"2"}, "repeat_year_day": {"29"}, "repeat_interval": {"2"}}, summary: "Every 2 years on Feb 29, or Feb 28 in other years", check: func(t *testing.T, f uiRepeatFields) {
			if f.IntervalUnit() != "years" {
				t.Fatalf("unit %q", f.IntervalUnit())
			}
		}},
		{name: "cron in a zone", values: url.Values{"repeat_frequency": {"cron"}, "repeat_cron": {"0 9 * * 1"}, "repeat_time_zone": {"Asia/Tokyo"}}, summary: "On the cron schedule 0 9 * * 1", upcoming: "Mon, Oct 5, 2026|Mon, Oct 12, 2026|Mon, Oct 19, 2026", check: func(t *testing.T, f uiRepeatFields) {
			if f.TimeZone != "Asia/Tokyo" {
				t.Fatalf("zone %q", f.TimeZone)
			}
		}},
		{name: "cron not written yet", values: url.Values{"repeat_frequency": {"cron"}}, check: func(t *testing.T, f uiRepeatFields) {
			if f.rule != nil || f.Error != "" {
				t.Fatalf("empty cron = %+v", f)
			}
		}},
		{name: "bad cron", values: url.Values{"repeat_frequency": {"cron"}, "repeat_cron": {"61 * * * *"}}, err: "Cron: "},
		{name: "unknown frequency", values: url.Values{"repeat_frequency": {"hourly"}}, err: "Choose how often it repeats."},
		{name: "bad interval", values: url.Values{"repeat_frequency": {"daily"}, "repeat_interval": {"0"}}, err: "INTERVAL must be 1 to 99."},
		{name: "unknown zone", values: url.Values{"repeat_frequency": {"daily"}, "repeat_time_zone": {"Mars/Base"}}, err: "Unknown time zone."},
		{name: "bad start date", values: url.Values{"repeat_frequency": {"daily"}, "repeat_starts_on": {"soon"}}, err: "Use YYYY-MM-DD for the start date."},
		{name: "unknown zone wins over bad start", values: url.Values{"repeat_frequency": {"daily"}, "repeat_time_zone": {"Mars/Base"}, "repeat_starts_on": {"soon"}}, err: "Unknown time zone."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := uiRepeatFieldsFromValues(uiIssueRepeatPrefix, tc.values, uiRepeatTestNow)
			if f.Summary != tc.summary {
				t.Fatalf("summary = %q, want %q", f.Summary, tc.summary)
			}
			if tc.upcoming != "" && strings.Join(f.Upcoming, "|") != tc.upcoming {
				t.Fatalf("upcoming = %q, want %q", strings.Join(f.Upcoming, "|"), tc.upcoming)
			}
			if !strings.HasPrefix(f.Error, tc.err) || (tc.err == "" && f.Error != "") {
				t.Fatalf("error = %q, want %q", f.Error, tc.err)
			}
			if tc.check != nil {
				tc.check(t, f)
			}
		})
	}
}

func TestUIRepeatFieldsForIssue(t *testing.T) {
	t.Parallel()
	due := uiRepeatTestDate(t, "2026-11-30")
	plain := uiRepeatFieldsForIssue(model.Issue{DueDate: &due}, uiRepeatTestNow)
	if plain.Frequency != "" || plain.StartsOn != "2026-11-30" || !plain.StartsOnGiven || plain.Prefix != uiIssueRepeatPrefix {
		t.Fatalf("plain issue fields = %+v", plain)
	}
	for _, tc := range []struct {
		rule string
		want string
	}{
		{"FREQ=DAILY;INTERVAL=2", "daily"},
		{"FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", "weekdays"},
		{"FREQ=WEEKLY;BYDAY=TU", "weekly"},
		{"FREQ=MONTHLY;BYMONTHDAY=15", "monthly"},
		{"FREQ=MONTHLY;BYDAY=2TH", "monthly"},
		{"FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=1", "yearly"},
		{"CRON:0 9 * * 1", "cron"},
	} {
		occurrence := uiRepeatTestDate(t, "2026-10-08")
		issue := model.Issue{Repeat: &model.IssueRepeat{Rule: tc.rule, TimeZone: "Europe/London", Current: true, Occurrence: occurrence}}
		f := uiRepeatFieldsForIssue(issue, uiRepeatTestNow)
		if f.Frequency != tc.want || f.TimeZone != "Europe/London" || f.StartsOn != "2026-10-08" || f.Error != "" {
			t.Fatalf("%s: fields = %+v", tc.rule, f)
		}
		text, _ := f.ruleText()
		parsed, err := model.ParseRepeatRule(text)
		if err != nil || parsed.Anchored(occurrence).String() != tc.rule {
			t.Fatalf("%s: round trip = %q (%v)", tc.rule, text, err)
		}
	}
	// A stopped repeat opens on "Doesn't repeat".
	stopped := uiRepeatFieldsForIssue(model.Issue{Repeat: &model.IssueRepeat{Rule: "FREQ=DAILY", Current: false}}, uiRepeatTestNow)
	if stopped.Frequency != "" {
		t.Fatalf("stopped repeat fields = %+v", stopped)
	}
}

func TestUIRepeatFieldsSetting(t *testing.T) {
	t.Parallel()
	user := uuid.New()
	if s, err := uiRepeatFieldsFromValues(uiIssueRepeatPrefix, url.Values{}, uiRepeatTestNow).setting(user); s != nil || err != nil {
		t.Fatalf("doesn't repeat = %+v, %v", s, err)
	}
	if _, err := uiRepeatFieldsFromValues(uiIssueRepeatPrefix, url.Values{"repeat_frequency": {"hourly"}}, uiRepeatTestNow).setting(user); err == nil || err.Error() != "Choose how often it repeats." {
		t.Fatalf("bad frequency err = %v", err)
	}
	if _, err := uiRepeatFieldsFromValues(uiIssueRepeatPrefix, url.Values{"repeat_frequency": {"cron"}}, uiRepeatTestNow).setting(user); !errors.Is(err, errUIRepeatNeedsCron) {
		t.Fatalf("empty cron err = %v", err)
	}
	s, err := uiRepeatFieldsFromValues(uiIssueRepeatPrefix, url.Values{"repeat_frequency": {"daily"}, "repeat_starts_on": {"2026-10-09"}, "repeat_time_zone": {"Europe/Paris"}}, uiRepeatTestNow).setting(user)
	if err != nil || s == nil || s.TimeZone != "Europe/Paris" || s.StartsOn == nil || s.StartsOn.String() != "2026-10-09" || s.CreatedBy == nil || *s.CreatedBy != user || s.Rule.String() != "FREQ=DAILY" {
		t.Fatalf("daily setting = %+v, %v", s, err)
	}
}

func TestUIRepeatFieldOptions(t *testing.T) {
	t.Parallel()
	f := uiRepeatFields{Frequency: "monthly", Interval: 1, Weekdays: []string{"TU"}, MonthDay: model.LastRepeatMonthDay, Ordinal: model.LastRepeatOrdinal, OrdinalWeekday: "SU", YearMonth: 12}
	selected := func(options []uiRepeatOption) []string {
		out := []string{}
		for _, o := range options {
			if o.Selected {
				out = append(out, o.Value+"="+o.Label)
			}
		}
		return out
	}
	for name, tc := range map[string]struct {
		options []uiRepeatOption
		count   int
		want    string
	}{
		"frequency":       {f.FrequencyOptions(), 7, "monthly=Monthly"},
		"weekdays":        {f.WeekdayOptions(), 7, "TU=Tue"},
		"ordinal weekday": {f.OrdinalWeekdayOptions(), 7, "SU=Sunday"},
		"ordinal":         {f.OrdinalOptions(), 5, "-1=Last"},
		"month day":       {f.MonthDayOptions(), 32, "-1=Last day"},
		"month":           {f.MonthOptions(), 12, "12=December"},
	} {
		if len(tc.options) != tc.count || strings.Join(selected(tc.options), ",") != tc.want {
			t.Fatalf("%s options = %+v", name, tc.options)
		}
	}
	if f.IntervalUnit() != "month" {
		t.Fatalf("interval unit = %q", f.IntervalUnit())
	}
	if (uiRepeatFields{Frequency: "cron", Interval: 4}).IntervalUnit() != "" {
		t.Fatal("cron has no interval unit")
	}
}

func TestUIRepeatSmallHelpers(t *testing.T) {
	t.Parallel()
	if uiRepeatOrdinalOf(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) != 1 || uiRepeatOrdinalOf(time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)) != 4 || uiRepeatOrdinalOf(time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)) != model.LastRepeatOrdinal {
		t.Fatal("uiRepeatOrdinalOf")
	}
	if uiRepeatWeekdayName("SU") != "Sunday" || uiRepeatWeekdayName("XX") != "XX" || uiRepeatWeekdayCode(time.Sunday) != "SU" {
		t.Fatal("weekday names")
	}
	if got := uiRepeatErrorText(errors.New("invalid repeat: ")); got != "Invalid repeat." {
		t.Fatalf("bare error text = %q", got)
	}
	if !uiRepeatPrefixValid(uiNewIssueRepeatPrefix) || uiRepeatPrefixValid("x") {
		t.Fatal("uiRepeatPrefixValid")
	}
	d := func(raw string) model.Date { return uiRepeatTestDate(t, raw) }
	for _, tc := range []struct {
		dates []model.Date
		count int
		want  string
	}{
		{nil, 0, ""},
		{[]model.Date{d("2026-12-01")}, 1, "Dec 1, 2026"},
		{[]model.Date{d("2026-12-01"), d("2027-01-01")}, 2, "Dec 1, 2026 and Jan 1, 2027"},
		{[]model.Date{d("2026-12-01"), d("2027-01-01"), d("2027-02-01"), d("2027-03-01")}, 7, "Dec 1, 2026, Jan 1, 2027, Feb 1, 2027 and 4 more"},
	} {
		if got := uiRepeatDateList(tc.dates, tc.count); got != tc.want {
			t.Fatalf("uiRepeatDateList = %q, want %q", got, tc.want)
		}
	}
	if uiIssueRepeatEditable(model.Issue{Status: model.StatusClosed}) || !uiIssueRepeatEditable(model.Issue{Status: model.StatusInProgress}) {
		t.Fatal("uiIssueRepeatEditable")
	}
	if got := uiNewIssueRepeatFields(&uiNewIssuePanelData{}); got.Prefix != uiNewIssueRepeatPrefix || got.Frequency != "" {
		t.Fatalf("default new-issue picker = %+v", got)
	}
	given := uiRepeatFields{Prefix: uiNewIssueRepeatPrefix, Frequency: "daily"}
	if got := uiNewIssueRepeatFields(&uiNewIssuePanelData{Repeat: given}); got.Frequency != "daily" {
		t.Fatalf("given new-issue picker = %+v", got)
	}
}

func TestUIRepeatNotice(t *testing.T) {
	t.Parallel()
	d := func(raw string) model.Date { return uiRepeatTestDate(t, raw) }
	base := model.Issue{OwnerUsername: "bradley", ProjectKey: "TRACK", CreatedAt: time.Date(2026, 12, 2, 23, 30, 0, 0, time.UTC)}
	with := func(status model.Status, r *model.IssueRepeat) model.Issue {
		issue := base
		issue.Status = status
		issue.Repeat = r
		return issue
	}
	if uiRepeatNotice(with(model.StatusTodo, nil)) != nil {
		t.Fatal("no repeat, no notice")
	}
	if uiRepeatNotice(with(model.StatusDone, &model.IssueRepeat{})) != nil {
		t.Fatal("done with no next, no notice")
	}
	if uiRepeatNotice(with(model.StatusDone, &model.IssueRepeat{Next: &model.IssueRepetition{Number: 9, Identifier: "TRACK-9"}})) != nil {
		t.Fatal("done on time, no notice")
	}
	if uiRepeatNotice(with(model.StatusTodo, &model.IssueRepeat{})) != nil {
		t.Fatal("open with nothing skipped, no notice")
	}

	skipped := uiRepeatNotice(with(model.StatusClosed, &model.IssueRepeat{Next: &model.IssueRepetition{
		Number: 9, Identifier: "TRACK-9", Occurrence: d("2027-01-01"), SkippedDates: []model.Date{d("2026-12-01")}, SkippedCount: 1,
	}}))
	if skipped == nil || skipped.Kind != "skipped" || skipped.Before != "Completed after Dec 1, 2026, so that repetition was skipped. Next:" ||
		skipped.Link == nil || skipped.Link.Identifier != "TRACK-9" || skipped.After != "due Jan 1, 2027." {
		t.Fatalf("skipped notice = %+v", skipped)
	}

	// The completion date is the new repetition's creation in the repeat's
	// zone: 23:30 UTC on 2 December is 3 December in Tokyo.
	arrived := uiRepeatNotice(with(model.StatusTodo, &model.IssueRepeat{
		TimeZone: "Asia/Tokyo", SkippedDates: []model.Date{d("2026-12-01")}, SkippedCount: 1,
	}))
	if arrived == nil || arrived.Kind != "arrived" || arrived.Link != nil ||
		arrived.Before != "Skipped 1 repetition (Dec 1, 2026) because the previous repetition" || arrived.After != "was completed on Dec 3, 2026." {
		t.Fatalf("arrived notice = %+v", arrived)
	}
	// An unknown zone falls back to the stored time.
	fallback := uiRepeatNotice(with(model.StatusTodo, &model.IssueRepeat{
		TimeZone: "Nowhere", SkippedDates: []model.Date{d("2026-12-01"), d("2027-01-01")}, SkippedCount: 2,
		Previous: &model.IssueRepetition{Number: 7, Identifier: "TRACK-7"},
	}))
	if fallback == nil || fallback.Link == nil || fallback.Link.Identifier != "TRACK-7" ||
		fallback.Before != "Skipped 2 repetitions (Dec 1, 2026 and Jan 1, 2027) because the previous repetition" || fallback.After != "was completed on Dec 2, 2026." {
		t.Fatalf("fallback notice = %+v", fallback)
	}
}
