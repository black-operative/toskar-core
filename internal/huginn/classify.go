// Package huginn decides how a chat request should run: what kind of request
// it is, which installed model answers it when the user picks Auto, and which
// model to fall back to when that one fails.
package huginn

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/tools"
)

// Kind is the class of a request (AI experience spec §12).
type Kind string

const (
	// Chat is a simple question or conversation.
	Chat Kind = "chat"
	// Current needs information that changes: news, prices, weather, versions.
	Current Kind = "current"
	// Coding is about writing, reading, or fixing code.
	Coding Kind = "coding"
	// Research needs a long or careful answer: comparisons, plans, analysis.
	Research Kind = "research"
	// Local acts on this computer: files, commands, Git.
	Local Kind = "local"
)

// Describe is a plain-language name for a kind in lang, used in "Auto
// chose … for …".
func (k Kind) Describe(lang string) string {
	switch k {
	case Current, Coding, Research, Local:
		return locale.T(lang, "chat:steps.kinds."+string(k), nil)
	default:
		return locale.T(lang, "chat:steps.kinds.chat", nil)
	}
}

var (
	localRe    = regexp.MustCompile(`(?i)\b(my (files?|folders?|desktop|downloads|documents|repo|repository|project)|this (folder|directory|repo|repository|project)|run (the |a )?(command|script|tests?)|in (the )?terminal|git (status|log|diff|commit|push|pull|branch)|commit (my|the|these) changes|read the file|open the file|save (it|this) (as|to) )`)
	codingRe   = regexp.MustCompile("(?i)(```|\\b((my|this|the|source|sample|example) code|code (snippet|review|example)|stack ?trace|exception|compile[rsd]?|refactor|regex|regular expression|sql|unit tests?|python|golang|javascript|typescript|java|kotlin|php|bash script|shell script|html|css|json|yaml|dockerfile|kubernetes|syntax error|null pointer|segfault|bug in (my|the|this)|react (component|hook|app)|rust (code|program|crate)|swiftui|api endpoint|write a (function|script|program|class))\\b|\\b[a-z_]+\\.[a-z_]+\\(|=>|:=|\\bfunc |\\bdef |#include|\\bc(\\+\\+|#)\\s)")
	researchRe = regexp.MustCompile(`(?i)\b(compare|comparison|versus|vs\.?|pros and cons|trade-?offs?|analy[sz]e|analysis|in detail|detailed|step[- ]by[- ]step|plan for|make a plan|write an? (essay|report|proposal|memo|article)|research|evaluate|recommend(ation)?s? for|strategy|outline)\b`)
)

// researchLength is the message length, in runes, past which a request is
// treated as needing a careful answer.
const researchLength = 600

// Classify sorts a request. The order matters: acting on this computer and
// current information decide what the turn needs to do, so they win over the
// subject of the question.
func Classify(message string) Kind {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return Chat
	}
	if localRe.MatchString(msg) {
		return Local
	}
	if tools.MessageNeedsLiveWeb(msg) {
		return Current
	}
	if codingRe.MatchString(msg) {
		return Coding
	}
	if researchRe.MatchString(msg) || utf8.RuneCountInString(msg) > researchLength {
		return Research
	}
	return Chat
}
