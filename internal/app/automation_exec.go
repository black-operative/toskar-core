package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"slices"
	"strings"
	"sync"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/internal/share"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// automationExecutor runs a scheduled prompt through the profile's orchestrator.
// Placement goes through Norn. A model started for the run is marked used afterward
// so the normal idle sweeper can unload it. The run does not pin the model.
type automationExecutor struct {
	app *App
}

// Execute runs an automation and traces the run (§35).
func (e automationExecutor) Execute(ctx context.Context, automation automations.Automation) (automations.Execution, error) {
	run := runlog.New(uuid.NewString(), "", automation.ProfileID, egress.SourceAutomation)
	run.SetLanguage(e.app.appLanguage(ctx))
	run.Note("automation", map[string]any{"name": automation.Name})
	result, err := e.execute(runlog.With(ctx, run), automation)
	status, runErr := runlog.StatusCompleted, error(nil)
	switch {
	case ctx.Err() != nil:
		status = runlog.StatusStopped
	case err != nil:
		status, runErr = runlog.StatusFailed, codedError(err)
		// The runner retries by the error's code, not its English (#204).
		err = runErr
	}
	if e.app != nil && e.app.RunLog != nil {
		if saveErr := e.app.RunLog.Save(context.WithoutCancel(ctx), run.Finish(status, runErr)); saveErr != nil && e.app.Logger != nil {
			e.app.Logger.Warn("save automation run", "automation", automation.Name, "error", saveErr)
		}
	}
	return result, err
}

func (e automationExecutor) execute(ctx context.Context, automation automations.Automation) (automations.Execution, error) {
	if e.app == nil || e.app.Profiles == nil || e.app.OrchRegistry == nil {
		return automations.Execution{}, fmt.Errorf("automation executor is not configured")
	}
	if strings.TrimSpace(automation.ProfileID) == "" {
		return automations.Execution{}, fmt.Errorf("profile is required")
	}
	if strings.TrimSpace(automation.ModelID) == "" {
		return automations.Execution{}, fmt.Errorf("model is required")
	}
	profile, err := e.app.Profiles.Get(ctx, automation.ProfileID)
	if err != nil {
		return automations.Execution{}, err
	}
	// A chat on this computer goes first; the run waits for it instead of
	// loading a model alongside it (§60).
	ctx = egress.WithRun(ctx, egress.Run{Source: egress.SourceAutomation, TaskID: "automation:" + automation.ID})
	// Its answer's "today" is in the time zone its schedule runs in.
	ctx = locale.WithTimeZone(ctx, automation.Schedule.TimeZone)
	work, err := e.app.enterWork(ctx, share.Automation, automation.Name, nil)
	if err != nil {
		return automations.Execution{}, err
	}
	defer work.Done()
	// Same stack as chat (spec §30): Auto picks the model, and the run gets
	// connected knowledge, relevant memories, and Huginn's effort budget.
	modelID := automation.ModelID
	if modelID == huginn.AutoModelID {
		lang := e.app.replyLanguage(ctx, "", automation.Prompt, automation.ResponseLanguage).Tag
		choice, err := e.app.chooseAuto(ctx, automation.Prompt, e.app.turnHasData(ctx, "", profile), lang)
		if err != nil {
			return automations.Execution{}, err
		}
		modelID = choice.Model.ID
	}
	profile = tools.WithConnected(withChatModel(profile, modelID))
	orch, err := e.app.OrchRegistry.Get(profile.OrchestratorID)
	if err != nil {
		return automations.Execution{}, err
	}
	if err := orch.ValidateProfile(ctx, profile); err != nil {
		return automations.Execution{}, err
	}

	var disabled map[string]struct{}
	if e.app.Tools != nil {
		disabled = e.app.Tools.Disabled()
	}
	env := &automationEnv{
		base: &chatExecEnv{
			app:           e.app,
			ctx:           ctx,
			profile:       profile,
			modelOverride: modelID,
			taskID:        automation.ID,
			turnPrompt:    automation.Prompt,
			trace:         &turnTrace{lang: e.app.appLanguage(ctx)},
			// Results in the automation's response language (§22).
			responseLanguage: automation.ResponseLanguage,
		},
		granted: automation.Tools,
	}
	if e.app.Muninn != nil && e.app.Settings != nil {
		if on, _ := e.app.Settings.GetBool(ctx, "memory_enabled", true); on {
			if mems, err := e.app.Muninn.Relevant(ctx, automation.Prompt); err == nil {
				env.base.memories = mems
			}
		}
	}
	defer env.releaseModels()

	stream, err := orch.Run(ctx, contracts.Task{
		ID:        automation.ID,
		ProfileID: profile.ID,
		Prompt:    automation.Prompt,
		Status:    contracts.TaskRunning,
	}, tools.ForUnattended(profile, automation.Tools, disabled), env)
	if err != nil {
		return env.execution(""), err
	}
	text, nodeID, runErr := collectAutomationEvents(stream)
	if runErr == nil && ctx.Err() == nil {
		text = e.ensureStructured(ctx, env, automation, text)
	}
	out := env.execution(text)
	if nodeID != "" {
		out.NodeID = nodeID
	}
	out.Skipped = env.skippedTools()
	e.reportSkipped(ctx, automation, out.Skipped)
	return out, runErr
}

// reportSkipped tells the person which actions a run skipped because nobody
// approved them, and where to approve them for later runs (spec §59).
func (e automationExecutor) reportSkipped(ctx context.Context, automation automations.Automation, skipped []string) {
	if len(skipped) == 0 || e.app.Notifications == nil {
		return
	}
	names := make([]string, 0, len(skipped))
	for _, id := range skipped {
		if def, ok := tools.Lookup(id); ok {
			names = append(names, def.Name)
		} else {
			names = append(names, id)
		}
	}
	tools := strings.Join(names, ", ")
	m := notice("toolsSkippedUnnamed", map[string]any{"tools": tools}, "toolsSkippedBody", nil)
	if name := strings.TrimSpace(automation.Name); name != "" {
		m = notice("toolsSkipped", map[string]any{"automation": name, "tools": tools}, "toolsSkippedBody", nil)
	}
	_, _ = e.app.Notifications.Notify(context.WithoutCancel(ctx), gjallarhorn.Request{
		SourceType: "automation",
		SourceID:   automation.ID,
		Category:   gjallarhorn.CategoryApproval,
		Severity:   gjallarhorn.SeverityWarning,
		Message:    m,
		Link:       "/automations?id=" + automation.ID,
		DedupeKey:  "automation.skipped:" + automation.ID,
		Channels:   []string{"desktop"},
	})
}

type automationEnv struct {
	base    *chatExecEnv
	granted []string

	mu      sync.Mutex
	skipped []string
}

// ReferenceMaterial and TurnInstructions give a scheduled run the same
// knowledge and memories a chat turn gets.
func (e *automationEnv) ReferenceMaterial(ctx context.Context, prompt string) string {
	return e.base.ReferenceMaterial(ctx, prompt)
}

func (e *automationEnv) TurnInstructions(ctx context.Context, prompt string) string {
	return e.base.TurnInstructions(ctx, prompt)
}

func (e *automationEnv) skippedTools() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.skipped...)
}

func (e *automationEnv) Generate(ctx context.Context, role string, messages []pluginapi.ChatMessage) (<-chan pluginapi.ChatChunk, error) {
	return e.base.Generate(ctx, role, messages)
}

func (e *automationEnv) ExecuteTool(ctx context.Context, toolID string, args map[string]any) (map[string]any, error) {
	if e.base.app != nil && e.base.app.Tools != nil && e.base.app.Tools.IsDisabled(toolID) {
		return nil, fmt.Errorf("tool %q is disabled", toolID)
	}
	policy, err := tools.UnattendedPolicy(e.base.profile, e.granted, toolID)
	if errors.Is(err, tools.ErrNeedsApproval) {
		// Skip and report; never ask or widen with nobody watching (§59).
		e.mu.Lock()
		if !slices.Contains(e.skipped, toolID) {
			e.skipped = append(e.skipped, toolID)
		}
		e.mu.Unlock()
		return nil, fmt.Errorf("skipped: %s needs the person's approval for this automation; continue without it", toolID)
	}
	if err != nil {
		return nil, err
	}
	return e.base.app.Tools.Execute(ctx, toolID, args, policy, "scheduled automation", map[string]any{
		"automation_id": e.base.taskID,
	})
}

func (e *automationEnv) Emit(eventType string, payload map[string]any) {
	e.base.Emit(eventType, payload)
}

func (e *automationEnv) NodeForRole(role string) (string, error) {
	return e.base.NodeForRole(role)
}

func (e *automationEnv) execution(text string) automations.Execution {
	return automations.Execution{Text: text, ModelID: e.base.modelID()}
}

// releaseModels returns every model this run started on this computer to the
// normal idle policy. It does not stop a model the user already had loaded.
func (e *automationEnv) releaseModels() {
	if e.base == nil || e.base.app == nil {
		return
	}
	e.base.mu.Lock()
	var models []string
	for role, modelID := range e.base.roleModels {
		if modelID == "" || !e.base.app.modelOnThisComputer(e.base.roleNodes[role]) {
			continue
		}
		models = append(models, modelID)
	}
	e.base.mu.Unlock()
	seen := map[string]struct{}{}
	for _, id := range models {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		e.base.app.markModelUsed(id)
	}
}

func (a *App) modelOnThisComputer(nodeID string) bool {
	if nodeID == "" || nodeID == "local" {
		return true
	}
	if a == nil || a.Config == nil {
		return false
	}
	return nodeID == a.Config.Get().NodeID
}

func collectAutomationEvents(stream <-chan pluginapi.OrchestrationEvent) (text, nodeID string, err error) {
	var b strings.Builder
	for evt := range stream {
		if evt.NodeID != "" {
			nodeID = evt.NodeID
		}
		if evt.Error != "" {
			err = fmt.Errorf("%s", evt.Error)
		}
		if evt.Content != "" && evt.Done {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(evt.Content)
		}
	}
	return strings.TrimSpace(b.String()), nodeID, err
}
