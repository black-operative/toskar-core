package simple

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/structured"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func runWith(t *testing.T, env *planEnv, prompt string, o contracts.OrchestrationPolicy) string {
	t.Helper()
	events, err := New().Run(context.Background(), contracts.Task{Prompt: prompt}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		Tools: []contracts.ToolPolicy{
			{ToolID: "internet.search", Policy: "allow"},
			{ToolID: "internet.open", Policy: "allow"},
		},
		Orchestration: o,
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for evt := range events {
		if evt.Type == "agent.message" {
			text += evt.Content
		}
	}
	return text
}

// A profile's orchestration controls change how a request is worked
// through (§40).
func TestProfileOrchestrationControls(t *testing.T) {
	const compare = "Compare Ollama, llama.cpp, MLX and vLLM for running models"

	env := &planEnv{}
	runWith(t, env, compare, contracts.OrchestrationPolicy{MaxWorkers: 2, Parallel: "off"})
	p := env.payload(EventPlanCreated)
	if p == nil || len(p["steps"].([]string)) != 2 || p["parallel"] != false {
		t.Fatalf("plan = %+v", p)
	}

	env = &planEnv{}
	runWith(t, env, compare, contracts.OrchestrationPolicy{Planning: "off"})
	if env.payload(EventPlanCreated) != nil {
		t.Fatal("planned with planning off")
	}

	// With figures that do not match the material, a check normally runs;
	// verification off skips it.
	const prompt = "How many tires are in stock?"
	env = &planEnv{reference: "Inventory: 12 tires in stock.", replies: []string{"There are 18 tires in stock."}}
	runWith(t, env, prompt, contracts.OrchestrationPolicy{})
	if !slices.Contains(env.events, EventVerified) {
		t.Fatalf("default did not check: %v", env.events)
	}
	env = &planEnv{reference: "Inventory: 12 tires in stock.", replies: []string{"There are 18 tires in stock."}}
	text := runWith(t, env, prompt, contracts.OrchestrationPolicy{Verification: "off"})
	if slices.Contains(env.events, EventVerified) || slices.Contains(env.events, EventVerifying) || text != "There are 18 tires in stock." {
		t.Fatalf("verification off: events=%v text=%q", env.events, text)
	}
}

// A turn whose answer must be JSON makes one reply and returns it as is,
// even when it looks like a tool call, after looking up the web (§27).
func TestJSONOnlyTurn(t *testing.T) {
	env := &planEnv{replies: []string{`{"name": "internet.search", "moons": 146}`}}
	ctx := structured.WithSchema(context.Background(), []byte(`{"type":"object"}`))
	events, err := New().Run(ctx, contracts.Task{Prompt: "Compare Saturn, Jupiter and Mars moons today"}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		Tools: []contracts.ToolPolicy{{ToolID: "internet.search", Policy: "allow"}, {ToolID: "internet.open", Policy: "allow"}},
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for evt := range events {
		if evt.Type == "agent.message" {
			text += evt.Content
		}
	}
	if text != `{"name": "internet.search", "moons": 146}` || len(env.seen) != 1 {
		t.Fatalf("text=%q generations=%d", text, len(env.seen))
	}
	if env.payload(EventPlanCreated) != nil {
		t.Fatal("planned a JSON-only turn")
	}
	if sys := env.seen[0][0].Content; strings.Contains(sys, "tool_call") {
		t.Fatalf("tools offered: %q", sys)
	}
}
