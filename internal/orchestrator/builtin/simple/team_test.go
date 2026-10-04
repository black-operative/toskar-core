package simple

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// teamEnv answers by role, places each worker slot on its own computer,
// and records which roles ran and how many ran at once.
type teamEnv struct {
	mu       sync.Mutex
	plan     string
	review   string
	calls    []string
	running  int
	most     int
	nodes    map[string]string
	prompts  map[string][]string
	events   []string
	payloads []map[string]any
}

func (e *teamEnv) Generate(ctx context.Context, role string, messages []pluginapi.ChatMessage) (<-chan pluginapi.ChatChunk, error) {
	e.mu.Lock()
	e.calls = append(e.calls, role)
	if e.prompts == nil {
		e.prompts = map[string][]string{}
	}
	e.prompts[profiles.BaseRole(role)] = append(e.prompts[profiles.BaseRole(role)], messages[len(messages)-1].Content)
	e.running++
	e.most = max(e.most, e.running)
	e.mu.Unlock()
	// Long enough for parts on different computers to overlap.
	time.Sleep(20 * time.Millisecond)
	e.mu.Lock()
	e.running--
	e.mu.Unlock()

	content := "A full answer about brewing coffee at home, covering beans, grinding, and water."
	switch {
	case strings.HasPrefix(messages[0].Content, "You plan how"):
		// The planner, or the answering model when no planner is set.
		content = e.plan
	case profiles.BaseRole(role) == profiles.RoleWorker:
		content = "Notes for " + role
	case profiles.BaseRole(role) == profiles.RoleReviewer:
		content = e.review
	}
	ch := make(chan pluginapi.ChatChunk, 1)
	ch <- pluginapi.ChatChunk{Content: content, Done: true}
	close(ch)
	return ch, nil
}

func (e *teamEnv) ExecuteTool(ctx context.Context, toolID string, args map[string]any) (map[string]any, error) {
	return map[string]any{}, nil
}

func (e *teamEnv) Emit(eventType string, payload map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, eventType)
	e.payloads = append(e.payloads, payload)
}

func (e *teamEnv) NodeForRole(role string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.nodes == nil {
		e.nodes = map[string]string{}
	}
	if id, ok := e.nodes[role]; ok {
		return id, nil
	}
	id := "this-mac"
	if strings.HasPrefix(role, profiles.RoleWorker+":") {
		id = "computer-" + strings.TrimPrefix(role, profiles.RoleWorker+":")
	}
	e.nodes[role] = id
	return id, nil
}

func runTeam(t *testing.T, env *teamEnv, prompt string, o contracts.OrchestrationPolicy, roles ...contracts.ModelRole) (string, []pluginapi.OrchestrationEvent) {
	t.Helper()
	if len(roles) == 0 {
		roles = []contracts.ModelRole{{Role: profiles.RolePrimary, ModelID: "m"}}
	}
	events, err := New().Run(context.Background(), contracts.Task{Prompt: prompt}, contracts.AIProfile{
		Name: "Team", OrchestratorID: "simple", Roles: roles, Orchestration: o,
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	var answer strings.Builder
	var all []pluginapi.OrchestrationEvent
	for evt := range events {
		all = append(all, evt)
		if evt.Type == "agent.message" {
			answer.WriteString(evt.Content)
		}
	}
	return answer.String(), all
}

const coffee = "Write a detailed guide to brewing coffee at home"

// Team: the planner splits the request, a worker on each computer writes
// notes at the same time, the answer is written from them, and the
// reviewer's revision is kept.
func TestTeamPlansSpreadsAndReviews(t *testing.T) {
	env := &teamEnv{
		plan:   `{"parts": ["Choosing beans", "Grinding", "Water and temperature"], "parallel": true, "needs_web": false}`,
		review: "A reviewed answer about brewing coffee at home: choose fresh beans, grind just before brewing, and use water just off the boil.",
	}
	answer, events := runTeam(t, env, coffee, contracts.OrchestrationPolicy{Strategy: profiles.StrategyTeam})
	if !strings.HasPrefix(answer, "A reviewed answer") {
		t.Fatalf("answer = %q, want the reviewer's revision", answer)
	}
	workers := 0
	for _, c := range env.calls {
		if strings.HasPrefix(c, profiles.RoleWorker+":") {
			workers++
		}
	}
	if env.calls[0] != profiles.RolePlanner || workers != 3 || env.calls[len(env.calls)-1] != profiles.RoleReviewer {
		t.Fatalf("calls = %v", env.calls)
	}
	if env.most < 2 {
		t.Fatalf("parts on different computers ran one at a time (most at once: %d)", env.most)
	}
	// The answer is written from every worker's notes.
	final := env.prompts[profiles.RolePrimary][0]
	for _, slot := range []string{"worker:1", "worker:2", "worker:3"} {
		if !strings.Contains(final, "Notes for "+slot) {
			t.Fatalf("answer did not see %s's notes:\n%s", slot, final)
		}
	}
	// Each worker, the planner, and the reviewer are reported with their computer.
	reported := map[string]string{}
	for _, evt := range events {
		if evt.Type == "agent.completed" && !evt.Done {
			reported[evt.Role] = evt.NodeID
		}
	}
	if reported["worker:2"] != "computer-2" || reported[profiles.RolePlanner] == "" || reported[profiles.RoleReviewer] == "" {
		t.Fatalf("reported roles = %v", reported)
	}
}

// A reviewer that finds nothing to change keeps the answer, and a quick
// question skips the team entirely.
func TestTeamKeepsAnswerAndSkipsQuickQuestions(t *testing.T) {
	env := &teamEnv{plan: `{"parts": ["The whole request"]}`, review: "OK"}
	answer, _ := runTeam(t, env, coffee, contracts.OrchestrationPolicy{Strategy: profiles.StrategyTeam})
	if !strings.HasPrefix(answer, "A full answer about brewing coffee") {
		t.Fatalf("answer = %q", answer)
	}
	if !strings.Contains(strings.Join(env.calls, ","), "worker:1") {
		t.Fatalf("a plan of one part still has a worker draft it: %v", env.calls)
	}

	quick := &teamEnv{plan: `{"parts": ["a", "b"]}`, review: "changed"}
	runTeam(t, quick, "hi there", contracts.OrchestrationPolicy{Strategy: profiles.StrategyTeam})
	if len(quick.calls) != 1 || quick.calls[0] != profiles.RolePrimary {
		t.Fatalf("a quick question used %v", quick.calls)
	}
}

// Single model never plans, and Planning: Always splits a request without
// obvious parts but keeps every part on the answering model unless the
// profile assigns a worker model.
func TestSingleAndAlwaysPlan(t *testing.T) {
	env := &teamEnv{plan: `{"parts": ["Beans", "Grinding"], "parallel": true}`}
	runTeam(t, env, "Compare espresso, pour-over and French press for flavor", contracts.OrchestrationPolicy{Strategy: profiles.StrategySingle})
	for _, c := range env.calls {
		if c != profiles.RolePrimary {
			t.Fatalf("single model ran %v", env.calls)
		}
	}

	always := &teamEnv{plan: `{"parts": ["Beans", "Grinding"], "parallel": true}`}
	runTeam(t, always, coffee, contracts.OrchestrationPolicy{Planning: "always"})
	if got := strings.Join(always.calls, ","); got != "assistant,assistant,assistant,assistant" {
		t.Fatalf("calls = %s, want the planner, two parts, and the answer on one model", got)
	}

	withWorker := &teamEnv{plan: `{"parts": ["Beans", "Grinding"], "parallel": true}`}
	runTeam(t, withWorker, coffee, contracts.OrchestrationPolicy{Planning: "always"},
		contracts.ModelRole{Role: profiles.RolePrimary, ModelID: "m"}, contracts.ModelRole{Role: profiles.RoleWorker, ModelID: "w"})
	if got := strings.Join(withWorker.calls, ","); !strings.Contains(got, "worker:1") || !strings.Contains(got, "worker:2") {
		t.Fatalf("calls = %s, want worker slots", got)
	}
}

func TestParsePlan(t *testing.T) {
	p := parsePlan("Here you go:\n```json\n{\"parts\": [\"a\", \"\", \"b\", \"c\"], \"parallel\": true}\n```", 2)
	if strings.Join(p.Steps, "|") != "a|b" || !p.Parallel {
		t.Fatalf("plan = %+v", p)
	}
	if p := parsePlan(`["x", "y"]`, 4); len(p.Steps) != 2 {
		t.Fatalf("a bare list = %+v", p)
	}
	if p := parsePlan("no json here", 4); len(p.Steps) != 0 {
		t.Fatalf("prose = %+v", p)
	}
}
