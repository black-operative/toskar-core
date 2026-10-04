// Package quality runs Yggdrasil's quality test set (spec §64): a fixed set
// of representative requests, each with the behavior it must have. By
// default it runs against the stub model, in-process, with the model's
// replies scripted, so CI checks routing, retrieval, planning, checking,
// approvals, and history on every change. With TOSKAR_QUALITY_URL set
// to a running daemon, the same cases run against real models, so defaults
// can be changed with evidence.
package quality

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/yeixio/yggdrasil-core/internal/config"
	"github.com/yeixio/yggdrasil-core/internal/huginn"
	"github.com/yeixio/yggdrasil-core/internal/runlog"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

// Case is one request and the behavior it must have.
type Case struct {
	ID       string                  `json:"id"`
	What     string                  `json:"what"`
	Setup    Setup                   `json:"setup"`
	History  []pluginapi.ChatMessage `json:"history"`
	Message  string                  `json:"message"`
	Stub     []string                `json:"stub"`
	StubOnly bool                    `json:"stub_only"`
	Expect   Expect                  `json:"expect"`
}

// Setup is what a case needs before the request.
type Setup struct {
	Knowledge []struct {
		Filename string `json:"filename"`
		Text     string `json:"text"`
	} `json:"knowledge"`
	// Tools sets tool policies on the case's profile.
	Tools map[string]string `json:"tools"`
}

// Expect is the behavior a case checks. Empty fields are not checked.
type Expect struct {
	Effort         string   `json:"effort"`
	Plan           *bool    `json:"plan"`
	MinWorkers     int      `json:"min_workers"`
	Lookup         *bool    `json:"lookup"`
	NoToolsRun     bool     `json:"no_tools_run"`
	Sources        []string `json:"sources"`
	Verified       bool     `json:"verified"`
	NoticeMatches  string   `json:"notice_matches"`
	StepsMatch     string   `json:"steps_match"`
	ApprovalFor    []string `json:"approval_for"`
	NotRun         []string `json:"not_run"`
	PromptContains []string `json:"prompt_contains"`
	AnswerMatches  string   `json:"answer_matches"`
	// AnswerRealOnly checks AnswerMatches only against a real model; the
	// stub's scripted answer proves nothing.
	AnswerRealOnly bool `json:"answer_real_only"`
	// NoFalseClaims requires an answer that says it changed something,
	// when nothing that changes things ran, to carry a notice saying so.
	NoFalseClaims bool `json:"no_false_claims"`
}

// Result is what a request did.
type Result struct {
	Answer string
	Meta   *contracts.MessageMeta
	Run    *runlog.Run
	// Events are event types in order, with their payloads.
	Events []Event
	// Prompts are what the model was sent (stub only).
	Prompts [][]pluginapi.ChatMessage
}

// Event is one event during a request.
type Event struct {
	Type    string
	Payload map[string]any
}

// Driver runs a case against a model.
type Driver interface {
	Name() string
	Run(t *testing.T, c Case) Result
}

func loadCases(t *testing.T) []Case {
	t.Helper()
	raw, err := os.ReadFile("cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Cases []Case `json:"cases"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	return file.Cases
}

func TestQualitySet(t *testing.T) {
	var d Driver = stubDriver{}
	if url := config.Env("QUALITY_URL"); url != "" {
		d = realDriver{base: strings.TrimRight(url, "/")}
	}
	for _, c := range loadCases(t) {
		t.Run(c.ID, func(t *testing.T) {
			if c.StubOnly && d.Name() != "stub" {
				t.Skip("checks a scripted reply")
			}
			r := d.Run(t, c)
			check(t, d.Name(), c, r)
		})
	}
}

func (r Result) has(eventType string) bool {
	for _, e := range r.Events {
		if e.Type == eventType {
			return true
		}
	}
	return false
}

func (r Result) toolIDs(eventType string) []string {
	var out []string
	for _, e := range r.Events {
		if e.Type == eventType {
			if id, _ := e.Payload["tool_id"].(string); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

func check(t *testing.T, driver string, c Case, r Result) {
	t.Helper()
	x := c.Expect
	fail := func(format string, args ...any) {
		t.Helper()
		t.Errorf("%s (%s): "+format, append([]any{c.What, driver}, args...)...)
	}
	if x.Effort != "" && (r.Run == nil || r.Run.Effort != x.Effort) {
		fail("effort = %v, want %s", runField(r, func(run *runlog.Run) any { return run.Effort }), x.Effort)
	}
	if x.Plan != nil {
		planned := r.has("plan.created")
		if planned != *x.Plan {
			fail("planned = %v", planned)
		}
	}
	if x.MinWorkers > 0 && (r.Run == nil || r.Run.Workers < x.MinWorkers) {
		fail("workers = %v, want at least %d", runField(r, func(run *runlog.Run) any { return run.Workers }), x.MinWorkers)
	}
	if x.Lookup != nil && r.has("chat.lookup") != *x.Lookup {
		fail("looked up = %v", r.has("chat.lookup"))
	}
	if ran := r.toolIDs("tool.started"); x.NoToolsRun && len(ran) > 0 {
		fail("tools ran: %v", ran)
	}
	for _, kind := range x.Sources {
		if r.Meta == nil || !slices.ContainsFunc(r.Meta.Sources, func(s contracts.Citation) bool { return s.Kind == kind }) {
			fail("no %s source in %+v", kind, metaSources(r))
		}
	}
	if x.Verified && !r.has("verify.done") && (r.Run == nil || r.Run.VerificationPasses == 0) {
		fail("answer was not checked")
	}
	if x.NoticeMatches != "" && (r.Meta == nil || !regexp.MustCompile(x.NoticeMatches).MatchString(r.Meta.Notice)) {
		fail("notice = %q", metaNotice(r))
	}
	if x.StepsMatch != "" {
		re := regexp.MustCompile(x.StepsMatch)
		if r.Meta == nil || !slices.ContainsFunc(r.Meta.Steps, func(s contracts.ActivityStep) bool { return re.MatchString(s.Text) }) {
			fail("no step matches %q", x.StepsMatch)
		}
	}
	// A real model may never try the action; then there is nothing to approve.
	if driver == "stub" {
		for _, id := range x.ApprovalFor {
			if !slices.Contains(r.toolIDs("tool.requested"), id) {
				fail("%s did not ask first (requested: %v)", id, r.toolIDs("tool.requested"))
			}
		}
	}
	for _, id := range x.NotRun {
		if slices.Contains(r.toolIDs("tool.started"), id) {
			fail("%s ran without approval", id)
		}
	}
	if driver == "stub" {
		for _, want := range x.PromptContains {
			if !promptHas(r.Prompts, want) {
				fail("the model never saw %q", want)
			}
		}
	}
	if x.NoFalseClaims && huginn.ClaimsAction(r.Answer) && len(r.toolIDs("tool.started")) == 0 &&
		(r.Meta == nil || !strings.Contains(strings.ToLower(r.Meta.Notice), "nothing was changed")) {
		fail("answer claims a change that never happened, with no notice: %q", r.Answer)
	}
	if x.AnswerMatches != "" && (driver != "stub" || !x.AnswerRealOnly) {
		if !regexp.MustCompile(x.AnswerMatches).MatchString(r.Answer) {
			fail("answer %q does not match %q", r.Answer, x.AnswerMatches)
		}
	}
}

func promptHas(prompts [][]pluginapi.ChatMessage, want string) bool {
	for _, p := range prompts {
		for _, m := range p {
			if strings.Contains(m.Content, want) {
				return true
			}
		}
	}
	return false
}

func runField(r Result, f func(*runlog.Run) any) any {
	if r.Run == nil {
		return "(no run)"
	}
	return f(r.Run)
}

func metaSources(r Result) any {
	if r.Meta == nil {
		return nil
	}
	return r.Meta.Sources
}

func metaNotice(r Result) string {
	if r.Meta == nil {
		return ""
	}
	return r.Meta.Notice
}
