package llamacpp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestChatSendsExplicitAdapterScales(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	c := NewClient()
	ctx := context.Background()
	chat := func(adapter string) error {
		ch, err := c.Chat(ctx, pluginapi.ChatRequest{ModelEndpoint: srv.URL, Adapter: adapter,
			Messages: []pluginapi.ChatMessage{{Role: "user", Content: "hi"}}})
		if err != nil {
			return err
		}
		for range ch {
		}
		return nil
	}

	// No adapters loaded: no lora field, and naming an adapter is an error.
	if err := chat(""); err != nil {
		t.Fatal(err)
	}
	if _, ok := bodies[0]["lora"]; ok {
		t.Fatalf("plain endpoint got lora: %v", bodies[0])
	}
	if err := chat("tire@1"); err == nil {
		t.Fatal("an adapter that is not loaded must fail")
	}

	registerAdapters(srv.URL, []string{"tire@1", "tire@2"})
	defer registerAdapters(srv.URL, nil)

	// A base-model request still zeroes every adapter.
	if err := chat(""); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(bodies[len(bodies)-1]["lora"]); got != "[map[id:0 scale:0] map[id:1 scale:0]]" {
		t.Fatalf("base lora = %s", got)
	}
	if err := chat("tire@2"); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(bodies[len(bodies)-1]["lora"]); got != "[map[id:0 scale:0] map[id:1 scale:1]]" {
		t.Fatalf("specialized lora = %s", got)
	}
	if err := chat("other@1"); err == nil {
		t.Fatal("an unknown adapter must fail")
	}
}

func TestSameAdapters(t *testing.T) {
	if !sameAdapters(nil, []string{}) || sameAdapters([]string{"a"}, []string{"b"}) || sameAdapters([]string{"a", "b"}, []string{"b", "a"}) {
		t.Fatal("sameAdapters compares ids in load order")
	}
}
