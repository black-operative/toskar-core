// Package quality runs Toskar's quality test set (spec §64): a fixed set
// of representative requests, each with the behavior it must have. By
// default it runs against the stub model, in-process, with the model's
// replies scripted, so CI checks routing, retrieval, planning, checking,
// approvals, and history on every change. With TOSKAR_QUALITY_MODEL_URL
// set to an OpenAI-compatible server such as llama-server, the same
// in-process run sends every model call to that model, with the web still
// answered from web.json, so a release is checked against a real model
// with the same pages each time. With TOSKAR_QUALITY_URL set to a running
// daemon, the cases run against its real models and the live web.
//
// The iPhone app (yeixio/toskar-apps) runs the cases marked "phone"
// through its on-device chat, so its answers are held to the same
// expectations.
package quality

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Case is one request and the behavior it must have.
type Case struct {
	ID      string                  `json:"id"`
	What    string                  `json:"what"`
	Setup   Setup                   `json:"setup"`
	History []pluginapi.ChatMessage `json:"history"`
	Message string                  `json:"message"`
	Stub    []string                `json:"stub"`
	// StubQuery is what the stub writes when asked for a follow-up's web
	// search; without it, the message itself.
	StubQuery string `json:"stub_query"`
	StubOnly  bool   `json:"stub_only"`
	Expect    Expect `json:"expect"`
	// Platforms the case runs on: "core", "phone", or both. Empty is core.
	Platforms []string `json:"platforms"`
}

// On reports whether the case runs on platform.
func (c Case) On(platform string) bool {
	if len(c.Platforms) == 0 {
		return platform == "core"
	}
	return slices.Contains(c.Platforms, platform)
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
	// NoDeflection fails an answer that sends the person off to search,
	// or claims to browse, by the set's "deflection" pattern, instead of
	// answering: an answer that also matches AnswerMatches answered. A
	// scripted answer proves nothing, so the stub skips it.
	NoDeflection bool `json:"no_deflection"`
}

// caseFile is cases.json.
type caseFile struct {
	// Deflection matches answers that tell the person to find the answer
	// themselves. Core and the iPhone app check with the same pattern.
	Deflection string `json:"deflection"`
	Cases      []Case `json:"cases"`
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

func loadCases(t *testing.T) caseFile {
	t.Helper()
	raw, err := os.ReadFile("cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var file caseFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	return file
}

// minPass is the share of cases a real model must pass: every case for the
// stub, whose replies are scripted, and TOSKAR_QUALITY_MIN_PASS (default
// 0.9) for a real model, whose answers vary from run to run.
func minPass(driver string) float64 {
	if driver == "stub" {
		return 1
	}
	if v, err := strconv.ParseFloat(config.Env("QUALITY_MIN_PASS"), 64); err == nil && v > 0 && v <= 1 {
		return v
	}
	return 0.9
}

func TestQualitySet(t *testing.T) {
	var d Driver = stubDriver{}
	if url := config.Env("QUALITY_MODEL_URL"); url != "" {
		d = stubDriver{model: strings.TrimRight(url, "/")}
	}
	if url := config.Env("QUALITY_URL"); url != "" {
		d = realDriver{base: strings.TrimRight(url, "/")}
	}
	file := loadCases(t)
	deflection := regexp.MustCompile(file.Deflection)
	var report []reportRow
	for _, c := range file.Cases {
		if !c.On("core") {
			continue
		}
		t.Run(c.ID, func(t *testing.T) {
			if c.StubOnly && d.Name() != "stub" {
				t.Skip("checks a scripted reply")
			}
			r := d.Run(t, c)
			failures := check(d.Name(), c, r, deflection)
			report = append(report, reportRow{Case: c, Answer: r.Answer, Failures: failures})
			for _, f := range failures {
				// A real model is held to a pass rate, not every case.
				if d.Name() == "stub" {
					t.Error(f)
				} else {
					t.Log("FAIL: " + f)
				}
			}
		})
	}
	passed := 0
	for _, row := range report {
		if len(row.Failures) == 0 {
			passed++
		}
	}
	rate := 1.0
	if len(report) > 0 {
		rate = float64(passed) / float64(len(report))
	}
	t.Logf("quality: %d of %d cases passed (%.0f%%) with the %s model", passed, len(report), rate*100, d.Name())
	if path := config.Env("QUALITY_REPORT"); path != "" {
		if err := os.WriteFile(path, []byte(markdownReport(d.Name(), report, rate, minPass(d.Name()))), 0o644); err != nil {
			t.Errorf("report: %v", err)
		}
	}
	if rate < minPass(d.Name()) {
		t.Errorf("%.0f%% of cases passed; at least %.0f%% must", rate*100, minPass(d.Name())*100)
	}
}

// reportRow is one case's outcome, for the report.
type reportRow struct {
	Case     Case
	Answer   string
	Failures []string
}

// markdownReport lists every case with its answer, failures first, so a
// release can be read and not only scored.
func markdownReport(driver string, rows []reportRow, rate, min float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Chat quality: core, %s model\n\n", driver)
	fmt.Fprintf(&b, "%.0f%% of %d cases passed; at least %.0f%% must.\n\n", rate*100, len(rows), min*100)
	sorted := slices.Clone(rows)
	slices.SortStableFunc(sorted, func(a, b reportRow) int { return len(b.Failures) - len(a.Failures) })
	for _, row := range sorted {
		mark := "PASS"
		if len(row.Failures) > 0 {
			mark = "FAIL"
		}
		fmt.Fprintf(&b, "## %s %s\n\n%s\n\n> %s\n\n", mark, row.Case.ID, row.Case.What, strings.ReplaceAll(row.Case.Message, "\n", " "))
		for _, f := range row.Failures {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		answer := strings.TrimSpace(row.Answer)
		if answer == "" {
			answer = "(no answer)"
		}
		fmt.Fprintf(&b, "\n```text\n%s\n```\n\n", answer)
	}
	return b.String()
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

func check(driver string, c Case, r Result, deflection *regexp.Regexp) []string {
	x := c.Expect
	var failures []string
	fail := func(format string, args ...any) {
		failures = append(failures, fmt.Sprintf("%s (%s): "+format, append([]any{c.What, driver}, args...)...))
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
	// A deflection fails an answer that points elsewhere instead of
	// answering: one with the facts it was asked for and a "visit their site
	// for more" is an answer.
	answered := x.AnswerMatches != "" && regexp.MustCompile(x.AnswerMatches).MatchString(r.Answer)
	if x.NoDeflection && driver != "stub" && !answered && deflection.MatchString(r.Answer) {
		fail("answer sends the person off to find it themselves: %q", deflection.FindString(r.Answer))
	}
	return failures
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
