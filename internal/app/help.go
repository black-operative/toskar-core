package app

import (
	"context"
	"regexp"
	"strings"

	"github.com/yeixio/yggdrasil-core/internal/locale"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

// How-to questions about Yggdrasil itself are answered from what it offers,
// not by a model that would guess, such as telling people to edit another
// app's config file to "install MCP".
var (
	// mcpRe matches MCP, its common misspelling, and tool sources.
	mcpRe = regexp.MustCompile(`(?i)\b(mcp|mpc|model context protocol|tool sources?)\b`)
	// howToRe is a question about getting or using something.
	howToRe = regexp.MustCompile(`(?i)\b(how|where|install|add|set ?up|connect|enable|get|use)\b`)
	// buildRe is about writing an MCP server, which a model should answer.
	buildRe = regexp.MustCompile(`(?i)\b(write|build|create|code|implement|develop|program|example|sample|spec|protocol works)\b`)
	// otherAppRe asks to use Yggdrasil from another AI app.
	otherAppRe = regexp.MustCompile(`(?i)\b(claude desktop|claude code|cursor|vs ?code|windsurf|zed|other (ai )?apps?)\b`)
)

// helpMaxWords keeps how-to answers to short questions; a longer message is
// a real request.
const helpMaxWords = 25

// helpTopic is the how-to a message asks for, as a chat:help key, or "".
func helpTopic(message string) string {
	m := strings.TrimSpace(message)
	if len(strings.Fields(m)) > helpMaxWords || !mcpRe.MatchString(m) || !howToRe.MatchString(m) || buildRe.MatchString(m) {
		return ""
	}
	if lower := strings.ToLower(m); otherAppRe.MatchString(m) && (strings.Contains(lower, "toskar") || strings.Contains(lower, "yggdrasil")) {
		return "mcpServer"
	}
	return "mcpTools"
}

// answerHowTo replies to a short how-to question about Yggdrasil, in the
// App language, without a model.
func (a *App) answerHowTo(ctx context.Context, conversationID, message string) (<-chan pluginapi.ChatChunk, bool) {
	topic := helpTopic(message)
	if topic == "" {
		return nil, false
	}
	lang := a.appLanguage(ctx)
	// The screens and buttons are named with the UI's own labels.
	reply := locale.T(lang, "chat:help."+topic, map[string]any{
		"tools":        locale.T(lang, "common:nav.tools", nil),
		"addTools":     locale.T(lang, "tools:sources.add", nil),
		"apiAccess":    locale.T(lang, "common:nav.apiAccess", nil),
		"useElsewhere": locale.T(lang, "apiAccess:share.title", nil),
	})
	meta := &contracts.MessageMeta{Steps: []contracts.ActivityStep{{Kind: "share", Text: locale.T(lang, "chat:steps.helpAnswered", nil)}}}
	return a.replyDirectly(ctx, conversationID, message, reply, meta), true
}
