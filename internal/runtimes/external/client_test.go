package external

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func fakeServer(t *testing.T) (*httptest.Server, *map[string]any) {
	t.Helper()
	got := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided"}}`))
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"},{"id":"llama-3-70b"}]}`))
		case "/v1/chat/completions":
			_ = json.NewDecoder(r.Body).Decode(&got)
			w.Header().Set("Content-Type", "text/event-stream")
			for _, part := range []string{"Hel", "lo"} {
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", part)
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestExternalServer(t *testing.T) {
	srv, got := fakeServer(t)
	base, err := NormalizeURL(srv.URL + "/v1/")
	if err != nil || base != srv.URL {
		t.Fatalf("NormalizeURL = %q, %v", base, err)
	}
	r := New(Config{BaseURL: base, APIKey: "sk-test"})
	ctx := context.Background()
	models, err := r.ListModels(ctx)
	if err != nil || strings.Join(models, ",") != "gpt-4o-mini,llama-3-70b" {
		t.Fatalf("models = %v, %v", models, err)
	}
	ch, err := r.Chat(ctx, "gpt-4o-mini", pluginapi.ChatRequest{Messages: []pluginapi.ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	done := false
	for c := range ch {
		text += c.Content
		done = done || c.Done
		if c.Error != "" {
			t.Fatal(c.Error)
		}
	}
	if text != "Hello" || !done {
		t.Fatalf("reply %q done=%v", text, done)
	}
	if (*got)["model"] != "gpt-4o-mini" || (*got)["stream"] != true {
		t.Fatalf("request = %v", *got)
	}

	r.SetConfig(Config{BaseURL: base, APIKey: "wrong"})
	if _, err := r.ListModels(ctx); err == nil || !strings.Contains(err.Error(), "refused the API key") || !strings.Contains(err.Error(), "Incorrect API key") {
		t.Fatalf("wrong key err = %v", err)
	}
	if _, err := NormalizeURL("ftp://x"); err == nil {
		t.Fatal("ftp accepted")
	}
	if !IsModel("ext:gpt-4o-mini") || ServerModel("ext:gpt-4o-mini") != "gpt-4o-mini" || IsModel("qwen-7b") {
		t.Fatal("model ids")
	}
}

// The prompt's real size comes back in the stream's usage, for the context
// gauge; a server that rejects stream_options is asked again without it (#230).
func TestChatReportsUsage(t *testing.T) {
	for _, rejects := range []bool{false, true} {
		var asked []bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, withUsage := body["stream_options"]
			asked = append(asked, withUsage)
			if withUsage && rejects {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"Unrecognized request argument supplied: stream_options"}}`))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n")
			if withUsage {
				fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1234,\"completion_tokens\":5}}\n\n")
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
		}))
		r := New(Config{BaseURL: srv.URL})
		ch, err := r.Chat(context.Background(), "gpt-4o-mini", pluginapi.ChatRequest{Messages: []pluginapi.ChatMessage{{Role: "user", Content: "hi"}}})
		if err != nil {
			srv.Close()
			t.Fatalf("rejects=%v: %v", rejects, err)
		}
		var text string
		var metrics *pluginapi.GenerationMetrics
		for c := range ch {
			text += c.Content
			if c.Metrics != nil {
				metrics = c.Metrics
			}
		}
		srv.Close()
		if text != "Hi" {
			t.Fatalf("rejects=%v: text %q", rejects, text)
		}
		if !rejects && (metrics == nil || metrics.PromptTokens != 1234 || metrics.CompletionTokens != 5) {
			t.Fatalf("metrics = %+v", metrics)
		}
		if rejects && (len(asked) != 2 || !asked[0] || asked[1] || metrics != nil) {
			t.Fatalf("rejects: asked=%v metrics=%+v", asked, metrics)
		}
	}
}
