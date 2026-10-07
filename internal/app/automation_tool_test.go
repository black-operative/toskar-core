package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/locale"
)

// Chat drafts an automation from a request in the person's language, and
// the answer carries it for them to confirm (#204).
func TestScheduleToolDraftsForTheAnswer(t *testing.T) {
	a := &App{}
	tool := &scheduleTool{app: a}
	ctx := locale.WithTimeZone(context.Background(), "America/Juneau")
	ctx = withChatProfile(artifacts.WithConversation(ctx, "conv-1"), "general-assistant")
	args := map[string]any{"request": "Fasse jeden Morgen um 7:30 Uhr die Nachrichten zusammen."}
	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	draft := draftFrom(result)
	if draft == nil || draft.ID == "" || draft.ProfileID != "general-assistant" || draft.Name == "" {
		t.Fatalf("draft = %+v", draft)
	}
	var schedule automations.Schedule
	if err := json.Unmarshal(draft.Schedule, &schedule); err != nil {
		t.Fatal(err)
	}
	if schedule.Kind != automations.KindDaily || schedule.Hour != 7 || schedule.Minute != 30 || schedule.TimeZone != "America/Juneau" {
		t.Fatalf("schedule = %+v", schedule)
	}

	trace := &turnTrace{}
	trace.tool("automations.schedule", args, result)
	meta := trace.meta()
	if meta == nil || meta.Automation == nil || meta.Automation.ID != draft.ID || len(meta.Steps) != 1 {
		t.Fatalf("meta = %+v", meta)
	}

	// An automation's own run has no chat to confirm in.
	if _, err := tool.Execute(locale.WithTimeZone(context.Background(), "UTC"), args); err == nil {
		t.Fatal("drafted outside a chat")
	}
}
