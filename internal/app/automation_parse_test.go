package app

import (
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
)

// A model's reading of a request becomes an automation only when its
// schedule could run (#204).
func TestRequestFromModel(t *testing.T) {
	loc, _ := time.LoadLocation("America/Los_Angeles")
	read, ok := requestFromModel(map[string]any{
		"name":     "Weekday digest",
		"task":     "Summarize my unread email.",
		"schedule": map[string]any{"kind": "weekly", "weekday": float64(1), "hour": float64(7), "minute": float64(30)},
		"notify":   map[string]any{"mode": "condition", "condition": map[string]any{"kind": "threshold", "op": "below", "value": float64(500), "currency": "usd"}},
	}, loc, "America/Los_Angeles")
	if !ok || read.Schedule.Kind != automations.KindWeekly || *read.Schedule.Weekday != 1 || read.Schedule.Hour != 7 || read.Prompt != "Summarize my unread email." {
		t.Fatalf("read = %+v ok=%v", read, ok)
	}
	if c := read.Notification.Condition; c == nil || c.Currency != "USD" || c.Value != 500 {
		t.Fatalf("notification = %+v", read.Notification)
	}

	once, ok := requestFromModel(map[string]any{
		"task": "Remind me to call the shop.", "schedule": map[string]any{"kind": "once", "date": "2026-10-09", "hour": float64(15)},
	}, loc, "America/Los_Angeles")
	if !ok || once.Schedule.At == nil || !once.Schedule.At.Equal(time.Date(2026, 10, 9, 22, 0, 0, 0, time.UTC)) || once.Name == "" {
		t.Fatalf("once = %+v ok=%v", once, ok)
	}
	every, ok := requestFromModel(map[string]any{"task": "Check the queue.", "schedule": map[string]any{"kind": "interval", "every_minutes": float64(90)}}, loc, "UTC")
	if !ok || every.Schedule.EverySeconds != 5400 || every.Notification.Mode != automations.NotifyAlways {
		t.Fatalf("interval = %+v", every)
	}

	weekdays, ok := requestFromModel(map[string]any{"task": "Summarize the news.", "schedule": map[string]any{"kind": "weekly", "weekdays": []any{1.0, 2.0, 3.0, 4.0, 5.0}, "times": []any{"08:00", "17:30"}}}, loc, "UTC")
	if !ok || len(weekdays.Schedule.Weekdays) != 5 || len(weekdays.Schedule.Times) != 2 || weekdays.Schedule.Times[1].Minute != 30 {
		t.Fatalf("weekdays = %+v", weekdays.Schedule)
	}
	monthly, ok := requestFromModel(map[string]any{"task": "Pay rent.", "schedule": map[string]any{"kind": "monthly", "month_day": 1.0, "hour": 9.0}}, loc, "UTC")
	if !ok || monthly.Schedule.MonthDay != 1 || monthly.Schedule.Hour != 9 {
		t.Fatalf("monthly = %+v", monthly.Schedule)
	}
	if cron, ok := requestFromModel(map[string]any{"task": "x", "schedule": map[string]any{"kind": "cron", "cron": "0 9 * * 1-5"}}, loc, "UTC"); !ok || cron.Schedule.Cron != "0 9 * * 1-5" {
		t.Fatalf("cron = %+v", cron.Schedule)
	}

	for name, bad := range map[string]map[string]any{
		"no schedule":  {"task": "x"},
		"no task":      {"schedule": map[string]any{"kind": "daily", "hour": float64(8)}},
		"hour 25":      {"task": "x", "schedule": map[string]any{"kind": "daily", "hour": float64(25)}},
		"weekday 9":    {"task": "x", "schedule": map[string]any{"kind": "weekly", "weekday": float64(9)}},
		"no interval":  {"task": "x", "schedule": map[string]any{"kind": "interval"}},
		"bad date":     {"task": "x", "schedule": map[string]any{"kind": "once", "date": "next week"}},
		"unknown kind": {"task": "x", "schedule": map[string]any{"kind": "yearly"}},
		"no month day": {"task": "x", "schedule": map[string]any{"kind": "monthly"}},
		"bad cron":     {"task": "x", "schedule": map[string]any{"kind": "cron", "cron": "0 0 31 2 *"}},
		"day 7":        {"task": "x", "schedule": map[string]any{"kind": "weekly", "weekdays": []any{1.0, 7.0}}},
	} {
		if _, ok := requestFromModel(bad, loc, "UTC"); ok {
			t.Errorf("%s was accepted", name)
		}
	}
}
