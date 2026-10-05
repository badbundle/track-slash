package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

// Repeat picker element prefixes: the issue's Details sidebar and the
// new-issue form.
const (
	uiIssueRepeatPrefix    = "issue-repeat"
	uiNewIssueRepeatPrefix = "new-issue-repeat"
)

// uiRepeatPreviewDates is how many upcoming dates the picker previews.
const uiRepeatPreviewDates = 3

// uiRepeatFields is the repeat picker: what the person chose, and the rule,
// preview and error that follow from it. Its form fields all start with
// repeat_.
type uiRepeatFields struct {
	Prefix         string
	Frequency      string
	Interval       int
	Weekdays       []string
	MonthlyBy      string
	MonthDay       int
	Ordinal        int
	OrdinalWeekday string
	YearMonth      int
	YearDay        int
	Cron           string
	StartsOn       string
	// StartsOnGiven is true when the start date came with the request, such
	// as an issue's due date, so "Doesn't repeat" carries it along for when
	// a frequency is chosen. A default start date isn't carried: it would be
	// today in UTC before the browser has said its zone.
	StartsOnGiven bool
	TimeZone      string
	Summary       string
	Upcoming      []string
	Error         string

	rule     *model.RepeatRule
	startsOn model.Date
}

type uiRepeatOption struct {
	Value    string
	Label    string
	Selected bool
}

var uiRepeatFrequencies = []uiRepeatOption{
	{Value: "", Label: "Doesn't repeat"},
	{Value: "daily", Label: "Daily"},
	{Value: "weekdays", Label: "Every weekday"},
	{Value: "weekly", Label: "Weekly"},
	{Value: "monthly", Label: "Monthly"},
	{Value: "yearly", Label: "Yearly"},
	{Value: "cron", Label: "Cron expression"},
}

var uiRepeatWeekdayCodes = []string{"MO", "TU", "WE", "TH", "FR", "SA", "SU"}

func uiRepeatWeekdayCode(d time.Weekday) string {
	return uiRepeatWeekdayCodes[(int(d)+6)%7]
}

func uiRepeatWeekdayName(code string) string {
	for i, c := range uiRepeatWeekdayCodes {
		if c == code {
			return time.Weekday((i + 1) % 7).String()
		}
	}
	return code
}

func uiRepeatPrefixValid(prefix string) bool {
	return prefix == uiIssueRepeatPrefix || prefix == uiNewIssueRepeatPrefix
}

// uiRepeatFieldsFromValues reads the picker from a form or query. Fields
// the chosen frequency doesn't show take their defaults from the start date,
// so switching frequency starts somewhere sensible.
func uiRepeatFieldsFromValues(prefix string, values url.Values, now time.Time) uiRepeatFields {
	f := uiRepeatFields{
		Prefix:         prefix,
		Frequency:      strings.TrimSpace(values.Get("repeat_frequency")),
		TimeZone:       strings.TrimSpace(values.Get("repeat_time_zone")),
		StartsOn:       strings.TrimSpace(values.Get("repeat_starts_on")),
		MonthlyBy:      strings.TrimSpace(values.Get("repeat_monthly_by")),
		OrdinalWeekday: strings.TrimSpace(values.Get("repeat_ordinal_weekday")),
		Cron:           strings.TrimSpace(values.Get("repeat_cron")),
		Interval:       uiRepeatInt(values.Get("repeat_interval"), 1),
	}
	for _, code := range values["repeat_weekday"] {
		if uiRepeatWeekdayName(code) != code {
			f.Weekdays = append(f.Weekdays, code)
		}
	}
	loc, err := model.LoadRepeatLocation(f.TimeZone)
	if err != nil {
		f.Error = "Unknown time zone."
		loc = time.UTC
	}
	f.startsOn = model.TodayIn(loc, now)
	f.StartsOnGiven = f.StartsOn != ""
	if f.StartsOnGiven {
		if d, err := model.ParseDate(f.StartsOn); err == nil {
			f.startsOn = d
		} else if f.Error == "" {
			f.Error = "Use YYYY-MM-DD for the start date."
		}
	} else {
		f.StartsOn = f.startsOn.String()
	}
	start := f.startsOn.Time()
	f.MonthDay = uiRepeatInt(values.Get("repeat_month_day"), start.Day())
	f.Ordinal = uiRepeatInt(values.Get("repeat_ordinal"), uiRepeatOrdinalOf(start))
	f.YearMonth = uiRepeatInt(values.Get("repeat_year_month"), int(start.Month()))
	f.YearDay = uiRepeatInt(values.Get("repeat_year_day"), start.Day())
	if f.MonthlyBy != "weekday" {
		f.MonthlyBy = "day"
	}
	if uiRepeatWeekdayName(f.OrdinalWeekday) == f.OrdinalWeekday {
		f.OrdinalWeekday = uiRepeatWeekdayCode(start.Weekday())
	}
	f.compute(loc)
	return f
}

// uiNewIssueRepeatFields is the new-issue form's picker, starting on "Doesn't
// repeat" when the form came without one.
func uiNewIssueRepeatFields(data *uiNewIssuePanelData) uiRepeatFields {
	if data.Repeat.Prefix == "" {
		return uiRepeatFieldsFromValues(uiNewIssueRepeatPrefix, url.Values{}, time.Now())
	}
	return data.Repeat
}

// uiRepeatFieldsForIssue opens the picker on an issue's current repeat, or,
// for an issue that doesn't repeat, on "Doesn't repeat" starting from its due
// date.
func uiRepeatFieldsForIssue(issue model.Issue, now time.Time) uiRepeatFields {
	values := url.Values{}
	if issue.DueDate != nil {
		values.Set("repeat_starts_on", issue.DueDate.String())
	}
	if r := issue.Repeat; r != nil && r.Current {
		values.Set("repeat_time_zone", r.TimeZone)
		values.Set("repeat_starts_on", r.Occurrence.String())
		if rule, err := model.ParseRepeatRule(r.Rule); err == nil {
			uiRepeatRuleValues(values, rule)
		}
	}
	return uiRepeatFieldsFromValues(uiIssueRepeatPrefix, values, now)
}

func uiRepeatRuleValues(values url.Values, rule model.RepeatRule) {
	values.Set("repeat_interval", strconv.Itoa(max(rule.Interval, 1)))
	switch rule.Frequency {
	case model.RepeatDaily:
		values.Set("repeat_frequency", "daily")
	case model.RepeatWeekly:
		values.Set("repeat_frequency", "weekly")
		if rule.Summary() == "Every weekday" {
			values.Set("repeat_frequency", "weekdays")
		}
		for _, d := range rule.Weekdays {
			values.Add("repeat_weekday", uiRepeatWeekdayCode(d))
		}
	case model.RepeatMonthly:
		values.Set("repeat_frequency", "monthly")
		if rule.Ordinal != 0 {
			values.Set("repeat_monthly_by", "weekday")
			values.Set("repeat_ordinal", strconv.Itoa(rule.Ordinal))
			values.Set("repeat_ordinal_weekday", uiRepeatWeekdayCode(rule.Weekday))
		} else {
			values.Set("repeat_month_day", strconv.Itoa(rule.MonthDay))
		}
	case model.RepeatYearly:
		values.Set("repeat_frequency", "yearly")
		values.Set("repeat_year_month", strconv.Itoa(int(rule.Month)))
		values.Set("repeat_year_day", strconv.Itoa(rule.MonthDay))
	case model.RepeatCron:
		values.Set("repeat_frequency", "cron")
		values.Set("repeat_cron", rule.Cron)
	}
}

func uiRepeatInt(raw string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return n
}

// uiRepeatOrdinalOf is which weekday of its month a date is: 1 to 4, or last
// for the fifth.
func uiRepeatOrdinalOf(t time.Time) int {
	if n := (t.Day()-1)/7 + 1; n <= 4 {
		return n
	}
	return model.LastRepeatOrdinal
}

// ruleText turns the picker into rule text for model.ParseRepeatRule, which
// does the validating.
func (f uiRepeatFields) ruleText() (string, bool) {
	interval := fmt.Sprintf(";INTERVAL=%d", f.Interval)
	switch f.Frequency {
	case "daily":
		return "FREQ=DAILY" + interval, true
	case "weekdays":
		return "weekdays", true
	case "weekly":
		text := "FREQ=WEEKLY" + interval
		if len(f.Weekdays) > 0 {
			text += ";BYDAY=" + strings.Join(f.Weekdays, ",")
		}
		return text, true
	case "monthly":
		if f.MonthlyBy == "weekday" {
			return fmt.Sprintf("FREQ=MONTHLY%s;BYDAY=%d%s", interval, f.Ordinal, f.OrdinalWeekday), true
		}
		return fmt.Sprintf("FREQ=MONTHLY%s;BYMONTHDAY=%d", interval, f.MonthDay), true
	case "yearly":
		return fmt.Sprintf("FREQ=YEARLY%s;BYMONTH=%d;BYMONTHDAY=%d", interval, f.YearMonth, f.YearDay), true
	case "cron":
		return "CRON:" + f.Cron, f.Cron != ""
	}
	return "", false
}

func (f *uiRepeatFields) compute(loc *time.Location) {
	if f.Frequency == "" || f.Error != "" {
		return
	}
	text, ok := f.ruleText()
	if !ok {
		if f.Frequency != "cron" {
			f.Error = "Choose how often it repeats."
		}
		return
	}
	rule, err := model.ParseRepeatRule(text)
	if err != nil {
		f.Error = uiRepeatErrorText(err)
		return
	}
	schedule := model.NewRepeatSchedule(rule, f.startsOn, loc)
	f.rule = &rule
	f.TimeZone = loc.String()
	f.Summary = schedule.Rule.Summary()
	for _, d := range schedule.Upcoming(f.startsOn, uiRepeatPreviewDates) {
		f.Upcoming = append(f.Upcoming, d.Time().Format("Mon, Jan 2, 2006"))
	}
	if f.Frequency == "weekly" {
		f.Weekdays = f.Weekdays[:0]
		for _, d := range schedule.Rule.Weekdays {
			f.Weekdays = append(f.Weekdays, uiRepeatWeekdayCode(d))
		}
	}
}

// uiRepeatErrorText makes a rule error read as a sentence.
func uiRepeatErrorText(err error) string {
	text := strings.TrimPrefix(err.Error(), model.ErrInvalidRepeatRule.Error()+": ")
	text = strings.TrimSuffix(text, ": "+store.ErrConflict.Error())
	if text == "" {
		return "Invalid repeat."
	}
	return strings.ToUpper(text[:1]) + text[1:] + "."
}

var errUIRepeatNeedsCron = errors.New("Enter a cron expression.")

// setting is the store's version of the picker. It's nil for "Doesn't
// repeat".
func (f uiRepeatFields) setting(createdBy uuid.UUID) (*store.IssueRepeatSetting, error) {
	if f.Frequency == "" {
		return nil, nil
	}
	if f.Error != "" {
		return nil, errors.New(f.Error)
	}
	if f.rule == nil {
		return nil, errUIRepeatNeedsCron
	}
	startsOn := f.startsOn
	return &store.IssueRepeatSetting{Rule: *f.rule, TimeZone: f.TimeZone, StartsOn: &startsOn, CreatedBy: &createdBy}, nil
}

func (f uiRepeatFields) FrequencyOptions() []uiRepeatOption {
	out := make([]uiRepeatOption, 0, len(uiRepeatFrequencies))
	for _, o := range uiRepeatFrequencies {
		o.Selected = o.Value == f.Frequency
		out = append(out, o)
	}
	return out
}

// IntervalUnit names what "Every N" counts, or "" when the frequency has no
// interval.
func (f uiRepeatFields) IntervalUnit() string {
	unit := map[string]string{"daily": "day", "weekly": "week", "monthly": "month", "yearly": "year"}[f.Frequency]
	if unit != "" && f.Interval != 1 {
		unit += "s"
	}
	return unit
}

func (f uiRepeatFields) WeekdayOptions() []uiRepeatOption {
	out := make([]uiRepeatOption, 0, len(uiRepeatWeekdayCodes))
	for _, code := range uiRepeatWeekdayCodes {
		selected := false
		for _, chosen := range f.Weekdays {
			selected = selected || chosen == code
		}
		out = append(out, uiRepeatOption{Value: code, Label: uiRepeatWeekdayName(code)[:3], Selected: selected})
	}
	return out
}

func (f uiRepeatFields) OrdinalWeekdayOptions() []uiRepeatOption {
	out := make([]uiRepeatOption, 0, len(uiRepeatWeekdayCodes))
	for _, code := range uiRepeatWeekdayCodes {
		out = append(out, uiRepeatOption{Value: code, Label: uiRepeatWeekdayName(code), Selected: code == f.OrdinalWeekday})
	}
	return out
}

func (f uiRepeatFields) OrdinalOptions() []uiRepeatOption {
	labels := []struct {
		value int
		label string
	}{{1, "First"}, {2, "Second"}, {3, "Third"}, {4, "Fourth"}, {model.LastRepeatOrdinal, "Last"}}
	out := make([]uiRepeatOption, 0, len(labels))
	for _, l := range labels {
		out = append(out, uiRepeatOption{Value: strconv.Itoa(l.value), Label: l.label, Selected: l.value == f.Ordinal})
	}
	return out
}

func (f uiRepeatFields) MonthDayOptions() []uiRepeatOption {
	out := make([]uiRepeatOption, 0, 32)
	for day := 1; day <= 31; day++ {
		out = append(out, uiRepeatOption{Value: strconv.Itoa(day), Label: model.OrdinalNumber(day), Selected: day == f.MonthDay})
	}
	return append(out, uiRepeatOption{Value: strconv.Itoa(model.LastRepeatMonthDay), Label: "Last day", Selected: f.MonthDay == model.LastRepeatMonthDay})
}

func (f uiRepeatFields) MonthOptions() []uiRepeatOption {
	out := make([]uiRepeatOption, 0, 12)
	for m := time.January; m <= time.December; m++ {
		out = append(out, uiRepeatOption{Value: strconv.Itoa(int(m)), Label: m.String(), Selected: int(m) == f.YearMonth})
	}
	return out
}

// uiRepeatFieldsPanel re-renders the picker as its choices change.
func (s *Server) uiRepeatFieldsPanel(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("repeat_prefix")
	if !uiRepeatPrefixValid(prefix) {
		http.Error(w, "unknown repeat picker", http.StatusBadRequest)
		return
	}
	renderUITemplate(w, http.StatusOK, "repeat-fields", uiRepeatFieldsFromValues(prefix, r.URL.Query(), time.Now()))
}

func (s *Server) uiEditIssueRepeat(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	fields := uiRepeatFieldsForIssue(panel.Issue, time.Now())
	panel.EditRepeat = true
	panel.RepeatFields = &fields
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueRepeat(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	fields := uiRepeatFieldsFromValues(uiIssueRepeatPrefix, r.Form, time.Now())
	setting, err := fields.setting(currentUser(r).ID)
	if err != nil {
		s.renderUIIssuePanelWithRepeatError(w, r, issue.ID, fields, err.Error())
		return
	}
	params := store.UpdateIssueParams{Repeat: setting, ClearRepeat: setting == nil}
	if _, err := s.store.UpdateIssue(r.Context(), issue.ID, params); err != nil {
		switch {
		case errors.Is(err, store.ErrRepeatOnCompletedIssue):
			s.renderUIIssuePanelWithRepeatError(w, r, issue.ID, fields, "Reopen the issue to make it repeat.")
		case errors.Is(err, model.ErrInvalidRepeatRule):
			s.renderUIIssuePanelWithRepeatError(w, r, issue.ID, fields, uiRepeatErrorText(err))
		default:
			writeUIStoreError(w, err)
		}
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) renderUIIssuePanelWithRepeatError(w http.ResponseWriter, r *http.Request, issueID uuid.UUID, fields uiRepeatFields, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	fields.Error = message
	panel.EditRepeat = true
	panel.RepeatFields = &fields
	renderUITemplate(w, http.StatusUnprocessableEntity, "issue-panel", panel)
}

// uiIssueRepeatEditable reports whether the Details sidebar offers to change
// the repeat: a done or closed issue has to be reopened first.
func uiIssueRepeatEditable(issue model.Issue) bool {
	return !issue.Status.CountsAsDone()
}

// uiRepetitionIssue is another repetition as an issue the path helpers take.
func uiRepetitionIssue(issue model.Issue, rep *model.IssueRepetition) model.Issue {
	return model.Issue{OwnerUsername: issue.OwnerUsername, ProjectKey: issue.ProjectKey, Number: rep.Number, Identifier: rep.Identifier}
}

func uiRepeatDate(d model.Date) string {
	return d.Time().Format("Jan 2, 2006")
}

// uiRepeatDateList lists up to three dates, then how many more.
func uiRepeatDateList(dates []model.Date, count int) string {
	const shown = 3
	labels := []string{}
	for i, d := range dates {
		if i == shown {
			break
		}
		labels = append(labels, uiRepeatDate(d))
	}
	if extra := count - len(labels); extra > 0 {
		labels = append(labels, fmt.Sprintf("%d more", extra))
	}
	switch len(labels) {
	case 0:
		return ""
	case 1:
		return labels[0]
	}
	return strings.Join(labels[:len(labels)-1], ", ") + " and " + labels[len(labels)-1]
}

// uiRepeatNoticeData is the note at the top of a repeating issue when
// repetitions were skipped: on the completed issue, a warning that its
// completion skipped them; on the new one, why it skipped ahead.
type uiRepeatNoticeData struct {
	Kind   string
	Before string
	Link   *model.Issue
	After  string
}

func uiRepeatNotice(issue model.Issue) *uiRepeatNoticeData {
	r := issue.Repeat
	if r == nil {
		return nil
	}
	if issue.Status.CountsAsDone() {
		next := r.Next
		if next == nil || next.SkippedCount == 0 {
			return nil
		}
		those := "that repetition was"
		if next.SkippedCount > 1 {
			those = "those repetitions were"
		}
		link := uiRepetitionIssue(issue, next)
		return &uiRepeatNoticeData{
			Kind:   "skipped",
			Before: fmt.Sprintf("Completed after %s, so %s skipped. Next:", uiRepeatDateList(next.SkippedDates, next.SkippedCount), those),
			Link:   &link,
			After:  fmt.Sprintf("due %s.", uiRepeatDate(next.Occurrence)),
		}
	}
	if r.SkippedCount == 0 {
		return nil
	}
	repetitions := "repetition"
	if r.SkippedCount > 1 {
		repetitions = "repetitions"
	}
	completedOn := issue.CreatedAt
	if loc, err := model.LoadRepeatLocation(r.TimeZone); err == nil {
		completedOn = completedOn.In(loc)
	}
	notice := &uiRepeatNoticeData{
		Kind:   "arrived",
		Before: fmt.Sprintf("Skipped %d %s (%s) because the previous repetition", r.SkippedCount, repetitions, uiRepeatDateList(r.SkippedDates, r.SkippedCount)),
		After:  fmt.Sprintf("was completed on %s.", completedOn.Format("Jan 2, 2006")),
	}
	if r.Previous != nil {
		link := uiRepetitionIssue(issue, r.Previous)
		notice.Link = &link
	}
	return notice
}
