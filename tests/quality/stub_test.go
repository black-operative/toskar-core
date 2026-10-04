package quality

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/app"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// stubDriver runs a case in-process against the stub model, with its
// replies scripted and the web replaced by fixed pages, so it needs no
// model and no network.
type stubDriver struct{}

func (stubDriver) Name() string { return "stub" }

// fakeTool stands in for a web tool.
type fakeTool struct {
	id  string
	run func(args map[string]any) map[string]any
}

func (f fakeTool) ID() string          { return f.id }
func (f fakeTool) DisplayName() string { return f.id }
func (f fakeTool) Description() string { return f.id }
func (f fakeTool) Execute(_ context.Context, args map[string]any) (map[string]any, error) {
	return f.run(args), nil
}

func fakeWeb(a *app.App) {
	a.Tools.Register(fakeTool{id: "internet.search", run: func(args map[string]any) map[string]any {
		q, _ := args["query"].(string)
		slug := strings.ReplaceAll(strings.ToLower(q), " ", "-")
		return map[string]any{"results": []any{
			map[string]any{"title": q + " - Example", "url": "https://example.com/" + slug, "snippet": "Facts about " + q + "."},
		}}
	}})
	a.Tools.Register(fakeTool{id: "internet.open", run: func(args map[string]any) map[string]any {
		u, _ := args["url"].(string)
		return map[string]any{"title": "Example page", "url": u,
			"content": "Juneau weather: 48 F and cloudy. Ollama, llama.cpp, and MLX each run local models; this page explains how they differ in detail."}
	}})
}

func (stubDriver) Run(t *testing.T, c Case) Result {
	t.Helper()
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := app.New(app.Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	fakeWeb(a)
	ctx := context.Background()

	// The script: each model call gets the next reply; the rest say "done".
	var mu sync.Mutex
	var prompts [][]pluginapi.ChatMessage
	script := append([]string(nil), c.Stub...)
	a.StubReply = func(_ string, messages []pluginapi.ChatMessage) string {
		mu.Lock()
		defer mu.Unlock()
		prompts = append(prompts, append([]pluginapi.ChatMessage(nil), messages...))
		if len(script) == 0 {
			return "done"
		}
		next := script[0]
		script = script[1:]
		return next
	}

	profileID := setupProfile(t, ctx, a, c.Setup)
	conv, err := a.Conversations.Create(ctx, "quality "+c.ID, profileID, "auto")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range c.History {
		if _, err := a.Conversations.AddMessage(ctx, conv.ID, m.Role, m.Content); err != nil {
			t.Fatal(err)
		}
	}

	// Every approval is answered no, so nothing risky can run.
	var evMu sync.Mutex
	var got []Event
	subID, ch := a.Bus.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range ch {
			evMu.Lock()
			got = append(got, Event{Type: evt.Type, Payload: evt.Payload})
			evMu.Unlock()
			if evt.Type == events.ToolRequested {
				if id, _ := evt.Payload["request_id"].(string); id != "" {
					_ = a.Tools.Decide(id, false, false)
				}
			}
		}
	}()

	stream, err := a.RunChat(ctx, profileID, conv.ID, c.Message, false, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	var answer strings.Builder
	for chunk := range stream {
		if chunk.Error != "" {
			t.Fatalf("turn failed: %s", chunk.Error)
		}
		answer.WriteString(chunk.Content)
	}
	time.Sleep(50 * time.Millisecond) // let the last events arrive
	a.Bus.Unsubscribe(subID)
	<-done

	r := Result{Answer: answer.String(), Prompts: prompts}
	evMu.Lock()
	r.Events = got
	evMu.Unlock()
	if msgs, err := a.Conversations.ListMessages(ctx, conv.ID); err == nil {
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "assistant" {
				r.Meta = msgs[i].Meta
				if r.Answer == "" {
					r.Answer = msgs[i].Content
				}
				break
			}
		}
	}
	if r.Meta != nil && r.Meta.RunID != "" {
		if run, err := a.RunLog.Get(ctx, r.Meta.RunID); err == nil {
			r.Run = &run
		}
	}
	return r
}

// setupProfile copies the general assistant with the case's knowledge and
// tool policies.
func setupProfile(t *testing.T, ctx context.Context, a *app.App, s Setup) string {
	t.Helper()
	p, err := a.Profiles.Get(ctx, "general-assistant")
	if err != nil {
		t.Fatal(err)
	}
	p.ID, p.Name = "", "Quality"
	for _, k := range s.Knowledge {
		src, err := a.Mimir.Create(ctx, mimir.CreateInput{Kind: mimir.KindText, Filename: k.Filename, Text: k.Text})
		if err != nil {
			t.Fatal(err)
		}
		p.KnowledgeSources = append(p.KnowledgeSources, src.ID)
	}
	for id, policy := range s.Tools {
		found := false
		for i := range p.Tools {
			if p.Tools[i].ToolID == id {
				p.Tools[i].Policy, found = policy, true
			}
		}
		if !found {
			p.Tools = append(p.Tools, contracts.ToolPolicy{ToolID: id, Policy: policy})
		}
	}
	created, err := a.Profiles.Create(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	return created.ID
}
