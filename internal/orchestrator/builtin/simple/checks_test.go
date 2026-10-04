package simple

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// recordingEnv is scriptedEnv that keeps what was emitted.
type recordingEnv struct {
	scriptedEnv
	emitted map[string]map[string]any
}

func (e *recordingEnv) Emit(eventType string, payload map[string]any) {
	if e.emitted == nil {
		e.emitted = map[string]map[string]any{}
	}
	e.emitted[eventType] = payload
}

const broken = "Here is the function:\n\n```go\nfunc add(a, b int) int {\n\treturn a + b\n```\n\nIt adds two numbers, which is all the question needed, so nothing more is done here."
const fixed = "Here is the function:\n\n```go\nfunc add(a, b int) int {\n\treturn a + b\n}\n```\n\nIt adds two numbers, which is all the question needed, so nothing more is done here."

func TestCheckCodeSendsErrorsBackOnce(t *testing.T) {
	env := &recordingEnv{scriptedEnv: scriptedEnv{replies: []string{fixed}}}
	got := checkCode(context.Background(), env, "assistant", nil, broken, 1)
	if got != fixed {
		t.Fatalf("answer = %q", got)
	}
	if ask := env.seen[0][len(env.seen[0])-1].Content; !strings.Contains(ask, "go block 1") {
		t.Fatalf("the errors weren't named: %q", ask)
	}
	ev := env.emitted[EventCodeChecked]
	if ev["issues"] != 1 || ev["fixed"] != 1 || len(ev["remaining"].([]string)) != 0 {
		t.Fatalf("event = %v", ev)
	}

	// No corrections: reported, not sent back.
	env = &recordingEnv{}
	if got := checkCode(context.Background(), env, "assistant", nil, broken, 0); got != broken || env.n != 0 {
		t.Fatalf("check only: %q, %d calls", got, env.n)
	}
	if ev := env.emitted[EventCodeChecked]; ev["issues"] != 1 || len(ev["remaining"].([]string)) != 1 {
		t.Fatalf("check-only event = %v", ev)
	}
	// An answer without code costs nothing.
	env = &recordingEnv{}
	if got := checkCode(context.Background(), env, "assistant", nil, "Just words.", 2); got != "Just words." || env.n != 0 || env.emitted != nil {
		t.Fatal("an answer without code was checked")
	}
	// A revision that is still broken is not kept.
	env = &recordingEnv{scriptedEnv: scriptedEnv{replies: []string{broken}}}
	if got := checkCode(context.Background(), env, "assistant", nil, broken, 2); got != broken || env.n != 1 {
		t.Fatalf("still broken: %d calls", env.n)
	}
}

func TestCheckConsistency(t *testing.T) {
	answer := strings.Repeat("The shop opens at 9 on weekdays. ", 14) + "The shop opens at 10 on weekdays."
	revised := strings.Repeat("The shop opens at 9 on weekdays. ", 15)
	env := &recordingEnv{scriptedEnv: scriptedEnv{replies: []string{
		`{"contradictions": ["opens at 9 and at 10 on weekdays"]}`,
		revised,
		`{"contradictions": []}`,
	}}}
	got := checkConsistency(context.Background(), env, "reviewer", []pluginapi.ChatMessage{{Role: "user", Content: "When does it open?"}}, answer, "")
	if got != strings.TrimSpace(revised) {
		t.Fatalf("answer = %q", got)
	}
	if ev := env.emitted[EventConsistency]; ev["found"] != 1 || ev["fixed"] != 1 {
		t.Fatalf("event = %v", ev)
	}
	// None found: one call, answer kept.
	env = &recordingEnv{scriptedEnv: scriptedEnv{replies: []string{`{"contradictions": []}`}}}
	if got := checkConsistency(context.Background(), env, "reviewer", nil, answer, ""); got != answer || env.n != 1 {
		t.Fatalf("clean: %d calls", env.n)
	}
	// A reply that isn't JSON counts as none, so it never blocks an answer.
	env = &recordingEnv{scriptedEnv: scriptedEnv{replies: []string{"I think it's fine."}}}
	if got := checkConsistency(context.Background(), env, "reviewer", nil, answer, ""); got != answer {
		t.Fatal("an unreadable check changed the answer")
	}
	// A short answer isn't checked, with or without sources.
	env = &recordingEnv{}
	if got := checkConsistency(context.Background(), env, "reviewer", nil, "Yes.", "The shop opens at 9."); got != "Yes." || env.n != 0 {
		t.Fatal("a short answer was checked")
	}
}

func TestCheckLinksSendsGuessedLinksBackOnce(t *testing.T) {
	guessed := "Install Go by following the steps at https://go.dev/doc/install-guide-2026, which covers every platform in detail."
	sourced := "Install Go by following the steps at https://go.dev/doc/install, which covers every platform in detail and more."
	evidence := "From https://go.dev/doc/install: download the installer."
	env := &recordingEnv{scriptedEnv: scriptedEnv{replies: []string{sourced}}}
	if got := checkLinks(context.Background(), env, "assistant", nil, guessed, evidence, 1); got != sourced {
		t.Fatalf("answer = %q", got)
	}
	if ask := env.seen[0][len(env.seen[0])-1].Content; !strings.Contains(ask, "install-guide-2026") {
		t.Fatalf("the link wasn't named: %q", ask)
	}
	if ev := env.emitted[EventLinksChecked]; ev["issues"] != 1 || ev["fixed"] != 1 || len(ev["remaining"].([]string)) != 0 {
		t.Fatalf("event = %v", ev)
	}
	// No corrections: reported, not sent back.
	env = &recordingEnv{}
	if got := checkLinks(context.Background(), env, "assistant", nil, guessed, evidence, 0); got != guessed || env.n != 0 {
		t.Fatalf("check only: %q, %d calls", got, env.n)
	}
	if ev := env.emitted[EventLinksChecked]; len(ev["remaining"].([]string)) != 1 {
		t.Fatalf("check-only event = %v", ev)
	}
	// Links from the sources, or an answer without links, cost nothing.
	env = &recordingEnv{}
	if got := checkLinks(context.Background(), env, "assistant", nil, sourced, evidence, 2); got != sourced || env.n != 0 || env.emitted[EventLinksChecked]["issues"] != 0 {
		t.Fatalf("sourced link: %d calls, %v", env.n, env.emitted)
	}
	env = &recordingEnv{}
	if checkLinks(context.Background(), env, "assistant", nil, "Just words.", "", 2); env.n != 0 || env.emitted != nil {
		t.Fatal("an answer without links was checked")
	}
}
