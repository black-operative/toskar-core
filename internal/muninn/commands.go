package muninn

import (
	"regexp"
	"strings"
)

// CommandKind is a memory request a person makes in chat.
type CommandKind string

const (
	CommandNone     CommandKind = ""
	CommandRemember CommandKind = "remember"
	CommandForget   CommandKind = "forget"
	CommandList     CommandKind = "list"
)

// Command is a parsed memory request.
type Command struct {
	Kind CommandKind
	// Text is what to remember or forget.
	Text string
}

var (
	rememberRe = regexp.MustCompile(`(?is)^\s*(?:please\s+|hey,?\s+|ok(?:ay)?,?\s+)?(?:remember|don'?t forget|do not forget|keep in mind|note for the future)(?:\s+that|\s*:|\s*,)?\s+(.+?)\s*$`)
	forgetRe   = regexp.MustCompile(`(?is)^\s*(?:please\s+)?(?:forget|stop remembering|delete the memory)(?:\s+that|\s+about|\s*:)?\s+(.+?)\s*$`)
	// "My daughter's birthday is May 3, remember that." People often ask
	// after saying it; left to the model, a small one says "I'll remember"
	// and nothing is saved. A comma, period, or dash must come before the
	// request, so "Can you help me remember" is not one.
	trailingRememberRe = regexp.MustCompile(`(?is)^\s*(.+?)\s*[,.;:!—–-]+\s*(?:please\s+)?(?:remember(?:\s+(?:that|this|it))?|keep\s+(?:that|this|it)\s+in\s+mind|don'?t\s+forget(?:\s+(?:that|this|it))?|do\s+not\s+forget(?:\s+(?:that|this|it))?)(?:,?\s+please)?[\s.!]*$`)
	listRe             = regexp.MustCompile(`(?i)^\s*(?:what do you (?:remember|know) about me|what (?:do|did) you remember|what have you remembered|show (?:me )?(?:my|your) memor(?:y|ies)|list (?:my|your) memor(?:y|ies))\b`)
)

// ParseCommand recognizes "Remember that …" (or "…, remember that"),
// "Forget that …", and "What do you remember about me?". Yggdrasil handles these
// itself, so the result does not depend on how well a model follows them.
func ParseCommand(message string) Command {
	msg := strings.TrimSpace(message)
	if listRe.MatchString(msg) {
		return Command{Kind: CommandList}
	}
	if m := forgetRe.FindStringSubmatch(msg); m != nil {
		return Command{Kind: CommandForget, Text: tidy(m[1])}
	}
	if m := rememberRe.FindStringSubmatch(msg); m != nil {
		text := tidy(m[1])
		// "Remember this" with nothing after it is not a memory.
		if t := strings.ToLower(text); t == "this" || t == "that" || t == "it" {
			return Command{}
		}
		return Command{Kind: CommandRemember, Text: text}
	}
	if m := trailingRememberRe.FindStringSubmatch(msg); m != nil {
		// "Ok, remember that" points back at the chat; it is not a memory.
		if text := tidy(m[1]); len(strings.Fields(text)) >= 2 {
			return Command{Kind: CommandRemember, Text: text}
		}
	}
	return Command{}
}

// tidy trims trailing punctuation that is part of the request, and
// capitalizes the memory as a sentence.
func tidy(s string) string {
	s = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(s), ".!?"))
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}
