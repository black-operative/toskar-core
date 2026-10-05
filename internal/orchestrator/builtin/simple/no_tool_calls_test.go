package simple

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A model that cannot call tools (Gemma 2) still gets Toskar's own web
// look-up as reference material, and is not shown the tool protocol or
// given a tool loop.
func TestLookUpForAModelThatCannotCallTools(t *testing.T) {
	env := &searchPageEnv{scriptedEnv: scriptedEnv{replies: []string{
		`{"tool_call":{"id":"internet.search","args":{"query":"x"}}}`,
	}}}
	events, err := New().Run(context.Background(), contracts.Task{Prompt: "What's the weather in Juneau today?"}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "gemma-2-9b-q4"}},
		Tools: []contracts.ToolPolicy{
			{ToolID: "internet.search", Policy: "allow"},
			{ToolID: "internet.open", Policy: "allow"},
		},
		ModelCallsNoTools: true,
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	if len(env.calls) == 0 || env.calls[0] != "internet.search" {
		t.Fatalf("Toskar didn't look it up: calls=%v", env.calls)
	}
	searches := 0
	for _, c := range env.calls {
		if c == "internet.search" {
			searches++
		}
	}
	if searches != 1 {
		t.Errorf("the model's own tool call ran too: calls=%v", env.calls)
	}
	if len(env.seen) == 0 {
		t.Fatal("the model was never asked")
	}
	sys := env.seen[0][0].Content
	if strings.Contains(sys, "To call a tool") {
		t.Errorf("the model was shown the tool protocol:\n%s", sys)
	}
	if user := env.seen[0][len(env.seen[0])-1].Content; !strings.Contains(user, "Juneau") || !strings.Contains(user, "Web search results") {
		t.Errorf("the look-up isn't in the reference:\n%s", user)
	}
}
