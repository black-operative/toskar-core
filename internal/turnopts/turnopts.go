// Package turnopts carries what an API request asks of one chat turn (spec
// §62): the caller's earlier messages, memory and knowledge choices, tool
// narrowing, and where progress goes. Chat in the app sets none of it, and
// behaves as before.
package turnopts

import (
	"context"

	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Options are one turn's choices from an API request, already limited to
// what the caller's key allows.
type Options struct {
	// History is the conversation before the last user message, as the
	// caller sent it.
	History []pluginapi.ChatMessage
	// System is the caller's system messages, joined.
	System string
	// Memory uses the person's memories for this turn.
	Memory bool
	// Knowledge uses the profile's connected knowledge; KnowledgeSources
	// adds more.
	Knowledge        bool
	KnowledgeSources []string
	// Tools, when not nil, narrows the profile's tools to these ids; an
	// empty list allows none.
	Tools []string
	// ReadOnlyTools drops tools that change anything.
	ReadOnlyTools bool
	// Progress, when set, receives the turn's progress and tool activity.
	Progress func(eventType string, payload map[string]any)
	// Meta, when set, receives the answer's sources, steps, and notice.
	Meta func(*contracts.MessageMeta)
}

type key struct{}

// With carries options for a turn.
func With(ctx context.Context, o *Options) context.Context { return context.WithValue(ctx, key{}, o) }

// From returns a turn's options, or nil for an ordinary chat.
func From(ctx context.Context) *Options {
	o, _ := ctx.Value(key{}).(*Options)
	return o
}
