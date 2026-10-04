package huginn

import (
	"regexp"
	"strings"
)

// deflectRe matches answers that send the person off to find the answer
// themselves, or pretend to browse: "search for it on Google", "check
// websites like", "I don't have real-time access", "[Searching…]". The
// quality set's deflection pattern (tests/quality/cases.json) and the
// iPhone app's check match the same answers.
var deflectRe = regexp.MustCompile(`(?i)(search (for|online|the web|on google)|search engines?|(on|use|using) (google|bing|safari)\b|(check|visit|try|browse) (out )?(websites?|online|sites|retailers|the web|stores)|websites? (like|such as)|\bI (don't|do not|can't|cannot|am unable to|am not able to) (have )?(real-time|access|browse|search|look|check|provide (links|real-time|current))|real-time (access|data|information)|\[search|as an AI\b|I'm (just )?an? (text-based|language model|AI)|\b(the user|you) (needs?|should|can|could|may want|might want|will need|would need|have)( to)? (check|search|look|visit|contact|consult|try|google))`)

// Deflects reports an answer that tells the person to find it themselves
// instead of answering (§21).
func Deflects(answer string) bool {
	return deflectRe.MatchString(answer)
}

// smallTalkRe matches one phrase of small talk: a greeting, thanks, or "how
// are you". "As an AI, I don't have feelings" answers these; it is not a
// deflection to look up.
var smallTalkRe = regexp.MustCompile(`(?i)^(hi|hello|hey|hiya|howdy|yo|good (morning|afternoon|evening|night)|thanks?( you)?( (so|very) much)?|thank you|ty|ok(ay)?|cool|nice|great|awesome|bye|goodbye|see you|how are you( doing)?( today)?|how('s| is) it going|how have you been|what'?s up|sup|nice to meet you)( there| toskar)?$`)

// SmallTalk reports a message that is only small talk, such as "Hi! How
// are you?". Its answer is never looked up on the web.
func SmallTalk(message string) bool {
	parts := strings.FieldsFunc(message, func(r rune) bool { return strings.ContainsRune(".!?,;:\n", r) })
	seen := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !smallTalkRe.MatchString(part) {
			return false
		}
		seen = true
	}
	return seen
}
