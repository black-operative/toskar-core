package quality

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
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

// stubDriver runs a case in-process with the web replaced by the pages in
// web.json. Without a model URL the stub model's replies are scripted, so
// it needs no model and no network. With one, every model call goes to that
// OpenAI-compatible server instead, so a real model answers through the
// same routing, look-ups, tools, and checks.
type stubDriver struct {
	// model is an OpenAI-compatible server, such as llama-server, or "".
	model string
}

func (d stubDriver) Name() string {
	if d.model != "" {
		return "served"
	}
	return "stub"
}

// webSite is a topic in web.json: the searches it answers and its pages.
type webSite struct {
	Match   string `json:"match"`
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Snippet string `json:"snippet"`
	} `json:"results"`
	Pages map[string]string `json:"pages"`
}

// webFixtures is web.json, which the iPhone app's quality run serves too.
type webFixtures struct {
	Sites []webSite `json:"sites"`
}

func loadWeb(t *testing.T) webFixtures {
	t.Helper()
	raw, err := os.ReadFile("web.json")
	if err != nil {
		t.Fatal(err)
	}
	var w webFixtures
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	return w
}

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

// fakeWeb answers searches and page reads from web.json: a search gets the
// results of the first site whose pattern matches it, and a page is found
// by the longest URL prefix of any site.
func fakeWeb(t *testing.T, a *app.App) {
	w := loadWeb(t)
	a.Tools.Register(fakeTool{id: "internet.search", run: func(args map[string]any) map[string]any {
		q, _ := args["query"].(string)
		for _, site := range w.Sites {
			if regexp.MustCompile(site.Match).MatchString(q) {
				var results []any
				for _, r := range site.Results {
					results = append(results, map[string]any{"title": r.Title, "url": r.URL, "snippet": r.Snippet})
				}
				return map[string]any{"results": results}
			}
		}
		slug := strings.ReplaceAll(strings.ToLower(q), " ", "-")
		return map[string]any{"results": []any{
			map[string]any{"title": q + " - Example", "url": "https://example.com/" + slug, "snippet": "Facts about " + q + "."},
		}}
	}})
	// Places come from the same sites, so a run never reaches OpenStreetMap.
	a.Tools.Register(fakeTool{id: "places.search", run: func(args map[string]any) map[string]any {
		q, _ := args["query"].(string)
		near, _ := args["near"].(string)
		var places []any
		for _, site := range w.Sites {
			if regexp.MustCompile(site.Match).MatchString(q + " " + near) {
				for _, r := range site.Results {
					places = append(places, map[string]any{"name": r.Title, "address": r.Snippet, "website": r.URL})
				}
				break
			}
		}
		return map[string]any{"places": places}
	}})
	a.Tools.Register(fakeTool{id: "internet.open", run: func(args map[string]any) map[string]any {
		u, _ := args["url"].(string)
		best, content := "", ""
		for _, site := range w.Sites {
			for prefix, page := range site.Pages {
				if strings.HasPrefix(u, prefix) && len(prefix) > len(best) {
					best, content = prefix, page
				}
			}
		}
		if content == "" {
			content = "This page has no more about that."
		}
		return map[string]any{"title": "Page", "url": u, "content": content}
	}})
}

// served sends one model call to an OpenAI-compatible server, at
// temperature 0 with a fixed seed so a run repeats as closely as the
// server allows.
func served(base string, messages []pluginapi.ChatMessage) (string, error) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var out []msg
	for _, m := range messages {
		role := m.Role
		if role != "system" && role != "user" && role != "assistant" {
			// Tool results go back as the person's turn; llama-server's
			// chat templates take only these three roles.
			role = "user"
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content += "\n\n" + m.Content
			continue
		}
		out = append(out, msg{Role: role, Content: m.Content})
	}
	body, _ := json.Marshal(map[string]any{"messages": out, "temperature": 0, "seed": 1, "max_tokens": 768})
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Post(base+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", resp.Status, raw)
	}
	var reply struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil || len(reply.Choices) == 0 {
		return "", fmt.Errorf("unreadable reply: %.200s", raw)
	}
	return reply.Choices[0].Message.Content, nil
}

func (d stubDriver) Run(t *testing.T, c Case) Result {
	t.Helper()
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := app.New(app.Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	fakeWeb(t, a)
	ctx := context.Background()

	// The script: each model call gets the next reply; the rest say "done".
	var mu sync.Mutex
	var prompts [][]pluginapi.ChatMessage
	script := append([]string(nil), c.Stub...)
	a.StubReply = func(_ string, messages []pluginapi.ChatMessage) string {
		mu.Lock()
		defer mu.Unlock()
		prompts = append(prompts, append([]pluginapi.ChatMessage(nil), messages...))
		if d.model != "" {
			reply, err := served(d.model, messages)
			if err != nil {
				t.Errorf("model: %v", err)
			}
			return reply
		}
		// The search a follow-up asks for is written by the model, outside
		// the case's script of answers.
		if len(messages) > 0 && strings.HasPrefix(messages[0].Content, "You write web search queries") {
			if c.StubQuery != "" {
				return c.StubQuery
			}
			return c.Message
		}
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
	if d.model != "" {
		t.Logf("answer: %.300s", answer.String())
	}
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
