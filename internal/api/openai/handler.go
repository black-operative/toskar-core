package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/yeixio/yggdrasil-core/internal/auth"
	"github.com/yeixio/yggdrasil-core/internal/events"
	"github.com/yeixio/yggdrasil-core/internal/huginn"
	"github.com/yeixio/yggdrasil-core/internal/profiles"
	"github.com/yeixio/yggdrasil-core/internal/runtimes"
	"github.com/yeixio/yggdrasil-core/internal/structured"
	"github.com/yeixio/yggdrasil-core/internal/turnopts"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

// ChatService runs chat completions.
type ChatService interface {
	RunChat(ctx context.Context, profileID, conversationID, message string, stream bool, modelID, execution string) (<-chan pluginapi.ChatChunk, error)
}

// Handler serves OpenAI-compatible endpoints.
type Handler struct {
	Profiles *profiles.Manager
	Runtimes *runtimes.Manager
	Chat     ChatService
	Bus      *events.Bus
	Auth     func(r *http.Request) error
	// Permissions authorizes a request and returns what its key may ask of
	// the assistant (§62). When set, it replaces Auth for chat completions.
	Permissions func(r *http.Request) (auth.APIKeyPermissions, error)
	// Specialized lists deployed specialized AIs (sai: ids) for /v1/models.
	Specialized func(ctx context.Context) ([]contracts.Model, error)
}

type chatCompletionRequest struct {
	Model       string                  `json:"model"`
	Messages    []pluginapi.ChatMessage `json:"messages"`
	Stream      bool                    `json:"stream"`
	Temperature float64                 `json:"temperature"`
	MaxTokens   int                     `json:"max_tokens"`
	// ReasoningEffort is OpenAI's low, medium, or high.
	ReasoningEffort string `json:"reasoning_effort"`
	// Toskar holds the assistant's own controls (§62). Yggdrasil is its name
	// from before the rename, still accepted; Toskar wins when both are sent
	// (#237).
	Toskar    *assistantOptions `json:"toskar"`
	Yggdrasil *assistantOptions `json:"yggdrasil"`
	// ResponseFormat asks for JSON: json_object, or json_schema with a schema (§27).
	ResponseFormat *responseFormat `json:"response_format"`
}

type responseFormat struct {
	Type       string `json:"type"`
	JSONSchema *struct {
		Name   string          `json:"name"`
		Schema json.RawMessage `json:"schema"`
	} `json:"json_schema"`
}

// assistantOptions are the request's assistant controls. Each one can only
// use what the API key allows.
type assistantOptions struct {
	Memory           *bool    `json:"memory"`
	Knowledge        *bool    `json:"knowledge"`
	KnowledgeSources []string `json:"knowledge_sources"`
	Tools            []string `json:"tools"`
	Effort           string   `json:"effort"`
	Placement        string   `json:"placement"`
	// Progress streams the turn's progress and tool activity as chunks with
	// an empty delta and the extension fields.
	Progress bool `json:"progress"`
}

// HandleModels lists profiles as OpenAI models.
func (h *Handler) HandleModels(w http.ResponseWriter, r *http.Request) {
	if h.Auth != nil {
		if err := h.Auth(r); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
			return
		}
	}
	items, err := h.Profiles.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "MODELS_LIST_FAILED", err.Error())
		return
	}
	// "auto" lets Toskar pick an installed model for each request.
	// owned_by stays "yggdrasil" within contract 1.x: clients may match on
	// it, and it changes with the next major contract version (#237).
	data := make([]map[string]any, 0, len(items)+1)
	data = append(data, map[string]any{"id": "auto", "object": "model", "owned_by": "yggdrasil"})
	for _, p := range items {
		data = append(data, map[string]any{
			"id":       "profile:" + p.ID,
			"object":   "model",
			"owned_by": "yggdrasil",
		})
	}
	if h.Specialized != nil {
		if deployed, err := h.Specialized(r.Context()); err == nil {
			for _, m := range deployed {
				data = append(data, map[string]any{"id": m.ID, "object": "model", "owned_by": "yggdrasil"})
			}
		}
	}
	writeJSON(w, map[string]any{"object": "list", "data": data})
}

// HandleChatCompletions routes profile:ID to orchestrator chat.
func (h *Handler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	perms := auth.DefaultAPIKeyPermissions()
	switch {
	case h.Permissions != nil:
		p, err := h.Permissions(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
			return
		}
		perms = p
	case h.Auth != nil:
		if err := h.Auth(r); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
			return
		}
	}
	var req chatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	profileID := ""
	modelID := ""
	if strings.HasPrefix(req.Model, "profile:") {
		profileID = strings.TrimPrefix(req.Model, "profile:")
	} else {
		// Treat bare ids as a model override (profile falls back to default).
		modelID = req.Model
		profileID = req.Model // also try as profile id when no model files match
	}
	message, history, system := splitMessages(req.Messages)
	if message == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "messages must include a user message")
		return
	}
	opts, execution, effort, err := turnOptions(req, perms)
	if err != nil {
		writeError(w, http.StatusForbidden, "NOT_ALLOWED", err.Error())
		return
	}
	opts.History, opts.System = history, system
	schema, wantJSON, err := jsonFormat(req.ResponseFormat)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_RESPONSE_FORMAT", err.Error())
		return
	}
	if wantJSON {
		opts.System = strings.TrimSpace(opts.System + "\n\n" + jsonInstruction(schema))
	}

	if h.Chat == nil {
		writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "chat not configured")
		return
	}

	// Prefer profile when the id matches a known profile; otherwise use as model override.
	if modelID != "" && profileID == modelID {
		if _, err := h.Profiles.Get(r.Context(), profileID); err != nil {
			profileID = ""
		} else {
			modelID = ""
		}
	}

	// The answer's sources, steps, and notice, for the yggdrasil field.
	var meta *contracts.MessageMeta
	var metaMu sync.Mutex
	opts.Meta = func(m *contracts.MessageMeta) {
		metaMu.Lock()
		meta = m
		metaMu.Unlock()
	}
	sw := &streamWriter{w: w}
	if req.Stream {
		if !sw.start() {
			writeError(w, http.StatusInternalServerError, "STREAM_UNSUPPORTED", "streaming not supported")
			return
		}
		if o := req.assistant(); o != nil && o.Progress {
			opts.Progress = func(eventType string, payload map[string]any) { sw.progress(profileID, eventType, payload) }
		}
	}
	ctx := turnopts.With(r.Context(), opts)
	if effort != "" {
		ctx = huginn.WithEffort(ctx, huginn.ParseEffort(effort))
	}
	if wantJSON {
		// The model's reply is constrained to the schema where the runtime
		// supports it.
		if raw, err := json.Marshal(schema); err == nil {
			ctx = structured.WithSchema(ctx, raw)
		}
		// The answer must be data a program can read: checked, repaired,
		// and asked for once more if needed, then sent whole (§27).
		content, err := h.structuredAnswer(ctx, opts, profileID, message, modelID, execution, schema)
		if err != nil {
			if req.Stream {
				sw.data(map[string]any{"error": map[string]any{"message": err.Error()}})
				sw.done()
				return
			}
			writeError(w, http.StatusUnprocessableEntity, "INVALID_STRUCTURED_OUTPUT", err.Error())
			return
		}
		one := make(chan pluginapi.ChatChunk, 1)
		one <- pluginapi.ChatChunk{Content: content, Done: true}
		close(one)
		if !req.Stream {
			h.writeNonStream(w, one, profileID, func() map[string]any { return nil })
			return
		}
		h.writeStream(sw, r, one, profileID, func() map[string]any { return nil }, false)
		return
	}
	stream, err := h.Chat.RunChat(ctx, profileID, "", message, req.Stream, modelID, execution)
	if err != nil {
		if req.Stream {
			sw.data(map[string]any{"error": map[string]any{"message": err.Error()}})
			return
		}
		writeError(w, http.StatusInternalServerError, "CHAT_FAILED", err.Error())
		return
	}
	extension := func() map[string]any {
		metaMu.Lock()
		defer metaMu.Unlock()
		if meta == nil {
			return nil
		}
		return map[string]any{"sources": meta.Sources, "steps": meta.Steps, "notice": meta.Notice, "files": meta.Files}
	}

	if !req.Stream {
		h.writeNonStream(w, stream, profileID, extension)
		return
	}
	h.writeStream(sw, r, stream, profileID, extension, req.assistant() != nil && req.assistant().Progress)
}

func (h *Handler) writeNonStream(w http.ResponseWriter, stream <-chan pluginapi.ChatChunk, model string, extension func() map[string]any) {
	var content strings.Builder
	for chunk := range stream {
		if chunk.Error != "" {
			writeError(w, http.StatusInternalServerError, "CHAT_FAILED", chunk.Error)
			return
		}
		content.WriteString(chunk.Content)
	}
	out := map[string]any{
		"id":      "chatcmpl-ygg",
		"object":  "chat.completion",
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": content.String()}, "finish_reason": "stop"}},
	}
	if ext := extension(); ext != nil {
		withExtension(out, ext)
	}
	writeJSON(w, out)
}

func (h *Handler) writeStream(sw *streamWriter, r *http.Request, stream <-chan pluginapi.ChatChunk, model string, extension func() map[string]any, progress bool) {
	id := "chatcmpl-ygg"
	for chunk := range stream {
		if chunk.Error != "" {
			sw.data(map[string]any{"error": map[string]any{"message": chunk.Error}})
			return
		}
		if chunk.Content == "" && !chunk.Done {
			continue
		}
		sw.data(map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"model":   model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": chunk.Content}, "finish_reason": finishReason(chunk.Done)}},
		})
		if h.Bus != nil && chunk.Content != "" {
			h.Bus.Publish(events.New(events.ChatToken, map[string]any{"content": chunk.Content}))
		}
	}
	if ext := extension(); progress && ext != nil {
		sw.data(withExtension(map[string]any{
			"id": id, "object": "chat.completion.chunk", "model": model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": nil}},
		}, ext))
	}
	sw.done()
	if h.Bus != nil {
		h.Bus.Publish(events.New(events.ChatComplete, map[string]any{}))
	}
	_ = r.Context().Err()
}

func finishReason(done bool) any {
	if done {
		return "stop"
	}
	return nil
}

// splitMessages honors the whole message array (§62): the last user
// message is the turn, earlier user and assistant messages are its history,
// and system messages are the caller's instructions.
func splitMessages(messages []pluginapi.ChatMessage) (message string, history []pluginapi.ChatMessage, system string) {
	last := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			last = i
			break
		}
	}
	if last < 0 {
		return "", nil, ""
	}
	var sys []string
	for i, m := range messages {
		switch {
		case m.Role == "system" || m.Role == "developer":
			if s := strings.TrimSpace(m.Content); s != "" {
				sys = append(sys, s)
			}
		case i < last && (m.Role == "user" || m.Role == "assistant") && strings.TrimSpace(m.Content) != "":
			history = append(history, pluginapi.ChatMessage{Role: m.Role, Content: m.Content})
		}
	}
	return messages[last].Content, history, strings.Join(sys, "\n\n")
}

// turnOptions applies the request's controls within the key's permissions.
// Asking for something the key does not allow is refused, not ignored.
func turnOptions(req chatCompletionRequest, perms auth.APIKeyPermissions) (*turnopts.Options, string, string, error) {
	y := req.assistant()
	if y == nil {
		y = &assistantOptions{}
	}
	opts := &turnopts.Options{}
	use := func(level string, asked *bool, what string) (bool, error) {
		switch level {
		case auth.UseNever:
			if asked != nil && *asked {
				return false, fmt.Errorf("this API key may not use %s", what)
			}
			return false, nil
		case auth.UseAlways:
			return asked == nil || *asked, nil
		}
		return asked != nil && *asked, nil
	}
	var err error
	if opts.Memory, err = use(perms.Memory, y.Memory, "memory"); err != nil {
		return nil, "", "", err
	}
	if opts.Knowledge, err = use(perms.Knowledge, y.Knowledge, "connected knowledge"); err != nil {
		return nil, "", "", err
	}
	if len(y.KnowledgeSources) > 0 {
		if perms.Knowledge == auth.UseNever {
			return nil, "", "", fmt.Errorf("this API key may not use connected knowledge")
		}
		opts.KnowledgeSources = y.KnowledgeSources
	}
	switch perms.Tools {
	case auth.ToolsNone:
		if len(y.Tools) > 0 {
			return nil, "", "", fmt.Errorf("this API key may not use tools")
		}
		opts.Tools = []string{}
	case auth.ToolsReadOnly:
		opts.ReadOnlyTools = true
	}
	if y.Tools != nil && opts.Tools == nil {
		opts.Tools = y.Tools
	}
	execution := ""
	switch y.Placement {
	case "":
	case "local", "automatic":
		if !perms.Placement {
			return nil, "", "", fmt.Errorf("this API key may not choose where requests run")
		}
		execution = y.Placement
	default:
		return nil, "", "", fmt.Errorf("placement must be local or automatic")
	}
	effort := y.Effort
	if effort == "" {
		switch strings.ToLower(req.ReasoningEffort) {
		case "minimal", "low":
			effort = "fast"
		case "medium":
			effort = "balanced"
		case "high":
			effort = "thorough"
		}
	}
	return opts, execution, effort, nil
}

// streamWriter writes server-sent events; progress may arrive from several
// goroutines at once.
type streamWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

func (s *streamWriter) start() bool {
	f, ok := s.w.(http.Flusher)
	if !ok {
		return false
	}
	s.flusher = f
	s.w.Header().Set("Content-Type", "text/event-stream")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.Header().Set("Connection", "keep-alive")
	s.w.WriteHeader(http.StatusOK)
	f.Flush()
	return true
}

func (s *streamWriter) data(v any) {
	raw, _ := json.Marshal(v)
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.w, "data: %s\n\n", raw)
	s.flusher.Flush()
}

func (s *streamWriter) done() {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.w, "data: [DONE]\n\n")
	s.flusher.Flush()
}

// progressFields are the parts of an event an API client sees.
var progressFields = []string{"tool_id", "query", "steps", "parallel", "index", "step", "status", "model_id", "model_name", "reason", "effort", "issues", "fixed", "remaining", "error", "name"}

// progress writes one event as a chunk with an empty delta, so OpenAI
// clients ignore it and Toskar-aware ones can show it.
func (s *streamWriter) progress(model, eventType string, payload map[string]any) {
	event := map[string]any{"type": eventType}
	for _, k := range progressFields {
		if v, ok := payload[k]; ok {
			event[k] = v
		}
	}
	s.data(withExtension(map[string]any{
		"id": "chatcmpl-ygg", "object": "chat.completion.chunk", "model": model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": nil}},
	}, map[string]any{"event": event}))
}

// assistant is the request's assistant controls, under either name.
func (r chatCompletionRequest) assistant() *assistantOptions {
	if r.Toskar != nil {
		return r.Toskar
	}
	return r.Yggdrasil
}

// withExtension adds the answer's extension fields to a response or chunk
// as "toskar", and as "yggdrasil", its name from before the rename, which
// clients written for contract 1.x read (#237).
func withExtension(out, ext map[string]any) map[string]any {
	out["toskar"] = ext
	out["yggdrasil"] = ext
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(contracts.APIError{Error: contracts.ErrorBody{Code: code, Message: msg}})
}

// Drain closes unused stream on error paths.
func Drain(r io.Reader) { _, _ = io.Copy(io.Discard, r) }
