// Package contextusage splits a prompt into the parts a person can understand:
// instructions, tool definitions, the conversation, and tool results. It
// also fits earlier messages into a model's window. Both count with the
// running model's tokenizer when there is one, and estimate otherwise
// (AI experience spec §66).
package contextusage

import (
	"strings"
	"unicode"

	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// ReplyReserveTokens leaves room for the model's answer when older messages are kept.
const ReplyReserveTokens = 512

// DefaultWindow is the token window a local model runs with when its catalog
// window is missing or larger. It matches the llama.cpp start default.
const DefaultWindow = 8192

// Usage is one measured prompt. Section counts sum to PromptTokens.
type Usage struct {
	PromptTokens int  `json:"prompt_tokens"`
	Limit        int  `json:"limit"`
	Instructions int  `json:"instructions"`
	Tools        int  `json:"tools"`
	Conversation int  `json:"conversation"`
	ToolResults  int  `json:"tool_results"`
	Estimated    bool `json:"estimated"`
}

// Counter counts the tokens in a text with a model's tokenizer. exact is
// false when no tokenizer was available and n is an estimate.
type Counter func(text string) (n int, exact bool)

// Estimate approximates a token count for when no tokenizer is running
// (#230). Chinese, Japanese, and Korean characters are about a token each;
// punctuation and symbols, common in code and JSON, about half a token; and
// other text about four characters a token. Plain "characters ÷ 4" counted
// a Japanese sentence at a quarter of its size.
func Estimate(text string) int {
	var cjk, symbols, other int
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			cjk++
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			symbols++
		default:
			other++
		}
	}
	return cjk + (symbols+1)/2 + other/4
}

// Count counts text with count, or estimates when count is nil.
func (count Counter) Count(text string) (int, bool) {
	if text == "" {
		return 0, true
	}
	if count == nil {
		return Estimate(text), false
	}
	return count(text)
}

// Map is the chat.complete payload field.
func (u Usage) Map() map[string]any {
	return map[string]any{
		"prompt_tokens": u.PromptTokens,
		"limit":         u.Limit,
		"instructions":  u.Instructions,
		"tools":         u.Tools,
		"conversation":  u.Conversation,
		"tool_results":  u.ToolResults,
		"estimated":     u.Estimated,
	}
}

// Measure sizes a prompt. Each section is counted with count, the model's
// tokenizer, or estimated when there is none. When the runtime reports
// prompt tokens, the sections add up to that count: with exact counts, the
// tokens no section accounts for (the chat template and the model's own
// system text) count as instructions; with estimates, the sections are
// scaled.
func Measure(count Counter, instructions, toolPrompt string, messages []pluginapi.ChatMessage, promptTokens int) Usage {
	exact := true
	add := func(n int, ok bool) int {
		exact = exact && ok
		return n
	}
	parts := [4]int{
		add(count.Count(instructions)),
		add(count.Count(toolPrompt)),
	}
	for _, msg := range messages {
		if msg.Role == "system" {
			continue
		}
		n := add(count.Count(msg.Content))
		if msg.Role == "user" && isToolFeedback(msg.Content) {
			parts[3] += n
			continue
		}
		parts[2] += n
	}
	scaled, total := scale(parts, promptTokens, exact)
	return Usage{
		PromptTokens: total,
		Instructions: scaled[0],
		Tools:        scaled[1],
		Conversation: scaled[2],
		ToolResults:  scaled[3],
		Estimated:    promptTokens <= 0 && !exact,
	}
}

func isToolFeedback(content string) bool {
	return strings.HasPrefix(content, "Tool result for you") ||
		strings.HasPrefix(content, "The tool failed.") ||
		strings.HasPrefix(content, "That tool call was not valid")
}

func scale(parts [4]int, promptTokens int, exact bool) (out [4]int, total int) {
	sum := parts[0] + parts[1] + parts[2] + parts[3]
	if promptTokens <= 0 {
		return parts, sum
	}
	if exact && sum <= promptTokens {
		parts[0] += promptTokens - sum
		return parts, promptTokens
	}
	if sum == 0 {
		out[2] = promptTokens
		return out, promptTokens
	}
	used := 0
	largest := 0
	for i, n := range parts {
		out[i] = promptTokens * n / sum
		used += out[i]
		if out[i] >= out[largest] {
			largest = i
		}
	}
	out[largest] += promptTokens - used
	return out, promptTokens
}

// WithoutCurrentTurn drops the user message just saved for this turn.
func WithoutCurrentTurn(stored []contracts.Message, prompt string) []pluginapi.ChatMessage {
	if len(stored) > 0 {
		last := stored[len(stored)-1]
		if last.Role == "user" && last.Content == prompt {
			stored = stored[:len(stored)-1]
		}
	}
	out := make([]pluginapi.ChatMessage, 0, len(stored))
	for _, msg := range stored {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		out = append(out, pluginapi.ChatMessage{Role: msg.Role, Content: msg.Content})
	}
	return out
}

// Room is how many tokens of older chat can still fit in a window of
// limitTokens beside the reserved pieces and the reply.
func Room(limitTokens int, count Counter, reserved ...string) int {
	room := limitTokens - ReplyReserveTokens
	for _, text := range reserved {
		n, _ := count.Count(text)
		room -= n
	}
	if room < 0 {
		return 0
	}
	return room
}

// FitPrior keeps the newest messages that fit in roomTokens, in
// chronological order.
func FitPrior(prior []pluginapi.ChatMessage, roomTokens int, count Counter) []pluginapi.ChatMessage {
	if roomTokens <= 0 || len(prior) == 0 {
		return nil
	}
	start := len(prior)
	used := 0
	for i := len(prior) - 1; i >= 0; i-- {
		if prior[i].Content == "" {
			continue
		}
		n, _ := count.Count(prior[i].Content)
		if used+n > roomTokens {
			break
		}
		used += n
		start = i
	}
	if start >= len(prior) {
		return nil
	}
	out := make([]pluginapi.ChatMessage, len(prior)-start)
	copy(out, prior[start:])
	return out
}
