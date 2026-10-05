package model

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func repeatDate(t *testing.T, raw string) Date {
	t.Helper()
	d, err := ParseDate(raw)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", raw, err)
	}
	return d
}

func repeatDateList(ds []Date) string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.String())
	}
	return strings.Join(out, " ")
}

func repeatLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := LoadRepeatLocation(name)
	if err != nil {
		t.Fatalf("LoadRepeatLocation(%q): %v", name, err)
	}
	return loc
}

func mustParseRepeatRule(t *testing.T, raw string) RepeatRule {
	t.Helper()
	rule, err := ParseRepeatRule(raw)
	if err != nil {
		t.Fatalf("ParseRepeatRule(%q): %v", raw, err)
	}
	return rule
}

// sameRepeatRule compares every exported field of two rules.
func sameRepeatRule(a, b RepeatRule) bool {
	return a.Frequency == b.Frequency && a.Interval == b.Interval &&
		slices.Equal(a.Weekdays, b.Weekdays) && a.MonthDay == b.MonthDay &&
		a.Ordinal == b.Ordinal && a.Weekday == b.Weekday && a.Month == b.Month &&
		a.Cron == b.Cron
}

func TestParseRepeatRuleValid(t *testing.T) {
	t.Parallel()
	weekdays := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	cases := []struct {
		in   string
		want RepeatRule
		text string
	}{
		// Shorthands, case-insensitive and trimmed.
		{"daily", RepeatRule{Frequency: RepeatDaily, Interval: 1}, "FREQ=DAILY"},
		{"  DAILY \n", RepeatRule{Frequency: RepeatDaily, Interval: 1}, "FREQ=DAILY"},
		{"Weekdays", RepeatRule{Frequency: RepeatWeekly, Interval: 1, Weekdays: weekdays}, "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"},
		{"WEEKLY", RepeatRule{Frequency: RepeatWeekly, Interval: 1}, "FREQ=WEEKLY"},
		{"monthly", RepeatRule{Frequency: RepeatMonthly, Interval: 1}, "FREQ=MONTHLY"},
		{"Yearly", RepeatRule{Frequency: RepeatYearly, Interval: 1}, "FREQ=YEARLY"},
		// RRULE text, with or without the prefix, in any case.
		{"FREQ=DAILY", RepeatRule{Frequency: RepeatDaily, Interval: 1}, "FREQ=DAILY"},
		{"RRULE:FREQ=DAILY;INTERVAL=3", RepeatRule{Frequency: RepeatDaily, Interval: 3}, "FREQ=DAILY;INTERVAL=3"},
		{"rrule:freq=daily;interval=1", RepeatRule{Frequency: RepeatDaily, Interval: 1}, "FREQ=DAILY"},
		{"FREQ=DAILY;INTERVAL=99", RepeatRule{Frequency: RepeatDaily, Interval: 99}, "FREQ=DAILY;INTERVAL=99"},
		{";FREQ=DAILY;;", RepeatRule{Frequency: RepeatDaily, Interval: 1}, "FREQ=DAILY"},
		{"FREQ=DAILY" + strings.Repeat(";", MaxRepeatRuleLength-len("FREQ=DAILY")), RepeatRule{Frequency: RepeatDaily, Interval: 1}, "FREQ=DAILY"},
		{" FREQ = weekly ; ; byday = mo , we ;", RepeatRule{Frequency: RepeatWeekly, Interval: 1, Weekdays: []time.Weekday{time.Monday, time.Wednesday}}, "FREQ=WEEKLY;BYDAY=MO,WE"},
		// Weekdays are deduplicated and sorted with Monday first.
		{"FREQ=WEEKLY;BYDAY=SU,FR,MO,FR", RepeatRule{Frequency: RepeatWeekly, Interval: 1, Weekdays: []time.Weekday{time.Monday, time.Friday, time.Sunday}}, "FREQ=WEEKLY;BYDAY=MO,FR,SU"},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=TH,MO", RepeatRule{Frequency: RepeatWeekly, Interval: 2, Weekdays: []time.Weekday{time.Monday, time.Thursday}}, "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,TH"},
		{"FREQ=WEEKLY;INTERVAL=2", RepeatRule{Frequency: RepeatWeekly, Interval: 2}, "FREQ=WEEKLY;INTERVAL=2"},
		{"FREQ=MONTHLY;BYMONTHDAY=1", RepeatRule{Frequency: RepeatMonthly, Interval: 1, MonthDay: 1}, "FREQ=MONTHLY;BYMONTHDAY=1"},
		{"FREQ=MONTHLY;BYMONTHDAY=31", RepeatRule{Frequency: RepeatMonthly, Interval: 1, MonthDay: 31}, "FREQ=MONTHLY;BYMONTHDAY=31"},
		{"FREQ=MONTHLY;BYMONTHDAY=-1", RepeatRule{Frequency: RepeatMonthly, Interval: 1, MonthDay: LastRepeatMonthDay}, "FREQ=MONTHLY;BYMONTHDAY=-1"},
		{"FREQ=MONTHLY;INTERVAL=2;BYMONTHDAY=15", RepeatRule{Frequency: RepeatMonthly, Interval: 2, MonthDay: 15}, "FREQ=MONTHLY;INTERVAL=2;BYMONTHDAY=15"},
		{"FREQ=MONTHLY;BYDAY=3WE", RepeatRule{Frequency: RepeatMonthly, Interval: 1, Ordinal: 3, Weekday: time.Wednesday}, "FREQ=MONTHLY;BYDAY=3WE"},
		{"FREQ=MONTHLY;BYDAY=+2MO", RepeatRule{Frequency: RepeatMonthly, Interval: 1, Ordinal: 2, Weekday: time.Monday}, "FREQ=MONTHLY;BYDAY=2MO"},
		{"FREQ=MONTHLY;BYDAY=-1FR", RepeatRule{Frequency: RepeatMonthly, Interval: 1, Ordinal: LastRepeatOrdinal, Weekday: time.Friday}, "FREQ=MONTHLY;BYDAY=-1FR"},
		{"FREQ=MONTHLY;BYDAY=1su", RepeatRule{Frequency: RepeatMonthly, Interval: 1, Ordinal: 1, Weekday: time.Sunday}, "FREQ=MONTHLY;BYDAY=1SU"},
		{"FREQ=MONTHLY;INTERVAL=3;BYDAY=4SA", RepeatRule{Frequency: RepeatMonthly, Interval: 3, Ordinal: 4, Weekday: time.Saturday}, "FREQ=MONTHLY;INTERVAL=3;BYDAY=4SA"},
		{"FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=1", RepeatRule{Frequency: RepeatYearly, Interval: 1, Month: time.March, MonthDay: 1}, "FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=1"},
		{"FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=29", RepeatRule{Frequency: RepeatYearly, Interval: 1, Month: time.February, MonthDay: 29}, "FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=29"},
		{"FREQ=YEARLY;BYMONTH=12;BYMONTHDAY=31", RepeatRule{Frequency: RepeatYearly, Interval: 1, Month: time.December, MonthDay: 31}, "FREQ=YEARLY;BYMONTH=12;BYMONTHDAY=31"},
		{"FREQ=YEARLY;INTERVAL=2;BYMONTHDAY=1;BYMONTH=1", RepeatRule{Frequency: RepeatYearly, Interval: 2, Month: time.January, MonthDay: 1}, "FREQ=YEARLY;INTERVAL=2;BYMONTH=1;BYMONTHDAY=1"},
		// Cron, with whitespace normalised.
		{"CRON:0 9 * * 1", RepeatRule{Frequency: RepeatCron, Cron: "0 9 * * 1"}, "CRON:0 9 * * 1"},
		{"cron:  0\t9 *  * 1  ", RepeatRule{Frequency: RepeatCron, Cron: "0 9 * * 1"}, "CRON:0 9 * * 1"},
		{"Cron:0 9 * * MON-FRI", RepeatRule{Frequency: RepeatCron, Cron: "0 9 * * MON-FRI"}, "CRON:0 9 * * MON-FRI"},
		{"CRON:0 0 29 2 *", RepeatRule{Frequency: RepeatCron, Cron: "0 0 29 2 *"}, "CRON:0 0 29 2 *"},
		{"CRON:@weekly", RepeatRule{Frequency: RepeatCron, Cron: "@weekly"}, "CRON:@weekly"},
		{"CRON:@daily", RepeatRule{Frequency: RepeatCron, Cron: "@daily"}, "CRON:@daily"},
		{"CRON:@monthly", RepeatRule{Frequency: RepeatCron, Cron: "@monthly"}, "CRON:@monthly"},
		{"CRON:@yearly", RepeatRule{Frequency: RepeatCron, Cron: "@yearly"}, "CRON:@yearly"},
		{"CRON:@annually", RepeatRule{Frequency: RepeatCron, Cron: "@annually"}, "CRON:@annually"},
		{"CRON:@midnight", RepeatRule{Frequency: RepeatCron, Cron: "@midnight"}, "CRON:@midnight"},
		{"CRON:@hourly", RepeatRule{Frequency: RepeatCron, Cron: "@hourly"}, "CRON:@hourly"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParseRepeatRule(c.in)
			if err != nil {
				t.Fatalf("ParseRepeatRule(%q): %v", c.in, err)
			}
			if !sameRepeatRule(got, c.want) {
				t.Fatalf("ParseRepeatRule(%q) = %+v, want %+v", c.in, got, c.want)
			}
			if (got.schedule != nil) != (got.Frequency == RepeatCron) {
				t.Fatalf("ParseRepeatRule(%q) schedule set = %v", c.in, got.schedule != nil)
			}
			if s := got.String(); s != c.text {
				t.Fatalf("String() = %q, want %q", s, c.text)
			}
			again, err := ParseRepeatRule(got.String())
			if err != nil {
				t.Fatalf("round trip ParseRepeatRule(%q): %v", got.String(), err)
			}
			if !sameRepeatRule(again, got) || again.String() != c.text {
				t.Fatalf("round trip = %+v (%q), want %+v", again, again.String(), got)
			}
		})
	}
}

func TestParseRepeatRuleInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"", "rule required"},
		{" \t\n ", "rule required"},
		{"FREQ=DAILY" + strings.Repeat(";", MaxRepeatRuleLength+1-len("FREQ=DAILY")), "rule max 200 chars"},
		{strings.Repeat("A", 500), "rule max 200 chars"},
		// Too short for either prefix.
		{"abc", "expected KEY=VALUE"},
		{"RRULE", "expected KEY=VALUE"},
		{"FREQ", "expected KEY=VALUE"},
		{"FREQ=", "expected KEY=VALUE"},
		{"FREQ=DAILY;INTERVAL", "expected KEY=VALUE"},
		{"FREQ=DAILY;INTERVAL= ", "expected KEY=VALUE"},
		{"FREQ=DAILY;freq=weekly", "FREQ given twice"},
		{"FREQ=WEEKLY;BYDAY=MO;byday=TU", "BYDAY given twice"},
		{"FREQ=DAILY;COUNT=3", "unsupported key COUNT"},
		{"FREQ=DAILY;until=20270101", "unsupported key UNTIL"},
		{"=DAILY", "unsupported key "},
		{"FREQ=DAILY;INTERVAL=0", "INTERVAL must be 1 to 99"},
		{"FREQ=DAILY;INTERVAL=100", "INTERVAL must be 1 to 99"},
		{"FREQ=DAILY;INTERVAL=-1", "INTERVAL must be 1 to 99"},
		{"FREQ=DAILY;INTERVAL=two", "INTERVAL must be 1 to 99"},
		{"FREQ=DAILY;INTERVAL=1.5", "INTERVAL must be 1 to 99"},
		{"FREQ=DAILY;BYDAY=MO", "FREQ=DAILY takes only INTERVAL"},
		{"FREQ=DAILY;BYMONTHDAY=1", "FREQ=DAILY takes only INTERVAL"},
		{"FREQ=DAILY;BYMONTH=1", "FREQ=DAILY takes only INTERVAL"},
		{"FREQ=WEEKLY;BYMONTHDAY=1", "FREQ=WEEKLY takes only INTERVAL and BYDAY"},
		{"FREQ=WEEKLY;BYMONTH=1;BYDAY=MO", "FREQ=WEEKLY takes only INTERVAL and BYDAY"},
		{"FREQ=WEEKLY;BYDAY=XX", "BYDAY for FREQ=WEEKLY"},
		{"FREQ=WEEKLY;BYDAY=MO,,TU", "BYDAY for FREQ=WEEKLY"},
		{"FREQ=WEEKLY;BYDAY=1MO", "BYDAY for FREQ=WEEKLY"},
		{"FREQ=WEEKLY;BYDAY=MONDAY", "BYDAY for FREQ=WEEKLY"},
		{"FREQ=MONTHLY;BYMONTH=1", "FREQ=MONTHLY doesn't take BYMONTH"},
		{"FREQ=MONTHLY;BYDAY=1MO;BYMONTHDAY=1", "FREQ=MONTHLY takes BYDAY or BYMONTHDAY, not both"},
		{"FREQ=MONTHLY;BYMONTHDAY=0", "BYMONTHDAY must be 1 to 31, or -1"},
		{"FREQ=MONTHLY;BYMONTHDAY=32", "BYMONTHDAY must be 1 to 31, or -1"},
		{"FREQ=MONTHLY;BYMONTHDAY=-2", "BYMONTHDAY must be 1 to 31, or -1"},
		{"FREQ=MONTHLY;BYMONTHDAY=first", "BYMONTHDAY must be 1 to 31, or -1"},
		{"FREQ=MONTHLY;BYMONTHDAY=1,15", "BYMONTHDAY must be 1 to 31, or -1"},
		{"FREQ=MONTHLY;BYDAY=0WE", "BYDAY for FREQ=MONTHLY"},
		{"FREQ=MONTHLY;BYDAY=5WE", "BYDAY for FREQ=MONTHLY"},
		{"FREQ=MONTHLY;BYDAY=-2FR", "BYDAY for FREQ=MONTHLY"},
		{"FREQ=MONTHLY;BYDAY=MO", "BYDAY for FREQ=MONTHLY"},
		{"FREQ=MONTHLY;BYDAY=+WE", "BYDAY for FREQ=MONTHLY"},
		{"FREQ=MONTHLY;BYDAY=XWE", "BYDAY for FREQ=MONTHLY"},
		{"FREQ=MONTHLY;BYDAY=3XX", "BYDAY for FREQ=MONTHLY"},
		{"FREQ=MONTHLY;BYDAY=1MO,3MO", "BYDAY for FREQ=MONTHLY"},
		{"FREQ=MONTHLY;BYDAY=MO,TU", "BYDAY for FREQ=MONTHLY"},
		{"FREQ=YEARLY;BYDAY=MO", "FREQ=YEARLY takes BYMONTH and BYMONTHDAY, not BYDAY"},
		{"FREQ=YEARLY;BYMONTH=3", "FREQ=YEARLY takes BYMONTH and BYMONTHDAY together"},
		{"FREQ=YEARLY;BYMONTHDAY=3", "FREQ=YEARLY takes BYMONTH and BYMONTHDAY together"},
		{"FREQ=YEARLY;BYMONTH=0;BYMONTHDAY=1", "BYMONTH must be 1 to 12"},
		{"FREQ=YEARLY;BYMONTH=13;BYMONTHDAY=1", "BYMONTH must be 1 to 12"},
		{"FREQ=YEARLY;BYMONTH=MAR;BYMONTHDAY=1", "BYMONTH must be 1 to 12"},
		{"FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=30", "BYMONTHDAY isn't a day of that month"},
		{"FREQ=YEARLY;BYMONTH=4;BYMONTHDAY=31", "BYMONTHDAY isn't a day of that month"},
		{"FREQ=YEARLY;BYMONTH=1;BYMONTHDAY=32", "BYMONTHDAY isn't a day of that month"},
		{"FREQ=YEARLY;BYMONTH=1;BYMONTHDAY=0", "BYMONTHDAY isn't a day of that month"},
		{"FREQ=YEARLY;BYMONTH=1;BYMONTHDAY=-1", "BYMONTHDAY isn't a day of that month"},
		{"FREQ=YEARLY;BYMONTH=1;BYMONTHDAY=x", "BYMONTHDAY isn't a day of that month"},
		{"INTERVAL=2", "FREQ required"},
		{"RRULE:", "FREQ required"},
		{";", "FREQ required"},
		{"FREQ=HOURLY", "FREQ must be DAILY, WEEKLY, MONTHLY or YEARLY"},
		{"FREQ=CRON", "FREQ must be DAILY, WEEKLY, MONTHLY or YEARLY"},
		{"CRON:", "cron expression required"},
		{"cron:   ", "cron expression required"},
		{"CRON:TZ=Europe/London 0 9 * * *", "set the time zone separately"},
		{"CRON:  tz=UTC 0 9 * * *", "set the time zone separately"},
		{"CRON:CRON_TZ=Europe/London 0 9 * * *", "set the time zone separately"},
		{"CRON:@every 1h", "@every isn't supported"},
		{"CRON:@EVERY 5m", "@every isn't supported"},
		{"CRON:not a cron", "cron: "},
		{"CRON:0 9 * *", "cron: "},
		{"CRON:0 0 9 * * 1", "cron: "},
		{"CRON:61 * * * *", "cron: "},
		{"CRON:@fortnightly", "cron: "},
		{"CRON:0 0 30 2 *", "cron expression never matches a date"},
		{"CRON:0 0 31 4 *", "cron expression never matches a date"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParseRepeatRule(c.in)
			if err == nil {
				t.Fatalf("ParseRepeatRule(%q) = %+v, want error", c.in, got)
			}
			if !errors.Is(err, ErrInvalidRepeatRule) {
				t.Fatalf("ParseRepeatRule(%q) error %v doesn't wrap ErrInvalidRepeatRule", c.in, err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ParseRepeatRule(%q) error %q, want it to contain %q", c.in, err, c.want)
			}
			if !sameRepeatRule(got, RepeatRule{}) || got.schedule != nil {
				t.Fatalf("ParseRepeatRule(%q) returned non-zero rule %+v with its error", c.in, got)
			}
		})
	}
}

func TestRepeatRuleAnchored(t *testing.T) {
	t.Parallel()
	cases := []struct {
		rule     string
		startsOn string
		want     string
	}{
		{"weekly", "2026-10-07", "FREQ=WEEKLY;BYDAY=WE"},
		{"FREQ=WEEKLY;INTERVAL=2", "2026-10-11", "FREQ=WEEKLY;INTERVAL=2;BYDAY=SU"},
		{"FREQ=WEEKLY;BYDAY=MO", "2026-10-07", "FREQ=WEEKLY;BYDAY=MO"},
		{"weekdays", "2026-10-10", "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"},
		{"monthly", "2026-10-31", "FREQ=MONTHLY;BYMONTHDAY=31"},
		{"FREQ=MONTHLY;INTERVAL=2", "2026-10-05", "FREQ=MONTHLY;INTERVAL=2;BYMONTHDAY=5"},
		{"FREQ=MONTHLY;BYMONTHDAY=15", "2026-10-05", "FREQ=MONTHLY;BYMONTHDAY=15"},
		{"FREQ=MONTHLY;BYMONTHDAY=-1", "2026-10-05", "FREQ=MONTHLY;BYMONTHDAY=-1"},
		{"FREQ=MONTHLY;BYDAY=3WE", "2026-10-05", "FREQ=MONTHLY;BYDAY=3WE"},
		{"yearly", "2028-02-29", "FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=29"},
		{"FREQ=YEARLY;INTERVAL=2", "2026-10-05", "FREQ=YEARLY;INTERVAL=2;BYMONTH=10;BYMONTHDAY=5"},
		{"FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=1", "2026-10-05", "FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=1"},
		{"daily", "2026-10-05", "FREQ=DAILY"},
		{"CRON:0 9 * * 1", "2026-10-07", "CRON:0 9 * * 1"},
	}
	for _, c := range cases {
		t.Run(c.rule+"@"+c.startsOn, func(t *testing.T) {
			rule := mustParseRepeatRule(t, c.rule)
			before := rule.String()
			if got := rule.Anchored(repeatDate(t, c.startsOn)).String(); got != c.want {
				t.Fatalf("Anchored(%s) = %q, want %q", c.startsOn, got, c.want)
			}
			if rule.String() != before {
				t.Fatalf("Anchored changed the original rule to %q", rule.String())
			}
		})
	}
}

func TestRepeatRuleStringUnparsed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		rule RepeatRule
		want string
	}{
		{RepeatRule{Frequency: RepeatCron, Cron: "0 9 * * 1"}, "CRON:0 9 * * 1"},
		{RepeatRule{Frequency: RepeatDaily}, "FREQ=DAILY"},
		{RepeatRule{Frequency: RepeatDaily, Interval: 2}, "FREQ=DAILY;INTERVAL=2"},
		{RepeatRule{Frequency: RepeatWeekly, Interval: 1, Weekdays: []time.Weekday{time.Saturday}}, "FREQ=WEEKLY;BYDAY=SA"},
		{RepeatRule{Frequency: RepeatMonthly, Interval: 1, Ordinal: LastRepeatOrdinal, Weekday: time.Friday}, "FREQ=MONTHLY;BYDAY=-1FR"},
		{RepeatRule{Frequency: RepeatMonthly, Interval: 3, MonthDay: 15}, "FREQ=MONTHLY;INTERVAL=3;BYMONTHDAY=15"},
		{RepeatRule{Frequency: RepeatYearly, Interval: 1, Month: time.March, MonthDay: 1}, "FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=1"},
	}
	for _, c := range cases {
		if got := c.rule.String(); got != c.want {
			t.Fatalf("%+v.String() = %q, want %q", c.rule, got, c.want)
		}
	}
}

func TestRepeatRuleSummary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		rule string
		want string
	}{
		{"daily", "Every day"},
		{"FREQ=DAILY;INTERVAL=3", "Every 3 days"},
		{"weekdays", "Every weekday"},
		{"FREQ=WEEKLY;BYDAY=FR,TH,WE,TU,MO", "Every weekday"},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,TU,WE,TH,FR", "Every 2 weeks on Monday, Tuesday, Wednesday, Thursday and Friday"},
		{"FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR,SA", "Every week on Monday, Tuesday, Wednesday, Thursday, Friday and Saturday"},
		{"FREQ=WEEKLY;BYDAY=SU", "Every week on Sunday"},
		{"FREQ=WEEKLY;BYDAY=WE,MO", "Every week on Monday and Wednesday"},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR", "Every 2 weeks on Monday, Wednesday and Friday"},
		{"FREQ=MONTHLY;BYDAY=1MO", "Every month on the first Monday"},
		{"FREQ=MONTHLY;BYDAY=2TU", "Every month on the second Tuesday"},
		{"FREQ=MONTHLY;BYDAY=3WE", "Every month on the third Wednesday"},
		{"FREQ=MONTHLY;BYDAY=4TH", "Every month on the fourth Thursday"},
		{"FREQ=MONTHLY;BYDAY=-1FR", "Every month on the last Friday"},
		{"FREQ=MONTHLY;INTERVAL=2;BYDAY=-1FR", "Every 2 months on the last Friday"},
		{"FREQ=MONTHLY;BYMONTHDAY=-1", "Every month on the last day"},
		{"FREQ=MONTHLY;INTERVAL=6;BYMONTHDAY=-1", "Every 6 months on the last day"},
		{"FREQ=MONTHLY;BYMONTHDAY=29", "Every month on the 29th, or the last day of shorter months"},
		{"FREQ=MONTHLY;BYMONTHDAY=30", "Every month on the 30th, or the last day of shorter months"},
		{"FREQ=MONTHLY;BYMONTHDAY=31", "Every month on the 31st, or the last day of shorter months"},
		{"FREQ=MONTHLY;BYMONTHDAY=28", "Every month on the 28th"},
		{"FREQ=MONTHLY;BYMONTHDAY=1", "Every month on the 1st"},
		{"FREQ=MONTHLY;INTERVAL=2;BYMONTHDAY=15", "Every 2 months on the 15th"},
		{"FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=1", "Every year on Mar 1"},
		{"FREQ=YEARLY;INTERVAL=2;BYMONTH=12;BYMONTHDAY=25", "Every 2 years on Dec 25"},
		{"FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=29", "Every year on Feb 29, or Feb 28 in other years"},
		{"FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=28", "Every year on Feb 28"},
		{"FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=29", "Every year on Mar 29"},
		{"CRON:0 9 * * 1", "On the cron schedule 0 9 * * 1"},
		{"CRON:@weekly", "On the cron schedule @weekly"},
	}
	for _, c := range cases {
		t.Run(c.rule, func(t *testing.T) {
			if got := mustParseRepeatRule(t, c.rule).Summary(); got != c.want {
				t.Fatalf("Summary() = %q, want %q", got, c.want)
			}
		})
	}
	for _, rule := range []RepeatRule{{}, {Frequency: "HOURLY", Interval: 1}} {
		if got := rule.Summary(); got != "" {
			t.Fatalf("%+v.Summary() = %q, want empty", rule, got)
		}
	}
}

func TestRepeatOrdinalWord(t *testing.T) {
	t.Parallel()
	want := map[int]string{1: "first", 2: "second", 3: "third", 4: "fourth", LastRepeatOrdinal: "last"}
	for ordinal, word := range want {
		if got := repeatOrdinalWord(ordinal); got != word {
			t.Fatalf("repeatOrdinalWord(%d) = %q, want %q", ordinal, got, word)
		}
	}
}

func TestOrdinalNumber(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int
		want string
	}{
		{0, "0th"}, {1, "1st"}, {2, "2nd"}, {3, "3rd"}, {4, "4th"}, {10, "10th"},
		{11, "11th"}, {12, "12th"}, {13, "13th"}, {14, "14th"},
		{21, "21st"}, {22, "22nd"}, {23, "23rd"}, {31, "31st"},
		{100, "100th"}, {101, "101st"}, {102, "102nd"}, {103, "103rd"},
		{111, "111th"}, {112, "112th"}, {113, "113th"}, {1001, "1001st"},
	}
	for _, c := range cases {
		if got := OrdinalNumber(c.n); got != c.want {
			t.Fatalf("OrdinalNumber(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestJoinEnglish(t *testing.T) {
	t.Parallel()
	cases := []struct {
		items []string
		want  string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"Monday"}, "Monday"},
		{[]string{"Monday", "Friday"}, "Monday and Friday"},
		{[]string{"Monday", "Wednesday", "Friday"}, "Monday, Wednesday and Friday"},
		{[]string{"a", "b", "c", "d"}, "a, b, c and d"},
	}
	for _, c := range cases {
		if got := joinEnglish(c.items); got != c.want {
			t.Fatalf("joinEnglish(%q) = %q, want %q", c.items, got, c.want)
		}
	}
}

func TestLoadRepeatLocation(t *testing.T) {
	t.Parallel()
	valid := []struct {
		name string
		want string
	}{
		{"", "UTC"},
		{"   ", "UTC"},
		{"UTC", "UTC"},
		{"Europe/London", "Europe/London"},
		{" America/New_York ", "America/New_York"},
	}
	for _, c := range valid {
		loc, err := LoadRepeatLocation(c.name)
		if err != nil {
			t.Fatalf("LoadRepeatLocation(%q): %v", c.name, err)
		}
		if loc.String() != c.want {
			t.Fatalf("LoadRepeatLocation(%q) = %q, want %q", c.name, loc, c.want)
		}
	}
	if loc, _ := LoadRepeatLocation(""); loc != time.UTC {
		t.Fatalf("LoadRepeatLocation(\"\") = %p, want time.UTC", loc)
	}
	for _, name := range []string{
		"Local",
		" Local ",
		"Europe/" + strings.Repeat("x", 58), // 65 chars
		strings.Repeat("x", 64),
		"Mars/Olympus_Mons",
		"europe/nowhere",
		"../../etc/passwd",
	} {
		loc, err := LoadRepeatLocation(name)
		if err == nil || loc != nil {
			t.Fatalf("LoadRepeatLocation(%q) = %v, %v; want an error", name, loc, err)
		}
		if !errors.Is(err, ErrInvalidRepeatRule) || !strings.Contains(err.Error(), "unknown time zone") {
			t.Fatalf("LoadRepeatLocation(%q) error = %v", name, err)
		}
	}
}

func TestTodayIn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		zone string
		now  time.Time
		want string
	}{
		{"Europe/London", time.Date(2027, 5, 1, 23, 30, 0, 0, time.UTC), "2027-05-02"},
		{"UTC", time.Date(2027, 5, 1, 23, 30, 0, 0, time.UTC), "2027-05-01"},
		{"America/New_York", time.Date(2027, 5, 1, 3, 0, 0, 0, time.UTC), "2027-04-30"},
		{"Asia/Tokyo", time.Date(2027, 12, 31, 15, 0, 0, 0, time.UTC), "2028-01-01"},
	}
	for _, c := range cases {
		if got := TodayIn(repeatLocation(t, c.zone), c.now).String(); got != c.want {
			t.Fatalf("TodayIn(%s, %s) = %s, want %s", c.zone, c.now, got, c.want)
		}
	}
}

func TestDateAddDaysAndBefore(t *testing.T) {
	t.Parallel()
	adds := []struct {
		from string
		n    int
		want string
	}{
		{"2026-10-05", 0, "2026-10-05"},
		{"2026-12-31", 1, "2027-01-01"},
		{"2028-02-28", 1, "2028-02-29"},
		{"2027-02-28", 1, "2027-03-01"},
		{"2027-03-01", -1, "2027-02-28"},
		{"2027-03-28", 1, "2027-03-29"}, // a London DST change doesn't matter to dates
		{"2026-10-05", 365, "2027-10-05"},
		{"2026-10-05", -365, "2025-10-05"},
	}
	for _, c := range adds {
		if got := repeatDate(t, c.from).AddDays(c.n).String(); got != c.want {
			t.Fatalf("%s.AddDays(%d) = %s, want %s", c.from, c.n, got, c.want)
		}
	}
	befores := []struct {
		a, b string
		want bool
	}{
		{"2026-10-04", "2026-10-05", true},
		{"2026-10-05", "2026-10-05", false},
		{"2026-10-06", "2026-10-05", false},
		{"2025-12-31", "2026-01-01", true},
	}
	for _, c := range befores {
		if got := repeatDate(t, c.a).Before(repeatDate(t, c.b)); got != c.want {
			t.Fatalf("%s.Before(%s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestFloorDiv(t *testing.T) {
	t.Parallel()
	cases := []struct{ a, b, want int }{
		{0, 7, 0},
		{6, 7, 0},
		{7, 7, 1},
		{15, 7, 2},
		{-1, 7, -1},
		{-6, 7, -1},
		{-7, 7, -1},
		{-8, 7, -2},
		{-14, 7, -2},
		{1, -7, -1},
		{-1, -7, 0},
		{-7, -7, 1},
	}
	for _, c := range cases {
		if got := floorDiv(c.a, c.b); got != c.want {
			t.Fatalf("floorDiv(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestWeekIndexStartsOnMonday(t *testing.T) {
	t.Parallel()
	cases := []struct {
		date string
		want int
	}{
		{"2001-01-01", 0},
		{"2001-01-07", 0},
		{"2001-01-08", 1},
		{"2000-12-31", -1},
		{"2000-12-25", -1},
		{"2000-12-24", -2},
	}
	for _, c := range cases {
		if got := weekIndex(repeatDate(t, c.date)); got != c.want {
			t.Fatalf("weekIndex(%s) = %d, want %d", c.date, got, c.want)
		}
	}
}

func TestNewRepeatSchedule(t *testing.T) {
	t.Parallel()
	startsOn := repeatDate(t, "2026-10-07")
	s := NewRepeatSchedule(mustParseRepeatRule(t, "weekly"), startsOn, nil)
	if s.Location != time.UTC {
		t.Fatalf("nil location became %v, want UTC", s.Location)
	}
	if s.StartsOn != startsOn {
		t.Fatalf("StartsOn = %s, want %s", s.StartsOn, startsOn)
	}
	if got := s.Rule.String(); got != "FREQ=WEEKLY;BYDAY=WE" {
		t.Fatalf("rule wasn't anchored: %q", got)
	}
	london := repeatLocation(t, "Europe/London")
	if s := NewRepeatSchedule(mustParseRepeatRule(t, "daily"), startsOn, london); s.Location != london {
		t.Fatalf("Location = %v, want Europe/London", s.Location)
	}
}

func TestRepeatScheduleUpcoming(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		rule     string
		startsOn string
		zone     string
		from     string
		n        int
		want     string
	}{
		{"daily", "daily", "2026-10-01", "", "2026-10-05", 3, "2026-10-05 2026-10-06 2026-10-07"},
		{"daily across a year", "daily", "2026-10-01", "", "2026-12-30", 3, "2026-12-30 2026-12-31 2027-01-01"},
		{"every 3 days from start", "FREQ=DAILY;INTERVAL=3", "2026-10-01", "", "2026-10-01", 4, "2026-10-01 2026-10-04 2026-10-07 2026-10-10"},
		{"every 3 days between", "FREQ=DAILY;INTERVAL=3", "2026-10-01", "", "2026-10-05", 3, "2026-10-07 2026-10-10 2026-10-13"},
		{"every 3 days on one", "FREQ=DAILY;INTERVAL=3", "2026-10-01", "", "2026-10-04", 2, "2026-10-04 2026-10-07"},
		{"daily before start", "FREQ=DAILY;INTERVAL=3", "2026-10-01", "", "2026-09-01", 2, "2026-10-01 2026-10-04"},
		{"none asked for", "daily", "2026-10-01", "", "2026-10-01", 0, ""},

		{"weekly anchored before start", "weekly", "2026-10-07", "", "2026-10-01", 3, "2026-10-07 2026-10-14 2026-10-21"},
		{"weekdays", "weekdays", "2026-10-09", "", "2026-10-09", 4, "2026-10-09 2026-10-12 2026-10-13 2026-10-14"},
		{"weekly several days", "FREQ=WEEKLY;BYDAY=MO,WE,FR", "2026-10-07", "", "2026-10-07", 5, "2026-10-07 2026-10-09 2026-10-12 2026-10-14 2026-10-16"},
		{"every 2 weeks several days", "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR", "2026-10-07", "", "2026-10-07", 6, "2026-10-07 2026-10-09 2026-10-19 2026-10-21 2026-10-23 2026-11-02"},
		// The start week runs Monday 5 October to Sunday 11 October.
		{"every 2 weeks from a Sunday", "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,SU", "2026-10-11", "", "2026-10-11", 5, "2026-10-11 2026-10-19 2026-10-25 2026-11-02 2026-11-08"},
		{"every 2 weeks from an off week", "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,SU", "2026-10-11", "", "2026-10-13", 2, "2026-10-19 2026-10-25"},
		{"every 3 weeks", "FREQ=WEEKLY;INTERVAL=3;BYDAY=TU", "2026-10-06", "", "2026-10-07", 2, "2026-10-27 2026-11-17"},
		{"every 2 weeks last day of week", "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO", "2026-10-05", "", "2026-10-11", 2, "2026-10-19 2026-11-02"},
		{"every 2 weeks before 2001", "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO", "2000-12-18", "", "2000-12-18", 3, "2000-12-18 2001-01-01 2001-01-15"},
		{"every 2 weeks from a Sunday before 2001", "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,SU", "2000-12-31", "", "2000-12-31", 3, "2000-12-31 2001-01-08 2001-01-14"},

		{"monthly 31st", "monthly", "2027-01-31", "", "2027-01-31", 4, "2027-01-31 2027-02-28 2027-03-31 2027-04-30"},
		{"monthly 31st in a leap year", "monthly", "2028-01-31", "", "2028-01-31", 3, "2028-01-31 2028-02-29 2028-03-31"},
		{"monthly 30th", "FREQ=MONTHLY;BYMONTHDAY=30", "2027-01-01", "", "2027-01-01", 3, "2027-01-30 2027-02-28 2027-03-30"},
		{"monthly last day", "FREQ=MONTHLY;BYMONTHDAY=-1", "2027-01-01", "", "2027-01-01", 4, "2027-01-31 2027-02-28 2027-03-31 2027-04-30"},
		{"monthly last day leap", "FREQ=MONTHLY;BYMONTHDAY=-1", "2028-02-01", "", "2028-02-01", 1, "2028-02-29"},
		{"monthly 3rd Wednesday", "FREQ=MONTHLY;BYDAY=3WE", "2027-01-01", "", "2027-01-01", 3, "2027-01-20 2027-02-17 2027-03-17"},
		{"monthly 1st Monday after start", "FREQ=MONTHLY;BYDAY=1MO", "2027-02-02", "", "2027-02-02", 2, "2027-03-01 2027-04-05"},
		{"monthly 4th Sunday", "FREQ=MONTHLY;BYDAY=4SU", "2027-01-01", "", "2027-01-01", 3, "2027-01-24 2027-02-28 2027-03-28"},
		{"monthly last Friday", "FREQ=MONTHLY;BYDAY=-1FR", "2027-01-01", "", "2027-01-01", 4, "2027-01-29 2027-02-26 2027-03-26 2027-04-30"},
		{"monthly last Sunday on the last day", "FREQ=MONTHLY;BYDAY=-1SU", "2027-01-01", "", "2027-01-01", 2, "2027-01-31 2027-02-28"},
		{"every 2 months", "FREQ=MONTHLY;INTERVAL=2;BYMONTHDAY=15", "2026-01-15", "", "2026-02-01", 3, "2026-03-15 2026-05-15 2026-07-15"},
		{"every 2 months just after one", "FREQ=MONTHLY;INTERVAL=2;BYMONTHDAY=15", "2026-01-15", "", "2026-01-16", 1, "2026-03-15"},
		{"every 2 months across a year", "FREQ=MONTHLY;INTERVAL=2;BYMONTHDAY=15", "2026-11-20", "", "2026-11-20", 2, "2027-01-15 2027-03-15"},
		{"every 2 months last Friday", "FREQ=MONTHLY;INTERVAL=2;BYDAY=-1FR", "2027-01-01", "", "2027-01-01", 2, "2027-01-29 2027-03-26"},
		{"every 12 months", "FREQ=MONTHLY;INTERVAL=12;BYMONTHDAY=1", "2026-01-02", "", "2026-01-02", 2, "2027-01-01 2028-01-01"},

		{"yearly 29 February", "yearly", "2028-02-29", "", "2028-02-29", 5, "2028-02-29 2029-02-28 2030-02-28 2031-02-28 2032-02-29"},
		{"every 2 years", "FREQ=YEARLY;INTERVAL=2;BYMONTH=3;BYMONTHDAY=1", "2026-03-01", "", "2026-03-02", 2, "2028-03-01 2030-03-01"},
		{"yearly date already passed in start year", "FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=1", "2026-06-01", "", "2026-06-01", 2, "2027-03-01 2028-03-01"},
		{"every 4 years from a non-leap year", "FREQ=YEARLY;INTERVAL=4;BYMONTH=2;BYMONTHDAY=29", "2027-01-01", "", "2027-01-01", 3, "2027-02-28 2031-02-28 2035-02-28"},

		{"cron Mondays", "CRON:0 9 * * 1", "2026-10-01", "Europe/London", "2026-10-01", 3, "2026-10-05 2026-10-12 2026-10-19"},
		{"cron on its date", "CRON:0 9 * * 1", "2026-10-01", "Europe/London", "2026-10-05", 1, "2026-10-05"},
		{"cron before start", "CRON:0 9 * * *", "2026-10-05", "Europe/London", "2026-10-01", 2, "2026-10-05 2026-10-06"},
		// 00:30 on a Monday in Tokyo is Sunday in UTC.
		{"cron dates in zone east", "CRON:30 0 * * 1", "2026-10-01", "Asia/Tokyo", "2026-10-01", 2, "2026-10-05 2026-10-12"},
		// 23:30 on a Monday in New York is Tuesday in UTC.
		{"cron dates in zone west", "CRON:30 23 * * 1", "2026-10-01", "America/New_York", "2026-10-01", 2, "2026-10-05 2026-10-12"},
		{"cron several times a day", "CRON:0 */6 * * *", "2026-10-01", "Europe/London", "2026-10-05", 3, "2026-10-05 2026-10-06 2026-10-07"},
		{"cron every hour", "CRON:@hourly", "2026-10-01", "America/New_York", "2026-10-05", 3, "2026-10-05 2026-10-06 2026-10-07"},
		{"cron midnight", "CRON:@midnight", "2026-10-01", "Europe/London", "2026-10-05", 2, "2026-10-05 2026-10-06"},
		{"cron monthly", "CRON:@monthly", "2026-10-02", "", "2026-10-02", 2, "2026-11-01 2026-12-01"},
		{"cron spring DST", "CRON:0 9 * * *", "2027-03-01", "Europe/London", "2027-03-27", 4, "2027-03-27 2027-03-28 2027-03-29 2027-03-30"},
		{"cron spring DST midnight", "CRON:0 0 * * *", "2027-03-01", "Europe/London", "2027-03-27", 3, "2027-03-27 2027-03-28 2027-03-29"},
		{"cron autumn DST", "CRON:0 9 * * *", "2027-10-01", "Europe/London", "2027-10-30", 4, "2027-10-30 2027-10-31 2027-11-01 2027-11-02"},
		// 01:30 happens twice on 31 October in London; it's one date.
		{"cron autumn DST repeated hour", "CRON:30 1 * * *", "2027-10-01", "Europe/London", "2027-10-30", 3, "2027-10-30 2027-10-31 2027-11-01"},
		{"cron autumn DST every 30 minutes", "CRON:*/30 * * * *", "2027-10-01", "Europe/London", "2027-10-30", 3, "2027-10-30 2027-10-31 2027-11-01"},
		// robfig/cron looks at most five years ahead, and 2100 isn't a leap
		// year, so the 29 February after 2096 isn't found.
		{"cron beyond the search horizon", "CRON:0 0 29 2 *", "2096-01-01", "", "2096-01-01", 3, "2096-02-29"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewRepeatSchedule(mustParseRepeatRule(t, c.rule), repeatDate(t, c.startsOn), repeatLocation(t, c.zone))
			got := s.Upcoming(repeatDate(t, c.from), c.n)
			if list := repeatDateList(got); list != c.want {
				t.Fatalf("Upcoming(%s, %d) = %q, want %q", c.from, c.n, list, c.want)
			}
			if got == nil {
				t.Fatal("Upcoming returned nil, want an empty slice")
			}
		})
	}
}

func TestRepeatScheduleFirstAndNext(t *testing.T) {
	t.Parallel()
	cases := []struct {
		rule     string
		startsOn string
		zone     string
		d        string
		first    string
		next     string
	}{
		{"daily", "2026-10-01", "", "2026-12-31", "2026-12-31", "2027-01-01"},
		{"FREQ=DAILY;INTERVAL=2", "2026-10-01", "", "2026-10-02", "2026-10-03", "2026-10-03"},
		{"FREQ=WEEKLY;BYDAY=MO", "2026-10-05", "", "2026-10-05", "2026-10-05", "2026-10-12"},
		{"FREQ=MONTHLY;BYDAY=3WE", "2027-01-01", "", "2027-01-20", "2027-01-20", "2027-02-17"},
		{"FREQ=MONTHLY;BYDAY=3WE", "2027-01-01", "", "2027-01-19", "2027-01-20", "2027-01-20"},
		{"FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=1", "2026-01-01", "", "2026-03-01", "2026-03-01", "2027-03-01"},
		{"CRON:0 9 * * 1", "2026-10-01", "Europe/London", "2026-10-05", "2026-10-05", "2026-10-12"},
		{"CRON:0 9 * * 1", "2026-10-01", "Europe/London", "2026-09-01", "2026-10-05", "2026-10-05"},
	}
	for _, c := range cases {
		t.Run(c.rule+"@"+c.d, func(t *testing.T) {
			s := NewRepeatSchedule(mustParseRepeatRule(t, c.rule), repeatDate(t, c.startsOn), repeatLocation(t, c.zone))
			d := repeatDate(t, c.d)
			if got, ok := s.FirstOnOrAfter(d); !ok || got.String() != c.first {
				t.Fatalf("FirstOnOrAfter(%s) = %s, %v; want %s", c.d, got, ok, c.first)
			}
			if got, ok := s.NextAfter(d); !ok || got.String() != c.next {
				t.Fatalf("NextAfter(%s) = %s, %v; want %s", c.d, got, ok, c.next)
			}
		})
	}
}

func TestRepeatScheduleUnparsedCron(t *testing.T) {
	t.Parallel()
	london := repeatLocation(t, "Europe/London")
	from := repeatDate(t, "2026-10-01")
	s := NewRepeatSchedule(RepeatRule{Frequency: RepeatCron, Cron: "0 9 * * 1"}, from, london)
	if s.Rule.schedule != nil {
		t.Fatal("test rule should have no parsed schedule")
	}
	if got := repeatDateList(s.Upcoming(from, 3)); got != "2026-10-05 2026-10-12 2026-10-19" {
		t.Fatalf("Upcoming = %q", got)
	}
}

func TestRepeatScheduleNoDates(t *testing.T) {
	t.Parallel()
	startsOn := repeatDate(t, "2026-10-05")
	cases := []struct {
		name string
		rule RepeatRule
	}{
		// Built directly, so never anchored.
		{"weekly without weekdays", RepeatRule{Frequency: RepeatWeekly, Interval: 1}},
		{"weekly on no real weekday", RepeatRule{Frequency: RepeatWeekly, Interval: 1, Weekdays: []time.Weekday{7}}},
		{"unknown frequency", RepeatRule{Frequency: "HOURLY", Interval: 1}},
		{"zero rule", RepeatRule{}},
		{"invalid cron", RepeatRule{Frequency: RepeatCron, Cron: "not a cron"}},
		{"never-matching cron", RepeatRule{Frequency: RepeatCron, Cron: "0 0 30 2 *"}},
		{"empty cron", RepeatRule{Frequency: RepeatCron}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := RepeatSchedule{Rule: c.rule, StartsOn: startsOn, Location: time.UTC}
			if d, ok := s.FirstOnOrAfter(startsOn); ok || d != (Date{}) {
				t.Fatalf("FirstOnOrAfter = %s, %v; want zero, false", d, ok)
			}
			if d, ok := s.NextAfter(startsOn); ok || d != (Date{}) {
				t.Fatalf("NextAfter = %s, %v; want zero, false", d, ok)
			}
			if got := s.Upcoming(startsOn, 5); got == nil || len(got) != 0 {
				t.Fatalf("Upcoming = %v, want empty", got)
			}
			plan := s.PlanAfterCompletion(startsOn, startsOn.AddDays(30))
			if plan.OK || plan.Next != (Date{}) || plan.SkippedCount != 0 || len(plan.Skipped) != 0 {
				t.Fatalf("PlanAfterCompletion = %+v, want not OK", plan)
			}
		})
	}
}

func TestPlanAfterCompletion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		rule        string
		startsOn    string
		current     string
		completedOn string
		next        string
		skipped     string
		count       int
	}{
		{"early", "daily", "2026-10-01", "2026-10-05", "2026-10-04", "2026-10-06", "", 0},
		{"on the current date", "daily", "2026-10-01", "2026-10-05", "2026-10-05", "2026-10-06", "", 0},
		{"on the next date", "daily", "2026-10-01", "2026-10-05", "2026-10-06", "2026-10-06", "", 0},
		{"one skip", "daily", "2026-10-01", "2026-10-05", "2026-10-07", "2026-10-07", "2026-10-06", 1},
		{"weekly late before next", "FREQ=WEEKLY;BYDAY=MO", "2026-10-05", "2026-10-05", "2026-10-08", "2026-10-12", "", 0},
		{"weekly on next", "FREQ=WEEKLY;BYDAY=MO", "2026-10-05", "2026-10-05", "2026-10-12", "2026-10-12", "", 0},
		{"weekly one skip", "FREQ=WEEKLY;BYDAY=MO", "2026-10-05", "2026-10-05", "2026-10-13", "2026-10-19", "2026-10-12", 1},
		{
			"many skips are capped", "daily", "2026-01-01", "2026-01-01", "2026-01-31", "2026-01-31",
			"2026-01-02 2026-01-03 2026-01-04 2026-01-05 2026-01-06 2026-01-07 2026-01-08 2026-01-09 2026-01-10 2026-01-11", 29,
		},
		{
			"exactly the cap", "daily", "2026-01-01", "2026-01-01", "2026-01-12", "2026-01-12",
			"2026-01-02 2026-01-03 2026-01-04 2026-01-05 2026-01-06 2026-01-07 2026-01-08 2026-01-09 2026-01-10 2026-01-11", 10,
		},
		// The product spec's monthly-on-the-1st table.
		{"spec: before the current date", "FREQ=MONTHLY;BYMONTHDAY=1", "2026-11-01", "2026-11-01", "2026-10-28", "2026-12-01", "", 0},
		{"spec: between dates", "FREQ=MONTHLY;BYMONTHDAY=1", "2026-11-01", "2026-11-01", "2026-11-20", "2026-12-01", "", 0},
		{"spec: on the next date", "FREQ=MONTHLY;BYMONTHDAY=1", "2026-11-01", "2026-11-01", "2026-12-01", "2026-12-01", "", 0},
		{"spec: just past the next date", "FREQ=MONTHLY;BYMONTHDAY=1", "2026-11-01", "2026-11-01", "2026-12-03", "2027-01-01", "2026-12-01", 1},
		{"spec: months late", "FREQ=MONTHLY;BYMONTHDAY=1", "2026-11-01", "2026-11-01", "2027-02-15", "2027-03-01", "2026-12-01 2027-01-01 2027-02-01", 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewRepeatSchedule(mustParseRepeatRule(t, c.rule), repeatDate(t, c.startsOn), time.UTC)
			plan := s.PlanAfterCompletion(repeatDate(t, c.current), repeatDate(t, c.completedOn))
			if !plan.OK || plan.Next.String() != c.next {
				t.Fatalf("Next = %s, OK = %v; want %s", plan.Next, plan.OK, c.next)
			}
			if got := repeatDateList(plan.Skipped); got != c.skipped {
				t.Fatalf("Skipped = %q, want %q", got, c.skipped)
			}
			if plan.SkippedCount != c.count {
				t.Fatalf("SkippedCount = %d, want %d", plan.SkippedCount, c.count)
			}
		})
	}
}

func TestPlanAfterCompletionStepLimit(t *testing.T) {
	t.Parallel()
	// A daily repeat left for 280 years (102,267 passed dates) has more passed
	// dates than the walk takes, so the plan jumps to the first date that
	// hasn't passed.
	startsOn := repeatDate(t, "1746-01-01")
	completedOn := repeatDate(t, "2026-01-01")
	s := NewRepeatSchedule(mustParseRepeatRule(t, "daily"), startsOn, time.UTC)
	plan := s.PlanAfterCompletion(startsOn, completedOn)
	want, ok := s.FirstOnOrAfter(completedOn)
	if !ok || want != completedOn {
		t.Fatalf("FirstOnOrAfter(%s) = %s, %v", completedOn, want, ok)
	}
	if !plan.OK || plan.Next != want {
		t.Fatalf("Next = %s, OK = %v; want %s", plan.Next, plan.OK, want)
	}
	if plan.SkippedCount != maxRepeatPlanSteps {
		t.Fatalf("SkippedCount = %d, want %d", plan.SkippedCount, maxRepeatPlanSteps)
	}
	if len(plan.Skipped) != MaxRepeatSkippedDates || plan.Skipped[0].String() != "1746-01-02" || plan.Skipped[MaxRepeatSkippedDates-1].String() != "1746-01-11" {
		t.Fatalf("Skipped = %q", repeatDateList(plan.Skipped))
	}
}

func TestRepeatScheduleCenturiesApart(t *testing.T) {
	t.Parallel()
	// Day counts span more than a time.Duration can hold (about 292 years).
	daily := mustParseRepeatRule(t, "daily")
	if got, ok := NewRepeatSchedule(daily, repeatDate(t, "1726-01-01"), nil).FirstOnOrAfter(repeatDate(t, "2026-01-01")); !ok || got.String() != "2026-01-01" {
		t.Fatalf("daily from 1726 = %s, %v", got, ok)
	}
	if got, ok := NewRepeatSchedule(daily, repeatDate(t, "2026-01-01"), nil).FirstOnOrAfter(repeatDate(t, "2400-01-01")); !ok || got.String() != "2400-01-01" {
		t.Fatalf("daily to 2400 = %s, %v", got, ok)
	}
	fortnightly := NewRepeatSchedule(mustParseRepeatRule(t, "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO"), repeatDate(t, "2300-01-01"), nil)
	if got := repeatDateList(fortnightly.Upcoming(repeatDate(t, "2300-01-01"), 4)); got != "2300-01-01 2300-01-15 2300-01-29 2300-02-12" {
		t.Fatalf("every 2 weeks from 2300 = %q", got)
	}
}

func TestRepeatScheduleCronMidnightClockChange(t *testing.T) {
	t.Parallel()
	// Chile's clocks go forward at midnight on 6 September 2026, so that day
	// has no midnight; each evening still counts once.
	s := NewRepeatSchedule(mustParseRepeatRule(t, "CRON:0 23 * * *"), repeatDate(t, "2026-09-05"), repeatLocation(t, "America/Santiago"))
	if got := repeatDateList(s.Upcoming(repeatDate(t, "2026-09-05"), 5)); got != "2026-09-05 2026-09-06 2026-09-07 2026-09-08 2026-09-09" {
		t.Fatalf("Upcoming = %q", got)
	}
	if got, ok := s.FirstOnOrAfter(repeatDate(t, "2026-09-06")); !ok || got.String() != "2026-09-06" {
		t.Fatalf("FirstOnOrAfter(2026-09-06) = %s, %v", got, ok)
	}
}

func TestParseRepeatRuleOrdinalSigns(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"FREQ=MONTHLY;BYDAY=++1WE", "FREQ=MONTHLY;BYDAY=+-1FR"} {
		if _, err := ParseRepeatRule(raw); !errors.Is(err, ErrInvalidRepeatRule) {
			t.Fatalf("ParseRepeatRule(%q) err = %v", raw, err)
		}
	}
}

func TestLoadRepeatLocationCanonicalNames(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"europe/london", "posix/Europe/London", "right/UTC", "Europe//London", "/UTC"} {
		if _, err := LoadRepeatLocation(name); !errors.Is(err, ErrInvalidRepeatRule) {
			t.Fatalf("LoadRepeatLocation(%q) err = %v", name, err)
		}
	}
	for _, name := range []string{"UTC", "America/Argentina/Buenos_Aires", "Etc/GMT+5", "EST5EDT"} {
		if _, err := LoadRepeatLocation(name); err != nil {
			t.Fatalf("LoadRepeatLocation(%q) err = %v", name, err)
		}
	}
}

func TestRepeatScheduleCronMidnightIgnoresClockChanges(t *testing.T) {
	t.Parallel()
	// Havana skips midnight on 8 March 2026 when its clocks go forward;
	// only dates matter, so a midnight cron still falls on that day.
	s := NewRepeatSchedule(mustParseRepeatRule(t, "CRON:0 0 * * *"), repeatDate(t, "2026-03-07"), repeatLocation(t, "America/Havana"))
	if got := repeatDateList(s.Upcoming(repeatDate(t, "2026-03-07"), 3)); got != "2026-03-07 2026-03-08 2026-03-09" {
		t.Fatalf("Upcoming = %q", got)
	}
}
