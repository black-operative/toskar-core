package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// queueChat answers each turn with the next reply and records what it saw.
type queueChat struct {
	replies  []string
	messages []string
	opts     []*turnopts.Options
}

func (q *queueChat) RunChat(ctx context.Context, profileID, conversationID, message string, stream bool, modelID, execution string) (<-chan pluginapi.ChatChunk, error) {
	q.messages = append(q.messages, message)
	q.opts = append(q.opts, turnopts.From(ctx))
	reply := "done"
	if len(q.replies) > 0 {
		reply, q.replies = q.replies[0], q.replies[1:]
	}
	ch := make(chan pluginapi.ChatChunk, 1)
	ch <- pluginapi.ChatChunk{Content: reply, Done: true}
	close(ch)
	return ch, nil
}

const priceSchema = `"response_format":{"type":"json_schema","json_schema":{"name":"price","schema":{"type":"object","required":["price","currency"],"properties":{"price":{"type":"number"},"currency":{"type":"string"}}}}}`

func content(t *testing.T, body string) string {
	t.Helper()
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || len(out.Choices) == 0 {
		t.Fatalf("body = %s", body)
	}
	return out.Choices[0].Message.Content
}

func TestResponseFormat(t *testing.T) {
	h := testHandler(t)

	// Repaired without asking again: fences and a price written as text.
	q := &queueChat{replies: []string{"Sure!\n```json\n{\"price\": \"$420\", \"currency\": \"USD\"}\n```"}}
	h.Chat = q
	rec := post(h, `{"model":"profile:general","messages":[{"role":"user","content":"Price?"}],`+priceSchema+`}`)
	if rec.Code != http.StatusOK || content(t, rec.Body.String()) != `{"currency":"USD","price":420}` || len(q.messages) != 1 {
		t.Fatalf("repaired: %d %s (%d turns)", rec.Code, rec.Body, len(q.messages))
	}
	if !strings.Contains(q.opts[0].System, "JSON Schema") {
		t.Fatalf("system = %q", q.opts[0].System)
	}

	// Asked again once with the problems named.
	q = &queueChat{replies: []string{`{"price": 420}`, `{"price": 420, "currency": "USD"}`}}
	h.Chat = q
	rec = post(h, `{"model":"profile:general","messages":[{"role":"user","content":"Price?"}],`+priceSchema+`}`)
	if rec.Code != http.StatusOK || len(q.messages) != 2 || !strings.Contains(q.messages[1], "currency: is required") ||
		len(q.opts[1].History) != 2 || q.opts[1].History[1].Content != `{"price": 420}` {
		t.Fatalf("retry: %d %s %q", rec.Code, rec.Body, q.messages)
	}

	// Still wrong: 422 that says why.
	h.Chat = &queueChat{replies: []string{"It is $420.", "Really, $420."}}
	rec = post(h, `{"model":"profile:general","messages":[{"role":"user","content":"Price?"}],`+priceSchema+`}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "no JSON object was found") {
		t.Fatalf("unfixable: %d %s", rec.Code, rec.Body)
	}

	// json_object, streamed as one chunk.
	h.Chat = &queueChat{replies: []string{`{"ok": true,}`}}
	rec = post(h, `{"model":"profile:general","stream":true,"messages":[{"role":"user","content":"ok?"}],"response_format":{"type":"json_object"}}`)
	if !strings.Contains(rec.Body.String(), `"content":"{\"ok\":true}"`) || !strings.Contains(rec.Body.String(), "[DONE]") {
		t.Fatalf("stream: %s", rec.Body)
	}

	// Bad formats are refused before anything runs.
	for _, f := range []string{`{"type":"xml"}`, `{"type":"json_schema"}`, `{"type":"json_schema","json_schema":{"schema":"{"}}`} {
		if rec := post(h, `{"model":"profile:general","messages":[{"role":"user","content":"x"}],"response_format":`+f+`}`); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", f, rec.Code)
		}
	}
}
