package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestHandleModelsRequiresAuthAndListsProfiles(t *testing.T) {
	h := testHandler(t)
	h.Auth = func(*http.Request) error { return errors.New("missing key") }
	rec := httptest.NewRecorder()
	h.HandleModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	h.Auth = nil
	rec = httptest.NewRecorder()
	h.HandleModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"profile:general"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleChatCompletions(t *testing.T) {
	h := testHandler(t)
	chat := &scriptChat{chunks: []pluginapi.ChatChunk{{Content: "hello "}, {Content: "there", Done: true}}}
	h.Chat = chat
	h.Bus = events.NewBus(4)
	_, eventsCh := h.Bus.Subscribe()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"profile:general","messages":[{"role":"system","content":"be brief"},{"role":"user","content":"hi"}]}`))
	h.HandleChatCompletions(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "hello there") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if chat.profileID != "general" || chat.message != "hi" || chat.modelID != "" {
		t.Fatalf("chat=%+v", chat)
	}

	chat.chunks = []pluginapi.ChatChunk{{Content: "streamed", Done: true}}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"missing-profile","messages":[{"role":"user","content":"go"}],"stream":true}`))
	h.HandleChatCompletions(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "streamed") || !strings.Contains(body, "[DONE]") {
		t.Fatalf("status=%d body=%s", rec.Code, body)
	}
	if chat.profileID != "" || chat.modelID != "missing-profile" || !chat.stream {
		t.Fatalf("chat=%+v", chat)
	}
	if !sawEvent(eventsCh, events.ChatToken) || !sawEvent(eventsCh, events.ChatComplete) {
		t.Fatal("stream did not publish chat events")
	}
}

func TestHandleChatCompletionsRejectsBadRequests(t *testing.T) {
	h := testHandler(t)
	h.Chat = &scriptChat{}

	cases := []struct {
		body string
		code int
	}{
		{body: "{", code: http.StatusBadRequest},
		{body: `{"model":"general","messages":[{"role":"assistant","content":"no user"}]}`, code: http.StatusBadRequest},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tc.body)))
		if rec.Code != tc.code {
			t.Fatalf("body %s status=%d", tc.body, rec.Code)
		}
	}

	h.Chat = nil
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"general","messages":[{"role":"user","content":"hi"}]}`)))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d", rec.Code)
	}

	h.Chat = &scriptChat{err: errors.New("down")}
	rec = httptest.NewRecorder()
	h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"general","messages":[{"role":"user","content":"hi"}]}`)))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "down") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	h.Chat = &scriptChat{chunks: []pluginapi.ChatChunk{{Error: "model failed"}}}
	rec = httptest.NewRecorder()
	h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"general","messages":[{"role":"user","content":"hi"}]}`)))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "model failed") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDrainDiscardsTheBody(t *testing.T) {
	Drain(strings.NewReader("unused"))
	Drain(io.LimitReader(strings.NewReader("unused"), 2))
}

func testHandler(t *testing.T) *Handler {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mgr := profiles.NewManager(db.SQL)
	if _, err := mgr.Create(context.Background(), profiles.Profile{
		ID:             "general",
		Name:           "General",
		Purpose:        "chat",
		OrchestratorID: "simple",
		Roles:          []contracts.ModelRole{{Role: "assistant", ModelID: "m1", Required: true}},
	}); err != nil {
		t.Fatal(err)
	}
	return &Handler{Profiles: mgr}
}

type scriptChat struct {
	chunks    []pluginapi.ChatChunk
	err       error
	profileID string
	modelID   string
	message   string
	stream    bool
	execution string
	effort    huginn.Effort
	opts      *turnopts.Options
	// emit runs during the turn, as the orchestrator would.
	emit func(o *turnopts.Options)
}

func (s *scriptChat) RunChat(ctx context.Context, profileID, conversationID, message string, stream bool, modelID, execution string) (<-chan pluginapi.ChatChunk, error) {
	s.profileID = profileID
	s.modelID = modelID
	s.message = message
	s.stream = stream
	s.execution = execution
	s.effort = huginn.EffortFrom(ctx)
	s.opts = turnopts.From(ctx)
	if s.err != nil {
		return nil, s.err
	}
	if s.emit != nil && s.opts != nil {
		s.emit(s.opts)
	}
	ch := make(chan pluginapi.ChatChunk, len(s.chunks))
	for _, chunk := range s.chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func sawEvent(ch <-chan events.Event, eventType string) bool {
	for {
		select {
		case event := <-ch:
			if event.Type == eventType {
				return true
			}
		default:
			return false
		}
	}
}
