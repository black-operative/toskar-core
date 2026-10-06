package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// A chat's task ends completed when the reply is done, so it stops counting
// as active, and raises no "task finished" notice of its own.
func TestChatTaskEndsCompleted(t *testing.T) {
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	a.StubReply = func(string, []pluginapi.ChatMessage) string { return "Hello there." }
	ctx := context.Background()
	conv, err := a.Conversations.Create(ctx, "task check", "general-assistant", "auto")
	if err != nil {
		t.Fatal(err)
	}
	subID, evs := a.Bus.Subscribe()
	defer a.Bus.Unsubscribe(subID)
	stream, err := a.RunChat(ctx, "general-assistant", conv.ID, "Hi! How are you?", false, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	list, err := a.Tasks.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Status != contracts.TaskCompleted {
		t.Fatalf("tasks: %+v", list)
	}
	for {
		select {
		case evt := <-evs:
			if evt.Type == events.TaskCompleted || evt.Type == events.TaskFailed {
				t.Fatalf("a chat raised %s", evt.Type)
			}
			continue
		default:
		}
		break
	}
}

// Tasks an earlier run left unfinished, such as chats from before tasks
// were settled, are ended at the next start.
func TestSettleInterrupted(t *testing.T) {
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	ctx := context.Background()
	stuck, _ := a.Tasks.Create(ctx, "", "", "an old chat")
	done, _ := a.Tasks.Create(ctx, "", "", "a finished chat")
	a.Tasks.SetChatStatus(ctx, done.ID, contracts.TaskCompleted, "")
	if n, err := a.Tasks.SettleInterrupted(ctx); err != nil || n != 1 {
		t.Fatalf("settled %d, %v", n, err)
	}
	got, _ := a.Tasks.Get(ctx, stuck.ID)
	if got.Status != contracts.TaskFailed {
		t.Errorf("stuck task: %+v", got)
	}
	if got, _ := a.Tasks.Get(ctx, done.ID); got.Status != contracts.TaskCompleted {
		t.Errorf("finished task changed: %+v", got)
	}
}
