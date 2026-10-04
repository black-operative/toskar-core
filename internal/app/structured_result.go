package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/internal/structured"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// ensureStructured makes sure a condition automation's result ends with
// the JSON its condition reads (spec §27). A result whose JSON is missing
// or wrong after safe repairs is sent back to the model once, with the
// problems named. The prose is kept as written; the JSON stays internal.
func (e automationExecutor) ensureStructured(ctx context.Context, env *automationEnv, automation automations.Automation, text string) string {
	schema := automations.ConditionSchema(automation.Notification)
	if schema == nil {
		return text
	}
	first := structured.Parse(text, schema)
	if first.OK() {
		return text
	}
	role := "assistant"
	if roles := env.base.profile.Roles; len(roles) > 0 && roles[0].Role != "" {
		role = roles[0].Role
	}
	ask := []pluginapi.ChatMessage{
		{Role: "system", Content: "You turn an answer into the JSON a program reads. Use only facts in the answer."},
		{Role: "user", Content: "Answer:\n" + text + "\n\n" + structured.FixPrompt(first, schema)},
	}
	reply, err := collectText(ctx, env.base, role, ask)
	if err != nil {
		return text
	}
	fixed := structured.Parse(reply, schema)
	if !fixed.OK() {
		if e.app.Logger != nil {
			e.app.Logger.Info("automation result has no usable JSON", "automation", automation.Name, "issues", structured.Describe(fixed.Issues))
		}
		return text
	}
	runlog.From(ctx).Note("repaired", nil)
	raw, _ := json.Marshal(fixed.Value)
	prose := text
	if first.Value != nil {
		prose = structured.Prose(text, first.Found)
	}
	return strings.TrimSpace(prose) + "\n" + string(raw)
}

// collectText runs one model call and returns its text.
func collectText(ctx context.Context, env *chatExecEnv, role string, messages []pluginapi.ChatMessage) (string, error) {
	ch, err := env.Generate(ctx, role, messages)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for chunk := range ch {
		if chunk.Error != "" {
			return b.String(), errString(chunk.Error)
		}
		b.WriteString(chunk.Content)
	}
	return b.String(), nil
}
