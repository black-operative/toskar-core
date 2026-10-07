package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/config"
)

const automationsUsage = `usage: toskarctl automations <list|get|parse|create|update|delete|run|pause|resume|hook>
  list
  get <id>
  parse <request> [--zone <tz>] [--language <tag>]
  create --request <text> [--profile <id>] [--model <id>] [--zone <tz>] [any flag below to change what it read]
  create --name <name> --prompt <text> --profile <id> --model <id> --schedule <once|daily|weekly|monthly|interval|cron|manual> [--at <time>] [--every <duration>] [--weekday <days>] [--day <1-31>] [--cron <expr>] [--zone <tz>] [--tool <id>] [--notify <mode>] [--save-folder <path>] [--trigger <page|feed|folder|webhook|none> --trigger-url <url> | --trigger-path <path>] [--disabled]
  update <id> [--name <name>] [--prompt <text>] [--profile <id>] [--model <id>] [--schedule ...] [--notify <mode>]
  delete <id>
  run <id>
  pause <id>
  resume <id>
  hook <id>                print a new webhook link for an automation with --trigger webhook; the old one stops working
Times: daily, weekly, and monthly use HH:MM, or several such as 08:00,17:00. once uses RFC3339.
interval uses Go durations such as 6h. --weekday takes 0-6 (Sunday is 0) or names, several
such as 1,3,5 or mon-fri, or "weekdays". monthly runs on --day, or a month's last day when it
has fewer. cron takes five fields, such as --cron "0 9 * * 1-5".
The daemon address is TOSKAR_URL, or 127.0.0.1:7331. A remote daemon uses TOSKAR_API_KEY.`

// runPoll is how often `run` checks whether the run has finished.
var runPoll = time.Second

func automationsCommand(args []string, out io.Writer) error {
	base := strings.TrimRight(config.Env("URL"), "/")
	if base == "" {
		base = "http://" + config.DefaultConfig().APIAddr()
	}
	return runAutomations(args, daemonClient{
		base:   base,
		key:    config.Env("API_KEY"),
		client: &http.Client{},
	}, out)
}

type daemonClient struct {
	base   string
	key    string
	client *http.Client
}

func runAutomations(args []string, client daemonClient, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", automationsUsage)
	}
	switch args[0] {
	case "list":
		var items []automations.Automation
		if err := client.call(http.MethodGet, "/automations", nil, &items); err != nil {
			return err
		}
		return writeJSON(out, items)
	case "get":
		id, err := oneID(args[1:])
		if err != nil {
			return err
		}
		var detail automations.Detail
		if err := client.call(http.MethodGet, "/automations/"+id, nil, &detail); err != nil {
			return err
		}
		return writeJSON(out, detail)
	case "parse":
		fs := flag.NewFlagSet("parse", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		zone := fs.String("zone", "", "IANA time zone; empty is the daemon's")
		lang := fs.String("language", "", "language the request is written in besides English; empty is the App language")
		text, rest := splitRequest(args[1:])
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if text == "" {
			text = strings.Join(fs.Args(), " ")
		}
		parsed, err := parseRequest(client, text, *zone, *lang)
		if err != nil {
			return err
		}
		return writeJSON(out, parsed)
	case "create":
		body, err := automationBody(args[1:], true, func(text, zone string) (automations.ParsedRequest, error) {
			return parseRequest(client, text, zone, "")
		})
		if err != nil {
			return err
		}
		var created automations.Automation
		if err := client.call(http.MethodPost, "/automations", body, &created); err != nil {
			return err
		}
		return writeJSON(out, created)
	case "update":
		if len(args) < 2 {
			return fmt.Errorf("update requires an automation id")
		}
		body, err := automationBody(args[2:], false, nil)
		if err != nil {
			return err
		}
		var updated automations.Automation
		if err := client.call(http.MethodPatch, "/automations/"+args[1], body, &updated); err != nil {
			return err
		}
		return writeJSON(out, updated)
	case "delete":
		id, err := oneID(args[1:])
		if err != nil {
			return err
		}
		if err := client.call(http.MethodDelete, "/automations/"+id, nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(out, "deleted %s\n", id)
		return nil
	case "run":
		id, err := oneID(args[1:])
		if err != nil {
			return err
		}
		var run automations.Run
		if err := client.call(http.MethodPost, "/automations/"+id+"/run", map[string]any{}, &run); err != nil {
			return err
		}
		// The daemon answers once the run has started (#204); wait for how
		// it ends, as before.
		for run.Status == automations.RunClaimed || run.Status == automations.RunRunning {
			time.Sleep(runPoll)
			var detail automations.Detail
			if err := client.call(http.MethodGet, "/automations/"+id, nil, &detail); err != nil {
				return err
			}
			for _, h := range detail.History {
				if h.ID == run.ID {
					run = h
				}
			}
		}
		return writeJSON(out, run)
	case "hook":
		id, err := oneID(args[1:])
		if err != nil {
			return err
		}
		var link struct {
			Path string `json:"path"`
		}
		if err := client.call(http.MethodPost, "/automations/"+id+"/hook", map[string]any{}, &link); err != nil {
			return err
		}
		// Shown once: only its hash is kept (#204).
		_, err = fmt.Fprintln(out, strings.TrimRight(client.base, "/")+link.Path)
		return err
	case "pause", "resume":
		id, err := oneID(args[1:])
		if err != nil {
			return err
		}
		var updated automations.Automation
		if err := client.call(http.MethodPost, "/automations/"+id+"/"+args[0], map[string]any{}, &updated); err != nil {
			return err
		}
		return writeJSON(out, updated)
	default:
		return fmt.Errorf("%s", automationsUsage)
	}
}

func oneID(args []string) (string, error) {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return "", fmt.Errorf("an automation id is required")
	}
	return args[0], nil
}

// parseRequest reads a request on the daemon, the way the Automations page
// does (#204).
func parseRequest(client daemonClient, text, zone, lang string) (automations.ParsedRequest, error) {
	var parsed automations.ParsedRequest
	err := client.call(http.MethodPost, "/automations/parse", map[string]any{"text": text, "time_zone": zone, "language": lang}, &parsed)
	return parsed, err
}

// splitRequest takes the request text that comes before any flag.
func splitRequest(args []string) (string, []string) {
	var words []string
	for i, a := range args {
		if strings.HasPrefix(a, "-") {
			return strings.Join(words, " "), args[i:]
		}
		words = append(words, a)
	}
	return strings.Join(words, " "), nil
}

func automationBody(args []string, create bool, parse func(text, zone string) (automations.ParsedRequest, error)) (any, error) {
	fs := flag.NewFlagSet("automations", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	request := fs.String("request", "", "describe the automation, such as \"every morning at 8, tell me if the price is below $500\"")
	name := fs.String("name", "", "automation name")
	prompt := fs.String("prompt", "", "prompt to run")
	profile := fs.String("profile", "", "profile id")
	model := fs.String("model", "", "installed model id")
	schedule := fs.String("schedule", "", "once, daily, weekly, monthly, interval, cron, or manual (only when started)")
	at := fs.String("at", "", "HH:MM, several such as 08:00,17:00, or RFC3339")
	every := fs.String("every", "", "interval duration, such as 6h")
	weekday := fs.String("weekday", "", "days 0-6 (Sunday is 0) or names, such as 1,3,5, mon-fri, or weekdays")
	day := fs.Int("day", 0, "day of the month, 1-31")
	cron := fs.String("cron", "", "cron expression, such as \"0 9 * * 1-5\"")
	zone := fs.String("zone", "UTC", "IANA time zone")
	notify := fs.String("notify", "always", "always, condition, change, failure, or none")
	kind := fs.String("condition-kind", "", "threshold, available, or significant")
	op := fs.String("condition-op", "", "below or above")
	value := fs.Float64("condition-value", 0, "threshold value")
	disabled := fs.Bool("disabled", false, "create the automation paused")
	trigger := fs.String("trigger", "", "page, feed, or folder: run only when it changed, checked on the schedule; webhook: run when its link is called; none runs on the schedule again")
	triggerURL := fs.String("trigger-url", "", "the page or feed to watch")
	triggerPath := fs.String("trigger-path", "", "the folder or file to watch, in your home folder, such as ~/Documents/Invoices")
	saveFolder := fs.String("save-folder", "", "also save each result as a Markdown file in this folder, such as ~/Documents/Toskar; \"\" stops saving")
	var tools stringList
	fs.Var(&tools, "tool", "tool id allowed for this automation, repeatable")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { seen[f.Name] = true })

	if create && strings.TrimSpace(*request) != "" {
		if parse == nil {
			return nil, fmt.Errorf("--request needs the daemon")
		}
		readZone := ""
		if seen["zone"] {
			readZone = *zone
		}
		read, err := parse(*request, readZone)
		if err != nil {
			return nil, err
		}
		// What the request says, with the flags given changing it.
		in := automations.CreateInput{
			Name:         read.Name,
			Prompt:       read.Prompt,
			ProfileID:    "general-assistant",
			ModelID:      "auto",
			Schedule:     read.Schedule,
			Tools:        []string(tools),
			Notification: read.Notification,
		}
		if seen["name"] {
			in.Name = *name
		}
		if seen["prompt"] {
			in.Prompt = *prompt
		}
		if seen["profile"] {
			in.ProfileID = *profile
		}
		if seen["model"] {
			in.ModelID = *model
		}
		if seen["schedule"] {
			sched, err := buildSchedule(scheduleFlags{kind: *schedule, at: *at, every: *every, zone: *zone, weekdays: *weekday, day: *day, cron: *cron})
			if err != nil {
				return nil, err
			}
			in.Schedule = sched
		}
		if seen["notify"] {
			note, err := buildNotification(*notify, *kind, *op, *value)
			if err != nil {
				return nil, err
			}
			in.Notification = note
		}
		if seen["save-folder"] {
			in.SaveFolder = *saveFolder
		}
		if seen["trigger"] {
			in.Trigger = buildTrigger(*trigger, *triggerURL, *triggerPath)
		}
		if *disabled {
			off := false
			in.Enabled = &off
		}
		return in, nil
	}

	if create {
		if strings.TrimSpace(*name) == "" || strings.TrimSpace(*prompt) == "" || strings.TrimSpace(*profile) == "" || strings.TrimSpace(*model) == "" {
			return nil, fmt.Errorf("create requires --request, or --name, --prompt, --profile, and --model")
		}
		sched, err := buildSchedule(scheduleFlags{kind: *schedule, at: *at, every: *every, zone: *zone, weekdays: *weekday, day: *day, cron: *cron})
		if err != nil {
			return nil, err
		}
		note, err := buildNotification(*notify, *kind, *op, *value)
		if err != nil {
			return nil, err
		}
		in := automations.CreateInput{
			Name:         *name,
			Prompt:       *prompt,
			ProfileID:    *profile,
			ModelID:      *model,
			Schedule:     sched,
			Tools:        []string(tools),
			Notification: note,
			SaveFolder:   *saveFolder,
			Trigger:      buildTrigger(*trigger, *triggerURL, *triggerPath),
		}
		if *disabled {
			off := false
			in.Enabled = &off
		}
		return in, nil
	}

	patch := automations.Patch{}
	if seen["name"] {
		patch.Name = name
	}
	if seen["prompt"] {
		patch.Prompt = prompt
	}
	if seen["profile"] {
		patch.ProfileID = profile
	}
	if seen["model"] {
		patch.ModelID = model
	}
	if seen["tool"] {
		copied := []string(tools)
		patch.Tools = &copied
	}
	if seen["schedule"] || seen["at"] || seen["every"] || seen["weekday"] || seen["day"] || seen["cron"] || seen["zone"] {
		if !seen["schedule"] {
			return nil, fmt.Errorf("pass --schedule to change the schedule")
		}
		sched, err := buildSchedule(scheduleFlags{kind: *schedule, at: *at, every: *every, zone: *zone, weekdays: *weekday, day: *day, cron: *cron})
		if err != nil {
			return nil, err
		}
		patch.Schedule = &sched
	}
	if seen["notify"] || seen["condition-kind"] || seen["condition-op"] || seen["condition-value"] {
		note, err := buildNotification(*notify, *kind, *op, *value)
		if err != nil {
			return nil, err
		}
		patch.Notification = &note
	}
	if seen["save-folder"] {
		patch.SaveFolder = saveFolder
	}
	if seen["trigger"] {
		t := buildTrigger(*trigger, *triggerURL, *triggerPath)
		if t == nil {
			t = &automations.Trigger{}
		}
		patch.Trigger = t
	}
	if patch == (automations.Patch{}) {
		return nil, fmt.Errorf("update needs at least one change")
	}
	return patch, nil
}

type scheduleFlags struct {
	kind, at, every, zone, weekdays, cron string
	day                                   int
}

func buildSchedule(f scheduleFlags) (automations.Schedule, error) {
	sched := automations.Schedule{Kind: automations.Kind(f.kind), TimeZone: f.zone}
	switch sched.Kind {
	case automations.KindDaily, automations.KindWeekly, automations.KindMonthly:
		if f.at == "" {
			return automations.Schedule{}, fmt.Errorf("--at HH:MM is required")
		}
		for _, part := range strings.Split(f.at, ",") {
			parsed, err := time.Parse("15:04", strings.TrimSpace(part))
			if err != nil {
				return automations.Schedule{}, fmt.Errorf("--at must be HH:MM, or several such as 08:00,17:00, for a %s schedule", f.kind)
			}
			sched.Times = append(sched.Times, automations.ClockTime{Hour: parsed.Hour(), Minute: parsed.Minute()})
		}
		switch sched.Kind {
		case automations.KindWeekly:
			days, err := parseWeekdays(f.weekdays)
			if err != nil {
				return automations.Schedule{}, err
			}
			sched.Weekdays = days
		case automations.KindMonthly:
			if f.day < 1 || f.day > 31 {
				return automations.Schedule{}, fmt.Errorf("monthly schedule requires --day 1-31")
			}
			sched.MonthDay = f.day
		}
	case automations.KindManual:
	case automations.KindCron:
		if strings.TrimSpace(f.cron) == "" {
			return automations.Schedule{}, fmt.Errorf("cron schedule requires --cron, such as \"0 9 * * 1-5\"")
		}
		sched.Cron = f.cron
	case automations.KindOnce:
		when, err := time.Parse(time.RFC3339, f.at)
		if err != nil {
			return automations.Schedule{}, fmt.Errorf("--at must be RFC3339 for a one-time schedule")
		}
		sched.At = &when
	case automations.KindInterval:
		d, err := time.ParseDuration(f.every)
		if err != nil || d < time.Second {
			return automations.Schedule{}, fmt.Errorf("--every must be a duration of at least 1s, such as 6h")
		}
		sched.EverySeconds = int(d / time.Second)
	default:
		return automations.Schedule{}, fmt.Errorf("--schedule must be once, daily, weekly, monthly, interval, cron, or manual")
	}
	if err := sched.Validate(); err != nil {
		return automations.Schedule{}, err
	}
	return sched.Normalized(), nil
}

// buildTrigger reads --trigger, --trigger-url, and --trigger-path; none or nothing is no
// trigger (#204).
func buildTrigger(kind, url, path string) *automations.Trigger {
	if kind == "" || kind == "none" {
		return nil
	}
	return &automations.Trigger{Kind: kind, URL: url, Path: path}
}

var weekdayFlagNames = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

// parseWeekdays reads --weekday: numbers or names, lists and ranges, or
// "weekdays" for Monday to Friday.
func parseWeekdays(text string) ([]int, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil, fmt.Errorf("weekly schedule requires --weekday, such as 1, 1,3,5, or weekdays")
	}
	if text == "weekdays" {
		return []int{1, 2, 3, 4, 5}, nil
	}
	one := func(s string) (int, error) {
		s = strings.TrimSpace(s)
		if len(s) >= 3 {
			if v, ok := weekdayFlagNames[s[:3]]; ok {
				return v, nil
			}
		}
		v, err := strconv.Atoi(s)
		if err != nil || v < 0 || v > 6 {
			return 0, fmt.Errorf("--weekday %q is not a day: use 0-6 (Sunday is 0) or a name such as mon", s)
		}
		return v, nil
	}
	var days []int
	for _, part := range strings.Split(text, ",") {
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := one(lo)
		if err != nil {
			return nil, err
		}
		b := a
		if isRange {
			if b, err = one(hi); err != nil {
				return nil, err
			}
		}
		for d := a; ; d = (d + 1) % 7 {
			days = append(days, d)
			if d == b {
				break
			}
		}
	}
	return days, nil
}

func buildNotification(mode, kind, op string, value float64) (automations.Notification, error) {
	note := automations.Notification{Mode: automations.NotifyMode(mode)}
	if kind != "" {
		note.Condition = &automations.Condition{Kind: kind, Op: op, Value: value}
	}
	if err := note.Validate(); err != nil {
		return automations.Notification{}, err
	}
	return note, nil
}

func (c daemonClient) call(method, path string, body, dest any) error {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+"/api/v1"+path, payload)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	hc := c.client
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error.Message != "" {
			return fmt.Errorf("%s", apiErr.Error.Message)
		}
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	if dest == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return json.Unmarshal(data, dest)
}

func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}
