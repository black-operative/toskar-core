package simple

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/yeixio/yggdrasil-core/internal/contextusage"
	"github.com/yeixio/yggdrasil-core/internal/events"
	"github.com/yeixio/yggdrasil-core/internal/huginn"
	"github.com/yeixio/yggdrasil-core/internal/mimir"
	"github.com/yeixio/yggdrasil-core/internal/structured"
	"github.com/yeixio/yggdrasil-core/internal/tools"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

const id = "simple"

// maxToolCalls is the Balanced budget's cap; huginn.Budget sets each turn's.
const maxToolCalls = 10

// EventEffort reports the effort a turn ran at, after Auto is resolved.
const EventEffort = "chat.effort"

// stoppedNotes is the partial answer kept when a plan is stopped.
func stoppedNotes(notes string) string {
	body := strings.TrimSpace(strings.TrimPrefix(notes, "Notes from each part of the request:"))
	return "_Stopped before the answer was written. Here is what was found so far._\n\n" + body
}

// Orchestrator runs single-model chat with optional tools.
type Orchestrator struct{}

func New() *Orchestrator { return &Orchestrator{} }

func (o *Orchestrator) ID() string          { return id }
func (o *Orchestrator) DisplayName() string { return "Simple" }

func (o *Orchestrator) Capabilities() pluginapi.OrchestratorCapabilities {
	return pluginapi.OrchestratorCapabilities{
		SupportsTools: true,
		SupportsTeam:  false,
		Roles:         []string{"assistant", "worker"},
	}
}

func (o *Orchestrator) ValidateProfile(ctx context.Context, profile contracts.AIProfile) error {
	if len(profile.Roles) == 0 {
		return fmt.Errorf("simple orchestrator requires at least one role")
	}
	hasModel := false
	for _, r := range profile.Roles {
		if r.ModelID != "" {
			hasModel = true
			break
		}
	}
	if !hasModel {
		return fmt.Errorf("simple orchestrator requires a model assignment")
	}
	return nil
}

func (o *Orchestrator) Run(
	ctx context.Context,
	task contracts.Task,
	profile contracts.AIProfile,
	env pluginapi.ExecutionEnvironment,
) (<-chan pluginapi.OrchestrationEvent, error) {
	role := pickRole(profile.Roles)
	ch := make(chan pluginapi.OrchestrationEvent, 8)
	go func() {
		defer close(ch)
		ch <- pluginapi.OrchestrationEvent{Type: "agent.started", Role: role}

		instructions := "Format answers in Markdown with short paragraphs and lists. Do not wrap the whole answer in a code fence. " +
			"Link only to addresses from the reference material, tool results, or the conversation; otherwise name the site instead of guessing a page's address, since remembered addresses are often wrong."
		reference := referenceMaterial(ctx, env, task.Prompt)
		// The effort the user chose, or Auto's pick for this kind of request,
		// sets how much planning, reading, and checking the turn gets (§15).
		plan, hasParts := huginn.MakePlan(task.Prompt)
		kind := huginn.Classify(task.Prompt)
		if (hasParts || reference != "") && kind == huginn.Chat {
			// A request with several parts, or one answered from the user's
			// files or knowledge, is not a quick question.
			kind = huginn.Research
		}
		// The profile's own controls apply on top of the effort's budget (§40).
		budget := huginn.BudgetFor(huginn.EffortFrom(ctx), kind).With(profile.Orchestration)
		// An answer that must be JSON (§27) is one constrained reply: no
		// plan, tool calls, file, or figure check, which would each need
		// output of another shape. A web look-up still runs first.
		jsonOnly := len(structured.SchemaFrom(ctx)) > 0
		// The profile's strategy (§20). A quick question is answered
		// directly whatever the strategy: escalate only when it helps (§6).
		strat := strategyOf(profile)
		escalate := kind != huginn.Chat && !jsonOnly
		switch {
		case strat.single:
			budget.Plan = false
		case strat.planned:
			budget.Plan = true
		case strat.team && escalate:
			budget.Plan, budget.Verify = true, true
			budget.Corrections = max(budget.Corrections, 1)
		}
		if jsonOnly {
			budget.Plan, budget.Verify = false, false
		}
		env.Emit(EventEffort, map[string]any{"effort": string(budget.Effort), "chosen": string(huginn.EffortFrom(ctx))})
		// Offer only the tools this request needs (spec §16). The profile
		// still decides what is allowed; this decides what is shown.
		offered := huginn.ToolsFor(kind, task.Prompt, enabledIDs(profile))
		// An attached audio file is transcribed, and an attached image can
		// be edited, when the profile allows it, whatever the message says
		// ("summarize this", "make it brighter").
		for _, id := range []string{"speech.transcribe", "image.edit"} {
			if strings.Contains(reference, "call "+id) && slices.Contains(enabledIDs(profile), id) && !slices.Contains(offered, id) {
				offered = append(offered, id)
			}
		}
		profile = offerOnly(profile, offered)
		// A request with several parts is worked through part by part;
		// otherwise a current question is looked up first.
		planned := false
		// Planning set to Always, and the Team strategy, ask the planner to
		// split a request that has no obvious parts (§12). Team keeps a plan
		// of one part, so a worker drafts and the answer is written from it.
		if !hasParts && budget.Plan && strat.alwaysPlan && escalate {
			limit := budget.MaxWorkers
			if limit == 0 {
				limit = defaultTeamParts
			}
			if p, ok := askPlanner(ctx, env, ch, plannerRole(profile, role), task.Prompt, limit); ok && (len(p.Steps) > 1 || strat.team) {
				plan, hasParts = p, true
			}
		}
		if hasParts && budget.Plan {
			if budget.Sequential {
				plan.Parallel = false
			}
			if budget.MaxWorkers > 0 && len(plan.Steps) > budget.MaxWorkers {
				plan.Steps = plan.Steps[:budget.MaxWorkers]
			}
			notes := runPlan(ctx, env, ch, profile, role, plan, task.Prompt, reference, budget.Pages)
			if ctx.Err() != nil {
				// Stopped while working through the parts: keep what is done (§67).
				if notes != "" {
					ch <- pluginapi.OrchestrationEvent{Type: "agent.message", Role: role, Content: stoppedNotes(notes)}
				}
				ch <- pluginapi.OrchestrationEvent{Type: "agent.error", Role: role, Error: ctx.Err().Error(), Done: true}
				return
			}
			if notes != "" {
				reference = joinReference(reference, notes)
				instructions += "\n" + planGuidance
				if plan.NeedsWeb && webAllowed(profile) {
					profile = withoutWeb(profile)
				}
				planned = true
			}
		}
		if !planned {
			// A message about a connected service is answered from that
			// service, not the web.
			if found, ok := serviceFirst(ctx, env, profile); ok {
				reference = joinReference(reference, found)
				instructions += "\n" + serviceGuidance
				profile = withoutFetched(profile)
			} else if found, ok := lookUpFirst(ctx, env, profile, task.Prompt, budget.Pages); ok {
				reference = joinReference(reference, found)
				instructions += "\n" + lookupGuidance
				profile = withoutWeb(profile)
			}
		}
		// evidence is what the answer may draw figures from, for the check.
		evidence := reference
		toolPrompt := tools.PromptFor(profile)
		if jsonOnly {
			toolPrompt = ""
		}
		sys := instructions
		if toolPrompt != "" {
			sys += "\n" + toolPrompt
		}
		// plainSys is the same prompt without tools, for a retry when the
		// model describes tools instead of answering. The context gauge counts
		// it as instructions, so turn guidance (personalization, memories, the
		// guide) is counted too (#230).
		plainSys := instructions
		if extra := turnGuidance(ctx, env, task.Prompt); extra != "" {
			sys = extra + "\n\n" + sys
			plainSys = extra + "\n\n" + plainSys
		}
		// count is the answering model's tokenizer, or an estimate (§66).
		count := tokenCounter(ctx, env, role)
		userMsg := withReference(task.Prompt, reference)
		messages := []pluginapi.ChatMessage{{Role: "system", Content: sys}}
		if prior := priorMessages(ctx, env, count, userMsg, sys, profile.Orchestration.ContextShare); len(prior) > 0 {
			messages = append(messages, prior...)
		}
		messages = append(messages, pluginapi.ChatMessage{Role: "user", Content: userMsg})

		var metrics *pluginapi.GenerationMetrics
		var usage contextusage.Usage
		toolsOn := len(tools.Enabled(profile, nil)) > 0
		nodeID, _ := env.NodeForRole(role)

		if jsonOnly {
			content, m, err := generateText(ctx, env, role, messages)
			if err != nil {
				ch <- pluginapi.OrchestrationEvent{Type: "agent.error", Role: role, Error: err.Error(), Done: true}
				return
			}
			promptTokens := 0
			if m != nil {
				promptTokens = m.PromptTokens
			}
			streamText(ch, role, nodeID, content, m, contextusage.Measure(count, plainSys, toolPrompt, messages, promptTokens))
			return
		}
		if reply, m, made := makeFileFirst(ctx, env, profile, role, messages, task.Prompt); made {
			promptTokens := 0
			if m != nil {
				promptTokens = m.PromptTokens
			}
			streamText(ch, role, nodeID, reply, m, contextusage.Measure(count, plainSys, toolPrompt, messages, promptTokens))
			return
		}
		calls := 0
		malformed := 0
		// changed records that a tool that changes things ran.
		changed := false
		retriedPlain := false

		for {
			content, m, err := generateText(ctx, env, role, messages)
			if err != nil {
				if content != "" {
					ch <- pluginapi.OrchestrationEvent{Type: "agent.message", Role: role, NodeID: nodeID, Content: content}
				}
				ch <- pluginapi.OrchestrationEvent{Type: "agent.error", Role: role, Error: err.Error(), Done: true}
				return
			}
			if m != nil {
				metrics = m
			}
			promptTokens := 0
			if m != nil {
				promptTokens = m.PromptTokens
			}
			usage = contextusage.Measure(count, plainSys, toolPrompt, messages, promptTokens)

			parsed := tools.ParseModelOutput(content)
			if parsed.Sanitized {
				env.Emit(events.ToolParsed, map[string]any{"sanitized": true})
			}
			if parsed.Call == nil && toolsOn {
				// Small models sometimes write the call out instead of
				// sending it; take it when it names an offered tool.
				if call, ok := looseToolCall(parsed.Text, profile); ok {
					parsed.Call, parsed.Text = call, ""
				}
			}
			if parsed.Call != nil {
				parsed.Call.ID = tools.Canonical(parsed.Call.ID)
			}
			if parsed.Call != nil && !toolEnabled(profile, parsed.Call.ID) && malformed < 2 {
				// A tool that was not offered is refused, whatever the profile
				// allows: the model cannot widen its own tools (spec §17).
				// That holds when no tools were offered at all.
				malformed++
				env.Emit(events.ToolFailed, map[string]any{"tool_id": parsed.Call.ID, "kind": tools.ErrKindNotOffered, "error": tools.ErrNotOffered.Error()})
				messages = append(messages,
					pluginapi.ChatMessage{Role: "assistant", Content: content},
					pluginapi.ChatMessage{Role: "user", Content: "The tool " + parsed.Call.ID + " is not available for this request. Use one of the listed tools, or answer in plain text."},
				)
				continue
			}
			if parsed.Call != nil && toolsOn && toolEnabled(profile, parsed.Call.ID) {
				if calls >= budget.MaxToolCalls {
					streamText(ch, role, nodeID, "I stopped after "+fmt.Sprint(budget.MaxToolCalls)+" tool calls so this request would not loop.", metrics, usage)
					return
				}
				calls++
				env.Emit(events.ToolParsed, map[string]any{
					"accepted": true,
					"tool_id":  parsed.Call.ID,
					"format":   parsed.Format,
				})
				if parsed.Text != "" {
					ch <- pluginapi.OrchestrationEvent{Type: "agent.message", Role: role, NodeID: nodeID, Content: parsed.Text}
				}
				result, err := env.ExecuteTool(ctx, parsed.Call.ID, parsed.Call.Args)
				if err == nil {
					if def, ok := tools.Lookup(parsed.Call.ID); ok && def.Risk != tools.RiskRead {
						changed = true
					}
				}
				var resultNote string
				if err != nil {
					payload, _ := json.Marshal(map[string]any{"ok": false, "error": publicToolError(err)})
					resultNote = "The tool failed. Details for you, not for the user:\n" + string(payload)
				} else {
					raw, _ := json.Marshal(map[string]any{"ok": true, "result": result})
					resultNote = "Tool result for you, not for the user. " + untrustedNote + "\n" + string(raw)
				}
				followUp := answerAfterTools
				if err == nil && parsed.Call.ID == "internet.search" && calls < budget.MaxToolCalls {
					if page, opened := followLiveSearch(ctx, env, profile, task.Prompt, parsed.Call.Args, result); opened {
						calls++
						raw, _ := json.Marshal(map[string]any{"ok": true, "result": page})
						resultNote += "\n\nPage for you, not for the user. " + untrustedNote + "\n" + string(raw)
						followUp = answerFromPage
					}
				}
				if err == nil {
					evidence = joinReference(evidence, resultNote)
				}
				messages = append(messages,
					pluginapi.ChatMessage{Role: "assistant", Content: content},
					pluginapi.ChatMessage{Role: "user", Content: resultNote + followUp},
				)
				continue
			}
			if toolsOn && parsed.Malformed && malformed < 2 && calls < budget.MaxToolCalls {
				malformed++
				env.Emit(events.ToolParsed, map[string]any{"accepted": false, "format": "rejected", "error": "invalid tool call"})
				env.Emit(events.ToolFailed, map[string]any{"malformed": true, "error": "invalid tool call"})
				messages = append(messages,
					pluginapi.ChatMessage{Role: "assistant", Content: content},
					pluginapi.ChatMessage{Role: "user", Content: "That tool call was not valid and was not run. Reply with one JSON object {\"tool_call\":{\"id\":\"...\",\"args\":{}}} using a listed tool, or answer in plain text."},
				)
				continue
			}
			// An answer that talks about tools instead of using them is not an
			// answer (spec §24, tool success). Ask once more without tools,
			// from the material already gathered.
			if toolsOn && !retriedPlain && narratesTools(parsed.Text) {
				retriedPlain = true
				toolsOn = false
				env.Emit(events.ToolFailed, map[string]any{"narrated": true, "error": "described tools instead of answering"})
				messages[0] = pluginapi.ChatMessage{Role: "system", Content: plainSys}
				continue
			}
			answer := parsed.Text
			if budget.Verify {
				answer = verifyAnswer(ctx, env, reviewerRole(profile, role), messages, parsed.Text, evidence, task.Prompt, budget.Corrections)
				answer = checkCode(ctx, env, reviewerRole(profile, role), messages, answer, budget.Corrections)
				answer = checkLinks(ctx, env, reviewerRole(profile, role), messages, answer, evidence, budget.Corrections)
				if budget.Effort == huginn.EffortThorough && budget.Corrections > 0 {
					answer = checkConsistency(ctx, env, reviewerRole(profile, role), messages, answer, evidence)
				}
			}
			// The Team strategy's reviewer reads every answer it escalated.
			if strat.team && escalate {
				answer = reviewAnswer(ctx, env, ch, reviewerRole(profile, role), task.Prompt, answer, evidence)
			}
			// An answer that says it changed something, when nothing that
			// changes things ran, is called out (§24; found by §64).
			if !changed && huginn.ClaimsAction(answer) {
				env.Emit(EventUnconfirmedAction, map[string]any{})
			}
			streamText(ch, role, nodeID, answer, metrics, usage)
			return
		}
	}()
	return ch, nil
}

func generateText(
	ctx context.Context,
	env pluginapi.ExecutionEnvironment,
	role string,
	messages []pluginapi.ChatMessage,
) (string, *pluginapi.GenerationMetrics, error) {
	stream, err := env.Generate(ctx, role, messages)
	if err != nil {
		return "", nil, err
	}
	var b strings.Builder
	var metrics *pluginapi.GenerationMetrics
	for chunk := range stream {
		if chunk.Error != "" {
			return b.String(), metrics, fmt.Errorf("%s", chunk.Error)
		}
		if chunk.Metrics != nil {
			metrics = chunk.Metrics
		}
		if chunk.Content != "" {
			b.WriteString(chunk.Content)
		}
		if chunk.Done {
			break
		}
	}
	return b.String(), metrics, nil
}

func streamText(ch chan<- pluginapi.OrchestrationEvent, role, nodeID, content string, metrics *pluginapi.GenerationMetrics, usage contextusage.Usage) {
	visible := tools.VisibleText(content)
	if visible != "" {
		ch <- pluginapi.OrchestrationEvent{Type: "agent.message", Role: role, NodeID: nodeID, Content: visible}
	}
	ch <- pluginapi.OrchestrationEvent{
		Type:    "agent.completed",
		Role:    role,
		NodeID:  nodeID,
		Content: visible,
		Metrics: metrics,
		Payload: map[string]any{"context": usage.Map()},
		Done:    true,
	}
}

// untrustedNote tells the model that retrieved content is data (§58).
const untrustedNote = mimir.UntrustedNote

// referenceSource is implemented by environments that retrieve reference
// material, such as connected knowledge, for a turn.
type referenceSource interface {
	ReferenceMaterial(ctx context.Context, prompt string) string
}

func referenceMaterial(ctx context.Context, env pluginapi.ExecutionEnvironment, prompt string) string {
	src, ok := env.(referenceSource)
	if !ok {
		return ""
	}
	return strings.TrimSpace(src.ReferenceMaterial(ctx, prompt))
}

func withReference(prompt, reference string) string { return mimir.WithReference(prompt, reference) }

// turnInstructions is implemented by environments that add instructions for
// one turn: a specialized AI's system instructions and connected knowledge.
type turnInstructions interface {
	TurnInstructions(ctx context.Context, prompt string) string
}

func turnGuidance(ctx context.Context, env pluginapi.ExecutionEnvironment, prompt string) string {
	src, ok := env.(turnInstructions)
	if !ok {
		return ""
	}
	return strings.TrimSpace(src.TurnInstructions(ctx, prompt))
}

type conversationMemory interface {
	PriorMessages(ctx context.Context) []pluginapi.ChatMessage
	ContextLimit() int
}

type tokenCounting interface {
	TokenCounter(ctx context.Context, role string) contextusage.Counter
}

// tokenCounter is the tokenizer of the model a role uses, or nil to estimate.
func tokenCounter(ctx context.Context, env pluginapi.ExecutionEnvironment, role string) contextusage.Counter {
	if src, ok := env.(tokenCounting); ok {
		return src.TokenCounter(ctx, role)
	}
	return nil
}

// priorMessages fits earlier messages into the room the model's window
// leaves, in tokens. share, when set by the profile (§40), caps earlier
// messages at that part of the window.
func priorMessages(ctx context.Context, env pluginapi.ExecutionEnvironment, count contextusage.Counter, prompt, reserved string, share float64) []pluginapi.ChatMessage {
	src, ok := env.(conversationMemory)
	if !ok {
		return nil
	}
	limit := src.ContextLimit()
	if limit <= 0 {
		limit = contextusage.DefaultWindow
	}
	room := contextusage.Room(limit, count, reserved, prompt)
	if share > 0 {
		room = min(room, int(share*float64(limit)))
	}
	return contextusage.FitPrior(src.PriorMessages(ctx), room, count)
}

func publicToolError(err error) string {
	msg := strings.TrimSpace(err.Error())
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	if len(msg) > 240 {
		msg = msg[:240]
	}
	return msg
}

func parseToolCall(content string) (toolID string, args map[string]any, ok bool) {
	parsed := tools.ParseModelOutput(content)
	if parsed.Call == nil {
		return "", nil, false
	}
	return parsed.Call.ID, parsed.Call.Args, true
}

func userVisibleReply(content string) string {
	return tools.VisibleText(content)
}

func pickRole(roles []contracts.ModelRole) string {
	for _, r := range roles {
		if r.Required && r.Role != "" {
			return r.Role
		}
	}
	if len(roles) > 0 {
		return roles[0].Role
	}
	return "assistant"
}
