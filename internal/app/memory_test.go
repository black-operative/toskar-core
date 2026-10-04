package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

func memoryApp(t *testing.T) (*App, string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	a := &App{
		DB:            db,
		Muninn:        muninn.NewStore(db.SQL),
		Settings:      repositories.NewSettingsRepo(db.SQL),
		Conversations: repositories.NewConversationRepo(db.SQL),
		Bus:           events.NewBus(32),
	}
	conv, err := a.Conversations.Create(context.Background(), "t", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return a, conv.ID
}

func reply(t *testing.T, a *App, convID, msg string) (string, bool) {
	t.Helper()
	ch, ok := a.handleMemoryCommand(context.Background(), convID, msg)
	if !ok {
		return "", false
	}
	var b strings.Builder
	for c := range ch {
		b.WriteString(c.Content)
	}
	return b.String(), true
}

func TestMemoryCommandsInChat(t *testing.T) {
	a, conv := memoryApp(t)
	ctx := context.Background()

	if _, ok := reply(t, a, conv, "What is the capital of France?"); ok {
		t.Fatal("an ordinary question was handled as a memory command")
	}
	got, ok := reply(t, a, conv, "Remember that I prefer metric units.")
	if !ok || got != "Got it. I'll remember: “I prefer metric units”." {
		t.Fatalf("remember = %q", got)
	}
	if got, _ := reply(t, a, conv, "please remember I prefer metric units"); !strings.HasPrefix(got, "I already remember that") {
		t.Fatalf("duplicate = %q", got)
	}
	if got, _ := reply(t, a, conv, "Remember my password is hunter2"); !strings.Contains(got, "didn't save that") {
		t.Fatalf("secret = %q", got)
	}
	if got, _ := reply(t, a, conv, "What do you remember about me?"); !strings.Contains(got, "- I prefer metric units") {
		t.Fatalf("list = %q", got)
	}
	// The exchange is saved, with the memory step on the answer.
	msgs, _ := a.Conversations.ListMessages(ctx, conv)
	if len(msgs) != 8 || msgs[1].Meta == nil || msgs[1].Meta.Steps[0].Kind != "memory" {
		t.Fatalf("messages = %+v", msgs)
	}
	if got, _ := reply(t, a, conv, "Forget that I prefer metric units"); got != "Done. I forgot: “I prefer metric units”." {
		t.Fatalf("forget = %q", got)
	}
	if got, _ := reply(t, a, conv, "Forget that I live on Mars"); !strings.Contains(got, "couldn't find a memory") {
		t.Fatalf("forget missing = %q", got)
	}
	if all, _ := a.Muninn.List(ctx); len(all) != 0 {
		t.Fatalf("memories left = %+v", all)
	}
}

func TestMemoryOffStillSavesButDoesNotInject(t *testing.T) {
	a, conv := memoryApp(t)
	ctx := context.Background()
	off := true
	if _, err := a.Conversations.Update(ctx, conv, repositories.ConversationPatch{MemoryOff: &off}); err != nil {
		t.Fatal(err)
	}
	if a.memoryOn(ctx, conv) {
		t.Fatal("memory should be off for this conversation")
	}
	got, _ := reply(t, a, conv, "Remember that my name is Sam")
	if !strings.Contains(got, "Memory is off for this chat") {
		t.Fatalf("reply = %q", got)
	}
	other, _ := a.Conversations.Create(ctx, "other", "", "")
	if !a.memoryOn(ctx, other.ID) {
		t.Fatal("memory should be on for other conversations")
	}
	_ = a.Settings.SetBool(ctx, "memory_enabled", false)
	if a.memoryOn(ctx, other.ID) {
		t.Fatal("the global setting turns memory off everywhere")
	}
}

func TestMemoriesReachInstructionsAndSources(t *testing.T) {
	a, _ := memoryApp(t)
	ctx := context.Background()
	m, _, _ := a.Muninn.Add(ctx, "I prefer metric units", "", muninn.SourceManual, "")
	env := &chatExecEnv{app: a, ctx: ctx, profile: profiles.Profile{}, trace: &turnTrace{}, memories: []muninn.Memory{m}}
	env.trace.memories(env.memories)
	got := env.TurnInstructions(ctx, "How tall is Everest?")
	if !strings.Contains(got, "- I prefer metric units") {
		t.Fatalf("instructions = %q", got)
	}
	meta := env.trace.meta()
	if meta == nil || meta.Sources[0].Kind != "memory" || meta.Steps[0].Text != "Used 1 memory" {
		t.Fatalf("meta = %+v", meta)
	}
}
