package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/internal/personal"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// memoryOn reports whether persistent memory feeds this conversation: the
// global setting, unless the conversation turned it off.
func (a *App) memoryOn(ctx context.Context, conversationID string) bool {
	if a.Muninn == nil {
		return false
	}
	if on, _ := a.Settings.GetBool(ctx, "memory_enabled", true); !on {
		return false
	}
	if conversationID == "" {
		return true
	}
	conv, err := a.Conversations.Get(ctx, conversationID)
	return err != nil || !conv.MemoryOff
}

// handleMemoryCommand answers "Remember that …", "Forget that …", and "What
// do you remember about me?" without a model, so they work the same with any
// model and even when none is installed. It returns false for any other
// message.
func (a *App) handleMemoryCommand(ctx context.Context, conversationID, message string) (<-chan pluginapi.ChatChunk, bool) {
	if a.Muninn == nil {
		return nil, false
	}
	cmd := muninn.ParseCommand(message)
	if cmd.Kind == muninn.CommandNone {
		return nil, false
	}
	on := a.memoryOn(ctx, conversationID)
	var reply string
	var step *contracts.ActivityStep
	switch cmd.Kind {
	case muninn.CommandRemember:
		m, created, err := a.Muninn.Add(ctx, cmd.Text, "", muninn.SourceExplicit, conversationID)
		switch {
		case errors.Is(err, muninn.ErrSensitive):
			reply = "I didn't save that because it looks like a password, key, or card number. A password manager is the safer place for those."
		case errors.Is(err, personal.ErrPermission):
			reply = "I didn't save that, because a memory can't give me permission to do things. What tools may do without asking is set in Settings › Tool permissions or in a profile, so it never changes by accident. I'm happy to remember a preference, like which tools you use most."
		case err != nil:
			reply = "I couldn't save that: " + err.Error() + "."
		case !created:
			reply = fmt.Sprintf("I already remember that: “%s”.", m.Content)
		default:
			reply = fmt.Sprintf("Got it. I'll remember: “%s”.", m.Content)
			step = &contracts.ActivityStep{Kind: "memory", Text: "Saved to memory: " + m.Content}
			a.Bus.Publish(events.New("memory.saved", map[string]any{"memory": m, "conversation_id": conversationID}))
		}
		if err == nil && !on {
			reply += " Memory is off for this chat, so I'll use it in your other chats."
		}
	case muninn.CommandForget:
		m, ok, err := a.Muninn.Find(ctx, cmd.Text)
		switch {
		case err != nil:
			reply = "I couldn't look through my memories: " + err.Error() + "."
		case !ok:
			reply = "I couldn't find a memory like that. The Memory page lists everything I remember."
		default:
			if err := a.Muninn.Delete(ctx, m.ID); err != nil {
				reply = "I couldn't remove that memory: " + err.Error() + "."
			} else {
				reply = fmt.Sprintf("Done. I forgot: “%s”.", m.Content)
				step = &contracts.ActivityStep{Kind: "memory", Text: "Removed from memory: " + m.Content}
				a.Bus.Publish(events.New("memory.deleted", map[string]any{"id": m.ID, "conversation_id": conversationID}))
			}
		}
	case muninn.CommandList:
		reply = a.describeMemories(ctx, on)
	}

	var meta *contracts.MessageMeta
	if step != nil {
		meta = &contracts.MessageMeta{Steps: []contracts.ActivityStep{*step}}
	}
	return a.replyDirectly(ctx, conversationID, message, reply, meta), true
}

// replyDirectly answers without a model: it saves the turn, tells every
// client, and returns the reply as a finished stream.
func (a *App) replyDirectly(ctx context.Context, conversationID, message, reply string, meta *contracts.MessageMeta) <-chan pluginapi.ChatChunk {
	if conversationID != "" {
		if save, _ := a.Settings.GetBool(ctx, "save_chat_history", true); save {
			_, _ = a.Conversations.AddMessage(ctx, conversationID, "user", message)
			_, _ = a.Conversations.AddMessageWithMeta(ctx, conversationID, "assistant", reply, meta)
		}
	}
	a.Bus.Publish(events.New(events.ChatToken, map[string]any{"conversation_id": conversationID, "content": reply}))
	complete := map[string]any{"conversation_id": conversationID}
	if meta != nil {
		complete["meta"] = meta
	}
	a.Bus.Publish(events.New(events.ChatComplete, complete))

	ch := make(chan pluginapi.ChatChunk, 2)
	ch <- pluginapi.ChatChunk{Content: reply}
	ch <- pluginapi.ChatChunk{Done: true}
	close(ch)
	return ch
}

func (a *App) describeMemories(ctx context.Context, on bool) string {
	all, err := a.Muninn.List(ctx)
	if err != nil {
		return "I couldn't read my memories: " + err.Error() + "."
	}
	var enabled []muninn.Memory
	for _, m := range all {
		if m.Enabled {
			enabled = append(enabled, m)
		}
	}
	if len(enabled) == 0 {
		return "I don't remember anything yet. Say “Remember that …” and I'll keep it for future chats."
	}
	var b strings.Builder
	b.WriteString("Here's what I remember:\n\n")
	for i, m := range enabled {
		if i == 15 {
			fmt.Fprintf(&b, "\n…and %d more on the Memory page.", len(enabled)-15)
			break
		}
		b.WriteString("- " + m.Content + "\n")
	}
	if !on {
		b.WriteString("\nMemory is off for this chat, so I'm not using these here.")
	}
	b.WriteString("\nYou can edit or remove any of these on the Memory page.")
	return b.String()
}

// summarizeLater checks, after a reply, whether a long conversation needs a
// new summary, and writes one in the background with the model that just
// answered. The reply is never delayed by it.
func (a *App) summarizeLater(conversationID, modelID string, windowTokens int) {
	if a.Muninn == nil || a.summarizer == nil || a.stubInference || modelID == "" || strings.HasPrefix(modelID, "sai:") {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		stored, err := a.Conversations.ListMessages(ctx, conversationID)
		if err != nil {
			return
		}
		job := &muninn.Summarizer{Store: a.Muninn, ModelID: modelID, Count: a.tokenCounter(ctx, modelID),
			Generate: func(ctx context.Context, msgs []pluginapi.ChatMessage) (string, error) {
				return a.generateOnce(ctx, modelID, "", a.localAdapters(ctx, modelID), msgs)
			}}
		ran, err := a.summarizer.RunWith(ctx, job, conversationID, stored, windowTokens)
		switch {
		case err != nil:
			a.Logger.Warn("summarize conversation", "conversation_id", conversationID, "error", err)
		case ran:
			a.Bus.Publish(events.New("chat.summarized", map[string]any{"conversation_id": conversationID}))
		}
	}()
}
