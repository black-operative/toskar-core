package app

import (
	"context"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
)

func TestStopChatCancelsTheRun(t *testing.T) {
	a := &App{}
	ctx, _, end := a.startRun(context.Background(), "c1")
	if !a.StopChat("c1") {
		t.Fatal("a running turn was not found")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("the run was not cancelled")
	}
	end()
	if a.StopChat("c1") {
		t.Fatal("a finished turn is not running")
	}
	if a.StopChat("other") {
		t.Fatal("nothing runs in another chat")
	}
}

func TestANewTurnReplacesOneStillRunning(t *testing.T) {
	a := &App{}
	first, _, endFirst := a.startRun(context.Background(), "c1")
	second, _, endSecond := a.startRun(context.Background(), "c1")
	if first.Err() == nil {
		t.Fatal("the earlier turn should stop")
	}
	endFirst() // must not unregister the newer turn
	if !a.StopChat("c1") || second.Err() == nil {
		t.Fatal("the newer turn should still be stoppable")
	}
	endSecond()
}

func TestStoppedAnswersAreKept(t *testing.T) {
	a, conv := memoryApp(t)
	subID, sub := a.Bus.Subscribe()
	defer a.Bus.Unsubscribe(subID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env := &chatExecEnv{app: a, trace: &turnTrace{}}
	a.keepStopped(ctx, env, conv, "The first half of an answer")
	msgs, _ := a.Conversations.ListMessages(context.Background(), conv)
	if len(msgs) != 1 || msgs[0].Content != "The first half of an answer" || msgs[0].Meta == nil ||
		msgs[0].Meta.Notice != "Stopped before the answer was finished." || msgs[0].Meta.Steps[0].Text != "Stopped by you" {
		t.Fatalf("saved = %+v", msgs)
	}
	select {
	case evt := <-sub:
		if evt.Type != events.ChatStopped || evt.Payload["kept"] != true {
			t.Fatalf("event = %+v", evt)
		}
	case <-time.After(time.Second):
		t.Fatal("no chat.stopped event")
	}
}

func TestAStopWithNothingWrittenIsStillShown(t *testing.T) {
	a, conv := memoryApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env := &chatExecEnv{app: a, trace: &turnTrace{}}
	env.trace.tool("internet.search", map[string]any{"query": "ollama"}, map[string]any{"results": []any{map[string]any{"title": "Ollama", "url": "https://ollama.com"}}})
	a.keepStopped(ctx, env, conv, "")
	msgs, _ := a.Conversations.ListMessages(context.Background(), conv)
	if len(msgs) != 1 || msgs[0].Content != "_Stopped before the answer was written._" || len(msgs[0].Meta.Sources) != 1 || msgs[0].Meta.Notice != "" {
		t.Fatalf("saved = %+v", msgs)
	}
}
