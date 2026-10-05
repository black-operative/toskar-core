package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/contextusage"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/guide"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/models"
	modelhealth "github.com/yeixio/toskar-core/internal/models/health"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/internal/orchestrator/builtin/simple"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/remotetools"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/internal/runtimes/external"
	"github.com/yeixio/toskar-core/internal/runtimes/llamacpp"
	"github.com/yeixio/toskar-core/internal/share"
	"github.com/yeixio/toskar-core/internal/structured"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// RunChat executes a chat turn with streaming support.
// modelID, when set, overrides the profile's role model bindings for this turn.
// execution is automatic | local (empty keeps the profile node policy).
func (a *App) RunChat(ctx context.Context, profileID, conversationID, message string, stream bool, modelID, execution string) (<-chan pluginapi.ChatChunk, error) {
	turnStart := time.Now()
	if conversationID != "" {
		if conv, err := a.Conversations.Get(ctx, conversationID); err == nil {
			if profileID == "" {
				profileID = conv.ProfileID
			}
			if modelID == "" {
				modelID = conv.ModelID
			}
		}
	}
	// Memory requests are answered by Yggdrasil, not the model.
	opts := turnopts.From(ctx)
	// A short question about what Yggdrasil can do is answered from its
	// inventory, not by a model that may claim what it cannot do (§37).
	if len(structured.SchemaFrom(ctx)) == 0 {
		if ch, ok := a.answerCapabilityQuestion(ctx, conversationID, message); ok {
			return ch, nil
		}
		// "How do I install MCP?" is answered from what Yggdrasil offers.
		if ch, ok := a.answerHowTo(ctx, conversationID, message); ok {
			return ch, nil
		}
	}
	// An API caller changes memories only when it opted into memory (§62).
	if opts != nil && !opts.Memory {
		// Fall through: "Remember …" is an ordinary message for this caller.
	} else if ch, ok := a.handleMemoryCommand(ctx, conversationID, message); ok {
		return ch, nil
	}
	if profileID == "" {
		profileID = a.defaultProfileID(ctx)
	}
	profile, err := a.Profiles.Get(ctx, profileID)
	if err != nil {
		// Fall back to a chat-ready default when the stored id is missing.
		if modelID == "" {
			return nil, err
		}
		profile = profiles.Profile{
			ID:             "chat",
			Name:           "Chat",
			Purpose:        "general",
			OrchestratorID: "simple",
			NodePolicy:     contracts.NodePolicy{Mode: "automatic"},
		}
	}
	// A request for something that is not installed but can be, such as an
	// image before image generation is set up, is offered the setup (§29).
	if ch, ok := a.offerSetup(ctx, profile, conversationID, message); ok {
		return ch, nil
	}
	// A profile's own effort applies when the chat leaves effort on Auto (§40).
	if pe := profile.Orchestration.Effort; pe != "" && huginn.EffortFrom(ctx) == huginn.EffortAuto {
		ctx = huginn.WithEffort(ctx, huginn.ParseEffort(pe))
	}
	// An API request chooses its connected knowledge (§62).
	if opts != nil {
		if !opts.Knowledge {
			profile.KnowledgeSources = nil
		}
		profile.KnowledgeSources = append(append([]string(nil), profile.KnowledgeSources...), opts.KnowledgeSources...)
	}
	// Auto: Huginn picks the installed model that suits this message. A
	// question about files or connected knowledge counts as one that needs a
	// careful answer.
	// Steps and notices are written in the App language.
	appLang := a.appLanguage(ctx)
	routeReason, routeNotice := "", ""
	if modelID == huginn.AutoModelID {
		// A specialized AI trained for exactly this answers first (§61).
		if id, reason, ok := a.chooseSpecialist(ctx, message); ok {
			modelID, routeReason = id, reason.Render(appLang)
		} else if id, reason, ok := a.profileRoleModel(ctx, appLang, profile, message, a.turnHasData(ctx, conversationID, profile)); ok {
			// The profile's own coding or fast model (§20).
			modelID, routeReason = id, reason
		} else {
			lang := a.replyLanguage(ctx, conversationID, message, "").Tag
			choice, err := a.chooseAuto(ctx, message, a.turnHasData(ctx, conversationID, profile), lang)
			if err != nil {
				return nil, err
			}
			modelID, routeReason = choice.Model.ID, choice.Reason(appLang)
			if choice.LanguageWeak {
				// Nothing installed writes the language well (§16): the
				// answer is still in it, with a word that it may read less well.
				routeNotice = a.languageWeakNotice(ctx, choice.Model, lang)
			}
		}
	}
	// OpenAI /v1 and Chat both may hit presets with empty role model_ids.
	// Chat usually supplies a UI pick; when none is given, fill from an installed model.
	if modelID == "" && profileNeedsModelFill(profile) {
		mid, pickErr := a.defaultInstalledModelID(ctx)
		if pickErr != nil {
			return nil, fmt.Errorf(
				"profile %q needs model assignments (or pass a model id); %w",
				profileID, pickErr,
			)
		}
		modelID = mid
	}
	if err := a.refuseSupporting(ctx, modelID); err != nil {
		return nil, err
	}
	special, err := a.resolveSpecialized(ctx, modelID)
	if err != nil {
		return nil, err
	}
	if special != nil {
		modelID = special.baseModelID
	}
	if modelID != "" {
		profile = withChatModel(profile, modelID)
	}
	profile = applyExecutionPolicy(profile, execution)
	// Connected services' tools, at their default policies unless the
	// profile sets its own (§32).
	profile = tools.WithConnected(profile)
	if opts != nil {
		// An API key or request can narrow the tools, never widen them (§62).
		profile = narrowTools(profile, opts)
	}
	if special != nil {
		// A specialized AI answers in the style it was trained on, without the
		// tool protocol, on the computer that holds its adapter.
		profile = withoutTools(profile)
		execution = "local"
	}
	if execution == "automatic" && a.Models != nil && routeReason == "" && special == nil {
		installed, listErr := a.Models.List(ctx)
		if listErr == nil {
			if next, _ := routeToolCapableModel(execution, message, modelID, installed); next != "" && next != modelID {
				modelID = next
				profile = withChatModel(profile, modelID)
				profile = applyExecutionPolicy(profile, execution)
				routeReason = locale.T(appLang, "chat:steps.currentInfo", map[string]any{"model": a.modelName(modelID)})
			}
		}
	}
	if routeReason != "" {
		routedID, routedName := modelID, a.modelName(modelID)
		if special != nil {
			routedID, routedName = special.id, special.name
		}
		routed := map[string]any{
			"conversation_id": conversationID,
			"model_id":        routedID,
			"model_name":      routedName,
			"reason":          routeReason,
		}
		a.Bus.Publish(events.New(events.ChatModelRouted, routed))
		if opts != nil && opts.Progress != nil {
			opts.Progress(events.ChatModelRouted, routed)
		}
	}
	profile = a.withModelTools(profile, modelID)
	orch, err := a.OrchRegistry.Get(profile.OrchestratorID)
	if err != nil {
		return nil, err
	}
	if err := orch.ValidateProfile(ctx, profile); err != nil {
		return nil, err
	}

	attached, err := a.resolveAttachments(ctx, conversationID, artifacts.AttachmentsFrom(ctx))
	if err != nil {
		return nil, err
	}
	if conversationID != "" {
		if save, _ := a.Settings.GetBool(ctx, "save_chat_history", true); save {
			if len(attached) > 0 {
				_, _ = a.Conversations.AddMessageWithMeta(ctx, conversationID, "user", message, &contracts.MessageMeta{Files: fileRefs(attached)})
			} else {
				_, _ = a.Conversations.AddMessage(ctx, conversationID, "user", message)
			}
		}
	}

	task, err := a.Tasks.Create(ctx, profileID, conversationID, message)
	if err != nil {
		return nil, err
	}

	convTitle := ""
	if conversationID != "" {
		if conv, err := a.Conversations.Get(ctx, conversationID); err == nil {
			convTitle = conv.Title
		}
	}

	ch := make(chan pluginapi.ChatChunk, 32)
	go func() {
		defer close(ch)
		// Stop (from this client, another one, or the API) cancels ctx, and
		// with it every model call, tool, plan step, and paired computer.
		ctx, _, endRun := a.startRun(ctx, conversationID)
		defer endRun()
		// A profile's time limit stops a turn that runs too long (§40).
		if secs := profile.Orchestration.TimeoutSeconds; secs > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(secs)*time.Second)
			defer cancel()
		}
		// What leaves this computer is recorded against this turn (§63).
		source := egress.SourceChat
		if opts != nil {
			source = egress.SourceAPI
		}
		ctx = egress.WithRun(ctx, egress.Run{Source: source, ConversationID: conversationID, TaskID: task.ID})
		// Trace the run (§35): every layer adds what it used and how long it
		// took, and the run is saved however the turn ends.
		run := runlog.New(task.ID, conversationID, profile.ID, source)
		ctx = runlog.With(ctx, run)
		run.SetLanguage(appLang)
		if key := strategyNote(profile); key != "" {
			run.Note(key, nil)
		}
		if special != nil {
			run.Note("specialist", map[string]any{"name": special.name})
		}
		if routeReason != "" {
			run.Strategy(routeReason)
		}
		runStatus, runErr := runlog.StatusFailed, error(nil)
		defer func() {
			status := runStatus
			if ctx.Err() != nil && status != runlog.StatusCompleted {
				status = runlog.StatusStopped
			}
			if err := a.RunLog.Save(context.WithoutCancel(ctx), run.Finish(status, runErr)); err != nil && a.Logger != nil {
				a.Logger.Warn("save run", "run_id", task.ID, "error", err)
			}
		}()
		// Chat comes first: automations, benchmarks, and training wait
		// for it (§60). Chat itself never waits.
		work, _ := a.enterWork(ctx, share.Interactive, "chat", nil)
		defer work.Done()
		env := &chatExecEnv{
			app:            a,
			ctx:            ctx,
			profile:        profile,
			modelOverride:  modelID,
			conversationID: conversationID,
			taskID:         task.ID,
			turnPrompt:     message,
			trace:          &turnTrace{runID: task.ID, notice: routeNotice, lang: appLang},
			startedAt:      turnStart,
			attachments:    attached,
		}
		if routeReason != "" {
			env.trace.routed(routeReason)
		}
		env.opts = opts
		if facts := a.capabilityFacts(ctx, message); facts != "" {
			env.capabilities = facts
			env.trace.sharing(locale.T(appLang, "chat:steps.capabilities", nil))
		}
		// A question about Yggdrasil itself is answered from its own guide.
		if ps := guide.About(message); len(ps) > 0 {
			env.guide = guide.Block(ps)
			var sections []string
			for _, p := range ps {
				if !slices.Contains(sections, p.Section) {
					sections = append(sections, p.Section)
				}
			}
			env.trace.sharing(locale.T(appLang, "chat:steps.guide", map[string]any{"sections": listIn(appLang, sections)}))
		}
		if a.memoryOn(ctx, conversationID) && (opts == nil || opts.Memory) && profile.Orchestration.Memory != "off" {
			if mems, err := a.Muninn.Relevant(ctx, message); err == nil {
				env.memories = mems
				env.trace.memories(mems)
				for _, m := range mems {
					if m.LocalOnly {
						env.markLocalOnly()
					}
				}
			}
		}
		if special != nil {
			env.adapter = special.adapter
			env.instructions = special.instructions
			env.knowledge = special.knowledge
		}
		var full string
		var metrics *pluginapi.GenerationMetrics
		var roleSteps []contracts.GenerationRoleStep
		var contextUsage map[string]any
		// Quiet retries when the model fails before showing or changing
		// anything (spec §14, §26): first the same model on another
		// computer, then another model, up to the profile's retry count.
		maxRetries := profile.Orchestration.Retries
		if maxRetries == 0 {
			maxRetries = 1
		}
		for attempt := 0; ; attempt++ {
			eventsCh, err := orch.Run(ctx, task, profile, env)
			if err != nil {
				runErr = codedError(err)
				ch <- pluginapi.ChatChunk{Error: a.explainFailure(appLang, err.Error()), Done: true}
				return
			}
			retry := false
			for evt := range eventsCh {
				if evt.Error != "" {
					if ctx.Err() != nil {
						// Stopped: keep what was already written (§67).
						a.keepStopped(ctx, env, conversationID, full)
						ch <- pluginapi.ChatChunk{Done: true}
						go func(rest <-chan pluginapi.OrchestrationEvent) {
							for range rest {
							}
						}(eventsCh)
						return
					}
					if attempt < maxRetries && special == nil && profile.Orchestration.Fallback != "off" && recoverable(ctx, evt.Error, full, env) {
						failedID := firstNonEmpty(env.modelID(), modelID)
						failedNode, _ := env.roleNode(profiles.RolePrimary)
						if failedNode == "" {
							failedNode = a.Config.Get().NodeID
						}
						exclude := append(env.excludedNodes(), failedNode)
						if alt, ok := a.alternateNode(ctx, profile, profiles.RolePrimary, failedID, exclude); ok && !env.keepsLocal() {
							step := locale.T(appLang, "chat:steps.fallbackNode", map[string]any{"model": a.modelName(failedID), "failed": a.nodeDisplayName(failedNode), "computer": a.nodeDisplayName(alt)})
							run.Retried()
							run.Note("otherComputer", map[string]any{"computer": a.nodeDisplayName(alt), "model": a.modelName(failedID), "failed": a.nodeDisplayName(failedNode)})
							a.Logger.Warn("chat model failed; retrying on another computer", "model", failedID, "failed_node", failedNode, "next_node", alt, "error", evt.Error)
							env.trace.recovered(step, "")
							env.retryElsewhere(failedNode)
							metrics, roleSteps, contextUsage = nil, nil, nil
							retry = true
							go func(rest <-chan pluginapi.OrchestrationEvent) {
								for range rest {
								}
							}(eventsCh)
							break
						}
						if next, step, notice, ok := a.fallback(ctx, env.trace.lang, failedID, evt.Error, profile.Orchestration.FallbackModels); ok {
							run.Retried()
							run.Note("otherModel", map[string]any{"model": a.modelName(failedID)})
							a.Logger.Warn("chat model failed; retrying on another model", "failed", failedID, "next", next.ID, "error", evt.Error)
							a.noteModelFailed(failedID)
							env.trace.recovered(step, notice)
							profile = a.withModelTools(applyExecutionPolicy(withChatModel(profile, next.ID), execution), next.ID)
							env.switchModel(profile, next.ID)
							a.Bus.Publish(events.New(events.ChatModelRouted, map[string]any{
								"conversation_id": conversationID,
								"model_id":        next.ID,
								"model_name":      huginn.Name(next),
								"reason":          step,
								"fallback":        true,
							}))
							metrics, roleSteps, contextUsage = nil, nil, nil
							retry = true
							go func(rest <-chan pluginapi.OrchestrationEvent) {
								for range rest {
								}
							}(eventsCh)
							break
						}
					}
					if _, healthFailure := modelhealth.Parse(evt.Error); healthFailure && full != "" {
						if conversationID != "" {
							if saveChat, _ := a.Settings.GetBool(ctx, "save_chat_history", true); saveChat {
								_, _ = a.Conversations.AddMessage(ctx, conversationID, "assistant", full)
							}
						}
					}
					runErr = codedChatError(evt.Error)
					ch <- pluginapi.ChatChunk{Error: a.explainFailure(appLang, evt.Error), Done: true}
					return
				}
				if evt.Type == "agent.completed" && evt.Role != "" {
					// Each role that finished: the planner, each worker, the
					// reviewer, and the model that wrote the answer.
					step := roleStepFromEvent(a, profile, evt)
					if step.NodeID == "" {
						if id, ok := env.roleNode(evt.Role); ok {
							step.NodeID = id
							step.NodeName = a.nodeDisplayName(id)
						}
					}
					if step.ModelID == "" {
						if mid, ok := env.roleModel(evt.Role); ok {
							step.ModelID = mid
						}
					}
					replaced := false
					for i := range roleSteps {
						if roleSteps[i].Role == step.Role {
							roleSteps[i] = step
							replaced = true
							break
						}
					}
					if !replaced {
						roleSteps = append(roleSteps, step)
					}
				}
				if evt.Metrics != nil {
					metrics = evt.Metrics
				}
				if copied := copyContextUsage(evt.Payload); copied != nil {
					contextUsage = copied
				}
				if evt.Content != "" && evt.Type != "agent.completed" {
					visible := tools.VisibleText(evt.Content)
					if visible == "" {
						continue
					}
					full += visible
					ch <- pluginapi.ChatChunk{Content: visible}
					a.Bus.Publish(events.New(events.ChatToken, map[string]any{
						"conversation_id": conversationID,
						"content":         visible,
					}))
				}
				if evt.Done {
					ch <- pluginapi.ChatChunk{Done: true, Metrics: metrics}
				}
			}
			if !retry {
				break
			}
		}
		if full != "" {
			env.trace.noticeIfNone(a.smallModelNotice(ctx, env.trace.lang, env.trace.dataKind(), env.modelID(), routeReason != ""))
		}
		if ctx.Err() != nil {
			// Stopped after the model finished speaking but before the turn
			// was saved: keep it as a stopped answer (§67).
			a.keepStopped(ctx, env, conversationID, full)
			return
		}
		if contextUsage != nil {
			contextUsage["limit"] = env.ContextLimit()
			// What the window reserves in memory, when the model ran here.
			if _, memory, ok := a.localContext(ctx, env.modelID()); ok && memory > 0 {
				contextUsage["memory_bytes"] = memory
			}
			env.mu.Lock()
			if env.summarized > 0 {
				contextUsage["summarized_messages"] = env.summarized
			}
			env.mu.Unlock()
		}
		// The answer keeps its context reading, so the gauge shows it when the
		// chat is opened again.
		answerMeta := withContextUsage(env.trace.meta(), contextUsage)
		if conversationID != "" && full != "" {
			saveChat, _ := a.Settings.GetBool(ctx, "save_chat_history", true)
			msgID := ""
			if saveChat {
				msg, _ := a.Conversations.AddMessageWithMeta(ctx, conversationID, "assistant", full, answerMeta)
				msgID = msg.ID
			}
			a.recordGeneration(ctx, profile, conversationID, convTitle, msgID, env.modelID(), metrics, roleSteps)
		} else if metrics != nil || len(roleSteps) > 0 {
			a.recordGeneration(ctx, profile, conversationID, convTitle, "", env.modelID(), metrics, roleSteps)
		}
		runStatus = runlog.StatusCompleted
		if metrics != nil {
			run.Context(metrics.PromptTokens, env.ContextLimit())
		}
		payload := map[string]any{"conversation_id": conversationID}
		if ms, ok := env.pipelineMS(); ok {
			run.Pipeline(time.Duration(ms) * time.Millisecond)
			payload["pipeline_ms"] = ms
			a.Logger.Debug("chat pipeline overhead", "conversation_id", conversationID, "ms", ms)
		}
		if answerMeta != nil {
			payload["meta"] = answerMeta
			if opts != nil && opts.Meta != nil {
				opts.Meta(answerMeta)
			}
		}
		if metrics != nil {
			payload["metrics"] = metrics
			payload["model_id"] = env.modelID()
		}
		if contextUsage != nil {
			payload["context"] = contextUsage
		}
		// Summarize with the model that answered, only when it ran here, so a
		// turn placed on a paired computer never loads a model on this one.
		if conversationID != "" && full != "" && env.ranLocally() {
			a.summarizeLater(conversationID, env.modelID(), env.ContextLimit())
		}
		if len(roleSteps) > 0 {
			payload["role_steps"] = roleSteps
			payload["cross_machine"] = distinctNodeCount(roleSteps) > 1
		}
		a.Bus.Publish(events.New(events.ChatComplete, payload))
	}()
	return ch, nil
}

func roleStepFromEvent(a *App, profile profiles.Profile, evt pluginapi.OrchestrationEvent) contracts.GenerationRoleStep {
	modelID := evt.ModelID
	if modelID == "" {
		for _, r := range profile.Roles {
			if r.Role == evt.Role && r.ModelID != "" {
				modelID = r.ModelID
				break
			}
		}
	}
	step := contracts.GenerationRoleStep{
		Role:     evt.Role,
		NodeID:   evt.NodeID,
		NodeName: a.nodeDisplayName(evt.NodeID),
		ModelID:  modelID,
	}
	if evt.Metrics != nil {
		step.PromptTokens = evt.Metrics.PromptTokens
		step.CompletionTokens = evt.Metrics.CompletionTokens
		step.TotalTokens = evt.Metrics.TotalTokens
		step.TTFTMs = evt.Metrics.TTFTMs
		step.PromptMs = evt.Metrics.PromptMs
		step.EvalMs = evt.Metrics.EvalMs
		step.TotalMs = evt.Metrics.TotalMs
		step.PromptTokPerSec = evt.Metrics.PromptTokPerSec
		step.EvalTokPerSec = evt.Metrics.EvalTokPerSec
	}
	return step
}

// withContextUsage adds a turn's context reading to the answer's metadata,
// making metadata for an answer that has nothing else to keep.
func withContextUsage(meta *contracts.MessageMeta, usage map[string]any) *contracts.MessageMeta {
	if usage == nil {
		return meta
	}
	b, err := json.Marshal(usage)
	if err != nil {
		return meta
	}
	var cu contracts.ContextUsage
	if json.Unmarshal(b, &cu) != nil || (cu.PromptTokens <= 0 && cu.Instructions+cu.Tools+cu.Conversation+cu.ToolResults <= 0) {
		return meta
	}
	if meta == nil {
		meta = &contracts.MessageMeta{Contract: contracts.ContractVersion}
	}
	meta.Context = &cu
	return meta
}

func copyContextUsage(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	raw, ok := payload["context"].(map[string]any)
	if !ok || raw == nil {
		return nil
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		out[key] = value
	}
	return out
}

func distinctNodeCount(steps []contracts.GenerationRoleStep) int {
	seen := map[string]struct{}{}
	for _, s := range steps {
		if s.NodeID != "" {
			seen[s.NodeID] = struct{}{}
		}
	}
	return len(seen)
}

func (a *App) recordGeneration(
	ctx context.Context,
	profile profiles.Profile,
	conversationID, conversationTitle, messageID, modelID string,
	metrics *pluginapi.GenerationMetrics,
	roleSteps []contracts.GenerationRoleStep,
) {
	if a.Metrics == nil {
		return
	}
	if metrics == nil && len(roleSteps) == 0 {
		return
	}
	run := contracts.GenerationRun{
		ConversationID:    conversationID,
		ConversationTitle: conversationTitle,
		MessageID:         messageID,
		ProfileID:         profile.ID,
		ProfileName:       profile.Name,
		ModelID:           modelID,
		RuntimeID:         "llamacpp",
		RoleSteps:         roleSteps,
		CreatedAt:         time.Now().UTC(),
	}
	if metrics != nil {
		run.PromptTokens = metrics.PromptTokens
		run.CompletionTokens = metrics.CompletionTokens
		run.TotalTokens = metrics.TotalTokens
		run.TTFTMs = metrics.TTFTMs
		run.PromptMs = metrics.PromptMs
		run.EvalMs = metrics.EvalMs
		run.TotalMs = metrics.TotalMs
		run.PromptTokPerSec = metrics.PromptTokPerSec
		run.EvalTokPerSec = metrics.EvalTokPerSec
	}
	if _, err := a.Metrics.Insert(ctx, run); err != nil {
		a.Logger.Warn("record generation metrics", "error", err)
	}
}

// HandleHTTPChat serves POST /api/v1/chat with optional SSE streaming.
func (a *App) HandleHTTPChat(w http.ResponseWriter, r *http.Request, conversationID, profileID, modelID, message string, stream bool, execution string) error {
	ch, err := a.RunChat(r.Context(), profileID, conversationID, message, stream, modelID, execution)
	if err != nil {
		if code, _ := contracts.ErrorCode(err); code == "" {
			if code, params := chatErrorCode(err.Error()); code != "" {
				return contracts.NewError(code, params, err)
			}
		}
		return err
	}
	if !stream {
		var content string
		var metrics *pluginapi.GenerationMetrics
		for chunk := range ch {
			if chunk.Error != "" {
				return codedChatError(chunk.Error)
			}
			content += chunk.Content
			if chunk.Metrics != nil {
				metrics = chunk.Metrics
			}
		}
		w.Header().Set("Content-Type", "application/json")
		out := map[string]any{"content": content}
		if metrics != nil {
			out["metrics"] = metrics
		}
		return json.NewEncoder(w).Encode(out)
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming unsupported")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	for chunk := range ch {
		if chunk.Error != "" {
			// error_code comes first, so a client that knows it shows the
			// error in the App language; older clients ignore it and show
			// the text in error.
			if code, params := chatErrorCode(chunk.Error); code != "" {
				data, _ := json.Marshal(contracts.ErrorBody{Code: code, Message: chunk.Error, Details: params})
				fmt.Fprintf(w, "event: error_code\ndata: %s\n\n", data)
			}
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", chunk.Error)
			flusher.Flush()
			return nil
		}
		if chunk.Content != "" {
			data, _ := json.Marshal(map[string]any{"content": chunk.Content})
			fmt.Fprintf(w, "event: token\ndata: %s\n\n", data)
			flusher.Flush()
		}
		if chunk.Done {
			payload := map[string]any{}
			if chunk.Metrics != nil {
				payload["metrics"] = chunk.Metrics
			}
			data, _ := json.Marshal(payload)
			fmt.Fprintf(w, "event: done\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
	return nil
}

func (a *App) defaultProfileID(ctx context.Context) string {
	items, err := a.Profiles.List(ctx)
	if err != nil || len(items) == 0 {
		return ""
	}
	preferred := []string{profiles.PresetGeneral, profiles.PresetProgramming, profiles.PresetResearch}
	byID := make(map[string]profiles.Profile, len(items))
	for _, p := range items {
		byID[p.ID] = p
	}
	for _, id := range preferred {
		if p, ok := byID[id]; ok && len(p.Roles) > 0 {
			return p.ID
		}
	}
	for _, p := range items {
		if len(p.Roles) > 0 {
			return p.ID
		}
	}
	return items[0].ID
}

func (a *App) nodeDisplayName(nodeID string) string {
	cfg := a.Config.Get()
	if nodeID == "" || nodeID == cfg.NodeID {
		return cfg.NodeName
	}
	if a.Nodes == nil {
		return nodeID
	}
	list, err := a.Nodes.List(context.Background())
	if err != nil {
		return nodeID
	}
	for _, n := range list {
		if n.ID == nodeID {
			return n.Name
		}
	}
	return nodeID
}

// profileNeedsModelFill reports whether empty role model_ids would fail orchestrator validation.
func profileNeedsModelFill(p profiles.Profile) bool {
	for _, r := range p.Roles {
		if r.ModelID != "" {
			return false
		}
	}
	return true
}

// defaultInstalledModelID picks a concrete model for empty profile roles.
// Prefers the most recently used installed model; skips stub unless it is the only option.
func (a *App) defaultInstalledModelID(ctx context.Context) (string, error) {
	if a.Models == nil {
		return "", fmt.Errorf("no models manager")
	}
	list, err := a.Models.List(ctx)
	if err != nil {
		return "", err
	}
	var (
		best     string
		bestUsed time.Time
		stub     string
	)
	for _, m := range list {
		if m.Status != "installed" || huginn.Supporting(m) {
			continue
		}
		if m.ID == models.StubModelID {
			stub = m.ID
			continue
		}
		if m.LastUsedAt != nil && (best == "" || m.LastUsedAt.After(bestUsed)) {
			best = m.ID
			bestUsed = *m.LastUsedAt
			continue
		}
		if best == "" {
			best = m.ID
		}
	}
	if best != "" {
		return best, nil
	}
	if stub != "" {
		return stub, nil
	}
	return "", contracts.Errorf("NO_MODEL_INSTALLED", nil, "no installed models")
}

// withChatModel applies the chat's model to a profile for this turn: it
// writes the answer. The profile's other roles (planner, workers, reviewer,
// and the fast and coding models) keep their own models and computers; a
// role without a model uses the chat's.
func withChatModel(p profiles.Profile, modelID string) profiles.Profile {
	out := profiles.Normalize(p)
	if out.NodePolicy.Mode == "" {
		out.NodePolicy.Mode = "automatic"
	}
	out.OrchestratorID = "simple"
	roles := []contracts.ModelRole{{Role: profiles.RolePrimary, ModelID: modelID}}
	for _, r := range out.Roles {
		if r.Role == profiles.RolePrimary || (r.ModelID == "" && r.NodeID == "") {
			continue
		}
		roles = append(roles, r)
	}
	out.Roles = roles
	return out
}

// strategyNote is the chat:run.notes key describing a profile's strategy
// for run details (§35), or "".
func strategyNote(p profiles.Profile) string {
	switch p.Orchestration.Strategy {
	case profiles.StrategyTeam:
		return "team"
	case profiles.StrategySingle:
		return "single"
	}
	return ""
}

// applyExecutionPolicy maps Chat UI execution preference onto node_policy.
func withoutTools(p profiles.Profile) profiles.Profile {
	if len(p.Tools) == 0 {
		return p
	}
	toolsCopy := make([]contracts.ToolPolicy, len(p.Tools))
	for i, tool := range p.Tools {
		tool.Policy = tools.PolicyDeny
		toolsCopy[i] = tool
	}
	p.Tools = toolsCopy
	return p
}

func applyExecutionPolicy(p profiles.Profile, execution string) profiles.Profile {
	switch execution {
	case "local":
		p.NodePolicy.Mode = "prefer_local"
	case "automatic":
		p.NodePolicy.Mode = "automatic"
	}
	return p
}

type chatExecEnv struct {
	// trainingNoted is set once the turn's steps say this computer is
	// training.
	trainingNoted  bool
	app            *App
	ctx            context.Context
	profile        profiles.Profile
	modelOverride  string
	conversationID string
	taskID         string
	turnPrompt     string
	// adapter, instructions, and knowledge come from a specialized AI, on top
	// of the profile.
	adapter      string
	instructions string
	knowledge    []string
	// trace records sources and steps for the answer.
	trace *turnTrace
	// memories are the persistent memories relevant to this turn.
	memories []muninn.Memory
	// opts are an API request's choices for this turn, or nil (§62).
	opts *turnopts.Options
	// guide is excerpts from the user guide for a question about Yggdrasil.
	guide string
	// capabilities are inventory facts for a question about what
	// Yggdrasil can do (§37).
	capabilities string
	// excluded are computers the turn no longer uses, because the model
	// failed there (§20, Execution).
	excluded []string
	// localOnly is set once the turn uses a memory or knowledge source
	// marked this computer only, so it is never sent elsewhere (§63).
	localOnly   bool
	keptLocally bool
	// responseLanguage is an automation's response language (§22), or ""
	// for the assistant language setting.
	responseLanguage string
	// attachments are the files attached to this message.
	attachments []artifacts.Artifact
	// summarized counts saved messages replaced by a summary this turn.
	summarized int
	// startedAt and firstGenerate measure the pipeline's overhead before
	// the first model call (AI experience spec §65).
	startedAt     time.Time
	firstGenerate time.Time

	mu         sync.Mutex
	lastModel  string
	roleNodes  map[string]string
	roleModels map[string]string
	usedNodes  []string
}

func (e *chatExecEnv) modelID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastModel
}

func (e *chatExecEnv) ContextLimit() int {
	catalog := 0
	id := e.modelOverride
	if id == "" {
		id = e.modelID()
	}
	// A model running here reports its real window; the catalog is a guess
	// that can differ from what llama-server was started with (#230).
	if e.app != nil && e.ctx != nil {
		if window, _, ok := e.app.localContext(e.ctx, id); ok {
			return window
		}
	}
	if id != "" && e.app != nil && e.app.Models != nil {
		if entry, ok := e.app.Models.Catalog().Get(id); ok {
			catalog = entry.Context
		}
	}
	return llamacpp.ContextWindow(catalog)
}

// TokenCounter counts with the tokenizer of the model a role uses, while it
// runs on this computer, and estimates otherwise (§66).
func (e *chatExecEnv) TokenCounter(ctx context.Context, role string) contextusage.Counter {
	if e.app == nil {
		return nil
	}
	return e.app.tokenCounter(ctx, e.modelForRole(role))
}

func (e *chatExecEnv) PriorMessages(ctx context.Context) []pluginapi.ChatMessage {
	if e.conversationID == "" && e.opts != nil {
		// An API caller sends the whole conversation each time (§62).
		return e.opts.History
	}
	if e.conversationID == "" || e.app == nil || e.app.Conversations == nil {
		return nil
	}
	stored, err := e.app.Conversations.ListMessages(ctx, e.conversationID)
	if err != nil {
		return nil
	}
	prior := contextusage.WithoutCurrentTurn(stored, e.turnPrompt)
	// Older messages may be replaced by Muninn's summary; all of them stay saved.
	if e.app.Muninn != nil {
		if sum, ok, err := e.app.Muninn.GetSummary(ctx, e.conversationID); err == nil && ok {
			var n int
			prior, n = muninn.WithSummary(prior, stored, &sum)
			e.mu.Lock()
			e.summarized = n
			e.mu.Unlock()
		}
	}
	return prior
}

// pipelineMS is the time from receiving the message to the first model call.
func (e *chatExecEnv) pipelineMS() (int64, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.startedAt.IsZero() || e.firstGenerate.IsZero() {
		return 0, false
	}
	return e.firstGenerate.Sub(e.startedAt).Milliseconds(), true
}

// ranLocally reports whether every role in this turn ran on this computer.
func (e *chatExecEnv) ranLocally() bool {
	local := ""
	if e.app != nil && e.app.Config != nil {
		local = e.app.Config.Get().NodeID
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, id := range e.roleNodes {
		if id != "" && id != local {
			return false
		}
	}
	return true
}

func (e *chatExecEnv) roleNode(role string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	id, ok := e.roleNodes[role]
	return id, ok
}

func (e *chatExecEnv) roleModel(role string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	id, ok := e.roleModels[role]
	return id, ok
}

func (e *chatExecEnv) Generate(ctx context.Context, role string, messages []pluginapi.ChatMessage) (<-chan pluginapi.ChatChunk, error) {
	modelID := e.modelForRole(role)
	e.mu.Lock()
	e.lastModel = modelID
	if e.firstGenerate.IsZero() {
		e.firstGenerate = time.Now()
	}
	e.mu.Unlock()
	nodeID, err := e.NodeForRole(role)
	if err != nil {
		return nil, err
	}
	nodeID = e.keepLocalIfNeeded(role, nodeID)
	e.app.Bus.Publish(events.New(events.ModelLoadStarted, map[string]any{
		"model_id": modelID, "node_id": nodeID, "role": role,
		"node_name": e.app.nodeDisplayName(nodeID),
	}))
	started := time.Now()
	ch, err := e.app.generateOnNode(ctx, nodeID, modelID, role, e.adapter, messages)
	if err != nil {
		return nil, err
	}
	e.app.Bus.Publish(events.New(events.ModelLoadCompleted, map[string]any{
		"model_id": modelID, "node_id": nodeID, "role": role,
		"node_name": e.app.nodeDisplayName(nodeID),
	}))
	return traceGeneration(runlog.From(ctx), ch, modelID, role, e.app.nodeDisplayName(nodeID), started), nil
}

func (e *chatExecEnv) ExecuteTool(ctx context.Context, toolID string, args map[string]any) (map[string]any, error) {
	policy := effectivePolicy(tools.PolicyForProfile(e.profile, toolID), toolID, e.trace != nil && e.trace.sawUntrusted())
	meta := map[string]any{}
	if e.conversationID != "" {
		meta["conversation_id"] = e.conversationID
	}
	if e.taskID != "" {
		meta["task_id"] = e.taskID
	}
	ctx = artifacts.WithConversation(ctx, e.conversationID)
	// A heavy tool may run on another computer, as the profile allows.
	ctx = remotetools.WithPolicy(ctx, e.profile.NodePolicy)
	e.progress(events.ToolStarted, map[string]any{"tool_id": toolID, "args": args})
	toolStarted := time.Now()
	result, err := e.app.Tools.Execute(ctx, toolID, args, policy, "chat requested tool", meta)
	runlog.From(ctx).ToolCall(tools.Canonical(toolID), time.Since(toolStarted), err != nil)
	if err == nil && e.trace != nil {
		e.trace.tool(toolID, args, result)
	}
	if err != nil {
		e.progress(events.ToolFailed, map[string]any{"tool_id": toolID, "error": err.Error()})
	} else {
		e.progress(events.ToolCompleted, map[string]any{"tool_id": toolID})
	}
	return result, err
}

func (e *chatExecEnv) Emit(eventType string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	} else {
		cp := make(map[string]any, len(payload)+4)
		for k, v := range payload {
			cp[k] = v
		}
		payload = cp
	}
	if e.conversationID != "" {
		if _, ok := payload["conversation_id"]; !ok {
			payload["conversation_id"] = e.conversationID
		}
	}
	if e.taskID != "" {
		if _, ok := payload["task_id"]; !ok {
			payload["task_id"] = e.taskID
		}
	}
	e.progress(eventType, payload)
	traceEvent(runlog.From(e.ctx), eventType, payload)
	if e.trace != nil {
		switch eventType {
		case simple.EventPlanCreated:
			steps, _ := payload["steps"].([]string)
			parallel, _ := payload["parallel"].(bool)
			e.trace.planned(len(steps), parallel)
		case simple.EventEffort:
			if chosen, _ := payload["chosen"].(string); chosen != "" && chosen != string(huginn.EffortAuto) {
				e.trace.effort(huginn.ParseEffort(chosen))
			}
		case simple.EventVerified:
			issues, _ := payload["issues"].(int)
			fixed, _ := payload["fixed"].(int)
			figures, _ := payload["figures"].([]string)
			e.trace.verified(issues, fixed, figures)
		case simple.EventUnconfirmedAction:
			e.trace.unconfirmedAction()
		case simple.EventCodeChecked:
			issues, _ := payload["issues"].(int)
			fixed, _ := payload["fixed"].(int)
			remaining, _ := payload["remaining"].([]string)
			e.trace.codeChecked(issues, fixed, remaining)
		case simple.EventLinksChecked:
			issues, _ := payload["issues"].(int)
			fixed, _ := payload["fixed"].(int)
			remaining, _ := payload["remaining"].([]string)
			e.trace.linksChecked(issues, fixed, remaining)
		case simple.EventConsistency:
			found, _ := payload["found"].(int)
			fixed, _ := payload["fixed"].(int)
			remaining, _ := payload["remaining"].([]string)
			e.trace.consistencyChecked(found, fixed, remaining)
		}
	}
	if e.app.Tools != nil && (eventType == events.ToolParsed || eventType == events.ToolFailed) {
		toolID, _ := payload["tool_id"].(string)
		status := "parsed"
		if accepted, ok := payload["accepted"].(bool); ok && !accepted {
			status = "rejected"
		}
		if eventType == events.ToolFailed {
			status = "failed"
		}
		if sanitized, _ := payload["sanitized"].(bool); sanitized {
			status = "sanitized"
		}
		errText, _ := payload["error"].(string)
		format, _ := payload["format"].(string)
		e.app.Tools.Note(tools.Activity{
			ToolID:  toolID,
			Status:  status,
			Summary: format,
			Error:   errText,
		})
	}
	if eventType == events.OrchestrationRole || eventType == "orchestration.role" {
		if nodeID, ok := payload["node_id"].(string); ok && nodeID != "" {
			if _, has := payload["node_name"]; !has {
				payload["node_name"] = e.app.nodeDisplayName(nodeID)
			}
		}
	}
	e.app.Bus.Publish(events.New(eventType, payload))
}

func (e *chatExecEnv) NodeForRole(role string) (string, error) {
	e.mu.Lock()
	if e.roleNodes == nil {
		e.roleNodes = map[string]string{}
	}
	if e.roleModels == nil {
		e.roleModels = map[string]string{}
	}
	if id, ok := e.roleNodes[role]; ok && id != "" {
		e.mu.Unlock()
		return id, nil
	}
	if e.adapter != "" && e.app.Config != nil {
		// The adapter file lives here, so a specialized AI never leaves this computer.
		local := e.app.Config.Get().NodeID
		e.roleNodes[role] = local
		e.mu.Unlock()
		e.noteTraining(local)
		return local, nil
	}
	avoid := append([]string(nil), e.usedNodes...)
	e.mu.Unlock()

	modelID := e.modelForRole(role)
	// A model on the external server runs there, chosen explicitly, never
	// for a profile or a chat that keeps work here (#111).
	if external.IsModel(modelID) {
		if err := externalAllowed(e.profile, e.keepsLocal()); err != nil {
			return "", err
		}
		local := e.app.Config.Get().NodeID
		e.mu.Lock()
		e.roleNodes[role], e.roleModels[role], e.lastModel = local, modelID, modelID
		e.mu.Unlock()
		return local, nil
	}
	e.mu.Lock()
	excluded := append([]string(nil), e.excluded...)
	e.mu.Unlock()
	nodeID, err := e.app.placeRoleWith(e.ctx, e.profile, role, modelID, avoid, excluded, false)
	if err != nil {
		return "", err
	}
	e.noteTraining(nodeID)

	e.mu.Lock()
	e.roleNodes[role] = nodeID
	e.roleModels[role] = modelID
	e.lastModel = modelID
	seen := false
	for _, id := range e.usedNodes {
		if id == nodeID {
			seen = true
			break
		}
	}
	if !seen && nodeID != "" {
		e.usedNodes = append(e.usedNodes, nodeID)
	}
	e.mu.Unlock()
	return nodeID, nil
}

func (e *chatExecEnv) modelForRole(role string) string {
	if id := profiles.RoleModel(e.profile, role); id != "" {
		return id
	}
	if e.modelOverride != "" {
		return e.modelOverride
	}
	for _, r := range e.profile.Roles {
		if r.ModelID != "" {
			return r.ModelID
		}
	}
	return ""
}

// TurnInstructions returns trusted instructions for this turn: a specialized
// AI's system instructions. The simple orchestrator places them ahead of its
// own system prompt.
// nowLine tells the model the date and time where the person is, such as
// "Friday, October 2, 2026, 6:42 PM AKDT (UTC-08:00, America/Juneau)".
func nowLine(now time.Time, loc *time.Location) string {
	if now.IsZero() {
		now = time.Now()
	}
	t := now.In(loc)
	zone := t.Format("MST (UTC-07:00")
	if name := loc.String(); name != "" && name != "Local" && name != "UTC" {
		zone += ", " + name
	}
	return "The current date and time for the user: " + t.Format("Monday, January 2, 2006, 3:04 PM ") + zone +
		"). Use it for anything relative to now, such as \"today\", \"this Friday\", or \"in 3 hours\"."
}

func (e *chatExecEnv) TurnInstructions(ctx context.Context, prompt string) string {
	var parts []string
	if s := strings.TrimSpace(e.instructions); s != "" {
		parts = append(parts, s)
	}
	// How the person likes answers (§38): style only, never permission.
	if e.app != nil {
		if block := e.app.personalBlock(ctx); block != "" {
			parts = append(parts, block)
		}
		// The language to answer in (multilingual spec §11), from the
		// person's own message: a worker's prompt in a plan is the planner's.
		message := e.turnPrompt
		if strings.TrimSpace(message) == "" {
			message = prompt
		}
		parts = append(parts, e.app.replyLanguage(ctx, e.conversationID, message, e.responseLanguage).Instruction())
	}
	// The model has no clock: "this Friday" or "in 3 hours" needs the date
	// and time where the person is.
	tctx := e.ctx
	if tctx == nil {
		tctx = ctx
	}
	parts = append(parts, nowLine(e.startedAt, locale.TimeZone(tctx)))
	// What Yggdrasil can do comes from its own inventory (§37).
	if e.capabilities != "" {
		parts = append(parts, e.capabilities)
	}
	// So does how to use it: the user guide ships with Yggdrasil.
	if e.guide != "" {
		parts = append(parts, e.guide)
	}
	// Memories come from the person, so they are trusted instructions.
	if block := muninn.Block(e.memories); block != "" {
		parts = append(parts, block)
	}
	// The app calling the API holds a key to this computer, so its system
	// messages are trusted too. They cannot change what tools may do.
	if e.opts != nil && strings.TrimSpace(e.opts.System) != "" {
		parts = append(parts, "Instructions from the application using the API:\n"+strings.TrimSpace(e.opts.System))
	}
	return strings.Join(parts, "\n\n")
}

// ReferenceMaterial returns connected knowledge for this turn. It is
// untrusted content (§58), so the orchestrator delivers it as labelled data
// next to the question, never in the system prompt.
func (e *chatExecEnv) ReferenceMaterial(ctx context.Context, prompt string) string {
	parts := []string{}
	for _, block := range []string{e.attachmentBlock(ctx, prompt), e.knowledgeBlock(ctx, prompt)} {
		if block != "" {
			parts = append(parts, block)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (e *chatExecEnv) knowledgeBlock(ctx context.Context, prompt string) string {
	ids := append(append([]string(nil), e.profile.KnowledgeSources...), e.knowledge...)
	if len(ids) == 0 || e.app == nil || e.app.Mimir == nil {
		return ""
	}
	hits, err := e.app.Mimir.Search(ctx, mimir.SearchInput{Query: prompt, SourceIDs: ids})
	if err != nil {
		e.Emit("knowledge.failed", map[string]any{"error": err.Error()})
		return ""
	}
	names := []string{}
	seen := map[string]bool{}
	for _, h := range hits {
		if !seen[h.SourceName] {
			seen[h.SourceName] = true
			names = append(names, h.SourceName)
		}
	}
	e.markLocalSources(ctx, hits)
	e.Emit("knowledge.retrieved", map[string]any{"passages": len(hits), "sources": names})
	if e.trace != nil {
		e.trace.knowledge(hits)
	}
	return mimir.ContextBlock(hits, 0)
}

// ListNodesAdapter for task manager.
func (a *App) listNodes(ctx context.Context) ([]contracts.Node, error) {
	return a.listNodesWithHardware(ctx)
}

// switchModel points the turn at another model after the first one failed.
// Placement is redone for the new model.
// retryElsewhere keeps the model and places the turn again, never on
// failedNode, where the model just failed.
func (e *chatExecEnv) retryElsewhere(failedNode string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.excluded = append(e.excluded, failedNode)
	e.lastModel = ""
	e.roleNodes = nil
	e.roleModels = nil
	e.usedNodes = nil
}

// excludedNodes are the computers this turn no longer uses.
func (e *chatExecEnv) excludedNodes() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.excluded...)
}

// keepsLocal reports whether the turn must stay on this computer.
func (e *chatExecEnv) keepsLocal() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.localOnly || e.adapter != ""
}

func (e *chatExecEnv) switchModel(profile profiles.Profile, modelID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.profile = profile
	e.modelOverride = modelID
	e.lastModel = ""
	e.roleNodes = nil
	e.roleModels = nil
	e.usedNodes = nil
}

// withModelTools marks a turn whose model cannot call tools. Its tools stay
// allowed, so the look-ups Toskar runs itself still give it the web; it is
// only not asked to call any. Denying them all, as before, left such a model
// answering current questions from memory.
func (a *App) withModelTools(profile profiles.Profile, modelID string) profiles.Profile {
	profile.ModelCallsNoTools = false
	if modelID != "" && a.Models != nil {
		if entry, ok := a.Models.Catalog().Get(modelID); ok && toolCallSupport(entry.Capabilities) == "unsupported" {
			profile.ModelCallsNoTools = true
		}
	}
	return profile
}

// modelName is how a model is shown to the user.
func (a *App) modelName(modelID string) string {
	if a.Models != nil {
		if entry, ok := a.Models.Catalog().Get(modelID); ok && entry.DisplayName != "" {
			return entry.DisplayName
		}
	}
	return modelID
}

// keepStopped saves the part of an answer written before the user stopped
// the turn, marked as stopped, and tells every client the turn ended.
func (a *App) keepStopped(ctx context.Context, env *chatExecEnv, conversationID, full string) {
	// ctx is cancelled by now; the save must still happen.
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	ctx = context.WithoutCancel(ctx)
	kept := strings.TrimSpace(full) != ""
	env.trace.stopped(kept, timedOut)
	// Keep the answer so far; with none, keep a short note, so the stop is
	// visible and the sources and steps already gathered are not lost.
	content := full
	if !kept {
		content = "_Stopped before the answer was written._"
	}
	if conversationID != "" {
		if save, _ := a.Settings.GetBool(ctx, "save_chat_history", true); save {
			_, _ = a.Conversations.AddMessageWithMeta(ctx, conversationID, "assistant", content, env.trace.meta())
		}
	}
	a.Bus.Publish(events.New(events.ChatStopped, map[string]any{
		"conversation_id": conversationID,
		"kept":            kept,
	}))
}

// noteTraining says once per turn that a training run holds this computer:
// that the answer may be slower when it runs here, or which computer
// answers instead.
func (e *chatExecEnv) noteTraining(nodeID string) {
	e.mu.Lock()
	if e.trainingNoted {
		e.mu.Unlock()
		return
	}
	e.trainingNoted = true
	e.mu.Unlock()
	local := ""
	if e.app.Config != nil {
		local = e.app.Config.Get().NodeID
	}
	if nodeID != "" && nodeID != local && e.app.isTraining() {
		name := nodeID
		if n, err := e.app.findPairedNode(e.ctx, nodeID); err == nil {
			name = nodeDisplayName(n)
		}
		e.trace.sharing(locale.T(e.trace.lang, "chat:steps.trainingMoved", map[string]any{"computer": name}))
		return
	}
	if busy, ok := e.app.trainingStep(e.trace.lang); ok {
		e.trace.sharing(busy)
	}
}
