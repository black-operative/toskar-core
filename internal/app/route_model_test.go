package app

import (
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestRoutePrefersToolModelForWeather(t *testing.T) {
	chatOnly := toolModel("gemma", false, 100)
	capable := toolModel("qwen", true, 200)
	next, reason := routeToolCapableModel("automatic", "What's the weather in Juneau today?", "gemma", []contracts.Model{chatOnly, capable})
	if next != "qwen" || reason == "" {
		t.Fatalf("next=%s reason=%s", next, reason)
	}
}

func TestRouteKeepsToolModel(t *testing.T) {
	capable := toolModel("qwen", true, 200)
	next, reason := routeToolCapableModel("automatic", "What's the weather in Juneau today?", "qwen", []contracts.Model{capable})
	if next != "qwen" || reason != "" {
		t.Fatalf("next=%s reason=%s", next, reason)
	}
}

func TestRouteDoesNotOverrideLocalPlacement(t *testing.T) {
	chatOnly := toolModel("gemma", false, 100)
	capable := toolModel("qwen", true, 200)
	next, _ := routeToolCapableModel("local", "What's the weather in Juneau today?", "gemma", []contracts.Model{chatOnly, capable})
	if next != "gemma" {
		t.Fatalf("next=%s", next)
	}
}

func TestRouteSkipsLimitedWhenCompatibleExists(t *testing.T) {
	limited := toolModel("gemma", true, 100)
	limited.Capabilities.ToolCallSupport = "limited"
	capable := toolModel("qwen", true, 200)
	next, reason := routeToolCapableModel("automatic", "What's the weather in Juneau today?", "gemma", []contracts.Model{limited, capable})
	if next != "qwen" || reason == "" {
		t.Fatalf("next=%s reason=%s", next, reason)
	}
}

func toolModel(id string, tools bool, memory uint64) contracts.Model {
	return contracts.Model{
		ID:           id,
		Installed:    true,
		MemoryNeeded: memory,
		Capabilities: contracts.ModelCapabilities{ToolCalling: tools},
	}
}
