package automations

import (
	"testing"
	"time"
)

// Several weekdays, several times a day, monthly, and cron (#204).
func TestRicherSchedules(t *testing.T) {
	juneau, _ := time.LoadLocation("America/Juneau")
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, juneau) }
	created := at(2026, 1, 1, 0, 0)
	for name, tc := range map[string]struct {
		s    Schedule
		now  time.Time
		last time.Time
		want time.Time
	}{
		// 2026-10-06 is a Tuesday.
		"Mon, Wed, Fri":        {Schedule{Kind: KindWeekly, Weekdays: []int{5, 1, 3}, Hour: 9}, at(2026, 10, 6, 10, 0), at(2026, 10, 5, 9, 0), at(2026, 10, 7, 9, 0)},
		"weekdays from Friday": {Schedule{Kind: KindWeekly, Weekdays: []int{1, 2, 3, 4, 5}, Hour: 9}, at(2026, 10, 9, 10, 0), at(2026, 10, 9, 9, 0), at(2026, 10, 12, 9, 0)},
		"twice a day":          {Schedule{Kind: KindDaily, Times: []ClockTime{{17, 0}, {8, 0}}}, at(2026, 10, 6, 12, 0), at(2026, 10, 6, 8, 0), at(2026, 10, 6, 17, 0)},
		"missed evening":       {Schedule{Kind: KindDaily, Times: []ClockTime{{8, 0}, {17, 0}}}, at(2026, 10, 6, 18, 0), at(2026, 10, 6, 8, 0), at(2026, 10, 6, 17, 0)},
		"31st in February":     {Schedule{Kind: KindMonthly, MonthDay: 31, Hour: 9}, at(2026, 2, 1, 0, 0), at(2026, 1, 31, 9, 0), at(2026, 2, 28, 9, 0)},
		"15th":                 {Schedule{Kind: KindMonthly, MonthDay: 15, Times: []ClockTime{{9, 0}, {21, 30}}}, at(2026, 10, 15, 10, 0), at(2026, 10, 15, 9, 0), at(2026, 10, 15, 21, 30)},
		"cron weekdays":        {Schedule{Kind: KindCron, Cron: "0 9 * * MON-FRI"}, at(2026, 10, 10, 10, 0), at(2026, 10, 9, 9, 0), at(2026, 10, 12, 9, 0)},
		"cron every 15":        {Schedule{Kind: KindCron, Cron: "*/15 8-9 * * *"}, at(2026, 10, 6, 9, 50), at(2026, 10, 6, 9, 45), at(2026, 10, 7, 8, 0)},
		"cron leap day":        {Schedule{Kind: KindCron, Cron: "0 12 29 2 *"}, at(2026, 3, 1, 0, 0), at(2026, 1, 1, 0, 0), at(2028, 2, 29, 12, 0)},
		"cron @daily":          {Schedule{Kind: KindCron, Cron: "@daily"}, at(2026, 10, 6, 10, 0), at(2026, 10, 6, 0, 0), at(2026, 10, 7, 0, 0)},
		// Both restricted: either day runs, as in cron.
		"cron day or weekday": {Schedule{Kind: KindCron, Cron: "0 9 1 * 1"}, at(2026, 10, 6, 10, 0), at(2026, 10, 5, 9, 0), at(2026, 10, 12, 9, 0)},
	} {
		tc.s.TimeZone = "America/Juneau"
		if err := tc.s.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		got, ok, err := tc.s.NextRun(created, tc.last, tc.now)
		if err != nil || !ok || !got.Equal(tc.want.UTC()) {
			t.Errorf("%s: next = %s (%v, %v), want %s", name, got.In(juneau), ok, err, tc.want)
		}
	}
}

func TestRicherSchedulesValidate(t *testing.T) {
	many := make([]ClockTime, 25)
	for i := range many {
		many[i] = ClockTime{Hour: i % 24, Minute: i}
	}
	for name, s := range map[string]Schedule{
		"weekday 7":     {Kind: KindWeekly, Weekdays: []int{1, 7}},
		"no weekdays":   {Kind: KindWeekly},
		"hour 24":       {Kind: KindDaily, Times: []ClockTime{{24, 0}}},
		"25 times":      {Kind: KindDaily, Times: many},
		"month day 0":   {Kind: KindMonthly},
		"month day 32":  {Kind: KindMonthly, MonthDay: 32},
		"cron fields":   {Kind: KindCron, Cron: "0 9 * *"},
		"cron range":    {Kind: KindCron, Cron: "0 25 * * *"},
		"cron never":    {Kind: KindCron, Cron: "0 0 31 2 *"},
		"cron bad step": {Kind: KindCron, Cron: "*/0 * * * *"},
	} {
		s.TimeZone = "UTC"
		if s.Validate() == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Older clients read the first day and time; newer ones the lists.
func TestNormalizedKeepsBothForms(t *testing.T) {
	old := 3
	s := Schedule{Kind: KindWeekly, Weekday: &old, Hour: 9, Minute: 30}.Normalized()
	if len(s.Weekdays) != 1 || s.Weekdays[0] != 3 || len(s.Times) != 1 || s.Times[0] != (ClockTime{9, 30}) {
		t.Fatalf("old = %+v", s)
	}
	s = Schedule{Kind: KindWeekly, Weekdays: []int{5, 1, 1}, Times: []ClockTime{{17, 0}, {8, 15}}}.Normalized()
	if *s.Weekday != 1 || len(s.Weekdays) != 2 || s.Hour != 8 || s.Minute != 15 {
		t.Fatalf("new = %+v", s)
	}
	if c := (Schedule{Kind: KindDaily, Cron: "x", MonthDay: 4}).Normalized(); c.Cron != "" || c.MonthDay != 0 {
		t.Fatalf("daily kept other kinds' fields: %+v", c)
	}
}
