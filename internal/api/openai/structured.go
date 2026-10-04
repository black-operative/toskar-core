package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yeixio/toskar-core/internal/structured"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// jsonFormat reads OpenAI's response_format. It reports whether JSON was
// asked for and the schema to check it against.
func jsonFormat(f *responseFormat) (*structured.Schema, bool, error) {
	if f == nil || f.Type == "" || f.Type == "text" {
		return nil, false, nil
	}
	switch f.Type {
	case "json_object":
		return &structured.Schema{Type: "object"}, true, nil
	case "json_schema":
		if f.JSONSchema == nil || len(f.JSONSchema.Schema) == 0 {
			return nil, false, fmt.Errorf("response_format.json_schema.schema is required")
		}
		s, err := structured.ParseSchema(f.JSONSchema.Schema)
		if err != nil {
			return nil, false, err
		}
		return s, true, nil
	}
	return nil, false, fmt.Errorf("response_format.type must be text, json_object, or json_schema")
}

// jsonInstruction tells the model the shape to answer in.
func jsonInstruction(s *structured.Schema) string {
	if s == nil || (s.Type == "object" && len(s.Properties) == 0) {
		return "Reply with only a JSON object, no other text."
	}
	raw, _ := json.Marshal(s)
	return "Reply with only JSON that matches this JSON Schema, no other text: " + string(raw)
}

// structuredAnswer runs the turn, then checks its JSON against the schema
// after safe repairs. An answer that still does not fit is sent back once
// with the problems named. It returns compact JSON, or why none fit.
func (h *Handler) structuredAnswer(ctx context.Context, opts *turnopts.Options, profileID, message, modelID, execution string, s *structured.Schema) (string, error) {
	text, err := h.collect(ctx, profileID, message, modelID, execution)
	if err != nil {
		return "", err
	}
	r := structured.Parse(text, s)
	if !r.OK() {
		retry := *opts
		retry.History = append(append([]pluginapi.ChatMessage(nil), opts.History...),
			pluginapi.ChatMessage{Role: "user", Content: message},
			pluginapi.ChatMessage{Role: "assistant", Content: text})
		text, err = h.collect(turnopts.With(ctx, &retry), profileID, structured.FixPrompt(r, s), modelID, execution)
		if err != nil {
			return "", err
		}
		r = structured.Parse(text, s)
	}
	if !r.OK() {
		return "", fmt.Errorf("the model did not produce JSON that fits: %s", strings.TrimSpace(strings.ReplaceAll(structured.Describe(r.Issues), "\n", "; ")))
	}
	raw, err := json.Marshal(r.Value)
	return string(raw), err
}

// collect runs one turn and returns its text.
func (h *Handler) collect(ctx context.Context, profileID, message, modelID, execution string) (string, error) {
	ch, err := h.Chat.RunChat(ctx, profileID, "", message, false, modelID, execution)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for chunk := range ch {
		if chunk.Error != "" {
			return "", fmt.Errorf("%s", chunk.Error)
		}
		b.WriteString(chunk.Content)
	}
	return b.String(), nil
}
