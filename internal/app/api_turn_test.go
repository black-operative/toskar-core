package app

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestAPITurnOptions(t *testing.T) {
	profile := contracts.AIProfile{Tools: []contracts.ToolPolicy{
		{ToolID: "internet.search", Policy: tools.PolicyAllow},
		{ToolID: "filesystem.write", Policy: tools.PolicyAllow},
		{ToolID: "terminal", Policy: tools.PolicyAsk},
	}}
	ids := func(p contracts.AIProfile) string {
		var out []string
		for _, t := range p.Tools {
			out = append(out, t.ToolID+"="+t.Policy)
		}
		return strings.Join(out, ",")
	}
	if got := ids(narrowTools(profile, &turnopts.Options{ReadOnlyTools: true})); got != "internet.search=allow" {
		t.Errorf("read only = %s", got)
	}
	if got := ids(narrowTools(profile, &turnopts.Options{Tools: []string{"terminal", "git.push"}})); got != "terminal=ask" {
		t.Errorf("narrowed = %s; a request must not add tools or loosen policy", got)
	}
	if got := ids(narrowTools(profile, &turnopts.Options{Tools: []string{}})); got != "" {
		t.Errorf("none = %s", got)
	}
	if got := ids(narrowTools(profile, &turnopts.Options{})); got != ids(profile) {
		t.Errorf("unchanged = %s", got)
	}

	var seen []string
	opts := &turnopts.Options{
		History:  []pluginapi.ChatMessage{{Role: "user", Content: "Earlier question"}, {Role: "assistant", Content: "Earlier answer"}},
		System:   "Reply in French.",
		Progress: func(eventType string, _ map[string]any) { seen = append(seen, eventType) },
	}
	env := &chatExecEnv{app: &App{Bus: events.NewBus(8)}, opts: opts}
	if prior := env.PriorMessages(context.Background()); len(prior) != 2 || prior[1].Content != "Earlier answer" {
		t.Fatalf("prior = %+v", prior)
	}
	if instr := env.TurnInstructions(context.Background(), "q"); !strings.Contains(instr, "Instructions from the application using the API:\nReply in French.") {
		t.Fatalf("instructions = %q", instr)
	}
	env.Emit("chat.lookup", map[string]any{"query": "x"})
	if len(seen) != 1 || seen[0] != "chat.lookup" {
		t.Fatalf("progress = %v", seen)
	}
	// Ordinary chat has no options and keeps its own history.
	if prior := (&chatExecEnv{}).PriorMessages(context.Background()); prior != nil {
		t.Fatalf("chat without a conversation = %+v", prior)
	}
}
