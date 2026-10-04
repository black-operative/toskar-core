package llamacpp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// A response schema reaches llama-server as response_format, which it turns
// into a grammar (§27).
func TestChatSendsResponseSchema(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`))
	}))
	defer srv.Close()
	ch, err := NewClient().Chat(context.Background(), pluginapi.ChatRequest{
		ModelEndpoint: srv.URL, Messages: []pluginapi.ChatMessage{{Role: "user", Content: "ok?"}},
		ResponseSchema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	rf, _ := got["response_format"].(map[string]any)
	schema, _ := rf["schema"].(map[string]any)
	if rf["type"] != "json_object" || schema["type"] != "object" {
		t.Fatalf("request = %+v", got)
	}

	got = nil
	ch, _ = NewClient().Chat(context.Background(), pluginapi.ChatRequest{ModelEndpoint: srv.URL, Messages: []pluginapi.ChatMessage{{Role: "user", Content: "hi"}}})
	for range ch {
	}
	if _, ok := got["response_format"]; ok {
		t.Fatal("response_format sent without a schema")
	}
}
