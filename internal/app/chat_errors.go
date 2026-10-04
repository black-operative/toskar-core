package app

import (
	"regexp"
	"strings"

	modelhealth "github.com/yeixio/toskar-core/internal/models/health"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A chat error reaches the client as text: most come from a runtime, a model,
// or another computer, not from core's own coded errors. They are recognized
// here, once, so clients show text for a stable code (multilingual spec §10)
// in the App language instead of matching English themselves.

type chatErrorRule struct {
	code string
	test *regexp.Regexp
	// params reads the message's values from the match, if it has any.
	params func(match []string) map[string]any
}

var chatErrorRules = []chatErrorRule{
	// The external server (#111): refusals for profiles and chats that keep
	// work here, then the server's own failures, ahead of the generic rules.
	{code: "EXTERNAL_OFFLINE_PROFILE", test: regexp.MustCompile(`(?i)doesn't use the external server`)},
	{code: "EXTERNAL_LOCAL_ONLY", test: regexp.MustCompile(`(?i)can't go to the external server`)},
	{code: "EXTERNAL_FAILED", test: regexp.MustCompile(`(?is)^.*the external server.*$`),
		params: func(m []string) map[string]any { return map[string]any{"detail": m[0]} }},
	{code: "RUNTIME_NOT_INSTALLED", test: regexp.MustCompile(`(?i)llama-server (is )?not installed`),
		params: func([]string) map[string]any { return map[string]any{"runtime": "llama-server"} }},
	{code: "MODEL_NOT_INSTALLED", test: regexp.MustCompile(`(?i)model "([^"]+)" not installed`),
		params: func(m []string) map[string]any { return map[string]any{"model_id": m[1]} }},
	{code: "NO_MODEL_ASSIGNED", test: regexp.MustCompile(`(?i)no model assigned|needs model assignments`)},
	{code: "NO_MODEL_INSTALLED", test: regexp.MustCompile(`(?i)install a model|no installed model|model .* not installed`)},
	{code: "CONTEXT_TOO_LONG", test: regexp.MustCompile(`(?i)context (length|size|window)|too many tokens|exceeds the (available )?context|n_ctx`)},
	{code: "OUT_OF_MEMORY", test: regexp.MustCompile(`(?i)out of memory|\boom\b|failed to allocate|insufficient memory|not enough memory`)},
	{code: "COMPUTER_OFFLINE", test: regexp.MustCompile(`(?i)is offline|offline\.|unreachable|no route to host`)},
	{code: "CONNECTION_LOST", test: regexp.MustCompile(`(?i)econnreset|econnrefused|connection (reset|refused)|broken pipe|unexpected eof|\beof\b|deadline exceeded|timed? ?out|i/o timeout`)},
	{code: "RUNTIME_ERROR", test: regexp.MustCompile(`(?i)llama-server error 5\d\d|http 5\d\d|internal server error`)},
}

// chatErrorCode is the stable code for a chat error's text and the values
// its message needs, or "" when the text is not one Yggdrasil recognizes.
// A model that crashed sends its failure as JSON, which clients read.
func chatErrorCode(text string) (string, map[string]any) {
	t := strings.TrimSpace(text)
	if _, ok := modelhealth.Parse(t); ok {
		return "MODEL_UNHEALTHY", nil
	}
	for _, rule := range chatErrorRules {
		if m := rule.test.FindStringSubmatch(t); m != nil {
			if rule.params != nil {
				return rule.code, rule.params(m)
			}
			return rule.code, nil
		}
	}
	return "", nil
}

// codedChatError is a chat error's text with its stable code, when it has one.
func codedChatError(text string) error {
	code, params := chatErrorCode(text)
	if code == "" {
		return errString(text)
	}
	return contracts.NewError(code, params, errString(text))
}

// codedError is err with a stable code: its own, or one recognized from its
// text. nil stays nil.
func codedError(err error) error {
	if err == nil {
		return nil
	}
	if code, _ := contracts.ErrorCode(err); code != "" {
		return err
	}
	return codedChatError(err.Error())
}
