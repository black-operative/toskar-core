package simple

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A message about a connected service is answered from that service, fetched
// first, instead of from a web search (§32).
func TestConnectedServiceIsCheckedFirst(t *testing.T) {
	tools.SetConnected("homeassistant", []tools.Definition{
		{ID: "homeassistant.states", Name: "Check Home Assistant", Source: "connector:homeassistant", Risk: tools.RiskRead, DefaultPolicy: tools.PolicyAllow, Prefetch: true},
		{ID: "homeassistant.call", Name: "Control Home Assistant", Source: "connector:homeassistant", Risk: tools.RiskWrite, DefaultPolicy: tools.PolicyAsk},
	})
	t.Cleanup(func() { tools.SetConnected("homeassistant", nil) })
	profile := func(statesPolicy string) contracts.AIProfile {
		return contracts.AIProfile{
			Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
			Tools: []contracts.ToolPolicy{
				{ToolID: "internet.search", Policy: "allow"},
				{ToolID: "internet.open", Policy: "allow"},
				{ToolID: "homeassistant.states", Policy: statesPolicy},
				{ToolID: "homeassistant.call", Policy: "ask"},
			},
		}
	}

	env := &searchPageEnv{scriptedEnv: scriptedEnv{replies: []string{"The kitchen light is on."}}}
	events, err := New().Run(context.Background(), contracts.Task{Prompt: "Which lights are on in my house right now?"}, profile("allow"), env)
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	if len(env.calls) != 1 || env.calls[0] != "homeassistant.states" {
		t.Fatalf("calls = %v; want the service checked and no web search", env.calls)
	}
	sys, user := env.seen[0][0].Content, env.seen[0][len(env.seen[0])-1].Content
	if !strings.Contains(sys, "already fetched current data") || strings.Contains(sys, "homeassistant.") {
		t.Fatalf("system = %q", sys)
	}
	if !strings.Contains(user, "Current data from Check Home Assistant") {
		t.Fatalf("user = %q", user)
	}

	// When reading the service asks first, nothing is fetched up front.
	env = &searchPageEnv{scriptedEnv: scriptedEnv{replies: []string{"ok"}}}
	events, _ = New().Run(context.Background(), contracts.Task{Prompt: "Which lights are on in my house right now?"}, profile("ask"), env)
	for range events {
	}
	for _, c := range env.calls {
		if c == "homeassistant.states" {
			t.Fatalf("fetched without permission: %v", env.calls)
		}
	}
}

func TestReadableServiceData(t *testing.T) {
	got := readable(map[string]any{
		"matched": 2,
		"devices": []any{
			map[string]any{"entity_id": "light.porch", "name": "Porch light", "state": "off"},
			map[string]any{"entity_id": "sensor.porch_temperature", "name": "Porch temperature", "state": "12.5", "unit": "°C"},
		},
	})
	want := "devices:\n- Porch light (light.porch): off\n- Porch temperature (sensor.porch_temperature): 12.5 °C\nmatched: 2\n"
	if got != want {
		t.Fatalf("readable = %q", got)
	}
}
