package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

type chatProfileKey struct{}

// withChatProfile tells a tool which profile the chat runs with.
func withChatProfile(ctx context.Context, profileID string) context.Context {
	return context.WithValue(ctx, chatProfileKey{}, profileID)
}

func chatProfileFrom(ctx context.Context) string {
	id, _ := ctx.Value(chatProfileKey{}).(string)
	return id
}

// scheduleTool is automations.schedule (#204): it reads a request the way
// the Automations page and toskarctl do and drafts the automation. The
// answer shows the draft as a card, and nothing is scheduled until the
// person presses Create, so it asks first without stopping the turn.
type scheduleTool struct {
	app *App
}

func (t *scheduleTool) ID() string          { return "automations.schedule" }
func (t *scheduleTool) DisplayName() string { return "Schedule Task" }
func (t *scheduleTool) Description() string {
	return "Draft a task that runs on a schedule, for the user to confirm"
}

func (t *scheduleTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	request, _ := args["request"].(string)
	request = strings.TrimSpace(request)
	if request == "" {
		return nil, errors.New("request required: what to do and when, in the user's words")
	}
	conversationID := artifacts.ConversationFrom(ctx)
	if conversationID == "" {
		return nil, errors.New("tasks can be scheduled only from a chat")
	}
	zone := locale.TimeZone(ctx).String()
	if zone == "Local" {
		zone = ""
	}
	// The request is read in the language it's written in, and the name
	// and notes are in that language too.
	language := automations.RequestLanguage(request)
	if language == "" {
		language = t.app.replyLanguage(ctx, conversationID, request, "").Tag
	}
	parsed, err := t.app.parseAutomation(ctx, request, zone, language)
	if err != nil {
		return nil, err
	}
	schedule, err := json.Marshal(parsed.Schedule)
	if err != nil {
		return nil, err
	}
	notification, err := json.Marshal(parsed.Notification)
	if err != nil {
		return nil, err
	}
	draft := contracts.AutomationDraft{
		ID:           uuid.NewString(),
		Name:         parsed.Name,
		Prompt:       parsed.Prompt,
		Schedule:     schedule,
		Notification: notification,
		ProfileID:    chatProfileFrom(ctx),
		Notes:        parsed.Notes,
	}
	return map[string]any{
		"draft":        draft,
		"name":         parsed.Name,
		"task":         parsed.Prompt,
		"schedule":     parsed.Schedule,
		"notification": parsed.Notification,
		"notes":        parsed.Notes,
		"status":       "Not scheduled yet. The user sees a card with this automation below your answer and creates it there. Say briefly what it will do and when, and ask them to check it and press Create.",
	}, nil
}

// draftFrom reads the draft from the tool's result.
func draftFrom(result map[string]any) *contracts.AutomationDraft {
	switch d := result["draft"].(type) {
	case contracts.AutomationDraft:
		return &d
	case *contracts.AutomationDraft:
		return d
	case map[string]any:
		raw, err := json.Marshal(d)
		if err != nil {
			return nil
		}
		var draft contracts.AutomationDraft
		if json.Unmarshal(raw, &draft) != nil || draft.ID == "" {
			return nil
		}
		return &draft
	}
	return nil
}

// postAutomationResult adds a run's result to the chat the automation was
// made from (#204), as an answer the person can reply to.
func (a *App) postAutomationResult(ctx context.Context, automation automations.Automation, run automations.Run, text string) error {
	if a.Conversations == nil {
		return errors.New("conversations are not available")
	}
	_, err := a.Conversations.AddMessageWithMeta(ctx, automation.ConversationID, "assistant", text, &contracts.MessageMeta{
		AutomationRun: &contracts.AutomationRunRef{AutomationID: automation.ID, RunID: run.ID, Name: automation.Name},
		Contract:      contracts.ContractVersion,
	})
	return err
}
