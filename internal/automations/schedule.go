package automations

import (
	"fmt"
	"slices"
	"time"
)

// Kind is the v1 schedule shape. Price checks, stock checks, and one-shot research
// are all one of these, not separate product features.
type Kind string

const (
	KindOnce     Kind = "once"
	KindDaily    Kind = "daily"
	KindWeekly   Kind = "weekly"
	KindInterval Kind = "interval"
	// KindMonthly runs on a day of the month, and KindCron on a cron
	// expression (#204).
	KindMonthly Kind = "monthly"
	KindCron    Kind = "cron"
	// KindManual never runs on its own: only when started, by Run now or a
	// webhook (#204).
	KindManual Kind = "manual"
)

// ClockTime is a time of day, in the schedule's time zone.
type ClockTime struct {
	Hour   int `json:"hour"`
	Minute int `json:"minute"`
}

// maxTimes is how many times a day a schedule may name.
const maxTimes = 24

// Schedule is the time rule for an automation.
// Clock fields are civil time in TimeZone. At and Anchor are absolute instants.
type Schedule struct {
	Kind         Kind       `json:"kind"`
	TimeZone     string     `json:"time_zone"`
	At           *time.Time `json:"at,omitempty"`
	Hour         int        `json:"hour,omitempty"`
	Minute       int        `json:"minute,omitempty"`
	Weekday      *int       `json:"weekday,omitempty"`
	EverySeconds int        `json:"every_seconds,omitempty"`
	Anchor       *time.Time `json:"anchor,omitempty"`
	// Weekdays are a weekly schedule's days, 0 (Sunday) to 6, such as 1-5
	// for weekdays only; Weekday is the first, for clients from before
	// several days (#204).
	Weekdays []int `json:"weekdays,omitempty"`
	// Times are the times of day a daily, weekly, or monthly schedule runs
	// at; Hour and Minute are the first.
	Times []ClockTime `json:"times,omitempty"`
	// MonthDay is a monthly schedule's day, 1 to 31. A month without that
	// day runs on its last day.
	MonthDay int `json:"month_day,omitempty"`
	// Cron is a cron schedule's five-field expression, such as
	// "0 9 * * 1-5".
	Cron string `json:"cron,omitempty"`
}

// Normalized fills Weekdays and Times from the single-value fields older
// clients send, and the single-value fields from the lists, so both kinds
// of client read the same schedule. Lists are sorted without repeats.
func (s Schedule) Normalized() Schedule {
	switch s.Kind {
	case KindDaily, KindWeekly, KindMonthly:
		if len(s.Times) == 0 {
			s.Times = []ClockTime{{Hour: s.Hour, Minute: s.Minute}}
		}
		s.Times = slices.Clone(s.Times)
		slices.SortFunc(s.Times, func(a, b ClockTime) int { return (a.Hour*60 + a.Minute) - (b.Hour*60 + b.Minute) })
		s.Times = slices.Compact(s.Times)
		s.Hour, s.Minute = s.Times[0].Hour, s.Times[0].Minute
	default:
		s.Times = nil
	}
	if s.Kind == KindWeekly {
		if len(s.Weekdays) == 0 && s.Weekday != nil {
			s.Weekdays = []int{*s.Weekday}
		}
		s.Weekdays = slices.Compact(slices.Sorted(slices.Values(s.Weekdays)))
		if len(s.Weekdays) > 0 {
			first := s.Weekdays[0]
			s.Weekday = &first
		}
	} else {
		s.Weekdays = nil
	}
	if s.Kind != KindMonthly {
		s.MonthDay = 0
	}
	if s.Kind != KindCron {
		s.Cron = ""
	}
	return s
}

// Validate checks that the schedule can produce a next run.
func (s Schedule) Validate() error {
	switch s.Kind {
	case KindOnce, KindDaily, KindWeekly, KindInterval, KindMonthly, KindCron, KindManual:
	case "":
		return fmt.Errorf("schedule kind is required")
	default:
		return fmt.Errorf("unsupported schedule kind %q", s.Kind)
	}
	if s.TimeZone == "" {
		return fmt.Errorf("time zone is required")
	}
	if _, err := time.LoadLocation(s.TimeZone); err != nil {
		return fmt.Errorf("time zone %q: %w", s.TimeZone, err)
	}
	switch s.Kind {
	case KindOnce:
		if s.At == nil || s.At.IsZero() {
			return fmt.Errorf("one-time schedule requires at")
		}
	case KindDaily, KindWeekly, KindMonthly:
		n := s.Normalized()
		if len(n.Times) > maxTimes {
			return fmt.Errorf("a schedule can run at most %d times a day", maxTimes)
		}
		for _, t := range n.Times {
			if t.Hour < 0 || t.Hour > 23 {
				return fmt.Errorf("hour must be 0-23")
			}
			if t.Minute < 0 || t.Minute > 59 {
				return fmt.Errorf("minute must be 0-59")
			}
		}
		switch s.Kind {
		case KindWeekly:
			if len(n.Weekdays) == 0 {
				return fmt.Errorf("weekly schedule requires weekday")
			}
			for _, day := range n.Weekdays {
				if day < 0 || day > 6 {
					return fmt.Errorf("weekday must be 0-6")
				}
			}
		case KindMonthly:
			if s.MonthDay < 1 || s.MonthDay > 31 {
				return fmt.Errorf("monthly schedule requires month_day 1-31")
			}
		}
	case KindCron:
		spec, err := parseCron(s.Cron)
		if err != nil {
			return err
		}
		// An expression such as "0 0 31 2 *" never runs.
		reference := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		if s.withCron(spec).nextSlot(time.UTC, reference).IsZero() {
			return fmt.Errorf("cron %q never runs", s.Cron)
		}
	case KindInterval:
		if s.EverySeconds < 1 {
			return fmt.Errorf("interval must be at least 1 second")
		}
		const maxSeconds = int64(1<<63-1) / int64(time.Second)
		if int64(s.EverySeconds) > maxSeconds {
			return fmt.Errorf("interval is too long")
		}
	}
	return nil
}

// NextRun chooses the single occurrence the daemon should execute.
//
// createdAt ignores recurring slots from before the automation existed.
// lastOccurrence is when the previous occurrence finished. The zero time means it has never run.
// When several slots fall after that bound and at or before now, NextRun returns the latest
// one so a restart runs the most recent missed occurrence once.
// The boolean is false when a one-time schedule has already run.
func (s Schedule) NextRun(createdAt, lastOccurrence, now time.Time) (time.Time, bool, error) {
	if err := s.Validate(); err != nil {
		return time.Time{}, false, err
	}
	createdAt = wall(createdAt)
	lastOccurrence = wall(lastOccurrence)
	now = wall(now)

	switch s.Kind {
	case KindManual:
		return time.Time{}, false, nil
	case KindOnce:
		if !lastOccurrence.IsZero() {
			return time.Time{}, false, nil
		}
		return wall(*s.At), true, nil
	case KindInterval:
		every := time.Duration(s.EverySeconds) * time.Second
		start := createdAt
		if s.Anchor != nil && !s.Anchor.IsZero() {
			start = wall(*s.Anchor)
		}
		if start.IsZero() {
			return time.Time{}, false, fmt.Errorf("interval schedule requires a created time or anchor")
		}
		return nextOnGrid(start, every, later(createdAt, lastOccurrence), now), true, nil
	default:
		loc, err := time.LoadLocation(s.TimeZone)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("time zone %q: %w", s.TimeZone, err)
		}
		cal := s.calendar()
		bound := later(createdAt, lastOccurrence)
		latest := cal.latestSlot(loc, now)
		if !latest.IsZero() && latest.After(bound) && !latest.After(now) {
			return wall(latest), true, nil
		}
		after := now
		if bound.After(after) {
			after = bound
		}
		next := cal.nextSlot(loc, after)
		if next.IsZero() {
			return time.Time{}, false, nil
		}
		return wall(next), true, nil
	}
}

func wall(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC().Truncate(time.Second)
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// nextOnGrid walks start, start+every, start+2every, ...
// It returns the latest point in (notBefore, now], or the next point after now when nothing is due.
func nextOnGrid(start time.Time, every time.Duration, notBefore, now time.Time) time.Time {
	first := firstAfter(start, notBefore, every)
	if first.After(now) || now.Before(start) {
		return first
	}
	elapsed := now.Sub(start)
	latest := now.Add(-(elapsed % every))
	if latest.After(notBefore) {
		return latest
	}
	return first
}

func firstAfter(start, after time.Time, every time.Duration) time.Time {
	if start.After(after) {
		return start
	}
	elapsed := after.Sub(start)
	rem := elapsed % every
	if rem == 0 {
		return after.Add(every)
	}
	return after.Add(every - rem)
}

// calendar is a calendar schedule ready to step through day by day.
type calendar struct {
	s    Schedule
	cron *cronSpec
}

func (s Schedule) calendar() calendar {
	c := calendar{s: s.Normalized()}
	if s.Kind == KindCron {
		c.cron, _ = parseCron(s.Cron)
	}
	return c
}

func (s Schedule) withCron(spec *cronSpec) calendar {
	return calendar{s: s, cron: spec}
}

// span is how many days to look through for a slot: enough to find one in
// any schedule that runs at all.
func (c calendar) span() int {
	switch c.s.Kind {
	case KindDaily:
		return 3
	case KindWeekly:
		return 8
	case KindMonthly:
		return 63
	}
	// A cron on February 29th runs once in four years, or eight across a
	// century that skips a leap year.
	return 366*8 + 2
}

// times are the clock times a day runs at, or nil when it doesn't run.
func (c calendar) times(day time.Time) []ClockTime {
	switch c.s.Kind {
	case KindWeekly:
		if !slices.Contains(c.s.Weekdays, int(day.Weekday())) {
			return nil
		}
	case KindMonthly:
		last := time.Date(day.Year(), day.Month()+1, 0, 12, 0, 0, 0, day.Location()).Day()
		if day.Day() != min(c.s.MonthDay, last) {
			return nil
		}
	case KindCron:
		if c.cron == nil || !c.cron.matchesDay(day) {
			return nil
		}
		return c.cron.times()
	}
	return c.s.Times
}

// latestSlot is the last slot at or before now, or zero when there is none
// in the span.
func (c calendar) latestSlot(loc *time.Location, now time.Time) time.Time {
	local := now.In(loc)
	for i := 0; i <= c.span(); i++ {
		day := civilOnDay(local, -i, 12, 0, loc)
		times := c.times(day)
		for j := len(times) - 1; j >= 0; j-- {
			slot := civilOnDay(local, -i, times[j].Hour, times[j].Minute, loc)
			if !slot.After(now) {
				return slot
			}
		}
	}
	return time.Time{}
}

// nextSlot is the first slot after after, or zero when there is none in
// the span.
func (c calendar) nextSlot(loc *time.Location, after time.Time) time.Time {
	local := after.In(loc)
	for i := 0; i <= c.span(); i++ {
		day := civilOnDay(local, i, 12, 0, loc)
		for _, t := range c.times(day) {
			slot := civilOnDay(local, i, t.Hour, t.Minute, loc)
			if slot.After(after) {
				return slot
			}
		}
	}
	return time.Time{}
}

// civilOnDay returns hour:minute on the civil day offset from local, using noon as the
// day anchor so a DST transition does not shift the calendar date.
func civilOnDay(local time.Time, dayOffset, hour, minute int, loc *time.Location) time.Time {
	base := time.Date(local.Year(), local.Month(), local.Day(), 12, 0, 0, 0, loc).AddDate(0, 0, dayOffset)
	return time.Date(base.Year(), base.Month(), base.Day(), hour, minute, 0, 0, loc)
}
