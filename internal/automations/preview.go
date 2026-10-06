package automations

import (
	"context"
	"fmt"
	"strings"
)

// Preview is one unsaved run of a draft automation.
type Preview struct {
	Result      string `json:"result,omitempty"`
	Error       string `json:"error,omitempty"`
	ModelID     string `json:"model_id,omitempty"`
	NodeID      string `json:"node_id,omitempty"`
	WouldNotify bool   `json:"would_notify"`
	Reason      string `json:"reason"`
}

// Preview runs the draft once and reports whether its notification rule would match.
// It does not store an automation or a history row.
func (r *Runner) Preview(ctx context.Context, in CreateInput) (Preview, error) {
	if r == nil || r.Exec == nil {
		return Preview{}, fmt.Errorf("automation runner is not configured")
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return Preview{}, fmt.Errorf("prompt is required")
	}
	if strings.TrimSpace(in.ModelID) == "" {
		return Preview{}, fmt.Errorf("model is required")
	}
	if err := in.Schedule.Validate(); err != nil {
		return Preview{}, err
	}
	in.Notification.Normalize()
	if err := in.Notification.Validate(); err != nil {
		return Preview{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "Automation"
	}
	draft := Automation{
		ID:           "preview",
		Name:         name,
		Prompt:       strings.TrimSpace(in.Prompt),
		ProfileID:    strings.TrimSpace(in.ProfileID),
		ModelID:      strings.TrimSpace(in.ModelID),
		Tools:        in.Tools,
		Notification: in.Notification,
		// The preview answers as the saved automation will: in its response
		// language, with "today" in its schedule's time zone (#204).
		ResponseLanguage: strings.TrimSpace(in.ResponseLanguage),
		Schedule:         in.Schedule,
	}
	result, execErr := r.Exec.Execute(ctx, draft)
	decision := Decide(draft.Notification, result.Text, nil, false)
	out := Preview{
		Result:      strings.TrimSpace(result.Text),
		ModelID:     result.ModelID,
		NodeID:      result.NodeID,
		WouldNotify: execErr == nil && decision.Notify,
		Reason:      decision.Reason,
	}
	if execErr != nil {
		out.Error = execErr.Error()
		out.WouldNotify = false
		if out.Reason == "" {
			out.Reason = "the run did not finish"
		}
	}
	return out, nil
}
