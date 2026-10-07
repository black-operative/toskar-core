package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/structured"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// parseAutomation reads an automation request (#204): with the request
// words first, which are quick and predictable, and, when they find no
// schedule, with a model. An empty language is the App language, and an
// empty time zone this computer's.
func (a *App) parseAutomation(ctx context.Context, text, timeZone, language string) (automations.ParsedRequest, error) {
	if language == "" {
		language = a.appLanguage(ctx)
	}
	if timeZone == "" {
		timeZone = time.Now().Location().String()
	}
	now := time.Now()
	parsed, err := automations.ParseRequest(text, now, timeZone, language)
	if !errors.Is(err, automations.ErrRequestWhen) {
		return parsed, err
	}
	if read, ok := a.parseWithModel(ctx, text, now, timeZone, language); ok {
		return read, nil
	}
	return parsed, err
}

// modelRequestSchema is the JSON a model fills in for a request.
var modelRequestSchema = &structured.Schema{
	Type:     "object",
	Required: []string{"schedule", "task"},
	Properties: map[string]*structured.Schema{
		"name": {Type: "string"},
		"task": {Type: "string"},
		"schedule": {Type: "object", Required: []string{"kind"}, Properties: map[string]*structured.Schema{
			"kind":          {Type: "string", Enum: []any{"daily", "weekly", "monthly", "interval", "once", "cron"}},
			"hour":          {Type: "integer"},
			"minute":        {Type: "integer"},
			"weekday":       {Type: "integer"},
			"weekdays":      {Type: "array", Items: &structured.Schema{Type: "integer"}},
			"times":         {Type: "array", Items: &structured.Schema{Type: "string"}},
			"month_day":     {Type: "integer"},
			"every_minutes": {Type: "integer"},
			"date":          {Type: "string"},
			"cron":          {Type: "string"},
		}},
		"notify": {Type: "object", Properties: map[string]*structured.Schema{
			"mode": {Type: "string", Enum: []any{"always", "change", "none", "condition"}},
			"condition": {Type: "object", Properties: map[string]*structured.Schema{
				"kind":     {Type: "string", Enum: []any{"threshold", "available", "significant"}},
				"op":       {Type: "string", Enum: []any{"below", "above"}},
				"value":    {Type: "number"},
				"currency": {Type: "string"},
			}},
		}},
	},
}

// parseWithModel asks Auto's model to read a request the words couldn't,
// such as one that says "first thing on weekdays" or names its time in a
// way the words don't know. ok is false when it can't, or gives something
// that isn't a usable schedule.
func (a *App) parseWithModel(ctx context.Context, text string, now time.Time, timeZone, language string) (automations.ParsedRequest, bool) {
	if a.Profiles == nil || strings.TrimSpace(text) == "" {
		return automations.ParsedRequest{}, false
	}
	loc, err := time.LoadLocation(timeZone)
	if err != nil {
		return automations.ParsedRequest{}, false
	}
	profile, err := a.Profiles.Get(ctx, "general-assistant")
	if err != nil {
		return automations.ParsedRequest{}, false
	}
	choice, err := a.chooseAuto(ctx, text, false, language)
	if err != nil {
		return automations.ParsedRequest{}, false
	}
	env := &chatExecEnv{
		app:           a,
		ctx:           ctx,
		profile:       withChatModel(profile, choice.Model.ID),
		modelOverride: choice.Model.ID,
		trace:         &turnTrace{lang: language},
	}
	today := now.In(loc)
	ask := []pluginapi.ChatMessage{
		{Role: "system", Content: "You turn a request for a task that runs on a schedule into the JSON a program reads. Use only what the request says."},
		{Role: "user", Content: fmt.Sprintf(`Request: %q

Today is %s, %s, in %s.

Reply with only this JSON:
{"name": "a short name for the task, in %s",
 "task": "what to do each time it runs, without when or how to notify, in the request's language",
 "schedule": {"kind": "daily, weekly, monthly, interval, once, or cron", "hour": 0-23, "minute": 0-59,
   "times": ["HH:MM", ...] when it runs several times a day,
   "weekdays": [0-6, ...] for weekly (0 is Sunday; 1-5 for weekdays only), "month_day": 1-31 for monthly,
   "every_minutes": for interval, "date": "YYYY-MM-DD" for once, "cron": "five fields" only when nothing else fits},
 "notify": {"mode": "always, change, none, or condition",
   "condition": {"kind": "threshold, available, or significant", "op": "below or above", "value": a number, "currency": "an ISO 4217 code such as USD"}}}`,
			text, today.Weekday(), today.Format("2006-01-02"), timeZone, locale.LanguageName(language, locale.Source))},
	}
	reply, err := collectText(ctx, env, "assistant", ask)
	if err != nil {
		return automations.ParsedRequest{}, false
	}
	result := structured.Parse(reply, modelRequestSchema)
	if !result.OK() {
		return automations.ParsedRequest{}, false
	}
	obj, _ := result.Value.(map[string]any)
	read, ok := requestFromModel(obj, loc, timeZone)
	if !ok {
		return automations.ParsedRequest{}, false
	}
	read.Notes = append(read.Notes, locale.T(language, "automations:parse.byModel", nil))
	return read, true
}

// requestFromModel turns the model's JSON into an automation, refusing a
// schedule that couldn't run.
func requestFromModel(obj map[string]any, loc *time.Location, timeZone string) (automations.ParsedRequest, bool) {
	num := func(m map[string]any, key string) (int, bool) {
		v, ok := m[key].(float64)
		return int(v), ok
	}
	sched, _ := obj["schedule"].(map[string]any)
	if sched == nil {
		return automations.ParsedRequest{}, false
	}
	kind, _ := sched["kind"].(string)
	hour, _ := num(sched, "hour")
	minute, _ := num(sched, "minute")
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return automations.ParsedRequest{}, false
	}
	s := automations.Schedule{Kind: automations.Kind(kind), TimeZone: timeZone, Hour: hour, Minute: minute}
	switch s.Kind {
	case automations.KindWeekly:
		if days, ok := sched["weekdays"].([]any); ok && len(days) > 0 {
			for _, d := range days {
				v, ok := d.(float64)
				if !ok || v < 0 || v > 6 {
					return automations.ParsedRequest{}, false
				}
				s.Weekdays = append(s.Weekdays, int(v))
			}
		} else {
			day, ok := num(sched, "weekday")
			if !ok || day < 0 || day > 6 {
				return automations.ParsedRequest{}, false
			}
			s.Weekday = &day
		}
	case automations.KindMonthly:
		day, ok := num(sched, "month_day")
		if !ok {
			return automations.ParsedRequest{}, false
		}
		s.MonthDay = day
	case automations.KindCron:
		cron, _ := sched["cron"].(string)
		s.Cron, s.Hour, s.Minute = strings.TrimSpace(cron), 0, 0
	case automations.KindInterval:
		minutes, ok := num(sched, "every_minutes")
		if !ok || minutes < 1 {
			return automations.ParsedRequest{}, false
		}
		s.EverySeconds, s.Hour, s.Minute = minutes*60, 0, 0
	case automations.KindOnce:
		date, _ := sched["date"].(string)
		day, err := time.ParseInLocation("2006-01-02", date, loc)
		if err != nil {
			return automations.ParsedRequest{}, false
		}
		at := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, loc).UTC()
		s.At, s.Hour, s.Minute = &at, 0, 0
	case automations.KindDaily:
	default:
		return automations.ParsedRequest{}, false
	}
	// Several times a day, for a schedule that runs on days.
	if times, ok := sched["times"].([]any); ok && len(times) > 0 && (s.Kind == automations.KindDaily || s.Kind == automations.KindWeekly || s.Kind == automations.KindMonthly) {
		for _, v := range times {
			text, _ := v.(string)
			clock, err := time.Parse("15:04", strings.TrimSpace(text))
			if err != nil {
				return automations.ParsedRequest{}, false
			}
			s.Times = append(s.Times, automations.ClockTime{Hour: clock.Hour(), Minute: clock.Minute()})
		}
	}
	if s.Validate() != nil {
		return automations.ParsedRequest{}, false
	}
	s = s.Normalized()
	task := strings.TrimSpace(fmt.Sprint(obj["task"]))
	if task == "" || task == "<nil>" {
		return automations.ParsedRequest{}, false
	}
	name, _ := obj["name"].(string)
	read := automations.ParsedRequest{
		Name:         strings.TrimSpace(name),
		Prompt:       task,
		Schedule:     s,
		Notification: automations.Notification{Mode: automations.NotifyAlways},
		Notes:        []string{},
	}
	if read.Name == "" {
		read.Name = task
	}
	if notify, _ := obj["notify"].(map[string]any); notify != nil {
		n := automations.Notification{Mode: automations.NotifyMode(fmt.Sprint(notify["mode"]))}
		if c, _ := notify["condition"].(map[string]any); c != nil && n.Mode == automations.NotifyOnCondition {
			cond := &automations.Condition{Kind: fmt.Sprint(c["kind"])}
			if op, ok := c["op"].(string); ok {
				cond.Op = op
			}
			if v, ok := c["value"].(float64); ok {
				cond.Value = v
			}
			if cur, ok := c["currency"].(string); ok {
				cond.Currency = strings.ToUpper(cur)
			}
			n.Condition = cond
		}
		n.Normalize()
		if n.Validate() == nil {
			read.Notification = n
		}
	}
	return read, true
}
