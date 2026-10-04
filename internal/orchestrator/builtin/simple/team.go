package simple

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/structured"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Events for the Team strategy's planner and reviewer.
const (
	EventPlanner  = "plan.planner"
	EventReviewed = "answer.reviewed"
)

// defaultTeamParts caps a planner's plan when the profile sets no worker limit.
const defaultTeamParts = 4

const plannerInstructions = "You plan how to answer a request. Split it into the parts that can be worked on separately, " +
	"from 2 to %d parts, each one short instruction. Reply with JSON only: " +
	`{"parts": ["...", "..."], "parallel": true, "needs_web": false}. ` +
	"Set parallel to false when each part needs the ones before it, and needs_web to true when the parts need current information from the web. " +
	"If the request cannot be split, reply with one part: the whole request."

const reviewerInstructions = "You review an answer before the user sees it. Check it against the request and the reference material: " +
	"mistakes, figures that are not supported, parts of the request that were missed, and unclear wording. " +
	"If the answer needs changes, reply with the whole improved answer for the user and nothing else. " +
	"If it is already right, reply with exactly OK."

// strategy is how a profile works through a request (§20).
type strategy struct {
	// team: a planner splits the request, workers do the parts, and a
	// reviewer checks the answer.
	team bool
	// single: one model, no plan.
	single bool
	// planned: a plan whenever the request has several parts.
	planned bool
	// alwaysPlan asks the planner to split a request with no obvious parts.
	alwaysPlan bool
}

func strategyOf(p contracts.AIProfile) strategy {
	o := p.Orchestration
	s := strategy{
		team:    o.Strategy == profiles.StrategyTeam,
		single:  o.Strategy == profiles.StrategySingle,
		planned: o.Strategy == profiles.StrategyPlanned,
	}
	s.alwaysPlan = s.team || o.Planning == "always"
	return s
}

// spreadWorkers reports whether a plan's parts get their own worker slots,
// which Norn can place on different computers: for the Team strategy, or
// when the profile assigns a worker model. Otherwise every part runs on the
// model that answers, as it always has.
func spreadWorkers(p contracts.AIProfile) bool {
	return strategyOf(p).team || profiles.HasRole(p, profiles.RoleWorker)
}

// plannerRole is the role that splits a request: the planner when the
// profile assigns one, otherwise the model that answers.
func plannerRole(p contracts.AIProfile, answering string) string {
	if profiles.HasRole(p, profiles.RolePlanner) || strategyOf(p).team {
		return profiles.RolePlanner
	}
	return answering
}

// reviewerRole is the role that checks an answer: the reviewer when the
// profile assigns one, or for the Team strategy; otherwise the model that
// answered.
func reviewerRole(p contracts.AIProfile, answering string) string {
	if profiles.HasRole(p, profiles.RoleReviewer) || strategyOf(p).team {
		return profiles.RoleReviewer
	}
	return answering
}

// askPlanner asks the planner model to split a request with no obvious
// parts (§12). It returns a plan of one part, the whole request, when the
// planner cannot split it, and false when the planner did not answer.
func askPlanner(ctx context.Context, env pluginapi.ExecutionEnvironment, ch chan<- pluginapi.OrchestrationEvent, role, prompt string, maxParts int) (huginn.Plan, bool) {
	if maxParts <= 1 {
		maxParts = defaultTeamParts
	}
	ask := []pluginapi.ChatMessage{
		{Role: "system", Content: fmt.Sprintf(plannerInstructions, maxParts)},
		{Role: "user", Content: prompt},
	}
	announceRole(env, role)
	content, metrics, err := generateText(ctx, env, role, ask)
	if err != nil {
		return huginn.Plan{}, false
	}
	reportRole(ch, env, role, metrics)
	plan := parsePlan(content, maxParts)
	if len(plan.Steps) == 0 {
		plan = huginn.Plan{Steps: []string{prompt}}
	}
	env.Emit(EventPlanner, map[string]any{"parts": len(plan.Steps), "role": role})
	return plan, true
}

// parsePlan reads a planner's reply, keeping at most maxParts non-empty parts.
func parsePlan(content string, maxParts int) huginn.Plan {
	found, ok := structured.Extract(tools.VisibleText(content))
	if !ok {
		return huginn.Plan{}
	}
	obj, ok := found.Value.(map[string]any)
	if !ok {
		if list, isList := found.Value.([]any); isList {
			obj = map[string]any{"parts": list}
		} else {
			return huginn.Plan{}
		}
	}
	var plan huginn.Plan
	list, _ := obj["parts"].([]any)
	for _, item := range list {
		part, _ := item.(string)
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if utf8.RuneCountInString(part) > 400 {
			part = string([]rune(part)[:400])
		}
		plan.Steps = append(plan.Steps, part)
		if len(plan.Steps) == maxParts {
			break
		}
	}
	plan.Parallel, _ = obj["parallel"].(bool)
	plan.NeedsWeb, _ = obj["needs_web"].(bool)
	return plan
}

// reviewAnswer has the reviewer check an answer and keeps its revision
// when it is a real answer (§24). A reviewer that says OK keeps the answer.
func reviewAnswer(ctx context.Context, env pluginapi.ExecutionEnvironment, ch chan<- pluginapi.OrchestrationEvent, role, prompt, answer, evidence string) string {
	if strings.TrimSpace(answer) == "" || ctx.Err() != nil {
		return answer
	}
	ask := []pluginapi.ChatMessage{
		{Role: "system", Content: reviewerInstructions},
		{Role: "user", Content: withReference("The request: "+prompt+"\n\nThe answer to review:\n"+answer, evidence)},
	}
	announceRole(env, role)
	content, metrics, err := generateText(ctx, env, role, ask)
	if err != nil {
		return answer
	}
	reportRole(ch, env, role, metrics)
	revised := strings.TrimSpace(tools.ParseModelOutput(content).Text)
	changed := revised != "" && !strings.EqualFold(strings.Trim(revised, ". \n"), "ok") && substantial(revised, answer)
	env.Emit(EventReviewed, map[string]any{"role": role, "changed": changed})
	if !changed {
		return answer
	}
	return revised
}

// reportRole records that a role finished, with the computer it ran on and
// its metrics, so run details and performance show each one (§35).
func reportRole(ch chan<- pluginapi.OrchestrationEvent, env pluginapi.ExecutionEnvironment, role string, metrics *pluginapi.GenerationMetrics) {
	if ch == nil {
		return
	}
	nodeID, _ := env.NodeForRole(role)
	ch <- pluginapi.OrchestrationEvent{Type: "agent.completed", Role: role, NodeID: nodeID, Metrics: metrics}
}

// announceRole tells clients that a role started and where it runs, for the
// chat's team timeline.
func announceRole(env pluginapi.ExecutionEnvironment, role string) {
	nodeID, _ := env.NodeForRole(role)
	env.Emit(events.OrchestrationRole, map[string]any{"role": role, "node_id": nodeID})
}
