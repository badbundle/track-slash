package model

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	// Repeat time zones must resolve even where the host has no zoneinfo.
	_ "time/tzdata"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

// RepeatFrequency is how often a repeating issue comes round.
type RepeatFrequency string

const (
	RepeatDaily   RepeatFrequency = "DAILY"
	RepeatWeekly  RepeatFrequency = "WEEKLY"
	RepeatMonthly RepeatFrequency = "MONTHLY"
	RepeatYearly  RepeatFrequency = "YEARLY"
	RepeatCron    RepeatFrequency = "CRON"
)

const (
	// MaxRepeatInterval caps "every N days/weeks/months/years".
	MaxRepeatInterval = 99
	// MaxRepeatRuleLength caps the rule text people and agents send.
	MaxRepeatRuleLength = 200
	// MaxRepeatSkippedDates is how many skipped dates a repetition keeps for
	// its notice. The count goes on to maxRepeatPlanSteps.
	MaxRepeatSkippedDates = 10
	// LastRepeatOrdinal and LastRepeatMonthDay mean "the last" weekday or day
	// of the month.
	LastRepeatOrdinal  = -1
	LastRepeatMonthDay = -1
)

// ErrInvalidRepeatRule wraps every rule and time-zone validation error.
var ErrInvalidRepeatRule = errors.New("invalid repeat")

func repeatRuleError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRepeatRule, fmt.Sprintf(format, args...))
}

// RepeatRule is the schedule a repeating issue follows. It is stored as a
// subset of RFC 5545 RRULE text, or as a cron expression after "CRON:".
// Only dates matter: due dates are day-only, so a schedule that fires several
// times in one day counts once that day, and a cron expression's minute and
// hour fields don't change which dates it falls on.
type RepeatRule struct {
	Frequency RepeatFrequency
	// Interval is "every N" for daily, weekly, monthly and yearly rules.
	Interval int
	// Weekdays are a weekly rule's days.
	Weekdays []time.Weekday
	// MonthDay is a monthly or yearly rule's day of the month. Days past the
	// end of a shorter month fall on its last day. A monthly rule may use
	// LastRepeatMonthDay.
	MonthDay int
	// Ordinal and Weekday make a monthly rule fall on, for example, the 3rd
	// Wednesday (3, Wednesday) or the last Friday (LastRepeatOrdinal, Friday).
	Ordinal int
	Weekday time.Weekday
	// Month is a yearly rule's month.
	Month time.Month
	// Cron is a cron rule's expression.
	Cron     string
	schedule cron.Schedule
}

var repeatWeekdayCodes = map[string]time.Weekday{
	"MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday, "TH": time.Thursday,
	"FR": time.Friday, "SA": time.Saturday, "SU": time.Sunday,
}

func repeatWeekdayCode(d time.Weekday) string {
	return strings.ToUpper(d.String()[:2])
}

var repeatCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ParseRepeatRule reads rule text. It accepts:
//
//   - the shorthands daily, weekdays, weekly, monthly and yearly, which take
//     their day from the repeat's start date (see Anchored);
//   - RRULE text using FREQ, INTERVAL, BYDAY, BYMONTHDAY and BYMONTH, such as
//     FREQ=MONTHLY;BYDAY=3WE or FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,TH;
//   - a cron expression after "CRON:", such as CRON:0 9 * * 1 or CRON:@weekly.
//
// A shorthand, or RRULE text that leaves out its day, is incomplete until
// it's anchored to a start date.
func ParseRepeatRule(raw string) (RepeatRule, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return RepeatRule{}, repeatRuleError("rule required")
	}
	if len(text) > MaxRepeatRuleLength {
		return RepeatRule{}, repeatRuleError("rule max %d chars", MaxRepeatRuleLength)
	}
	switch strings.ToLower(text) {
	case "daily":
		return RepeatRule{Frequency: RepeatDaily, Interval: 1}, nil
	case "weekdays":
		return RepeatRule{Frequency: RepeatWeekly, Interval: 1, Weekdays: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}}, nil
	case "weekly":
		return RepeatRule{Frequency: RepeatWeekly, Interval: 1}, nil
	case "monthly":
		return RepeatRule{Frequency: RepeatMonthly, Interval: 1}, nil
	case "yearly":
		return RepeatRule{Frequency: RepeatYearly, Interval: 1}, nil
	}
	if len(text) >= 5 && strings.EqualFold(text[:5], "CRON:") {
		return parseRepeatCron(text[5:])
	}
	if len(text) >= 6 && strings.EqualFold(text[:6], "RRULE:") {
		text = text[6:]
	}
	return parseRepeatRRule(text)
}

func parseRepeatCron(raw string) (RepeatRule, error) {
	expr := strings.Join(strings.Fields(raw), " ")
	if expr == "" {
		return RepeatRule{}, repeatRuleError("cron expression required")
	}
	upper := strings.ToUpper(expr)
	if strings.HasPrefix(upper, "TZ=") || strings.HasPrefix(upper, "CRON_TZ=") {
		return RepeatRule{}, repeatRuleError("set the time zone separately, not in the cron expression")
	}
	if strings.HasPrefix(strings.ToLower(expr), "@every") {
		return RepeatRule{}, repeatRuleError("@every isn't supported; use a calendar schedule")
	}
	schedule, err := repeatCronParser.Parse(expr)
	if err != nil {
		return RepeatRule{}, repeatRuleError("cron: %v", err)
	}
	// robfig/cron gives up after five years without a match, so a schedule
	// that never fires (such as 30 February) shows up as a zero time.
	if schedule.Next(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)).IsZero() {
		return RepeatRule{}, repeatRuleError("cron expression never matches a date")
	}
	return RepeatRule{Frequency: RepeatCron, Cron: expr, schedule: schedule}, nil
}

func parseRepeatRRule(text string) (RepeatRule, error) {
	rule := RepeatRule{Interval: 1}
	seen := map[string]bool{}
	var byDay, byMonthDay, byMonth string
	for _, part := range strings.Split(text, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		key = strings.ToUpper(strings.TrimSpace(key))
		value = strings.ToUpper(strings.TrimSpace(value))
		if !ok || value == "" {
			return RepeatRule{}, repeatRuleError("expected KEY=VALUE, got %q", part)
		}
		if seen[key] {
			return RepeatRule{}, repeatRuleError("%s given twice", key)
		}
		seen[key] = true
		switch key {
		case "FREQ":
			rule.Frequency = RepeatFrequency(value)
		case "INTERVAL":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > MaxRepeatInterval {
				return RepeatRule{}, repeatRuleError("INTERVAL must be 1 to %d", MaxRepeatInterval)
			}
			rule.Interval = n
		case "BYDAY":
			byDay = value
		case "BYMONTHDAY":
			byMonthDay = value
		case "BYMONTH":
			byMonth = value
		default:
			return RepeatRule{}, repeatRuleError("unsupported key %s", key)
		}
	}
	switch rule.Frequency {
	case RepeatDaily:
		if byDay != "" || byMonthDay != "" || byMonth != "" {
			return RepeatRule{}, repeatRuleError("FREQ=DAILY takes only INTERVAL")
		}
	case RepeatWeekly:
		if byMonthDay != "" || byMonth != "" {
			return RepeatRule{}, repeatRuleError("FREQ=WEEKLY takes only INTERVAL and BYDAY")
		}
		if byDay != "" {
			for _, code := range strings.Split(byDay, ",") {
				day, ok := repeatWeekdayCodes[strings.TrimSpace(code)]
				if !ok {
					return RepeatRule{}, repeatRuleError("BYDAY for FREQ=WEEKLY is a list of MO, TU, WE, TH, FR, SA, SU")
				}
				if !slices.Contains(rule.Weekdays, day) {
					rule.Weekdays = append(rule.Weekdays, day)
				}
			}
			sortRepeatWeekdays(rule.Weekdays)
		}
	case RepeatMonthly:
		if byMonth != "" {
			return RepeatRule{}, repeatRuleError("FREQ=MONTHLY doesn't take BYMONTH")
		}
		if byDay != "" && byMonthDay != "" {
			return RepeatRule{}, repeatRuleError("FREQ=MONTHLY takes BYDAY or BYMONTHDAY, not both")
		}
		if byMonthDay != "" {
			day, err := strconv.Atoi(byMonthDay)
			if err != nil || (day != LastRepeatMonthDay && (day < 1 || day > 31)) {
				return RepeatRule{}, repeatRuleError("BYMONTHDAY must be 1 to 31, or -1 for the last day")
			}
			rule.MonthDay = day
		}
		if byDay != "" {
			ordinal, weekday, err := parseRepeatOrdinalWeekday(byDay)
			if err != nil {
				return RepeatRule{}, err
			}
			rule.Ordinal = ordinal
			rule.Weekday = weekday
		}
	case RepeatYearly:
		if byDay != "" {
			return RepeatRule{}, repeatRuleError("FREQ=YEARLY takes BYMONTH and BYMONTHDAY, not BYDAY")
		}
		if (byMonth == "") != (byMonthDay == "") {
			return RepeatRule{}, repeatRuleError("FREQ=YEARLY takes BYMONTH and BYMONTHDAY together")
		}
		if byMonth != "" {
			month, err := strconv.Atoi(byMonth)
			if err != nil || month < 1 || month > 12 {
				return RepeatRule{}, repeatRuleError("BYMONTH must be 1 to 12")
			}
			day, err := strconv.Atoi(byMonthDay)
			// 2000 is a leap year, so 29 February is allowed.
			if err != nil || day < 1 || day > daysInMonth(2000, time.Month(month)) {
				return RepeatRule{}, repeatRuleError("BYMONTHDAY isn't a day of that month")
			}
			rule.Month = time.Month(month)
			rule.MonthDay = day
		}
	case "":
		return RepeatRule{}, repeatRuleError("FREQ required")
	default:
		return RepeatRule{}, repeatRuleError("FREQ must be DAILY, WEEKLY, MONTHLY or YEARLY")
	}
	return rule, nil
}

func parseRepeatOrdinalWeekday(raw string) (int, time.Weekday, error) {
	bad := repeatRuleError("BYDAY for FREQ=MONTHLY is one weekday with 1 to 4 or -1 before it, such as 3WE or -1FR")
	if len(raw) < 3 || strings.Contains(raw, ",") {
		return 0, 0, bad
	}
	weekday, ok := repeatWeekdayCodes[raw[len(raw)-2:]]
	if !ok {
		return 0, 0, bad
	}
	ordinal, err := strconv.Atoi(raw[:len(raw)-2])
	if err != nil || (ordinal != LastRepeatOrdinal && (ordinal < 1 || ordinal > 4)) {
		return 0, 0, bad
	}
	return ordinal, weekday, nil
}

func sortRepeatWeekdays(days []time.Weekday) {
	// Weeks start on Monday.
	slices.SortFunc(days, func(a, b time.Weekday) int { return mondayIndex(a) - mondayIndex(b) })
}

func mondayIndex(d time.Weekday) int {
	return (int(d) + 6) % 7
}

// Anchored fills in what a shorthand left out from the repeat's start date:
// a weekly rule's day, a monthly rule's day of the month and a yearly rule's
// date.
func (r RepeatRule) Anchored(startsOn Date) RepeatRule {
	t := startsOn.Time()
	switch r.Frequency {
	case RepeatWeekly:
		if len(r.Weekdays) == 0 {
			r.Weekdays = []time.Weekday{t.Weekday()}
		}
	case RepeatMonthly:
		if r.MonthDay == 0 && r.Ordinal == 0 {
			r.MonthDay = t.Day()
		}
	case RepeatYearly:
		if r.Month == 0 {
			r.Month = t.Month()
			r.MonthDay = t.Day()
		}
	}
	return r
}

// String is the rule's canonical text, as stored and returned by the API.
func (r RepeatRule) String() string {
	if r.Frequency == RepeatCron {
		return "CRON:" + r.Cron
	}
	parts := []string{"FREQ=" + string(r.Frequency)}
	if r.Interval > 1 {
		parts = append(parts, fmt.Sprintf("INTERVAL=%d", r.Interval))
	}
	switch r.Frequency {
	case RepeatWeekly:
		if len(r.Weekdays) > 0 {
			codes := make([]string, 0, len(r.Weekdays))
			for _, d := range r.Weekdays {
				codes = append(codes, repeatWeekdayCode(d))
			}
			parts = append(parts, "BYDAY="+strings.Join(codes, ","))
		}
	case RepeatMonthly:
		if r.Ordinal != 0 {
			parts = append(parts, fmt.Sprintf("BYDAY=%d%s", r.Ordinal, repeatWeekdayCode(r.Weekday)))
		} else if r.MonthDay != 0 {
			parts = append(parts, fmt.Sprintf("BYMONTHDAY=%d", r.MonthDay))
		}
	case RepeatYearly:
		if r.Month != 0 {
			parts = append(parts, fmt.Sprintf("BYMONTH=%d", int(r.Month)), fmt.Sprintf("BYMONTHDAY=%d", r.MonthDay))
		}
	}
	return strings.Join(parts, ";")
}

// Summary describes the rule in plain English, such as "Every month on the
// third Wednesday".
func (r RepeatRule) Summary() string {
	every := func(unit string) string {
		if r.Interval > 1 {
			return fmt.Sprintf("Every %d %ss", r.Interval, unit)
		}
		return "Every " + unit
	}
	switch r.Frequency {
	case RepeatDaily:
		return every("day")
	case RepeatWeekly:
		if r.Interval <= 1 && slices.Equal(r.Weekdays, []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}) {
			return "Every weekday"
		}
		names := make([]string, 0, len(r.Weekdays))
		for _, d := range r.Weekdays {
			names = append(names, d.String())
		}
		return every("week") + " on " + joinEnglish(names)
	case RepeatMonthly:
		switch {
		case r.Ordinal != 0:
			return fmt.Sprintf("%s on the %s %s", every("month"), repeatOrdinalWord(r.Ordinal), r.Weekday)
		case r.MonthDay == LastRepeatMonthDay:
			return every("month") + " on the last day"
		case r.MonthDay > 28:
			return fmt.Sprintf("%s on the %s, or the last day of shorter months", every("month"), OrdinalNumber(r.MonthDay))
		default:
			return fmt.Sprintf("%s on the %s", every("month"), OrdinalNumber(r.MonthDay))
		}
	case RepeatYearly:
		summary := fmt.Sprintf("%s on %s %d", every("year"), r.Month.String()[:3], r.MonthDay)
		if r.Month == time.February && r.MonthDay == 29 {
			summary += ", or Feb 28 in other years"
		}
		return summary
	case RepeatCron:
		return "On the cron schedule " + r.Cron
	}
	return ""
}

func repeatOrdinalWord(ordinal int) string {
	switch ordinal {
	case 1:
		return "first"
	case 2:
		return "second"
	case 3:
		return "third"
	case 4:
		return "fourth"
	}
	return "last"
}

// OrdinalNumber writes 1 as "1st", 22 as "22nd" and so on.
func OrdinalNumber(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}

func joinEnglish(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// LoadRepeatLocation resolves a repeat's IANA time zone. An empty name is UTC.
// Only canonical names are taken, each part capitalised (Europe/London,
// America/Argentina/Buenos_Aires, UTC), so a name that only resolves on one
// host, such as a lower-case name on a case-insensitive disk or the posix/
// and right/ copies, can't be stored.
func LoadRepeatLocation(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return time.UTC, nil
	}
	if len(name) > 64 || name == "Local" || !canonicalZoneName(name) {
		return nil, repeatRuleError("unknown time zone %q", name)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, repeatRuleError("unknown time zone %q", name)
	}
	return loc, nil
}

func canonicalZoneName(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if part == "" || part[0] < 'A' || part[0] > 'Z' {
			return false
		}
	}
	return true
}

// TodayIn is the date it is now in loc.
func TodayIn(loc *time.Location, now time.Time) Date {
	return DateFromTime(now.In(loc))
}

// AddDays moves a date by n days.
func (d Date) AddDays(n int) Date {
	return Date(d.Time().AddDate(0, 0, n))
}

// Before reports whether d is earlier than other.
func (d Date) Before(other Date) bool {
	return d.Time().Before(other.Time())
}

// RepeatSchedule is a rule placed in time: its start date, which anchors
// intervals, and the time zone its dates are in.
type RepeatSchedule struct {
	Rule     RepeatRule
	StartsOn Date
	Location *time.Location
}

// NewRepeatSchedule anchors a parsed rule to its start date.
func NewRepeatSchedule(rule RepeatRule, startsOn Date, loc *time.Location) RepeatSchedule {
	if loc == nil {
		loc = time.UTC
	}
	return RepeatSchedule{Rule: rule.Anchored(startsOn), StartsOn: startsOn, Location: loc}
}

// FirstOnOrAfter is the first date on or after from (and on or after the
// start date) that the schedule falls on. ok is false when there is none,
// which only a cron expression can cause.
func (s RepeatSchedule) FirstOnOrAfter(from Date) (Date, bool) {
	start := from
	if start.Before(s.StartsOn) {
		start = s.StartsOn
	}
	r := s.Rule
	n := max(r.Interval, 1)
	switch r.Frequency {
	case RepeatDaily:
		offset := daysBetween(s.StartsOn, start)
		steps := (offset + n - 1) / n
		return s.StartsOn.AddDays(steps * n), true
	case RepeatWeekly:
		if len(r.Weekdays) == 0 {
			return Date{}, false
		}
		anchorWeek := weekIndex(s.StartsOn)
		for i := 0; i < 7*(n+1); i++ {
			d := start.AddDays(i)
			if (weekIndex(d)-anchorWeek)%n == 0 && slices.Contains(r.Weekdays, d.Time().Weekday()) {
				return d, true
			}
		}
	case RepeatMonthly:
		anchorMonth := monthIndex(s.StartsOn)
		for mi := monthIndex(start); mi <= monthIndex(start)+2*n+1; mi++ {
			if (mi-anchorMonth)%n != 0 {
				continue
			}
			if d := r.monthlyDate(mi/12, time.Month(mi%12+1)); !d.Before(start) {
				return d, true
			}
		}
	case RepeatYearly:
		anchorYear := s.StartsOn.Time().Year()
		for y := start.Time().Year(); y <= start.Time().Year()+2*n+1; y++ {
			if (y-anchorYear)%n != 0 {
				continue
			}
			d := civilDate(y, r.Month, min(r.MonthDay, daysInMonth(y, r.Month)))
			if !d.Before(start) {
				return d, true
			}
		}
	case RepeatCron:
		schedule := r.schedule
		if schedule == nil {
			parsed, err := parseRepeatCron(r.Cron)
			if err != nil {
				return Date{}, false
			}
			schedule = parsed.schedule
		}
		// Dates are matched in UTC, which has no clock changes, so a time
		// a zone skips (such as a midnight when the clocks go forward)
		// can't drop a date. The zone only decides what today is.
		next := schedule.Next(start.Time().Add(-time.Second))
		if next.IsZero() {
			return Date{}, false
		}
		return DateFromTime(next), true
	}
	return Date{}, false
}

// NextAfter is the first date after d that the schedule falls on.
func (s RepeatSchedule) NextAfter(d Date) (Date, bool) {
	return s.FirstOnOrAfter(d.AddDays(1))
}

func (r RepeatRule) monthlyDate(year int, month time.Month) Date {
	last := daysInMonth(year, month)
	switch {
	case r.Ordinal == LastRepeatOrdinal:
		lastDay := civilDate(year, month, last)
		back := (int(lastDay.Time().Weekday()) - int(r.Weekday) + 7) % 7
		return lastDay.AddDays(-back)
	case r.Ordinal > 0:
		first := civilDate(year, month, 1)
		forward := (int(r.Weekday) - int(first.Time().Weekday()) + 7) % 7
		return first.AddDays(forward + (r.Ordinal-1)*7)
	case r.MonthDay == LastRepeatMonthDay:
		return civilDate(year, month, last)
	default:
		return civilDate(year, month, min(r.MonthDay, last))
	}
}

// RepeatPlan is what completing a repetition leads to: the next repetition's
// date and the dates skipped on the way, because they had already passed.
type RepeatPlan struct {
	Next         Date
	OK           bool
	Skipped      []Date
	SkippedCount int
}

// maxRepeatPlanSteps bounds the walk over passed dates. A daily repeat would
// need 270 years without completion to reach it; past it, the plan jumps
// straight to the first date that hasn't passed.
const maxRepeatPlanSteps = 100_000

// PlanAfterCompletion picks the repetition that follows current when it's
// completed on completedOn (a date in the schedule's time zone). The very
// next date is used unless it has already passed, that is, it's before
// completedOn. Passed dates are skipped, so completing on the next date
// itself still creates it, due that day.
func (s RepeatSchedule) PlanAfterCompletion(current, completedOn Date) RepeatPlan {
	plan := RepeatPlan{}
	next, ok := s.NextAfter(current)
	for steps := 0; ok && next.Before(completedOn); steps++ {
		if steps == maxRepeatPlanSteps {
			next, ok = s.FirstOnOrAfter(completedOn)
			break
		}
		plan.SkippedCount++
		if len(plan.Skipped) < MaxRepeatSkippedDates {
			plan.Skipped = append(plan.Skipped, next)
		}
		next, ok = s.NextAfter(next)
	}
	plan.Next, plan.OK = next, ok
	return plan
}

// Upcoming lists up to n schedule dates on or after from, for previews.
func (s RepeatSchedule) Upcoming(from Date, n int) []Date {
	out := make([]Date, 0, n)
	d, ok := s.FirstOnOrAfter(from)
	for ok && len(out) < n {
		out = append(out, d)
		d, ok = s.NextAfter(d)
	}
	return out
}

func civilDate(year int, month time.Month, day int) Date {
	return Date(time.Date(year, month, day, 0, 0, 0, 0, time.UTC))
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func daysBetween(a, b Date) int {
	// Unix seconds, not Sub: a Duration tops out at about 292 years. Dates
	// are UTC midnights, so the division is exact.
	return int((b.Time().Unix() - a.Time().Unix()) / 86400)
}

func weekIndex(d Date) int {
	// Days since a Monday, in whole weeks. 2001-01-01 was a Monday.
	return floorDiv(daysBetween(civilDate(2001, time.January, 1), d), 7)
}

func monthIndex(d Date) int {
	t := d.Time()
	return t.Year()*12 + int(t.Month()) - 1
}

func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// IssueRepeat is how an issue fits into a repeating series.
type IssueRepeat struct {
	ID       uuid.UUID `json:"id"`
	Rule     string    `json:"rule"`
	Summary  string    `json:"summary"`
	TimeZone string    `json:"time_zone"`
	StartsOn Date      `json:"starts_on"`
	// Current is true while this issue is the series' live repetition, the
	// one whose completion creates the next. It's false on earlier
	// repetitions and once someone stops the repeat.
	Current bool `json:"current"`
	// Occurrence is this repetition's date in the schedule. It starts as the
	// due date, but moving the due date doesn't move the schedule.
	Occurrence Date `json:"occurrence"`
	// NextOccurrence is the date the next repetition is for if this one is
	// completed on time. Only on the current repetition.
	NextOccurrence *Date `json:"next_occurrence,omitempty"`
	// SkippedDates (up to MaxRepeatSkippedDates) and SkippedCount are the
	// repetitions skipped just before this one, because the previous one
	// was completed after their dates.
	SkippedDates []Date `json:"skipped_dates"`
	SkippedCount int    `json:"skipped_count"`
	// Previous and Next are the neighbouring repetitions, when they exist
	// and the reader may see them.
	Previous *IssueRepetition `json:"previous,omitempty"`
	Next     *IssueRepetition `json:"next,omitempty"`
}

// IssueRepetition names another repetition in the same series.
type IssueRepetition struct {
	Identifier   string    `json:"identifier"`
	Number       int       `json:"number"`
	Title        string    `json:"title"`
	Status       Status    `json:"status"`
	Occurrence   Date      `json:"occurrence"`
	SkippedDates []Date    `json:"skipped_dates"`
	SkippedCount int       `json:"skipped_count"`
	CreatedAt    time.Time `json:"created_at"`
}
