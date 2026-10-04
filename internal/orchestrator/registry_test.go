package orchestrator

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/internal/orchestrator/builtin/simple"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// other is a second orchestrator for the registry to hold.
type other struct{}

func (other) ID() string          { return "other" }
func (other) DisplayName() string { return "Other" }
func (other) Capabilities() pluginapi.OrchestratorCapabilities {
	return pluginapi.OrchestratorCapabilities{}
}
func (other) ValidateProfile(context.Context, contracts.AIProfile) error { return nil }
func (other) Run(context.Context, contracts.Task, contracts.AIProfile, pluginapi.ExecutionEnvironment) (<-chan pluginapi.OrchestrationEvent, error) {
	return nil, nil
}

func TestRegistryReplacesWithoutDuplicating(t *testing.T) {
	registry := NewRegistry()
	registry.Register(simple.New())
	registry.Register(other{})
	registry.Register(simple.New())

	listed := registry.List()
	if len(listed) != 2 || listed[0].ID() != "simple" || listed[1].ID() != "other" {
		t.Fatalf("list=%v", ids(listed))
	}
	got, err := registry.Get("other")
	if err != nil || got.DisplayName() != "Other" {
		t.Fatalf("get=%v err=%v", got, err)
	}
	if _, err := registry.Get("missing"); err == nil {
		t.Fatal("missing orchestrator was returned")
	}
}

func ids(items []pluginapi.Orchestrator) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.ID()
	}
	return out
}
