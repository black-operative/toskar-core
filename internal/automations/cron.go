package automations

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// cronSpec is a five-field cron expression: minute, hour, day of month,
// month, and day of week, read in the schedule's time zone (#204). Fields
// take *, numbers, ranges (1-5), steps (*/15, 8-18/2), lists (1,15), and
// month and day names (JAN, MON). @hourly, @daily, @weekly, @monthly, and
// @yearly stand for their usual expressions. As in cron, when both the
// day of month and the day of week are restricted, a day matching either
// runs.
type cronSpec struct {
	minutes, hours, days, months, weekdays []bool
	anyDay, anyWeekday                     bool
}

var cronMacros = map[string]string{
	"@hourly":   "0 * * * *",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@weekly":   "0 0 * * 0",
	"@monthly":  "0 0 1 * *",
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
}

var (
	monthNames   = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
	weekdayNames = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}
)

func parseCron(expr string) (*cronSpec, error) {
	expr = strings.TrimSpace(expr)
	if macro, ok := cronMacros[strings.ToLower(expr)]; ok {
		expr = macro
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron needs five fields: minute hour day-of-month month day-of-week")
	}
	var spec cronSpec
	var err error
	if spec.minutes, err = cronField(fields[0], 0, 59, nil); err != nil {
		return nil, fmt.Errorf("cron minute: %w", err)
	}
	if spec.hours, err = cronField(fields[1], 0, 23, nil); err != nil {
		return nil, fmt.Errorf("cron hour: %w", err)
	}
	if spec.days, err = cronField(fields[2], 1, 31, nil); err != nil {
		return nil, fmt.Errorf("cron day of month: %w", err)
	}
	if spec.months, err = cronField(fields[3], 1, 12, monthNames); err != nil {
		return nil, fmt.Errorf("cron month: %w", err)
	}
	// 7 is Sunday too.
	if spec.weekdays, err = cronField(fields[4], 0, 7, weekdayNames); err != nil {
		return nil, fmt.Errorf("cron day of week: %w", err)
	}
	if spec.weekdays[7] {
		spec.weekdays[0] = true
	}
	spec.anyDay = fields[2] == "*" || fields[2] == "?"
	spec.anyWeekday = fields[4] == "*" || fields[4] == "?"
	return &spec, nil
}

// cronField reads one field into a set indexed by value.
func cronField(field string, min, max int, names map[string]int) ([]bool, error) {
	set := make([]bool, max+1)
	for _, part := range strings.Split(field, ",") {
		step := 1
		if base, s, ok := strings.Cut(part, "/"); ok {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 {
				return nil, fmt.Errorf("bad step %q", s)
			}
			part, step = base, n
		}
		lo, hi := min, max
		switch {
		case part == "*" || part == "?":
		case strings.Contains(part, "-"):
			a, b, _ := strings.Cut(part, "-")
			var err error
			if lo, err = cronValue(a, names); err != nil {
				return nil, err
			}
			if hi, err = cronValue(b, names); err != nil {
				return nil, err
			}
		default:
			v, err := cronValue(part, names)
			if err != nil {
				return nil, err
			}
			lo, hi = v, v
			if step > 1 {
				hi = max
			}
		}
		if lo < min || hi > max || lo > hi {
			return nil, fmt.Errorf("%q is outside %d-%d", part, min, max)
		}
		for v := lo; v <= hi; v += step {
			set[v] = true
		}
	}
	return set, nil
}

func cronValue(s string, names map[string]int) (int, error) {
	if v, ok := names[strings.ToLower(s)]; ok {
		return v, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("bad value %q", s)
	}
	return v, nil
}

// matchesDay reports a civil day the expression runs on.
func (c *cronSpec) matchesDay(day time.Time) bool {
	if !c.months[int(day.Month())] {
		return false
	}
	dom, dow := c.days[day.Day()], c.weekdays[int(day.Weekday())]
	switch {
	case c.anyDay && c.anyWeekday:
		return true
	case c.anyDay:
		return dow
	case c.anyWeekday:
		return dom
	default:
		return dom || dow
	}
}

// times are the clock times it runs at on a day it runs, in order.
func (c *cronSpec) times() []ClockTime {
	var out []ClockTime
	for h, ok := range c.hours {
		if !ok {
			continue
		}
		for m, ok := range c.minutes {
			if ok {
				out = append(out, ClockTime{Hour: h, Minute: m})
			}
		}
	}
	return out
}
