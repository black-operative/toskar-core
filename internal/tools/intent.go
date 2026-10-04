package tools

import "regexp"

// liveWebRe matches questions that need current public information. Cues are
// whole words, so "score" does not match "underscore".
var liveWebRe = regexp.MustCompile(`(?i)(\b(weather|forecast|latest version|latest news|who won|last night|right now|today|look up|documentation|summarize (this|the) url|current price|stock price|search the web|search online|news|headlines|this week|this weekend|yesterday|tonight|scores?|exchange rate|price of|cost of|open now|near me|release date|just announced|the latest|what's new|election results)\b|https?://)`)

// MessageNeedsLiveWeb reports questions that need current public information.
func MessageNeedsLiveWeb(message string) bool {
	return liveWebRe.MatchString(message)
}

// askFindRe matches a request to find something rather than recall it: a
// recommendation, where to buy, what it costs, reviews, a result or a
// release, or "check" and "find" for me. Small models answer these from
// memory or tell the person to search, so Toskar looks them up first, as
// the iPhone app does (yeixio/toskar-desktop mobile/src/intent.ts).
var askFindRe = regexp.MustCompile(`(?i)\b(recommend\w*|suggest (a|an|some)\b.{0,30}\b(to buy|for sale|product|brand|model|bike|bicycle|machine|phone|laptop|car|tool|app)s?|best \w+(?: \w+){0,5} (for|under|to buy)|which \w+(?: \w+){0,5} should i (buy|get|choose)|where (can|could|do|should) i (buy|find|get|order|rent)|for sale|how much (is|are|does|do|did)|reviews?|check (for me|it|that|this|them|again)|can you (check|find|look)|find (me|out)|winner|won the|new release|released?|links? to)\b`)

// MessageAsksToFind reports requests to find something on the web.
func MessageAsksToFind(message string) bool {
	return askFindRe.MatchString(message)
}
