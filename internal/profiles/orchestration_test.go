package profiles

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestOrchestrationControls(t *testing.T) {
	for _, bad := range []contracts.OrchestrationPolicy{
		{Effort: "maximum"}, {Planning: "maybe"}, {Verification: "twice"}, {MaxWorkers: 1}, {MaxWorkers: 20},
		{MaxToolCalls: 99}, {TimeoutSeconds: 5}, {ContextShare: 0.95}, {Memory: "on"}, {Strategy: "swarm"},
		{FallbackModels: []string{""}},
	} {
		if err := ValidateOrchestration(bad); err == nil || !strings.HasPrefix(err.Error(), "orchestration.") {
			t.Errorf("%+v accepted (%v)", bad, err)
		}
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	m := NewManager(db.SQL)
	ctx := context.Background()
	o := contracts.OrchestrationPolicy{Strategy: StrategyTeam, Effort: "thorough", Planning: "always", MaxWorkers: 4, Parallel: "off",
		Verification: "correct", MaxToolCalls: 6, Memory: "off", ContextShare: 0.3, Fallback: "off",
		FallbackModels: []string{"qwen2.5-7b", "llama3.2-3b"}, TimeoutSeconds: 120}
	p, err := m.Create(ctx, Profile{Name: "Careful", OrchestratorID: "simple", Orchestration: o})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Orchestration, o) {
		t.Fatalf("stored %+v", p.Orchestration)
	}
	p.Orchestration = contracts.OrchestrationPolicy{}
	if err := m.Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Get(ctx, p.ID)
	if !reflect.DeepEqual(got.Orchestration, contracts.OrchestrationPolicy{}) {
		t.Fatalf("cleared = %+v", got.Orchestration)
	}
	if _, err := m.Create(ctx, Profile{Name: "Bad", OrchestratorID: "simple", Orchestration: contracts.OrchestrationPolicy{Effort: "max"}}); err == nil {
		t.Fatal("invalid controls saved")
	}
}

// A profile saved for the old Team orchestrator moves onto the main pipeline
// with the Team strategy, keeping its roles and models (§37).
func TestTeamProfilesMigrate(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	// Written the way older versions stored a Team profile.
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO profiles (id, name, purpose, orchestrator_id, node_policy_json, tools_json)
		VALUES ('crew', 'Crew', 'custom', 'team', '{"mode":"automatic"}', '[]')`); err != nil {
		t.Fatal(err)
	}
	for _, r := range [][2]string{{"coordinator", "big"}, {"worker", "mid"}, {"reviewer", "small"}} {
		if _, err := db.SQL.ExecContext(ctx, `INSERT INTO profile_roles (profile_id, role, model_id, required) VALUES ('crew', ?, ?, 1)`, r[0], r[1]); err != nil {
			t.Fatal(err)
		}
	}
	m := NewManager(db.SQL)
	if err := m.EnsurePresets(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(ctx, "crew")
	if err != nil {
		t.Fatal(err)
	}
	if got.OrchestratorID != "simple" || got.Orchestration.Strategy != StrategyTeam {
		t.Fatalf("migrated to %q / %q", got.OrchestratorID, got.Orchestration.Strategy)
	}
	if RoleModel(got, RolePlanner) != "big" || RoleModel(got, RoleWorker) != "mid" || RoleModel(got, RoleReviewer) != "small" {
		t.Fatalf("roles = %+v", got.Roles)
	}
	if RoleModel(got, "worker:3") != "mid" {
		t.Fatal("a worker slot does not use the worker model")
	}
	prog, _ := m.Get(ctx, PresetProgramming)
	if prog.OrchestratorID != "simple" || prog.Orchestration.Strategy != StrategyTeam {
		t.Fatalf("Programming = %q / %q", prog.OrchestratorID, prog.Orchestration.Strategy)
	}
	// The API still accepts the old id, and stores the new form.
	created, err := m.Create(ctx, Profile{Name: "Old client", OrchestratorID: "team", Roles: []contracts.ModelRole{{Role: "coordinator"}}})
	if err != nil {
		t.Fatal(err)
	}
	if created.OrchestratorID != "simple" || created.Orchestration.Strategy != StrategyTeam || created.Roles[0].Role != RolePlanner {
		t.Fatalf("created = %+v", created)
	}
}

func TestNodePolicyValidation(t *testing.T) {
	for _, bad := range []contracts.NodePolicy{
		{Mode: "everywhere"}, {Remote: "on"}, {PreferredNodes: []string{"a"}, DeniedNodes: []string{"a"}},
	} {
		if err := ValidateNodePolicy(bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	if err := ValidateNodePolicy(contracts.NodePolicy{Mode: "automatic", PreferredNodes: []string{"a"}, DeniedNodes: []string{"b"}, Remote: "off"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOrchestration(contracts.OrchestrationPolicy{Retries: 4}); err == nil {
		t.Fatal("4 retries accepted")
	}
}

// A built-in profile can be put back to how Yggdrasil ships it (§24).
func TestResetPreset(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	m := NewManager(db.SQL)
	if err := m.EnsurePresets(ctx); err != nil {
		t.Fatal(err)
	}
	p, _ := m.Get(ctx, PresetProgramming)
	p.Name = "My coding"
	p.Orchestration = contracts.OrchestrationPolicy{Strategy: StrategySingle}
	if err := m.Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := m.ResetPreset(ctx, PresetProgramming)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Programming" || got.Orchestration.Strategy != StrategyTeam {
		t.Fatalf("reset = %q / %q", got.Name, got.Orchestration.Strategy)
	}
	custom, _ := m.Create(ctx, Profile{Name: "Mine", OrchestratorID: "simple"})
	if _, err := m.ResetPreset(ctx, custom.ID); err == nil {
		t.Fatal("a custom profile was reset")
	}
}
