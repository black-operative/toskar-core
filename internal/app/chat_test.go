package app

import (
	"testing"

	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A profile's roles keep their own models and computers; the chat's model
// writes the answer, and an old Team profile moves to the Team strategy.
func TestWithChatModelKeepsRoles(t *testing.T) {
	p := profiles.Profile{
		ID:             "programming",
		OrchestratorID: "team",
		NodePolicy:     contracts.NodePolicy{Mode: "prefer_local"},
		Roles: []contracts.ModelRole{
			{Role: "coordinator", ModelID: "", NodeID: "node-a"},
			{Role: "worker", ModelID: "special-worker", NodeID: "node-b"},
			{Role: "reviewer"},
		},
	}
	got := withChatModel(p, "ui-model")
	if got.OrchestratorID != "simple" || got.Orchestration.Strategy != profiles.StrategyTeam {
		t.Fatalf("orchestrator=%q strategy=%q", got.OrchestratorID, got.Orchestration.Strategy)
	}
	if got.NodePolicy.Mode != "prefer_local" {
		t.Fatalf("node policy=%q, want prefer_local (preserved)", got.NodePolicy.Mode)
	}
	byRole := map[string]contracts.ModelRole{}
	for _, r := range got.Roles {
		byRole[r.Role] = r
	}
	if byRole[profiles.RolePrimary].ModelID != "ui-model" {
		t.Fatalf("primary model=%q, want ui-model", byRole[profiles.RolePrimary].ModelID)
	}
	if byRole["worker"].ModelID != "special-worker" || byRole["worker"].NodeID != "node-b" {
		t.Fatalf("worker=%+v", byRole["worker"])
	}
	if byRole[profiles.RolePlanner].NodeID != "node-a" {
		t.Fatalf("planner pin lost: %+v", byRole[profiles.RolePlanner])
	}
	if _, kept := byRole["reviewer"]; kept {
		t.Fatal("a role with no model or computer should use the primary model")
	}
}

func TestWithChatModelCollapsesNonTeam(t *testing.T) {
	p := profiles.Profile{
		ID:             "general-assistant",
		OrchestratorID: "simple",
		Roles: []contracts.ModelRole{
			{Role: "assistant", ModelID: "old"},
		},
	}
	got := withChatModel(p, "ui-model")
	if got.OrchestratorID != "simple" {
		t.Fatalf("orchestrator=%q, want simple", got.OrchestratorID)
	}
	if len(got.Roles) != 1 || got.Roles[0].Role != "assistant" || got.Roles[0].ModelID != "ui-model" {
		t.Fatalf("roles=%v, want single assistant/ui-model", got.Roles)
	}
}

func TestProfileNeedsModelFill(t *testing.T) {
	empty := profiles.Profile{
		OrchestratorID: "simple",
		Orchestration:  contracts.OrchestrationPolicy{Strategy: profiles.StrategyTeam},
		Roles:          []contracts.ModelRole{{Role: "planner"}, {Role: "worker"}, {Role: "reviewer"}},
	}
	if !profileNeedsModelFill(empty) {
		t.Fatal("roles without models should need a model")
	}
	partial := empty
	partial.Roles = []contracts.ModelRole{{Role: "planner", ModelID: "a"}, {Role: "worker"}}
	if profileNeedsModelFill(partial) {
		t.Fatal("a role without a model uses another role's model")
	}
	simpleEmpty := profiles.Profile{
		OrchestratorID: "simple",
		Roles:          []contracts.ModelRole{{Role: "assistant"}},
	}
	if !profileNeedsModelFill(simpleEmpty) {
		t.Fatal("simple empty should need fill")
	}
}

func TestChatExecEnvModelForRolePrefersPinnedModel(t *testing.T) {
	env := &chatExecEnv{
		modelOverride: "ui-model",
		profile: profiles.Profile{
			Roles: []contracts.ModelRole{
				{Role: "planner", ModelID: "planner-model"},
				{Role: "worker", ModelID: "worker-model"},
			},
		},
	}
	if got := env.modelForRole("worker"); got != "worker-model" {
		t.Fatalf("worker=%q, want worker-model", got)
	}
	if got := env.modelForRole("worker:3"); got != "worker-model" {
		t.Fatalf("worker slot=%q, want worker-model", got)
	}
	if got := env.modelForRole("reviewer"); got != "ui-model" {
		t.Fatalf("missing role fallback=%q, want ui-model", got)
	}
}
