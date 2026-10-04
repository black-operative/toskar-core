package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/yggdrasil-core/internal/auth"
	"github.com/yeixio/yggdrasil-core/internal/huginn"
	"github.com/yeixio/yggdrasil-core/internal/turnopts"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

func post(h *Handler, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	return rec
}

// The whole message array is honored (§62).
func TestFullConversationReachesTheTurn(t *testing.T) {
	h := testHandler(t)
	chat := &scriptChat{chunks: []pluginapi.ChatChunk{{Content: "Paris", Done: true}}}
	h.Chat = chat
	rec := post(h, `{"model":"profile:general","messages":[
		{"role":"system","content":"Answer in one word."},
		{"role":"user","content":"What is the capital of France?"},
		{"role":"assistant","content":"Paris"},
		{"role":"user","content":"And of Italy?"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d %s", rec.Code, rec.Body)
	}
	o := chat.opts
	if chat.message != "And of Italy?" || o == nil || o.System != "Answer in one word." || len(o.History) != 2 ||
		o.History[0].Content != "What is the capital of France?" || o.History[1].Role != "assistant" {
		t.Fatalf("message=%q opts=%+v", chat.message, o)
	}
	// Memory is used only when asked; the profile's knowledge stays on.
	if o.Memory || !o.Knowledge || o.Tools != nil {
		t.Fatalf("defaults = %+v", o)
	}
}

func TestControlsWithinTheKeysPermissions(t *testing.T) {
	h := testHandler(t)
	chat := &scriptChat{chunks: []pluginapi.ChatChunk{{Content: "ok", Done: true}}}
	h.Chat = chat
	perms := auth.DefaultAPIKeyPermissions()
	h.Permissions = func(*http.Request) (auth.APIKeyPermissions, error) { return perms, nil }

	rec := post(h, `{"model":"profile:general","reasoning_effort":"high","messages":[{"role":"user","content":"hi"}],
		"yggdrasil":{"memory":true,"knowledge_sources":["src-1"],"tools":["internet.search"],"placement":"local"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d %s", rec.Code, rec.Body)
	}
	o := chat.opts
	if !o.Memory || len(o.KnowledgeSources) != 1 || len(o.Tools) != 1 || chat.execution != "local" || chat.effort != huginn.EffortThorough {
		t.Fatalf("opts=%+v execution=%q effort=%q", o, chat.execution, chat.effort)
	}

	// A key that may not use memory refuses a request that asks for it.
	perms = auth.APIKeyPermissions{Memory: auth.UseNever, Knowledge: auth.UseNever, Tools: auth.ToolsReadOnly, Placement: false}
	for body, want := range map[string]string{
		`{"model":"profile:general","messages":[{"role":"user","content":"hi"}],"yggdrasil":{"memory":true}}`:             "memory",
		`{"model":"profile:general","messages":[{"role":"user","content":"hi"}],"yggdrasil":{"knowledge_sources":["x"]}}`: "knowledge",
		`{"model":"profile:general","messages":[{"role":"user","content":"hi"}],"yggdrasil":{"placement":"automatic"}}`:   "where requests run",
		`{"model":"profile:general","messages":[{"role":"user","content":"hi"}],"yggdrasil":{"placement":"somewhere"}}`:   "placement",
	} {
		rec := post(h, body)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: status=%d %s", body, rec.Code, rec.Body)
		}
	}
	rec = post(h, `{"model":"profile:general","messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusOK || chat.opts.Memory || chat.opts.Knowledge || !chat.opts.ReadOnlyTools {
		t.Fatalf("restricted defaults: %d %+v", rec.Code, chat.opts)
	}
	// A key with no tools gives the turn none.
	perms.Tools = auth.ToolsNone
	if rec := post(h, `{"model":"profile:general","messages":[{"role":"user","content":"hi"}],"yggdrasil":{"tools":["internet.search"]}}`); rec.Code != http.StatusForbidden {
		t.Fatalf("tools on a no-tools key: %d", rec.Code)
	}
	post(h, `{"model":"profile:general","messages":[{"role":"user","content":"hi"}]}`)
	if chat.opts.Tools == nil || len(chat.opts.Tools) != 0 {
		t.Fatalf("no-tools key: %+v", chat.opts)
	}
}

// Streaming clients that ask see progress and tool activity, and every
// response can carry the answer's sources and steps (§62).
func TestProgressAndSources(t *testing.T) {
	h := testHandler(t)
	meta := &contracts.MessageMeta{
		Sources: []contracts.Citation{{Kind: "web", Title: "Weather", URL: "https://example.com"}},
		Steps:   []contracts.ActivityStep{{Kind: "search", Text: "Searched the web"}},
	}
	chat := &scriptChat{chunks: []pluginapi.ChatChunk{{Content: "Sunny", Done: true}}, emit: func(o *turnopts.Options) {
		if o.Progress != nil {
			o.Progress("tool.started", map[string]any{"tool_id": "internet.search", "args": map[string]any{"query": "secret-ish"}, "task_id": "t"})
			o.Progress("chat.lookup", map[string]any{"query": "weather"})
		}
		o.Meta(meta)
	}}
	h.Chat = chat

	rec := post(h, `{"model":"profile:general","messages":[{"role":"user","content":"weather?"}]}`)
	var out struct {
		Yggdrasil struct {
			Sources []contracts.Citation `json:"sources"`
		} `json:"yggdrasil"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Yggdrasil.Sources) != 1 {
		t.Fatalf("non-stream = %s", rec.Body)
	}

	rec = post(h, `{"model":"profile:general","stream":true,"messages":[{"role":"user","content":"weather?"}],"yggdrasil":{"progress":true}}`)
	body := rec.Body.String()
	for _, want := range []string{`"event":{"tool_id":"internet.search","type":"tool.started"}`, `"type":"chat.lookup"`, `"content":"Sunny"`, `"sources":[`, "[DONE]"} {
		if !strings.Contains(body, want) {
			t.Errorf("stream lacks %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, "secret-ish") || strings.Contains(body, "task_id") {
		t.Errorf("stream leaks internals:\n%s", body)
	}

	// Without progress, a stream is plain OpenAI.
	rec = post(h, `{"model":"profile:general","stream":true,"messages":[{"role":"user","content":"weather?"}]}`)
	if strings.Contains(rec.Body.String(), "yggdrasil") {
		t.Errorf("plain stream has extension fields:\n%s", rec.Body)
	}
}

// The extension object is "toskar", and "yggdrasil", its name from before
// the rename, still works both ways (#237).
func TestToskarAndYggdrasilExtension(t *testing.T) {
	h := testHandler(t)
	meta := &contracts.MessageMeta{Sources: []contracts.Citation{{Kind: "web", Title: "Weather", URL: "https://example.com"}}}
	chat := &scriptChat{chunks: []pluginapi.ChatChunk{{Content: "ok", Done: true}}, emit: func(o *turnopts.Options) { o.Meta(meta) }}
	h.Chat = chat
	perms := auth.DefaultAPIKeyPermissions()
	h.Permissions = func(*http.Request) (auth.APIKeyPermissions, error) { return perms, nil }

	rec := post(h, `{"model":"profile:general","messages":[{"role":"user","content":"hi"}],"toskar":{"memory":true}}`)
	if rec.Code != http.StatusOK || !chat.opts.Memory {
		t.Fatalf("toskar controls ignored: %d %+v", rec.Code, chat.opts)
	}
	var out struct {
		Toskar    struct{ Sources []contracts.Citation } `json:"toskar"`
		Yggdrasil struct{ Sources []contracts.Citation } `json:"yggdrasil"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Toskar.Sources) != 1 || len(out.Yggdrasil.Sources) != 1 {
		t.Fatalf("response should carry both: %s", rec.Body)
	}

	// The new name wins when a request sends both.
	post(h, `{"model":"profile:general","messages":[{"role":"user","content":"hi"}],"toskar":{"memory":false},"yggdrasil":{"memory":true}}`)
	if chat.opts.Memory {
		t.Fatal("yggdrasil won over toskar")
	}

	// A stream's chunks carry the extension under both names too.
	rec = post(h, `{"model":"profile:general","stream":true,"messages":[{"role":"user","content":"hi"}],"toskar":{"progress":true}}`)
	if body := rec.Body.String(); !strings.Contains(body, `"toskar":{"files"`) || !strings.Contains(body, `"yggdrasil":{"files"`) {
		t.Fatalf("stream lacks the extension under both names:\n%s", body)
	}
}
