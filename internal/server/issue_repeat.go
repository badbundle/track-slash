package server

import (
	"errors"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

// issueRepeatInput is how REST and MCP take an issue's repeat.
type issueRepeatInput struct {
	Repeat         *string `json:"repeat,omitempty" jsonschema:"how the issue repeats: daily, weekdays, weekly, monthly, yearly, RRULE text using FREQ, INTERVAL, BYDAY, BYMONTHDAY and BYMONTH (such as FREQ=MONTHLY;BYDAY=3WE for the third Wednesday), or a cron expression after CRON: (such as CRON:0 9 * * 1). Only one repetition exists at a time; completing it creates the next"`
	RepeatTimeZone *string `json:"repeat_time_zone,omitempty" jsonschema:"IANA time zone the repeat's dates are in, such as Europe/London; UTC for a new repeat, and unchanged when changing one"`
	RepeatStartsOn *string `json:"repeat_starts_on,omitempty" jsonschema:"YYYY-MM-DD the schedule starts from; the issue becomes the repetition for the first date on or after it. Today for a new repeat, the current repetition's date when changing one"`
}

var (
	errRepeatFieldsNeedRepeat = errors.New("repeat_time_zone and repeat_starts_on need repeat")
	errRepeatStartsOnFormat   = errors.New("repeat_starts_on must be YYYY-MM-DD")
)

// given reports whether the request says anything about the repeat.
func (in issueRepeatInput) given() bool {
	return in.Repeat != nil || in.RepeatTimeZone != nil || in.RepeatStartsOn != nil
}

// setting validates the input. It returns nil when no repeat was given.
func (in issueRepeatInput) setting(createdBy uuid.UUID) (*store.IssueRepeatSetting, error) {
	if in.Repeat == nil {
		if in.given() {
			return nil, errRepeatFieldsNeedRepeat
		}
		return nil, nil
	}
	rule, err := model.ParseRepeatRule(*in.Repeat)
	if err != nil {
		return nil, err
	}
	var startsOn *model.Date
	if in.RepeatStartsOn != nil {
		d, err := model.ParseDate(*in.RepeatStartsOn)
		if err != nil {
			return nil, errRepeatStartsOnFormat
		}
		startsOn = &d
	}
	timeZone := ""
	if in.RepeatTimeZone != nil {
		loc, err := model.LoadRepeatLocation(*in.RepeatTimeZone)
		if err != nil {
			return nil, err
		}
		timeZone = loc.String()
	}
	return &store.IssueRepeatSetting{Rule: rule, TimeZone: timeZone, StartsOn: startsOn, CreatedBy: &createdBy}, nil
}
