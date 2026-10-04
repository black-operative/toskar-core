package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/automations"
	"github.com/yeixio/yggdrasil-core/internal/config"
)

const automationsUsage = `usage: toskarctl automations <list|get|create|update|delete|run|pause|resume>
  list
  get <id>
  create --name <name> --prompt <text> --profile <id> --model <id> --schedule <once|daily|weekly|interval> [--at <time>] [--every <duration>] [--weekday <0-6>] [--zone <tz>] [--tool <id>] [--notify <mode>] [--disabled]
  update <id> [--name <name>] [--prompt <text>] [--profile <id>] [--model <id>] [--schedule ...] [--notify <mode>]
  delete <id>
  run <id>
  pause <id>
  resume <id>
Times: daily and weekly use HH:MM. once uses RFC3339. interval uses Go durations such as 6h.
The daemon address is TOSKAR_URL, or 127.0.0.1:7331. A remote daemon uses TOSKAR_API_KEY.`

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
	case "create":
		body, err := automationBody(args[1:], true)
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
		body, err := automationBody(args[2:], false)
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
		return writeJSON(out, run)
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

func automationBody(args []string, create bool) (any, error) {
	fs := flag.NewFlagSet("automations", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("name", "", "automation name")
	prompt := fs.String("prompt", "", "prompt to run")
	profile := fs.String("profile", "", "profile id")
	model := fs.String("model", "", "installed model id")
	schedule := fs.String("schedule", "", "once, daily, weekly, or interval")
	at := fs.String("at", "", "HH:MM or RFC3339")
	every := fs.String("every", "", "interval duration, such as 6h")
	weekday := fs.Int("weekday", 0, "0-6, Sunday is 0")
	zone := fs.String("zone", "UTC", "IANA time zone")
	notify := fs.String("notify", "always", "always, condition, change, failure, or none")
	kind := fs.String("condition-kind", "", "threshold, available, or significant")
	op := fs.String("condition-op", "", "below or above")
	value := fs.Float64("condition-value", 0, "threshold value")
	disabled := fs.Bool("disabled", false, "create the automation paused")
	var tools stringList
	fs.Var(&tools, "tool", "tool id allowed for this automation, repeatable")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { seen[f.Name] = true })

	if create {
		if strings.TrimSpace(*name) == "" || strings.TrimSpace(*prompt) == "" || strings.TrimSpace(*profile) == "" || strings.TrimSpace(*model) == "" {
			return nil, fmt.Errorf("create requires --name, --prompt, --profile, and --model")
		}
		sched, err := buildSchedule(*schedule, *at, *every, *zone, weekday, seen["weekday"])
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
	if seen["schedule"] || seen["at"] || seen["every"] || seen["weekday"] || seen["zone"] {
		if !seen["schedule"] {
			return nil, fmt.Errorf("pass --schedule to change the schedule")
		}
		sched, err := buildSchedule(*schedule, *at, *every, *zone, weekday, seen["weekday"])
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
	if patch == (automations.Patch{}) {
		return nil, fmt.Errorf("update needs at least one change")
	}
	return patch, nil
}

func buildSchedule(kind, at, every, zone string, weekday *int, weekdaySet bool) (automations.Schedule, error) {
	sched := automations.Schedule{Kind: automations.Kind(kind), TimeZone: zone}
	switch sched.Kind {
	case automations.KindDaily, automations.KindWeekly:
		if at == "" {
			return automations.Schedule{}, fmt.Errorf("--at HH:MM is required")
		}
		parsed, err := time.Parse("15:04", at)
		if err != nil {
			return automations.Schedule{}, fmt.Errorf("--at must be HH:MM for a daily or weekly schedule")
		}
		sched.Hour = parsed.Hour()
		sched.Minute = parsed.Minute()
		if sched.Kind == automations.KindWeekly {
			if !weekdaySet {
				return automations.Schedule{}, fmt.Errorf("weekly schedule requires --weekday 0-6")
			}
			day := *weekday
			sched.Weekday = &day
		}
	case automations.KindOnce:
		when, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return automations.Schedule{}, fmt.Errorf("--at must be RFC3339 for a one-time schedule")
		}
		sched.At = &when
	case automations.KindInterval:
		d, err := time.ParseDuration(every)
		if err != nil || d < time.Second {
			return automations.Schedule{}, fmt.Errorf("--every must be a duration of at least 1s, such as 6h")
		}
		sched.EverySeconds = int(d / time.Second)
	default:
		return automations.Schedule{}, fmt.Errorf("--schedule must be once, daily, weekly, or interval")
	}
	return sched, nil
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
